package story

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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
func editorialSequence(ctx context.Context, apiCfg *config.APIConfig, logger *sse.LogBroadcaster, system, lang string, build func(pillarKey string) string) (string, error) {
	overall, err := editorialOverallCall(ctx, apiCfg, logger, system, build(""))
	if err != nil {
		return "", err
	}
	parts := make([]string, 0, len(analysisPillarKeys))
	for _, pk := range analysisPillarKeys {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if part, ok := editorialPillarCall(ctx, apiCfg, logger, system, lang, pk, build(pk)); ok {
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
// instead of being persisted as an unactionable verdict.
func editorialPillarCall(ctx context.Context, apiCfg *config.APIConfig, logger *sse.LogBroadcaster, system, lang, pillarKey, prompt string) (string, bool) {
	noSuggestions := false
	for attempt := 0; attempt < 2; attempt++ {
		p := prompt
		if attempt > 0 {
			if noSuggestions {
				p += editorialNoSuggestionsReminder(lang, pillarKey)
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
			PillarScores []PillarAssessment `json:"pillar_scores"`
			Suggestions  []json.RawMessage  `json:"suggestions"`
		}
		if json.Unmarshal([]byte(j), &probe) != nil {
			continue
		}
		graded, score := false, 0
		for _, pa := range probe.PillarScores {
			if pa.Key != pillarKey {
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
			break
		}
		if !graded {
			continue
		}
		if len(probe.Suggestions) == 0 && score > 0 && score < strongPillarScore && attempt == 0 {
			noSuggestions = true
			continue
		}
		return j, true
	}
	return "", false
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
		pillars = append(pillars, jsonArray(m["pillar_scores"])...)
		suggestions = append(suggestions, jsonArray(m["suggestions"])...)
	}
	out := map[string]any{
		"pillar_scores": pillars,
		"suggestions":   suggestions,
	}
	if raw, ok := head["summary"]; ok {
		out["summary"] = raw
	}
	if raw, ok := head["overall_score"]; ok {
		out["overall_score"] = raw
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
EVIDENCE RULE: every suggestion MUST include "evidence" — a short verbatim excerpt (max 25 words) from the material that proves the problem. For drafted prose, quote the actual sentence. For outline / beat-sheet material, cite the exact planned beat with its chapter number plus the planned line it comes from. A suggestion without evidence is invalid.`, pillarKey, pillarKey, pillarKey)
	}
	return fmt.Sprintf(`

=== ANULACIÓN DE ALCANCE — LEE ESTO AL FINAL; SUSTITUYE CUALQUIER INSTRUCCIÓN CONTRARIA ARRIBA ===
En esta llamada calificas SOLO el pilar %q. Devuelve SOLO JSON estricto {"pillar_scores":[<la única entrada %q, con criteria_levels calificando CADA ítem de la lista>],"suggestions":[<solo puntos que sirvan a %q>]}. No devuelvas summary, overall_score, otros pilares ni puntos de otros pilares.
REGLA DE EVIDENCIA: cada sugerencia DEBE incluir "evidence" — un extracto textual breve (máx. 25 palabras) del material que demuestra el problema. Para prosa borroneada, cita la frase real. Para outline / beat sheets, cita el beat planificado exacto con su número de capítulo y la línea del plan de la que sale. Una sugerencia sin evidencia es inválida.`, pillarKey, pillarKey, pillarKey)
}

// editorialNoSuggestionsReminder is the strict retry used when a pillar graded
// below the bestseller bar returned no actionable point.
func editorialNoSuggestionsReminder(lang, pillarKey string) string {
	if normalizeLangEN(lang) {
		return fmt.Sprintf("\n\nYour previous response graded %q below 90 but contained ZERO suggestions — that is a failed answer. Return the SAME JSON shape with at least ONE concrete suggestion (title, detail, action, evidence) telling the author exactly what to change for this pillar.", pillarKey)
	}
	return fmt.Sprintf("\n\nTu respuesta anterior calificó %q por debajo de 90 pero contenía CERO sugerencias — es una respuesta fallida. Devuelve la MISMA forma JSON con al menos UNA sugerencia concreta (title, detail, action, evidence) que diga exactamente qué cambiar para este pilar.", pillarKey)
}
