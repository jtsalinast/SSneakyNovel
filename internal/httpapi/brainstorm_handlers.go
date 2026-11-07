package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"showmethestory/internal/config"
	"showmethestory/internal/i18n"
	"showmethestory/internal/llm"
	"showmethestory/internal/story"
)

// brainstormRequest is one synchronous Brainstorm call. The panel fires three
// calls (variant 1..3) per option set: separate calls are the only reliable way
// to get three truly distinct cards with independent Copy buttons, because a
// single answer sometimes collapses into one option or hides them behind ===.
type brainstormRequest struct {
	Action      string              `json:"action"`
	Mode        string              `json:"mode"`
	Text        string              `json:"text"`
	Instruction string              `json:"instruction"`
	Variant     int                 `json:"variant"`
	More        bool                `json:"more"`
	RequestID   string              `json:"request_id"`
	EntityKind  string              `json:"entity_kind"`
	EntityID    string              `json:"entity_id"`
	History     []brainstormMessage `json:"history"`
	Context     string              `json:"context"`
}

type brainstormMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type brainstormExplain struct {
	Explanation   string   `json:"explanation"`
	Definition    string   `json:"definition"`
	Synonyms      []string `json:"synonyms"`
	Antonyms      []string `json:"antonyms"`
	SemanticField []string `json:"semantic_field"`
	Tone          struct {
		Label string `json:"label"`
		Note  string `json:"note"`
	} `json:"tone"`
	Readings []struct {
		Label string `json:"label"`
		Text  string `json:"text"`
	} `json:"readings"`
}

// GetBrainstormEntities lists the Lore records the Commune & Consult picker can
// offer. It only reads the loaded project; no file is written.
func (h *Handlers) GetBrainstormEntities(w http.ResponseWriter, r *http.Request) {
	if !h.ensureProject(w, r) {
		return
	}
	h.writeJSON(w, http.StatusOK, story.BrainstormEntities(h.settings))
}

// PostBrainstorm is the Brainstorm panel's generator. It talks to the optional
// second model (APIConfig.Brainstorm) so brainstorming never competes with the
// main agent's model, and streams request-scoped token usage over SSE.
func (h *Handlers) PostBrainstorm(w http.ResponseWriter, r *http.Request) {
	if !h.ensureProject(w, r) {
		return
	}
	var body brainstormRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	body.Action = strings.ToLower(strings.TrimSpace(body.Action))
	body.Text = strings.TrimSpace(body.Text)
	if body.Action == "" {
		body.Action = "alternatives"
	}
	if body.Text == "" {
		h.writeErrorReq(w, r, http.StatusBadRequest, "empty_text", "")
		return
	}
	cfg := h.apiCfg.Brainstorm
	if cfg == nil || strings.TrimSpace(cfg.BaseURL) == "" || strings.TrimSpace(cfg.Model) == "" {
		h.writeErrorReq(w, r, http.StatusBadRequest, "brainstorm_not_configured")
		return
	}
	bcfg := *cfg
	if bcfg.HTTPTimeoutSeconds <= 0 {
		bcfg.HTTPTimeoutSeconds = config.DefaultHTTPTimeoutSeconds
	}
	llm.EnsureContextBudget(&bcfg)

	system, user, errKey := h.brainstormPrompts(body)
	if errKey != "" {
		h.writeErrorReq(w, r, http.StatusBadRequest, errKey, "")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(bcfg.HTTPTimeoutSeconds)*time.Second)
	defer cancel()
	requestID := strings.TrimSpace(body.RequestID)
	if requestID == "" {
		requestID = fmt.Sprintf("bs-%d", time.Now().UnixNano())
	}
	variant := body.Variant
	if variant < 1 {
		variant = 1
	}
	if variant > 99 {
		variant = 99
	}
	ctx, usage := llm.WithBrainstormTokens(ctx, h.logger, requestID, variant)
	out := strings.TrimSpace(llm.CallAPIWithRetryLog(ctx, &bcfg, system, user, h.logger))
	if out == "" {
		h.writeErrorReq(w, r, http.StatusBadGateway, "empty_generation", "")
		return
	}
	promptTokens, completionTokens := usage.Snapshot()
	resp := map[string]any{
		"result":            out,
		"option":            out,
		"options":           []string{out},
		"request_id":        requestID,
		"variant":           variant,
		"prompt_tokens":     promptTokens,
		"completion_tokens": completionTokens,
	}
	if body.Action == "explain" {
		explain := brainstormExplain{}
		if j := llm.ExtractJSON(out); j != "" {
			_ = json.Unmarshal([]byte(j), &explain)
		}
		if explain.Explanation == "" {
			explain.Explanation = out
		}
		resp["explain"] = explain
	}
	h.writeJSON(w, http.StatusOK, resp)
}

// PostBrainstormStream is PostBrainstorm with token streaming over SSE. The
// panel opens one stream per card so every option fills in while the model
// writes it. A call that fails before emitting any text is retried once; once
// text reached the client the partial answer is kept and never restarted.
func (h *Handlers) PostBrainstormStream(w http.ResponseWriter, r *http.Request) {
	if !h.ensureProject(w, r) {
		return
	}
	var body brainstormRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	body.Action = strings.ToLower(strings.TrimSpace(body.Action))
	body.Text = strings.TrimSpace(body.Text)
	if body.Action == "" {
		body.Action = "alternatives"
	}
	if body.Text == "" {
		h.writeErrorReq(w, r, http.StatusBadRequest, "empty_text", "")
		return
	}
	cfg := h.apiCfg.Brainstorm
	if cfg == nil || strings.TrimSpace(cfg.BaseURL) == "" || strings.TrimSpace(cfg.Model) == "" {
		h.writeErrorReq(w, r, http.StatusBadRequest, "brainstorm_not_configured")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		h.writeErrorReq(w, r, http.StatusInternalServerError, "generate_failed", "streaming not supported")
		return
	}
	bcfg := *cfg
	if bcfg.HTTPTimeoutSeconds <= 0 {
		bcfg.HTTPTimeoutSeconds = config.DefaultHTTPTimeoutSeconds
	}
	llm.EnsureContextBudget(&bcfg)

	system, user, errKey := h.brainstormPrompts(body)
	if errKey != "" {
		h.writeErrorReq(w, r, http.StatusBadRequest, errKey, "")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	requestID := strings.TrimSpace(body.RequestID)
	if requestID == "" {
		requestID = fmt.Sprintf("bs-%d", time.Now().UnixNano())
	}
	variant := body.Variant
	if variant < 1 {
		variant = 1
	}
	if variant > 99 {
		variant = 99
	}

	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(bcfg.HTTPTimeoutSeconds)*time.Second)
	defer cancel()
	ctx, usage := llm.WithBrainstormTokens(ctx, h.logger, requestID, variant)

	writeEvent := func(v map[string]any) {
		b, _ := json.Marshal(v)
		fmt.Fprintf(w, "data: %s\n\n", b)
		flusher.Flush()
	}

	var full strings.Builder
	emitted := false
	messages := []llm.Message{
		{Role: "system", Content: system},
		{Role: "user", Content: user},
	}
	var result llm.CompletionResult
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		result, err = llm.CallAPIStreamMessages(ctx, &bcfg, messages, func(chunk string) {
			emitted = true
			full.WriteString(chunk)
			ev := map[string]any{"delta": chunk}
			if body.Action == "explain" {
				if p := explanationPreview(full.String()); p != "" {
					ev["preview"] = p
				}
			}
			writeEvent(ev)
		})
		if err == nil || emitted || ctx.Err() != nil {
			break
		}
	}
	if err != nil && !emitted {
		writeEvent(map[string]any{"error": err.Error()})
		return
	}
	out := strings.TrimSpace(result.Content)
	if out == "" {
		writeEvent(map[string]any{"error": i18n.T(i18n.FromRequest(r), "empty_generation")})
		return
	}
	promptTokens, completionTokens := usage.Snapshot()
	done := map[string]any{
		"done":              true,
		"result":            out,
		"option":            out,
		"request_id":        requestID,
		"variant":           variant,
		"prompt_tokens":     promptTokens,
		"completion_tokens": completionTokens,
	}
	if body.Action == "explain" {
		explain := brainstormExplain{}
		if j := llm.ExtractJSON(out); j != "" {
			_ = json.Unmarshal([]byte(j), &explain)
		}
		if explain.Explanation == "" {
			explain.Explanation = out
		}
		done["explain"] = explain
	}
	writeEvent(done)
}

// explanationPreview extracts the value of the "explanation" field from a
// partially streamed JSON object, so Explain can show prose before the whole
// JSON is valid.
func explanationPreview(s string) string {
	const key = `"explanation"`
	i := strings.Index(s, key)
	if i < 0 {
		return ""
	}
	i += len(key)
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r' || s[i] == ':') {
		i++
	}
	if i >= len(s) || s[i] != '"' {
		return ""
	}
	i++
	var b strings.Builder
	for i < len(s) {
		c := s[i]
		if c == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case '"':
				b.WriteByte('"')
			case '\\':
				b.WriteByte('\\')
			default:
				b.WriteByte(s[i+1])
			}
			i += 2
			continue
		}
		if c == '"' {
			break
		}
		b.WriteByte(c)
		i++
	}
	return b.String()
}

// brainstormPrompts builds the system and user prompts for one call. The
// returned error key is non-empty when the request is incomplete (for example
// Commune without a Lore entity).
func (h *Handlers) brainstormPrompts(req brainstormRequest) (system, user, errKey string) {
	// The prompt scaffolding and the answer language follow the Story language
	// (falling back to the project language). When it is explicitly pinned, the
	// optional brainstorms (Describe / Rewrite / Feedback / Explain / Commune)
	// must answer in it even if the selected text is in another language.
	contentLang := story.ContentLanguage(h.cfg)
	pinnedLang := config.EffectiveOutputLanguage(h.cfg) != ""
	en := i18n.NormalizeLanguage(contentLang) == i18n.LangEN
	system = brainstormSystem(en)
	if lock := brainstormLanguageLock(en, pinnedLang); lock != "" {
		system += lock
	}
	variant := req.Variant
	if variant < 1 {
		variant = 1
	}
	// Variants 1..3 pick the angle; larger slots (the Describe overview fires
	// one call per sense) cycle through the same three angles.
	variant = 1 + (variant-1)%3
	angle := brainstormVariantAngle(en, variant)

	// Retrieve lore against the author's phrase PLUS the surrounding chapter
	// context: a two-word selection ("la cocina") only matches the right places,
	// characters and facts when the scene is part of the query.
	query := req.Text
	if ctxText := strings.TrimSpace(req.Context); ctxText != "" {
		if runes := []rune(ctxText); len(runes) > 1200 {
			ctxText = string(runes[:1200])
		}
		query += "\n" + ctxText
	}
	lore := story.BrainstormLoreContext(h.settings, h.state, query, contentLang)
	if req.Action == "commune" {
		entity, entityCtx := story.BrainstormEntityContext(h.settings, h.state, req.EntityKind, req.EntityID, contentLang)
		if entityCtx == "" {
			return "", "", "brainstorm_entity_not_found"
		}
		sys, usr, k := communePrompts(en, entity, entityCtx, req)
		if lock := brainstormLanguageLock(en, pinnedLang); lock != "" {
			sys += lock
			usr += lock
		}
		return sys, usr, k
	}

	var instruction string
	switch req.Action {
	case "describe":
		label := brainstormSenseLabel(en, req.Mode)
		instruction = pick(en,
			"Escribe UNA opción de 3 a 5 frases que integre el sentido «"+label+"». No nombres el sentido; evoca una imagen o momento concreto. Escribe la escena en la misma persona narrativa y el mismo tiempo verbal que el TEXTO DEL AUTOR o, si es solo una frase suelta, que el CONTEXTO DEL CAPÍTULO. No resumas ni expliques: escribe la escena. "+angle,
			"Write ONE option of 3-5 sentences that weaves in the sense of \""+label+"\". Do not name the sense; evoke one concrete image or moment. Write the scene in the same narrative person and tense as the AUTHOR TEXT or, if it is only a short phrase, as the CHAPTER CONTEXT. Do not summarize or explain: write the scene. "+angle)
	case "feedback":
		label := brainstormDimensionLabel(en, req.Mode)
		instruction = pick(en,
			"Da UNA nota de retroalimentación sobre «"+label+"» en el idioma del TEXTO DEL AUTOR. Empieza con un veredicto de una línea y sigue con 3 viñetas, cada una con una corrección concreta y específica para este texto. "+angle,
			"Give ONE feedback note on \""+label+"\" in the language of the AUTHOR TEXT. Start with a one-line verdict, then 3 bullets, each with one concrete fix specific to this text. "+angle)
	case "explain":
		instruction = brainstormExplainInstruction(en, req.Text)
		// The author text decides the answer language, not the project or the
		// prompt language: models otherwise mirror the English schema.
		instruction += "\n\n" + pick(en,
			"IDIOMA DE LA RESPUESTA: el mismo que el TEXTO DEL AUTOR (detéctalo en el propio texto; si es una palabra o frase corta, usa el idioma del CONTEXTO DEL CAPÍTULO). Escribe en ese idioma TODOS los valores del JSON, incluidos definition, synonyms, antonyms, semantic_field, tone.label, tone.note y readings. No traduzcas la respuesta al inglés. Nunca devuelvas una explicación vacía: si el término es ambiguo, elige la lectura que encaje con el contexto.",
			"ANSWER LANGUAGE: the same as the AUTHOR TEXT (detect it from the text itself; if it is a single word or short phrase, use the language of the CHAPTER CONTEXT). Write every JSON value in that language, including definition, synonyms, antonyms, semantic_field, tone.label, tone.note and readings. Never translate the answer into English. Never return an empty explanation: if the term is ambiguous, pick the reading that fits the context.")
	case "rewrite":
		instruction = brainstormRewriteInstruction(en, req.Mode, req.Instruction) + " " + angle
	case "alternatives":
		instruction = pick(en,
			"Ofrece UNA alternativa para el texto siguiente. Mantén la misma persona narrativa, el tiempo verbal, la forma gramatical, la longitud y el registro. "+angle,
			"Offer ONE alternative for the text below. Keep the same narrative person, tense, grammatical form, length and register. "+angle)
	default:
		instruction = pick(en,
			"Ofrece UNA alternativa para el texto siguiente. "+angle,
			"Offer ONE alternative for the text below. "+angle)
	}
	if req.More {
		instruction += pick(en,
			" Es una opción de seguimiento: busca un ángulo claramente distinto de los más obvios.",
			" This is a follow-up option: take an angle clearly different from the most obvious ones.")
	}
	if req.Action != "explain" {
		instruction += pick(en,
			" Devuelve SOLO el texto pedido: sin etiquetas, sin numeración, sin ===, sin comentarios. Nunca devuelvas una respuesta vacía.",
			" Return ONLY the requested text: no label, no numbering, no ===, no commentary. Never return an empty answer.")
	}

	var b strings.Builder
	b.WriteString(instruction)
	if req.Context != "" {
		b.WriteString(pick(en, "\n\n[CONTEXTO DEL CAPÍTULO]\n", "\n\n[CHAPTER CONTEXT]\n"))
		b.WriteString(req.Context)
	}
	if params := story.NovelParametersBlock(h.cfg); params != "" {
		b.WriteString(pick(en, "\n\n[PARÁMETROS DE LA NOVELA]\n", "\n\n[NOVEL PARAMETERS]\n"))
		if runes := []rune(params); len(runes) > 4000 {
			params = string(runes[:4000])
		}
		b.WriteString(params)
	}
	if aesthetic := story.WorldbuildingAestheticBlock(h.settings, contentLang); aesthetic != "" {
		b.WriteString("\n\n")
		b.WriteString(aesthetic)
	}
	if lore != "" {
		b.WriteString(pick(en, "\n\n[LORE DE LA NOVELA]\n", "\n\n[NOVEL LORE]\n"))
		b.WriteString(lore)
	}
	b.WriteString(pick(en, "\n\n[TEXTO DEL AUTOR]\n", "\n\n[AUTHOR TEXT]\n"))
	b.WriteString(req.Text)
	if req.Action == "explain" {
		// Repeated after the text on purpose: the last line wins over the
		// English schema when a model mirrors the prompt language.
		b.WriteString(pick(en,
			"\n\nEscribe ahora el JSON en el mismo idioma que el TEXTO DEL AUTOR de arriba (texto en español → respuesta en español; texto en inglés → respuesta en inglés).",
			"\n\nNow write the JSON in the same language as the AUTHOR TEXT above (Spanish text → Spanish answer; English text → English answer)."))
	} else {
		// Final style lock: it repeats the language/voice rules at the very end,
		// where small models weight instructions most heavily.
		b.WriteString(pick(en,
			"\n\n[REGLAS FINALES] Responde en el idioma del TEXTO DEL AUTOR o, si es una palabra o frase suelta, en el del CONTEXTO DEL CAPÍTULO. Conserva la persona narrativa, el tiempo verbal, el registro y la voz del texto (o del contexto si el texto no tiene voz). No cambies de POV ni de idioma. No devuelvas una respuesta vacía.",
			"\n\n[FINAL RULES] Answer in the language of the AUTHOR TEXT or, if it is a single word or short phrase, in the language of the CHAPTER CONTEXT. Keep the narrative person, tense, register and voice of the text (or of the context when the text has no voice). Never switch POV or language. Never return an empty answer."))
	}
	// Pinned Story language wins over the author-text heuristic: last line, so
	// it overrides the FINAL RULES above when the author pinned the language.
	if lock := brainstormLanguageLock(en, pinnedLang); lock != "" {
		b.WriteString(lock)
	}
	return system, b.String(), ""
}

// brainstormLanguageLock returns a strong, localized rule that pins the answer
// to the book's Story language. Empty when the language is not pinned (the
// brainstorms then keep following the author's text, as designed).
func brainstormLanguageLock(en, pinned bool) string {
	if !pinned {
		return ""
	}
	if en {
		return "\n\n[MANDATORY LANGUAGE] This novel's Story language is English: write the ENTIRE answer in English, even if the author text, the instruction or the surrounding chapter context is in another language. Never answer in Spanish."
	}
	return "\n\n[IDIOMA OBLIGATORIO] El idioma de esta novela (Story language) es español: escribe TODA la respuesta en español, aunque el texto del autor, la instrucción o el contexto del capítulo estén en otro idioma. Nunca respondas en inglés."
}

func brainstormSystem(en bool) string {
	if en {
		return "You are a brainstorming partner for novelists: direct, concrete and useful. " +
			"LANGUAGE: always answer in the language of the AUTHOR TEXT; when the text is a single word or a short phrase, use the language of the CHAPTER CONTEXT. Never switch to the project or prompt language. " +
			"VOICE: preserve the narrative person (first, second or third), the tense, the register and the style of the author's text; when the text has no narrative voice, take the person, tense and register from the CHAPTER CONTEXT. Never switch POV. " +
			"CANON: the NOVEL LORE block is authoritative; never contradict it and never invent facts that conflict with it. " +
			"COMPLETENESS: always return a usable answer, never an empty response or a refusal. " +
			"No preamble, no meta commentary."
	}
	return "Eres un compañero de brainstorming para novelistas: directo, concreto y útil. " +
		"IDIOMA: responde siempre en el idioma del TEXTO DEL AUTOR; cuando el texto sea una palabra o una frase corta, usa el idioma del CONTEXTO DEL CAPÍTULO. Nunca cambies al idioma del proyecto ni al de las instrucciones. " +
		"VOZ: conserva la persona narrativa (primera, segunda o tercera), el tiempo verbal, el registro y el estilo del texto del autor; cuando el texto no tenga voz narrativa, toma la persona, el tiempo y el registro del CONTEXTO DEL CAPÍTULO. Nunca cambies el POV. " +
		"CANON: el bloque LORE DE LA NOVELA es autoritativo; no lo contradigas ni inventes hechos que choquen con él. " +
		"COMPLETITUD: devuelve siempre una respuesta utilizable, nunca una respuesta vacía ni una negativa. " +
		"Sin preámbulos ni meta-comentarios."
}

func brainstormVariantAngle(en bool, variant int) string {
	if en {
		switch variant {
		case 2:
			return "Take a clearly different angle, image or structure from the most obvious option."
		case 3:
			return "Offer a bolder variation in rhythm or structure while keeping the meaning."
		default:
			return "Stay close to the original intent, but polish it."
		}
	}
	switch variant {
	case 2:
		return "Toma un ángulo, imagen o estructura claramente distinta de la opción más obvia."
	case 3:
		return "Ofrece una variación más audaz en ritmo o estructura sin cambiar el significado."
	default:
		return "Mantente cerca de la intención original, pero púlsela."
	}
}

func brainstormRewriteInstruction(en bool, mode, custom string) string {
	if s := strings.TrimSpace(custom); s != "" {
		if en {
			return "Rewrite the text below following this author instruction: " + s
		}
		return "Reescribe el texto siguiente siguiendo esta instrucción del autor: " + s
	}
	modes := map[string][2]string{
		"shorter":        {"Hazlo claramente más corto sin perder la idea central.", "Make it noticeably shorter without losing the core meaning."},
		"longer":         {"Hazlo más largo añadiendo detalle concreto, no relleno.", "Make it longer by adding concrete detail, not filler."},
		"expand":         {"Amplíalo a aproximadamente el doble de longitud, con detalle concreto y la misma voz.", "Expand it to roughly twice its length, with concrete detail and the same voice."},
		"rephrase":       {"Reformúlalo con otras palabras y otra estructura de frase.", "Rephrase it with different wording and sentence structure."},
		"descriptive":    {"Hazlo más descriptivo, con detalle preciso y sensorial.", "Make it more descriptive with precise, sensory detail."},
		"show":           {"Muestra en vez de contar: sustituye lo abstracto por acción, imagen y subtexto.", "Show, don't tell: replace abstract statements with action, image and subtext."},
		"inner_conflict": {"Profundiza el conflicto interno del personaje; haz concretos los impulsos que chocan.", "Deepen the character's inner conflict; make the competing impulses concrete."},
		"intense":        {"Sube la intensidad emocional y las apuestas sin caer en el melodrama.", "Raise the emotional intensity and stakes without melodrama."},
	}
	m := modes[mode]
	clause := m[1]
	if !en {
		clause = m[0]
	}
	if clause == "" {
		clause = pick(en, "Reescribe el texto siguiente manteniendo el significado y la voz.", "Rewrite the text below while keeping the meaning and voice.")
	}
	return clause + pick(en, " Devuelve UNA sola versión reescrita. Mantén la persona narrativa, el tiempo verbal, el registro y la voz del texto.", " Return ONE rewritten version. Keep the narrative person, tense, register and voice of the text.")
}

func brainstormExplainInstruction(en bool, text string) string {
	single := len(strings.Fields(text)) == 1
	schema := `{"explanation":"brief explanation in the author text's language","definition":"dictionary-style definition (single word only)","synonyms":["synonyms (single word only)"],"antonyms":["antonyms (single word only)"],"semantic_field":["related concepts"],"tone":{"label":"sarcastic|cynical|sincere|injurious|neutral|...","note":"why"},"readings":[{"label":"alternative reading name","text":"the reading"}]}`
	if en {
		out := "Explain the following word, phrase or concept briefly and precisely. " +
			"Return ONLY one JSON object with this exact shape: " + schema + ". " +
			"Use empty strings or empty arrays when a field does not apply. Do not wrap the JSON in code fences."
		if single {
			return out + " The author text is a SINGLE WORD: fill definition, synonyms and antonyms."
		}
		return out + " The author text is a phrase, not a single word: leave definition, synonyms and antonyms empty (\"\" and []) and focus on explanation, semantic_field, tone and readings."
	}
	out := "Explica de forma breve y precisa la siguiente palabra, frase o concepto. " +
		"Devuelve SOLO un objeto JSON con esta forma exacta: " + schema + ". " +
		"Usa cadenas o listas vacías cuando un campo no aplique. No envuelvas el JSON en bloques de código."
	if single {
		return out + " El texto del autor es UNA SOLA PALABRA: rellena definition, synonyms y antonyms."
	}
	return out + " El texto del autor es una frase, no una palabra suelta: deja definition, synonyms y antonyms vacíos (\"\" y []) y céntrate en explanation, semantic_field, tone y readings."
}

func brainstormSenseLabel(en bool, mode string) string {
	senses := map[string][2]string{
		"sight":          {"vista", "sight"},
		"smell":          {"olfato", "smell"},
		"taste":          {"gusto", "taste"},
		"sound":          {"oído", "sound"},
		"tactile":        {"tacto", "touch"},
		"sixth_sense":    {"sexto sentido", "sixth sense"},
		"temperature":    {"temperatura", "temperature"},
		"ambient":        {"ambiente", "ambient atmosphere"},
		"spatial":        {"sentido espacial", "spatial awareness"},
		"pain":           {"dolor", "pain"},
		"proprioception": {"propiocepción", "proprioception (body position and movement)"},
	}
	s := senses[mode]
	if s[1] == "" {
		return pick(en, "vista", "sight")
	}
	if en {
		return s[1]
	}
	return s[0]
}

func brainstormDimensionLabel(en bool, mode string) string {
	dimensions := map[string][2]string{
		"originality": {"originalidad", "originality"},
		"clarity":     {"claridad", "clarity"},
		"pacing":      {"ritmo", "pacing"},
		"grammar":     {"gramática", "grammar"},
		"feasibility": {"verosimilitud", "feasibility"},
	}
	d := dimensions[mode]
	if d[1] == "" {
		return pick(en, "claridad", "clarity")
	}
	if en {
		return d[1]
	}
	return d[0]
}

func communePrompts(en bool, entity story.BrainstormEntity, entityCtx string, req brainstormRequest) (system, user, errKey string) {
	kind := entity.Kind
	if en {
		system = "You are " + entity.Name + ", a " + kind + " from this novel. Stay fully in character: answer as " + entity.Name +
			" would, with their voice, knowledge and biases. Use the ENTITY CANON and FACTS RECORDED SO FAR as truth; never invent events that contradict them, and admit uncertainty when the canon is silent. " +
			"Never mention being an AI, a model or a prompt. Answer in the language of the user's message. Keep answers focused and conversational."
	} else {
		system = "Eres " + entity.Name + ", un " + kind + " de esta novela. Mantente totalmente en personaje: responde como lo haría " + entity.Name +
			", con su voz, conocimiento y sesgos. Usa el CANON DE LA ENTIDAD y los HECHOS REGISTRADOS HASTA AHORA como verdad; no inventes sucesos que los contradigan y admite incertidumbre cuando el canon calle. " +
			"Nunca menciones ser una IA, un modelo o un prompt. Responde en el idioma del mensaje del usuario. Sé breve y conversacional."
	}
	var b strings.Builder
	b.WriteString(pick(en, "[CANON DE LA ENTIDAD]\n", "[ENTITY CANON]\n"))
	b.WriteString(entityCtx)
	if req.Context != "" {
		b.WriteString(pick(en, "\n\n[CONTEXTO DEL CAPÍTULO]\n", "\n\n[CHAPTER CONTEXT]\n"))
		b.WriteString(req.Context)
	}
	if len(req.History) > 0 {
		b.WriteString(pick(en, "\n\n[CONVERSACIÓN HASTA AHORA]\n", "\n\n[CONVERSATION SO FAR]\n"))
		for _, m := range req.History {
			role := "User"
			if m.Role == "assistant" {
				role = entity.Name
			}
			b.WriteString(role + ": " + strings.TrimSpace(m.Content) + "\n")
		}
	}
	b.WriteString(pick(en, "\n[USUARIO]\n", "\n[USER]\n"))
	b.WriteString(req.Text)
	return system, b.String(), ""
}

func pick(en bool, es, eng string) string {
	if en {
		return eng
	}
	return es
}
