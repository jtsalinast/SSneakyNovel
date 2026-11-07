package story

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"showmethestory/internal/config"
	"showmethestory/internal/llm"
	"showmethestory/internal/sse"
)

// ---------------------------------------------------------------------------
// Multi-call editorial passes
// ---------------------------------------------------------------------------
//
// All four critics (full outline, scene plans, drafted chapter, drafted act)
// grade the same four-pillar rubric. Until now only the chapter pass split the
// work; the other three asked for summary + four pillar grades + suggestions
// in ONE mega answer. That had two failure modes the chapter pass was designed
// to avoid:
//
//   - the "suggestions" tail was the first thing truncated by max_tokens, so a
//     report could ship four scores and zero actionable points;
//   - with everything in one answer the model graded four pillars from a
//     single overall impression instead of re-reading the material per pillar.
//
// These helpers run the sequence instead: one short first-impression call
// (editorial letter + global score) plus ONE call per pillar, each required to
// cite evidence, then merged back into the exact report JSON the UI already
// renders. The existing sanitizers/finalizers stay untouched.

// editorialSequence runs the first-impression call plus one call per pillar and
// returns the merged report JSON. build("") must return the overall prompt;
// build(pillarKey) the prompt for that single pillar.
func editorialSequence(ctx context.Context, apiCfg *config.APIConfig, logger *sse.LogBroadcaster, system, lang string, build func(pillarKey string) string, material string) (string, error) {
	overall, err := editorialOverallCall(ctx, apiCfg, logger, system, build(""))
	if err != nil {
		return "", err
	}
	parts := make([]string, 0, len(analysisPillarKeys))
	for _, pk := range analysisPillarKeys {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if part, ok := editorialPillarCall(ctx, apiCfg, logger, system, lang, pk, build(pk), material); ok {
			parts = append(parts, part)
			continue
		}
		if logger != nil {
			logger.WarnKey("log.critique_pillar_failed", pk)
		}
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("all editorial pillar calls failed")
	}
	return mergeCritiqueParts(overall, parts)
}

// editorialOverallCall asks for the editorial letter and global score only, so
// that answer can never be truncated where suggestions would live.
func editorialOverallCall(ctx context.Context, apiCfg *config.APIConfig, logger *sse.LogBroadcaster, system, prompt string) (string, error) {
	for attempt := 0; attempt < 2; attempt++ {
		p := prompt
		if attempt > 0 {
			p += "\n\nYour previous response was invalid. Return one complete JSON object only, using the required schema."
		}
		raw := llm.CallAPIWithRetryLog(ctx, apiCfg, system, p, logger)
		if raw == "" {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			return "", fmt.Errorf("API call failed or was canceled")
		}
		j := llm.ExtractJSON(raw)
		if j == "" {
			continue
		}
		var probe struct {
			Summary      string `json:"summary"`
			OverallScore int    `json:"overall_score"`
		}
		if json.Unmarshal([]byte(j), &probe) == nil && (strings.TrimSpace(probe.Summary) != "" || probe.OverallScore > 0) {
			return j, nil
		}
	}
	return "", fmt.Errorf("model response is not valid editorial JSON")
}

// editorialPillarCall grades exactly one pillar. A pillar below the bestseller
// bar with zero suggestions is a failed answer and gets one strict retry
// instead of being persisted as an unactionable verdict. The same applies when
// the answer skips checklist items: every sub-criterion must carry a level, so
// the derived score is never a bare number without rubric evidence.
func editorialPillarCall(ctx context.Context, apiCfg *config.APIConfig, logger *sse.LogBroadcaster, system, lang, pillarKey, prompt, material string) (string, bool) {
	retryNote := ""
	for attempt := 0; attempt < 2; attempt++ {
		p := prompt
		if attempt > 0 {
			if retryNote != "" {
				p += retryNote
			} else {
				p += "\n\nYour previous response was invalid. Return one complete JSON object only, using the required schema."
			}
		}
		raw := llm.CallAPIWithRetryLog(ctx, apiCfg, system, p, logger)
		if raw == "" {
			return "", false
		}
		j := llm.ExtractJSON(raw)
		if j == "" {
			continue
		}
		var probe struct {
			PillarScores []json.RawMessage `json:"pillar_scores"`
			Suggestions  []json.RawMessage `json:"suggestions"`
		}
		if json.Unmarshal([]byte(j), &probe) != nil {
			continue
		}
		graded, score := false, 0
		var missing []string
		for _, rawPillar := range probe.PillarScores {
			// Same lenient coercion the merge step uses: local models routinely
			// answer scores/levels as floats or strings, and a strict probe
			// would reject an otherwise usable pillar answer.
			normalized, ok := normalizeMergedPillar(rawPillar)
			if !ok {
				continue
			}
			var pa PillarAssessment
			if json.Unmarshal(normalized, &pa) != nil || pa.Key != pillarKey {
				continue
			}
			graded = true
			levels := make([]int, 0, len(pa.CriteriaLevels))
			for _, c := range pa.CriteriaLevels {
				levels = append(levels, c.Level)
			}
			score = pa.Score
			if derived := deriveScoreFromCriteria(levels); derived > 0 {
				score = derived
			}
			missing = missingPillarCriteria(pillarKey, pa)
			break
		}
		if !graded {
			continue
		}
		var notes []string
		if len(missing) > 0 {
			notes = append(notes, editorialMissingCriteriaReminder(lang, pillarKey, missing))
		}
		if n := missingEvidenceCount(probe.Suggestions); n > 0 {
			notes = append(notes, editorialMissingEvidenceReminder(lang, pillarKey, n))
		}
		if n := unsupportedEvidenceCount(probe.Suggestions, material); n > 0 {
			notes = append(notes, editorialUnsupportedEvidenceReminder(lang, pillarKey, n))
		}
		if len(probe.Suggestions) == 0 && score > 0 && score < strongPillarScore {
			notes = append(notes, editorialNoSuggestionsReminder(lang, pillarKey))
		}
		if len(notes) > 0 && attempt == 0 {
			retryNote = strings.Join(notes, "")
			continue
		}
		// Last attempt: keep the answer; finalization distributes the model's
		// own pillar score over any skipped item so the report never shows an
		// ungraded (-1) sub-criterion. A quote that still cannot be traced to
		// the material is dropped rather than persisted as a fabricated citation.
		return neutralizeUngroundedEvidence(j, material), true
	}
	return "", false
}

// missingEvidenceCount counts suggestions whose "evidence" excerpt is empty:
// the evidence rule is part of the contract, so a point without it is a failed
// answer and triggers one strict retry.
func missingEvidenceCount(suggestions []json.RawMessage) int {
	n := 0
	for _, raw := range suggestions {
		var s struct {
			Evidence string `json:"evidence"`
		}
		if json.Unmarshal(raw, &s) != nil || strings.TrimSpace(s.Evidence) == "" {
			n++
		}
	}
	return n
}

// unsupportedEvidenceCount counts suggestions whose non-empty evidence shares
// no real basis with the material: the model was told to quote the text, so an
// excerpt that is neither verbatim nor a paraphrase of it is a fabricated
// citation. Unlike an empty evidence field this is not rejected outright — the
// point may still be valid, only its quote is wrong — so the retry asks for a
// real excerpt from the same passage.
func unsupportedEvidenceCount(suggestions []json.RawMessage, material string) int {
	if strings.TrimSpace(material) == "" {
		return 0
	}
	n := 0
	for _, raw := range suggestions {
		var s struct {
			Evidence string `json:"evidence"`
		}
		if json.Unmarshal(raw, &s) != nil {
			continue
		}
		if ev := strings.TrimSpace(s.Evidence); ev != "" && !evidenceGrounded(ev, material) {
			n++
		}
	}
	return n
}

// evidenceGrounded reports whether an excerpt has a real basis in the material
// the critic was given. A verbatim quote always passes. A paraphrase passes
// when most of its content words occur in the material (same passage, own
// words) — that keeps honest paraphrases while a fabricated scene, which
// shares almost nothing, fails and triggers the corrective retry.
func evidenceGrounded(evidence, material string) bool {
	q := evidenceContentWords(evidence)
	m := evidenceContentWords(material)
	if len(q) == 0 || len(m) == 0 {
		return false
	}
	if strings.Contains(" "+strings.Join(m, " ")+" ", " "+strings.Join(q, " ")+" ") {
		return true
	}
	seen := make(map[string]bool, len(m))
	for _, w := range m {
		seen[evidenceStem(w)] = true
	}
	hits := 0
	for _, w := range q {
		if seen[evidenceStem(w)] {
			hits++
		}
	}
	return float64(hits)/float64(len(q)) >= evidenceGroundingThreshold
}

// evidenceGroundingThreshold is the share of an excerpt's content words that
// must exist in the material for it to count as a grounded paraphrase. A real
// paraphrase reuses the passage's nouns and verbs (measured ≈0.6 on live act
// reports); invented scenes land well below 0.5.
const evidenceGroundingThreshold = 0.5

var evidenceAccentReplacer = strings.NewReplacer(
	"á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u", "ñ", "n",
	"à", "a", "è", "e", "ì", "i", "ò", "o", "ù", "u", "â", "a", "ê", "e",
	"î", "i", "ô", "o", "û", "u", "ç", "c",
)

var evidenceStopwords = func() map[string]bool {
	words := `a al algo algunas algunos ante antes como con contra cual cuando de del desde donde durante e el ella ellas ellos en entre era eres es esa esas ese eso esos esta estaba estaban este esto estos fue fueron ha habia han hasta hay la las le les lo los mas me mi mis mucho muy nada ni no nos o os otra otro para pero poco por porque que se sea ser si sin sobre su sus tambien tan tanto te tiene tienen todo todos tu tus un una uno unos y ya
the a an and or but if then than that this these those it its is was were be been being of to in on at for from by with without as not no do does did have has had he she they them his her their you your i we our us
` + "\n"
	set := map[string]bool{}
	for _, w := range strings.Fields(words) {
		set[w] = true
	}
	return set
}()

// evidenceContentWords lowercases, strips Spanish accents and drops stopwords
// so the comparison focuses on the words that carry the passage's meaning.
func evidenceContentWords(s string) []string {
	fields := strings.FieldsFunc(strings.ToLower(evidenceAccentReplacer.Replace(s)), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := make([]string, 0, len(fields))
	for _, w := range fields {
		if len([]rune(w)) > 2 && !evidenceStopwords[w] {
			out = append(out, w)
		}
	}
	return out
}

// evidenceStem collapses Spanish inflection (plural, gender, verb endings) to a
// rough five-letter stem so "palabras" matches "palabra" without a stemmer.
func evidenceStem(w string) string {
	r := []rune(w)
	if len(r) > 5 {
		r = r[:5]
	}
	return string(r)
}

// neutralizeUngroundedEvidence removes evidence that the retry could not trace
// to the material. The suggestion stays (the problem may be real) but no
// fabricated quotation is persisted.
func neutralizeUngroundedEvidence(j, material string) string {
	if strings.TrimSpace(material) == "" {
		return j
	}
	var m map[string]json.RawMessage
	if json.Unmarshal([]byte(j), &m) != nil {
		return j
	}
	raws := jsonArray(m["suggestions"])
	if len(raws) == 0 {
		return j
	}
	changed := false
	out := make([]json.RawMessage, 0, len(raws))
	for _, raw := range raws {
		var s map[string]json.RawMessage
		if json.Unmarshal(raw, &s) == nil {
			var ev string
			if json.Unmarshal(s["evidence"], &ev) == nil && strings.TrimSpace(ev) != "" && !evidenceGrounded(ev, material) {
				s["evidence"] = json.RawMessage(`""`)
				if b, err := json.Marshal(s); err == nil {
					raw = b
					changed = true
				}
			}
		}
		out = append(out, raw)
	}
	if !changed {
		return j
	}
	if b, err := json.Marshal(out); err == nil {
		m["suggestions"] = b
	}
	if b, err := json.Marshal(m); err == nil {
		return string(b)
	}
	return j
}

// missingPillarCriteria lists the checklist keys the answer did not grade.
func missingPillarCriteria(pillarKey string, pa PillarAssessment) []string {
	have := map[string]bool{}
	for _, c := range pa.CriteriaLevels {
		have[strings.TrimSpace(c.Key)] = true
	}
	var missing []string
	for _, c := range PillarChecklist(pillarKey) {
		if !have[c.Key] {
			missing = append(missing, c.Key)
		}
	}
	return missing
}

// mergeCritiqueParts keeps the overall letter/score and concatenates every
// pillar's graded entry and suggestions into one report JSON.
func mergeCritiqueParts(overall string, parts []string) (string, error) {
	var head map[string]json.RawMessage
	if err := json.Unmarshal([]byte(overall), &head); err != nil {
		return "", err
	}
	pillars := make([]json.RawMessage, 0, len(parts))
	suggestions := make([]json.RawMessage, 0, len(parts))
	for _, part := range parts {
		var m map[string]json.RawMessage
		if json.Unmarshal([]byte(part), &m) != nil {
			continue
		}
		for _, p := range jsonArray(m["pillar_scores"]) {
			if np, ok := normalizeMergedPillar(p); ok {
				pillars = append(pillars, np)
			}
		}
		for _, s := range jsonArray(m["suggestions"]) {
			if ns, ok := normalizeMergedSuggestion(s); ok {
				suggestions = append(suggestions, ns)
			}
		}
	}
	out := map[string]any{
		"pillar_scores": pillars,
		"suggestions":   suggestions,
	}
	if raw, ok := head["summary"]; ok {
		if s, ok := coerceJSONString(raw); ok {
			out["summary"] = s
		}
	}
	if raw, ok := head["overall_score"]; ok {
		if n, ok := coerceJSONInt(raw); ok {
			out["overall_score"] = n
		}
	}
	data, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func jsonArray(raw json.RawMessage) []json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var arr []json.RawMessage
	if json.Unmarshal(raw, &arr) != nil {
		return nil
	}
	return arr
}

// normalizeMergedPillar repairs numeric drift in a pillar entry (score 82.5,
// "82"; criteria levels as strings or floats) so one sloppy number cannot fail
// the final strict unmarshal of the whole critique.
func normalizeMergedPillar(raw json.RawMessage) (json.RawMessage, bool) {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil || m == nil {
		return nil, false
	}
	if v, ok := m["score"]; ok {
		if n, ok := coerceJSONInt(v); ok {
			m["score"] = json.RawMessage(fmt.Sprintf("%d", n))
		} else {
			delete(m, "score")
		}
	}
	if v, ok := m["criteria_levels"]; ok {
		levels := make([]map[string]any, 0)
		for _, item := range jsonArray(v) {
			var obj map[string]json.RawMessage
			if json.Unmarshal(item, &obj) != nil || obj == nil {
				continue
			}
			var key string
			if k, ok := obj["key"]; ok {
				if s, ok := coerceJSONString(k); ok {
					_ = json.Unmarshal(s, &key)
				}
			}
			level, hasLevel := 0, false
			if lv, ok := obj["level"]; ok {
				level, hasLevel = coerceJSONInt(lv)
			}
			if key != "" && hasLevel {
				levels = append(levels, map[string]any{"key": key, "level": level})
			}
		}
		if b, err := json.Marshal(levels); err == nil {
			m["criteria_levels"] = b
		} else {
			delete(m, "criteria_levels")
		}
	}
	out, err := json.Marshal(m)
	if err != nil {
		return nil, false
	}
	return out, true
}

// normalizeMergedSuggestion repairs the type drift local models routinely
// introduce in a suggestion object — criteria as bare strings, chapters as an
// int or "3, 4", score_impact as "+10", prose fields as numbers. Without it a
// single bad field failed the report's final strict unmarshal and threw away
// the whole act/chapter/outline critique. Bundled interventions and
// recommendations are dropped here: every editorialSequence caller discards
// them anyway because the first pass is advice-only.
func normalizeMergedSuggestion(raw json.RawMessage) (json.RawMessage, bool) {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil || m == nil {
		return nil, false // non-object entry: drop it, keep the report parseable
	}
	delete(m, "interventions")
	delete(m, "recommendations")
	for _, k := range []string{"id", "category", "title", "detail", "action", "evidence", "status", "applied_at", "pillar", "priority"} {
		if v, ok := m[k]; ok {
			if s, ok := coerceJSONString(v); ok {
				m[k] = s
			} else {
				delete(m, k)
			}
		}
	}
	if v, ok := m["criteria"]; ok {
		if arr := coerceCriteria(v); arr != nil {
			m["criteria"] = arr
		} else {
			delete(m, "criteria")
		}
	}
	for _, k := range []string{"applies_to_chapters", "merge_scene_ids"} {
		if v, ok := m[k]; ok {
			if arr := coerceIntArray(v); arr != nil {
				m[k] = arr
			} else {
				delete(m, k)
			}
		}
	}
	if v, ok := m["score_impact"]; ok {
		if n, ok := coerceJSONInt(v); ok {
			m["score_impact"] = json.RawMessage(fmt.Sprintf("%d", n))
		} else {
			delete(m, "score_impact")
		}
	}
	out, err := json.Marshal(m)
	if err != nil {
		return nil, false
	}
	return out, true
}

func coerceJSONString(raw json.RawMessage) (json.RawMessage, bool) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return raw, true
	}
	var v any
	if json.Unmarshal(raw, &v) != nil || v == nil {
		return nil, false
	}
	switch v.(type) {
	case float64, bool:
		b, err := json.Marshal(fmt.Sprint(v))
		if err != nil {
			return nil, false
		}
		return b, true
	}
	return nil, false
}

// coerceCriteria accepts ["hook"], [{"key":"hook"}], {"key":"hook"} or "hook"
// and always returns an array of objects with a key.
func coerceCriteria(raw json.RawMessage) json.RawMessage {
	var arr []json.RawMessage
	if json.Unmarshal(raw, &arr) != nil {
		arr = []json.RawMessage{raw}
	}
	out := make([]json.RawMessage, 0, len(arr))
	for _, item := range arr {
		var s string
		if json.Unmarshal(item, &s) == nil {
			b, _ := json.Marshal(map[string]string{"key": s})
			out = append(out, b)
			continue
		}
		var obj map[string]json.RawMessage
		if json.Unmarshal(item, &obj) != nil {
			continue
		}
		k, ok := obj["key"]
		if !ok {
			continue
		}
		key, ok := coerceJSONString(k)
		if !ok {
			continue
		}
		obj["key"] = key
		if n, ok := obj["name"]; ok {
			if name, ok := coerceJSONString(n); ok {
				obj["name"] = name
			} else {
				delete(obj, "name")
			}
		}
		b, err := json.Marshal(obj)
		if err == nil {
			out = append(out, b)
		}
	}
	if len(out) == 0 {
		return nil
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil
	}
	return b
}

// coerceIntArray accepts 3, [3,4], ["3","4"], "3, 4" or "3-5".
func coerceIntArray(raw json.RawMessage) json.RawMessage {
	var nums []int
	if json.Unmarshal(raw, &nums) == nil {
		b, _ := json.Marshal(nums)
		return b
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	switch t := v.(type) {
	case float64:
		nums = append(nums, int(t))
	case string:
		for _, part := range strings.FieldsFunc(t, func(r rune) bool {
			return r == ',' || r == ';' || r == ' ' || r == '-' || r == '–'
		}) {
			if n, ok := coerceJSONInt(json.RawMessage(`"` + part + `"`)); ok {
				nums = append(nums, n)
			}
		}
	case []any:
		for _, item := range t {
			b, err := json.Marshal(item)
			if err != nil {
				continue
			}
			if n, ok := coerceJSONInt(b); ok {
				nums = append(nums, n)
			}
		}
	}
	if len(nums) == 0 {
		return nil
	}
	b, _ := json.Marshal(nums)
	return b
}

func coerceJSONInt(raw json.RawMessage) (int, bool) {
	var n int
	if json.Unmarshal(raw, &n) == nil {
		return n, true
	}
	var f float64
	if json.Unmarshal(raw, &f) == nil {
		return int(f), true
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		var parsed int
		if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &parsed); err == nil {
			return parsed, true
		}
	}
	return 0, false
}

// editorialScopeOverride is appended to the shared editorial prompt: it turns
// one prompt builder into both call shapes of the sequence.
//
// pillarKey == ""  → first-impression call (summary + overall score only).
// pillarKey != "" → single-pillar call, every suggestion must cite evidence.
func editorialScopeOverride(lang, pillarKey string) string {
	en := normalizeLangEN(lang)
	if strings.TrimSpace(pillarKey) == "" {
		if en {
			return "\n\n=== SCOPE OVERRIDE — READ LAST; IT SUPERSEDES CONFLICTING INSTRUCTIONS ABOVE ===\nReturn STRICT JSON {\"summary\":\"...\",\"overall_score\":75} and NOTHING else: no pillar grades, no suggestions, no artifact text. This call only produces the editorial letter and the global score."
		}
		return "\n\n=== ANULACIÓN DE ALCANCE — LEE ESTO AL FINAL; SUSTITUYE CUALQUIER INSTRUCCIÓN CONTRARIA ARRIBA ===\nDevuelve SOLO JSON estricto {\"summary\":\"...\",\"overall_score\":75} y NADA más: sin notas de pilares, sin sugerencias, sin repetir el material. Esta llamada solo produce la carta editorial y la nota global."
	}
	if en {
		return fmt.Sprintf(`

=== SCOPE OVERRIDE — READ LAST; IT SUPERSEDES CONFLICTING INSTRUCTIONS ABOVE ===
You are grading ONLY the pillar %q in this call. Return STRICT JSON {"pillar_scores":[<the single %q entry, with criteria_levels grading EVERY checklist item>],"suggestions":[<points that serve %q only>]}. Do not return summary, overall_score, other pillars, or points for other pillars.
EVIDENCE RULE: every suggestion MUST include "evidence" — a short verbatim excerpt (max 25 words) copied from the material that proves the problem. For drafted prose, quote the actual sentence from the chapter text above. For outline / beat-sheet material, cite the exact planned beat with its chapter number plus the planned line it comes from. The simulated beta reader findings are context, never evidence: never use a reader's paraphrase or your own summary as the excerpt. A suggestion without evidence is invalid.`, pillarKey, pillarKey, pillarKey)
	}
	return fmt.Sprintf(`

=== ANULACIÓN DE ALCANCE — LEE ESTO AL FINAL; SUSTITUYE CUALQUIER INSTRUCCIÓN CONTRARIA ARRIBA ===
En esta llamada calificas SOLO el pilar %q. Devuelve SOLO JSON estricto {"pillar_scores":[<la única entrada %q, con criteria_levels calificando CADA ítem de la lista>],"suggestions":[<solo puntos que sirvan a %q>]}. No devuelvas summary, overall_score, otros pilares ni puntos de otros pilares.
REGLA DE EVIDENCIA: cada sugerencia DEBE incluir "evidence" — un extracto textual breve (máx. 25 palabras) copiado del material que demuestra el problema. Para prosa borroneada, cita la frase real del texto de los capítulos de arriba. Para outline / beat sheets, cita el beat planificado exacto con su número de capítulo y la línea del plan de la que sale. Los hallazgos del panel simulado de beta readers son contexto, nunca evidencia: no uses la paráfrasis de un lector ni tu propio resumen como extracto. Una sugerencia sin evidencia es inválida.`, pillarKey, pillarKey, pillarKey)
}

// editorialNoSuggestionsReminder is the strict retry used when a pillar graded
// below the bestseller bar returned no actionable point.
func editorialNoSuggestionsReminder(lang, pillarKey string) string {
	if normalizeLangEN(lang) {
		return fmt.Sprintf("\n\nYour previous response graded %q below 90 but contained ZERO suggestions — that is a failed answer. Return the SAME JSON shape with at least ONE concrete suggestion (title, detail, action, evidence) telling the author exactly what to change for this pillar.", pillarKey)
	}
	return fmt.Sprintf("\n\nTu respuesta anterior calificó %q por debajo de 90 pero contenía CERO sugerencias — es una respuesta fallida. Devuelve la MISMA forma JSON con al menos UNA sugerencia concreta (title, detail, action, evidence) que diga exactamente qué cambiar para este pilar.", pillarKey)
}

// editorialMissingEvidenceReminder is the strict retry used when suggestions
// omit the verbatim excerpt that proves the problem.
func editorialMissingEvidenceReminder(lang, pillarKey string, n int) string {
	if normalizeLangEN(lang) {
		return fmt.Sprintf("\n\nYour previous response had %d suggestion(s) for %q with an EMPTY \"evidence\" field — that is invalid. Every suggestion MUST include \"evidence\": a short verbatim excerpt (max 25 words) from the drafted prose that proves the problem. Return the SAME JSON with a non-empty evidence string for every suggestion.", n, pillarKey)
	}
	return fmt.Sprintf("\n\nTu respuesta anterior tuvo %d sugerencia(s) de %q con el campo \"evidence\" VACÍO — eso es inválido. Cada sugerencia DEBE incluir \"evidence\": un extracto textual breve (máx. 25 palabras) de la prosa borroneada que demuestre el problema. Devuelve la MISMA forma JSON con un evidence no vacío en cada sugerencia.", n, pillarKey)
}

// editorialUnsupportedEvidenceReminder is the corrective retry used when an
// excerpt cannot be traced to the material at all. It keeps the point when the
// problem is real and only asks for a quotation that actually exists.
func editorialUnsupportedEvidenceReminder(lang, pillarKey string, n int) string {
	if normalizeLangEN(lang) {
		return fmt.Sprintf("\n\nYour previous response had %d suggestion(s) for %q whose \"evidence\" does not appear anywhere in the material — it is an invented quotation. Keep the point if the problem is real, but replace \"evidence\" with a short excerpt that exists verbatim in the material (or a paraphrase that reuses its words). If the point itself is not supported by the material, drop it. Return the SAME JSON.", n, pillarKey)
	}
	return fmt.Sprintf("\n\nTu respuesta anterior tuvo %d sugerencia(s) de %q cuyo \"evidence\" no aparece en ninguna parte del material — es una cita inventada. Mantén el punto si el problema es real, pero sustituye \"evidence\" por un extracto breve que exista textualmente en el material (o una paráfrasis que reutilice sus palabras). Si el punto en sí no está respaldado por el material, descártalo. Devuelve el MISMO JSON.", n, pillarKey)
}

// editorialMissingCriteriaReminder is the strict retry used when a pillar
// answer skipped checklist items: the score must be backed by a level for
// every sub-criterion, never by a bare number.
func editorialMissingCriteriaReminder(lang, pillarKey string, missing []string) string {
	if normalizeLangEN(lang) {
		return fmt.Sprintf("\n\nYour previous response left these %q checklist items UNGRADED: %s. Every sub-criterion must be graded 0-100 because the pillar score is DERIVED from those levels. Return the SAME JSON with criteria_levels containing ALL %d keys of the checklist, each with a concrete level and no omissions.", pillarKey, strings.Join(missing, ", "), len(PillarChecklist(pillarKey)))
	}
	return fmt.Sprintf("\n\nTu respuesta anterior dejó SIN CALIFICAR estos ítems de %q: %s. Cada subcriterio debe llevar una nota 0-100 porque la puntuación del pilar se DERIVA de esos niveles. Devuelve la MISMA forma JSON con criteria_levels incluyendo TODAS las %d claves de la lista, cada una con una nota concreta y sin omisiones.", pillarKey, strings.Join(missing, ", "), len(PillarChecklist(pillarKey)))
}
