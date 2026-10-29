package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"

	"showmethestory/internal/config"
	"showmethestory/internal/i18n"
	"showmethestory/internal/llm"
	"showmethestory/internal/story"
)

// brainstormOptionSep is the line the model must place between options. The
// backend splits on it so the UI can render one card per option.
var brainstormOptionSep = regexp.MustCompile(`(?m)^\s*={3,}\s*$`)

// PostBrainstorm is the Brainstorm panel's single-shot generator. It talks to
// the optional second model (APIConfig.Brainstorm) so brainstorming never
// competes with the main agent's model, and returns the Markdown options plus
// the tokens used by this call.
func (h *Handlers) PostBrainstorm(w http.ResponseWriter, r *http.Request) {
	if !h.ensureProject(w, r) {
		return
	}
	var body struct {
		Action string `json:"action"`
		Mode   string `json:"mode"`
		Text   string `json:"text"`
		More   bool   `json:"more"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	text := strings.TrimSpace(body.Text)
	if text == "" {
		h.writeErrorReq(w, r, http.StatusBadRequest, "empty_text", "")
		return
	}
	cfg := h.apiCfg.Brainstorm
	if cfg == nil || strings.TrimSpace(cfg.BaseURL) == "" || strings.TrimSpace(cfg.Model) == "" {
		h.writeErrorReq(w, r, http.StatusBadRequest, "brainstorm_not_configured", "")
		return
	}
	bcfg := *cfg
	if bcfg.HTTPTimeoutSeconds <= 0 {
		bcfg.HTTPTimeoutSeconds = config.DefaultHTTPTimeoutSeconds
	}
	llm.EnsureContextBudget(&bcfg)

	system, user := brainstormPrompt(h.cfg.Language, body.Action, body.Mode, body.More, text)
	if params := story.NovelParametersBlock(h.cfg); params != "" {
		user = params + "\n\n" + user
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(bcfg.HTTPTimeoutSeconds)*time.Second)
	defer cancel()
	// No logger: this call is synchronous and must not broadcast task tokens.
	ctx, usage := llm.WithTaskTokens(ctx, nil)
	out := strings.TrimSpace(llm.CallAPIWithRetryLog(ctx, &bcfg, system, user, h.logger))
	if out == "" {
		h.writeErrorReq(w, r, http.StatusBadGateway, "empty_generation", "")
		return
	}
	promptTokens, completionTokens := usage.Snapshot()
	h.writeJSON(w, http.StatusOK, map[string]any{
		"result":            out,
		"options":           splitBrainstormOptions(out),
		"prompt_tokens":     promptTokens,
		"completion_tokens": completionTokens,
	})
}

// brainstormPrompt turns the panel's action/mode pair into one focused prompt.
// Unknown actions fall back to rewrite so an old client cannot break the route.
func brainstormPrompt(lang, action, mode string, more bool, text string) (system, user string) {
	en := i18n.NormalizeLanguage(lang) == i18n.LangEN
	rewriteModes := map[string][2]string{
		"shorter":        {"Hazlo claramente más corto sin perder la idea central.", "Make it noticeably shorter without losing the core meaning."},
		"longer":         {"Hazlo más largo añadiendo detalle concreto, no relleno.", "Make it longer by adding concrete detail, not filler."},
		"rephrase":       {"Reformúlalo con otras palabras y otra estructura de frase.", "Rephrase it with different wording and sentence structure."},
		"descriptive":    {"Hazlo más descriptivo, con detalle preciso y sensorial.", "Make it more descriptive with precise, sensory detail."},
		"show":           {"Muestra en vez de contar: sustituye lo abstracto por acción, imagen y subtexto.", "Show, don't tell: replace abstract statements with action, image and subtext."},
		"inner_conflict": {"Profundiza el conflicto interno del personaje; haz concretos los impulsos que chocan.", "Deepen the character's inner conflict; make the competing impulses concrete."},
		"intense":        {"Sube la intensidad emocional y las apuestas sin caer en el melodrama.", "Raise the emotional intensity and stakes without melodrama."},
	}
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
	dimensions := map[string][2]string{
		"originality": {"originalidad", "originality"},
		"clarity":     {"claridad", "clarity"},
		"pacing":      {"ritmo", "pacing"},
		"grammar":     {"gramática", "grammar"},
		"feasibility": {"verosimilitud", "feasibility"},
	}

	var instruction string
	switch action {
	case "alternatives":
		instruction = pick(en, "Ofrece 3 alternativas claramente distintas para el texto siguiente. Mantén la misma forma gramatical, longitud y registro.",
			"Offer 3 clearly distinct alternatives for the text below. Keep the same grammatical form, length and register.")
	case "describe":
		s := senses[mode]
		label := s[1]
		if !en {
			label = s[0]
		}
		if label == "" {
			label = pick(en, "vista", "sight")
		}
		instruction = pick(en, "Escribe 3 opciones de 3 a 5 frases que integren el sentido «"+label+"». No nombres el sentido; cada opción debe evocar una imagen o momento distinto.",
			"Write 3 options of 3-5 sentences that weave in the sense of \""+label+"\". Do not name the sense; each option must evoke a different image or moment.")
	case "expand":
		instruction = pick(en, "Amplía el texto siguiente en 3 versiones, cada una hasta aproximadamente el doble de longitud, con detalle concreto y manteniendo la misma voz.",
			"Expand the text below into 3 versions, each roughly twice its length, with concrete detail and the same voice.")
	case "feedback":
		d := dimensions[mode]
		label := d[1]
		if !en {
			label = d[0]
		}
		if label == "" {
			label = pick(en, "claridad", "clarity")
		}
		instruction = pick(en, "Da 3 notas de retroalimentación independientes sobre «"+label+"». Cada una empieza con un veredicto de una línea y sigue con 3 viñetas con una corrección concreta. Sé específico con este texto.",
			"Give 3 independent feedback notes on \""+label+"\". Each starts with a one-line verdict, then 3 bullets with one concrete fix. Be specific to this text.")
	default: // rewrite
		m := rewriteModes[mode]
		clause := m[1]
		if !en {
			clause = m[0]
		}
		if clause == "" {
			clause = pick(en, "Reescribe el texto siguiente manteniendo el significado y la voz.", "Rewrite the text below while keeping the meaning and voice.")
		}
		instruction = clause + pick(en, " Da 3 versiones distintas, solo el texto reescrito, sin comentarios.", " Give 3 distinct versions, only the rewritten text, no commentary.")
	}

	instruction += pick(en,
		" Devuelve exactamente 3 opciones separadas por una línea que contenga solo ===. No numeres ni etiquetes las opciones. Conserva la persona narrativa (primera, segunda o tercera) y el tiempo verbal del texto original: no los cambies.",
		" Return exactly 3 options separated by a line containing only ===. Do not number or label them. Preserve the original narrative person (first, second or third) and tense: never change them.")
	if more {
		instruction += pick(en,
			" Son opciones de seguimiento: busca ángulos claramente distintos de los más obvios.",
			" These are follow-up options: take angles clearly different from the most obvious ones.")
	}

	system = pick(en,
		"Eres un compañero de brainstorming para novelistas: directo, concreto y útil. Nunca cambies la persona narrativa (primera, segunda o tercera) ni el tiempo verbal del texto del autor. Responde en el idioma de los parámetros de la novela (el idioma del manuscrito). No añadas preámbulos ni meta-comentarios.",
		"You are a brainstorming partner for novelists: direct, concrete and useful. Never change the narrative person (first, second or third) or tense of the author's text. Answer in the language of the novel parameters below (the manuscript language). No preamble or meta commentary.")
	user = instruction + "\n\n---\n" + text
	return system, user
}

// splitBrainstormOptions turns the model's delimited answer into cards. A
// response without the delimiter is kept as one option rather than dropped.
func splitBrainstormOptions(out string) []string {
	parts := brainstormOptionSep.Split(out, -1)
	options := make([]string, 0, len(parts))
	for _, part := range parts {
		if s := strings.TrimSpace(part); s != "" {
			options = append(options, s)
		}
	}
	return options
}

func pick(en bool, es, eng string) string {
	if en {
		return eng
	}
	return es
}
