package story

// ---------------------------------------------------------------------------
// CHAPTER-PLAN bestseller critic — a faithful copy of the scene-plan critic
// (scene_plan_analysis.go) adapted to the CHAPTER rubric.
//
// The drafted-chapter critic (drafted_analysis.go) kept failing to produce
// suggestions because its points carry concrete trope-unit targets that are
// validated against a fragile live scene/trope tiling: whenever map and
// validator diverged, EVERY intervention was discarded and the report shipped
// "score 68/100 — 0 suggestions". The scene-plan critic never had that problem
// because it is ADVICE-ONLY in one single call: suggestions carry no prose
// targets at all, they only name chapters, so sanitizing can never drop them.
//
// This file copies that proven flow verbatim (one LLM call, strict JSON,
// advice-only sanitize loop with spa-style IDs, finalizePillarReport contract,
// fingerprint caching) and changes exactly two things:
//   1. It reviews ONE DRAFTED CHAPTER (prose + written-structure map shown for
//      context) under the CHAPTER rubric instead of pending beat sheets.
//   2. Immediately after the critique, the pipeline AUTO-PROPOSES surgical
//      interventions per point (ProposeChapterInterventions) so beats/tropes
//      get concrete fixes without extra clicks, and the report's suggestion
//      cards render in the Writing page exactly like the scene-plan ones.
//
// It stores its report on the SAME ChapterAnalysisReport field as the old
// chapter critic, so handlers, ProgressView serialization and the frontend
// keep working untouched.
// ---------------------------------------------------------------------------

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"showmethestory/internal/config"
	"showmethestory/internal/llm"
	"showmethestory/internal/sse"
)

// buildChapterPlanEditorPrompt assembles the SINGLE LLM prompt for the
// bestseller-editor pass over ONE drafted chapter. Structurally identical to
// buildScenePlanEditorPrompt (same sections, same grading standard, same
// strict-JSON contract), except the artifact under review is the chapter's
// PROSE inside its act, judged against the four-pillar chapter rubric.
func buildChapterPlanEditorPrompt(cfg *config.Config, state *Progress, settings *ProjectSettings, idx int, lang string) string {
	// prevChecklist embeds the previous pass's graded rubric (when any) so a
	// re-analysis after corrections re-grades the SAME items and provably
	// raises them — instead of drifting around randomized example anchors.
	var prevChecklist string
	if ch := state.Chapters[idx].Analysis; ch != nil && len(ch.PillarScores) > 0 {
		prevChecklist = renderPreviousPillarChecklist(ch.PillarScores, "THIS CHAPTER")
	}
	sc := cfg.Story
	var meta strings.Builder
	add := func(label, v string) {
		if strings.TrimSpace(v) != "" {
			fmt.Fprintf(&meta, "%s: %s\n", label, strings.TrimSpace(v))
		}
	}
	add("Story idea", sc.StoryIdea)
	add("Genre / subgenre", strings.TrimSpace(sc.Type+" / "+sc.Subgenre))
	add("Theme", sc.Theme)
	add("Tone", sc.Tone)
	add("Story structure (framework the book must follow)", sc.Structure)
	add("Writing style & POV", strings.TrimSpace(sc.WritingStyle+" / "+sc.WritingPOV))
	add("Ideal reader profile", sc.AudienceProfile)
	add("Inspirational pieces (comp titles / mood references)", sc.InspirationalPieces)
	add("Conflict scale", sc.EffectiveConflict())
	add("Protagonist type", sc.EffectiveProtagonist())
	if settings != nil {
		cast := make([]string, 0, len(settings.Characters))
		for _, c := range settings.Characters {
			cast = append(cast, c.Name)
		}
		add("Registered characters", strings.Join(cast, ", "))
	}

	ch := state.Chapters[idx]
	actNo, actName, hasAct := actForChapter(state, ch.Num)
	structureName := structureLabel(sc.Structure, lang)
	history := Truncate(buildHistorySummary(state, idx), 4000)
	tropeMap := chapterTropeMapBlock(ch, lang)
	content := draftTruncate(ch.Content, 26000)

	whereEn := fmt.Sprintf("Chapter #%d \"%s\" of %d", ch.Num, ch.Title, len(state.Chapters))
	whereEs := fmt.Sprintf("Capítulo #%d \"%s\" de %d", ch.Num, ch.Title, len(state.Chapters))
	if hasAct {
		whereEn += fmt.Sprintf(", inside ACT %d (%s) of the book's planned structure", actNo, actName)
		whereEs += fmt.Sprintf(", dentro del ACTO %d (%s) de la estructura planificada", actNo, actName)
	}

	schemaEN := `{"summary":"2-4 paragraph editorial letter to the author, warm but honest, in LANGUAGE","overall_score":75,"pillar_scores":[{"key":"reader_profile","score":0,"criteria_levels":[{"key":"genre_promise","level":0},{"key":"reader_hooks","level":0},{"key":"tropes_freshness","level":0},{"key":"emotional_payoff","level":0},{"key":"comp_distance","level":0}],"note":"one-sentence verdict in LANGUAGE"},{"key":"inspirational_pieces","score":0,"criteria_levels":[...],"note":"..."},{"key":"tone","score":0,"criteria_levels":[...],"note":"..."},{"key":"story_structure","score":0,"criteria_levels":[...],"note":"..."}],"suggestions":[{"category":"hook|pacing|scene_economy|stakes|structure|emotion|pov|budget|other","pillar":"reader_profile|inspirational_pieces|tone|story_structure","criteria":[{"key":"checklist_key_it_repairs"}],"score_impact":10,"title":"short imperative headline","detail":"what the chapter currently does and why it falls short of bestseller level","action":"concrete guidance on which passages/scenes to change and how","applies_to_chapters":[N]}]}`

	if normalizeLangEN(lang) {
		return fmt.Sprintf(`You are %s, a world-famous literary editor whose lists have produced multiple global bestsellers. Your job: give the author a constructive, point-by-point editorial pass over ONE DRAFTED CHAPTER of their novel in progress, with ONE clear goal: turn this book into a global bestseller — every point you make must serve that bar, not mere competence.

CRITICAL CONTEXT — WHAT YOU ARE REVIEWING:
- You are reviewing %s AS DRAFTED PROSE. This chapter EXISTS as written text (shown below); judge actual sentences, scenes, dialogue, pacing and hooks — this is NOT an outline or a beat sheet.
- Grade the chapter both as a standalone reading experience AND as one link in its act's chain: does it deliver the function the structure assigns to this position, escalate stakes, and land a chapter-ending hook?
- This pass is ADVICE ONLY: do NOT draft replacement text here. Concrete micro-fixes on specific beats/subscenes are requested later, explicitly, via a "Propose interventions" action — so nothing gets rewritten before the author chooses to act on a specific point.

NOVEL PARAMETERS (the author's contract with the reader):
%s
STORY STRUCTURE FRAMEWORK IN USE: %s

[Story-so-far, condensed]
%s
%s
[Chapter outline — the contract this chapter must fulfil]
%s

%s
[CHAPTER PROSE (%d words)]
%s

YOUR MISSION: reverse-engineer what makes books in this genre hit #1 (an irresistible hook, relentless pacing, arcs that pay off, stakes that escalate) and grade THIS CHAPTER against that blueprint. Every suggestion must be a concrete step that measurably moves the chapter toward bestseller level on its pillar.

Analyze the chapter against ALL parameters above, giving special weight to these four pillars:
1. Ideal reader profile — does this chapter deliver what this reader came for? Genre expectations met without cliché?
2. Inspirational pieces — does the chapter live up to the comp titles' standard of craft without copying them?
3. Tone — is the intended tone sustained and escalating properly across the chapter's scenes?
4. Story structure (%s) — does the chapter advance the framework's beats for its position? Dead zones, redundant beats, rushed payoffs, weak ending hook?

Also weigh: outline coverage fidelity, scene economy inside the chapter, POV clarity, chapter-ending hooks.

Return STRICT JSON only (no markdown fences, no prose):
%s

Grading standard (you are ruthless — your reputation rests on books that hit #1 worldwide):
- Score each of the four pillars 0-100 against BESTSELLER level, not "competent". 90+ means the chapter is genuinely strong as-is. Reserve 90+ for truly exceptional pages; most drafted chapters land mid-range. Derive every score independently from THIS specific chapter: grade each pillar's checklist item (criteria_levels, 0–100) and let the pillar score be their average, so re-grading corrected material raises exactly the items that improved. Flat scenes, dead zones, rushed payoffs or weak chapter-ending hooks cap scores below 85.
- pillar_scores MUST contain exactly the four keys reader_profile, inspirational_pieces, tone, story_structure.
- Every pillar scoring below 90 MUST have at least one corresponding suggestion (reader_profile→hook/other, inspirational_pieces→other, tone→emotion, story_structure→structure/pacing/scene_economy/stakes). A pillar scoring 90+ may have zero suggestions — then the UI shows "No changes needed" for it.

Rules:
- 3 to 8 suggestions, ordered most impactful first.
- DO NOT include replacement prose in this pass: return every suggestion WITHOUT an "interventions" array and WITHOUT "recommendations" (micro-fixes are drafted later, ONLY after the author triggers "Propose interventions" — the pipeline does this automatically right after this pass).
- applies_to_chapters must just name this chapter (%d).
- action must describe WHAT to change and HOW at passage level (tighten this beat, move this reveal earlier, sharpen the closing hook), framed as closing specific checklist items (e.g. "raise pacing by cutting the middle beat of scene 2"), so the author sees exactly which area of which pillar moves toward 100%%.
- Write summary, titles, details and actions in the SAME language as the chapter prose above.`,
			editorPersona(lang), whereEn, meta.String(), structureName, history, prevChecklist,
			strings.TrimSpace(ch.Outline), tropeMap, ch.WordCount, content,
			structureName, strings.ReplaceAll(schemaEN, "LANGUAGE", lang), ch.Num)
	}
	return fmt.Sprintf(`Eres %s, un editor literario mundialmente famoso cuyas listas han producido múltiples bestsellers globales. Tu trabajo: dar al autor una pasada editorial constructiva y punto por punto sobre UN CAPÍTULO BORRONEADO de su novela en curso, con UN objetivo claro: convertir este libro en un bestseller global — cada punto que plantees debe servir a ese listón, no a la mera corrección.

CONTEXTO CRÍTICO — QUÉ ESTÁS REVISANDO:
- Estás revisando %s COMO PROSA BORRONEADA. Este capítulo EXISTE como texto escrito (se muestra abajo); juzga frases reales, escenas, diálogos, ritmo y ganchos — NO es un outline ni un beat sheet.
- Evalúa el capítulo tanto como experiencia de lectura autónoma como eslabón de la cadena de su acto: ¿cumple la función que la estructura asigna a esta posición, escala la apuesta y aterriza un gancho final?
- Esta pasada es SOLO CONSEJO: no redactes textos de reemplazo aquí. Las micro-correcciones concretas sobre beats/subescenas se piden después, explícitamente, vía la acción "Proponer intervenciones" — así nada se reescribe antes de que el autor decida actuar sobre un punto específico.

PARÁMETROS DE LA NOVELA (el contrato del autor con el lector):
%s
MARCO DE ESTRUCTURA NARRATIVA EN USO: %s

[Historia hasta ahora, condensada]
%s
%s
[Outline del capítulo — el contrato que este capítulo debe cumplir]
%s

%s
[PROSA DEL CAPÍTULO (%d palabras)]
%s

TU MISIÓN: ingeniería inversa de qué hace que los libros de este género lleguen a #1 (un gancho irresistible, ritmo implacable, arcos que pagan, apuesta creciente) y califica ESTE CAPÍTULO contra ese plano. Cada sugerencia debe ser un paso concreto que mueva medible el capítulo hacia nivel bestseller en su pilar.

Analiza el capítulo contra TODOS los parámetros de arriba, dando peso especial a estos cuatro pilares:
1. Perfil de lector ideal — ¿este capítulo entrega lo que este lector vino a buscar? ¿Expectativas de género cumplidas sin cliché?
2. Obras inspiracionales — ¿el capítulo está a la altura del estándar de las obras de referencia sin copiarlas?
3. Tono — ¿el tono previsto se sostiene y escala correctamente a lo largo de las escenas del capítulo?
4. Estructura narrativa (%s) — ¿el capítulo avanza los beats del marco para su posición? ¿Zonas muertas, beats redundantes, pagos apresurados, gancho final débil?

Evalúa además: fidelidad al outline, economía de escenas dentro del capítulo, claridad de POV, ganchos de cierre.

Devuelve SOLO JSON estricto (sin cercas markdown, sin prosa):
%s

Estándar de nota (eres despiadado — tu reputación descansa en libros que llegan a #1 mundial):
- Puntúa cada uno de los cuatro pilares 0-100 contra nivel BESTSELLER, no "correcto". 90+ significa que el capítulo es genuinamente fuerte tal cual. Reserva 90+ para páginas verdaderamente excepcionales; la mayoría de capítulos borroneados caen en la franja media. Deriva cada puntuación de forma independiente de ESTE capítulo concreto: califica cada ítem de la lista del pilar (criteria_levels, 0–100) y deja que la puntuación del pilar sea su promedio, así recalificar material corregido sube exactamente los ítems mejorados. Escenas planas, zonas muertas, pagos apresurados o ganchos finales débiles topen las notas por debajo de 85.
- pillar_scores DEBE contener exactamente las cuatro claves reader_profile, inspirational_pieces, tone, story_structure.
- Todo pilar por debajo de 90 DEBE tener al menos una sugerencia correspondiente (reader_profile→hook/other, inspirational_pieces→other, tone→emotion, story_structure→structure/pacing/scene_economy/stakes). Un pilar con 90+ puede no tener sugerencias — entonces la UI muestra "Sin cambios necesarios".

Reglas:
- De 3 a 8 sugerencias, ordenadas de mayor a menor impacto.
- NO incluyas prosa de reemplazo en esta pasada: devuelve cada sugerencia SIN array "interventions" y SIN "recommendations" (las micro-correcciones se redactan después, SOLO tras "Proponer intervenciones" — el pipeline lo ejecuta automáticamente justo después de esta pasada).
- applies_to_chapters debe nombrar solo este capítulo (%d).
- action debe describir QUÉ cambiar y CÓMO a nivel de pasaje (ajustar este beat, mover este revelado más adelante, afilar el gancho final), planteado como cierre de ítems específicos de la lista (p. ej. "subir pacing recortando el beat central de la escena 2"), para que el autor vea exactamente qué área de qué pilar se acerca al 100%%.
- Escribe resumen, títulos, detalles y acciones en el MISMO idioma que la prosa del capítulo de arriba.`,
		editorPersona(lang), whereEs, meta.String(), structureName, history, prevChecklist,
		strings.TrimSpace(ch.Outline), tropeMap, ch.WordCount, content,
		structureName, strings.ReplaceAll(schemaEN, "LANGUAGE", "español"), ch.Num)
}

// AnalyzeChapterPlan runs the bestseller-editor pass over ONE drafted chapter
// in ONE LLM call — the exact flow of the scene-plan critic (AnalyzePending
// ScenePlans) adapted to the chapter rubric — and stores the report on that
// ChapterState.Analysis. Callers persist progress. Identical fingerprint →
// cached report, no LLM call. Right after the critique it AUTO-PROPOSES
// surgical interventions per point so beats/tropes receive concrete fixes
// immediately in the pipeline while the suggestion cards render like the
// scene-plan critic's.
func AnalyzeChapterPlan(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, settings *ProjectSettings, chapterNum int, progressPath string, logger *sse.LogBroadcaster) error {
	idx := -1
	for i := range state.Chapters {
		if state.Chapters[i].Num == chapterNum {
			idx = i
			break
		}
	}
	if idx == -1 {
		return fmt.Errorf("chapter_not_found")
	}
	ch := &state.Chapters[idx]
	if strings.TrimSpace(ch.Content) == "" {
		loadDraftedChapterFilesInto(progressPath, state)
		ch = &state.Chapters[idx]
	}
	if strings.TrimSpace(ch.Content) == "" {
		return fmt.Errorf("chapter_not_drafted")
	}
	fingerprint := draftedChapterFingerprint(*ch)
	if prev := ch.Analysis; prev != nil && prev.DraftFingerprint != "" && prev.DraftFingerprint == fingerprint {
		logger.InfoKey("log.chapter_analyze_unchanged", ch.Num)
		return nil
	}
	lang := cfg.Language
	settingsForPrompt := settings
	if settings != nil {
		if selected, _ := retrieveSettings(settings, collectAllOutlines(state), settingsContextRunes); selected != nil {
			settingsForPrompt = selected
		}
	}
	prompt := buildChapterPlanEditorPrompt(cfg, state, settingsForPrompt, idx, lang)
	system := "You are a world-famous literary editor specializing in turning novels into global bestsellers. You are reviewing ONE DRAFTED CHAPTER (actual manuscript prose) of a novel in progress. Be constructive, specific and honest. Output strict JSON exactly as requested — no extra prose, no markdown code fences."

	logger.InfoKey("log.chapter_analyze_running", ch.Num)
	var result ChapterAnalysisReport
	for attempt := 0; attempt < 2; attempt++ {
		raw := llm.CallAPIWithRetryLog(ctx, apiCfg, system, prompt, logger)
		if raw == "" {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("API call failed or was canceled")
		}
		j := llm.ExtractJSON(raw)
		result = ChapterAnalysisReport{}
		if j != "" && json.Unmarshal([]byte(j), &result) == nil && (result.Summary != "" || len(result.Suggestions) > 0) {
			break
		}
		if attempt == 1 {
			return fmt.Errorf("model response is not valid chapter-analysis JSON")
		}
		prompt += "\n\nYour previous response was invalid. Return one complete JSON object only, using the required schema."
	}

	// Sanitize: the initial pass is advice-only; bundled interventions are
	// discarded so no prose can silently suggest rewrites before an explicit
	// proposal round. Chapters named in applies_to_chapters collapse to THIS
	// chapter only. Unlike the old multi-call path, dropping a target can NEVER
	// drop the suggestion itself — points carry no targets at all here, which is
	// precisely why the scene-plan critic always shows suggestions.
	chapSet := map[int]bool{ch.Num: true}
	kept := make([]DraftedSuggestion, 0, len(result.Suggestions))
	for i, s := range result.Suggestions {
		s.Title = strings.TrimSpace(s.Title)
		s.Detail = strings.TrimSpace(s.Detail)
		s.Action = strings.TrimSpace(s.Action)
		if !validToneCategories[s.Category] {
			s.Category = "other"
		}
		if s.Title == "" && s.Detail == "" {
			continue
		}
		s.Interventions = nil
		s.Recommendations = nil
		cleanApps := make([]int, 0, 1)
		for _, n := range s.AppliesToChapters {
			if chapSet[n] {
				cleanApps = append(cleanApps, n)
				break
			}
		}
		if len(cleanApps) == 0 {
			cleanApps = []int{ch.Num}
		}
		sort.Ints(cleanApps)
		s.AppliesToChapters = cleanApps
		s.ID = fmt.Sprintf("chpa_%d_%d", time.Now().Unix(), i)
		if s.Status != "applied" {
			s.Status = "pending"
		}
		kept = append(kept, s)
	}
	// Guarantee the rubric contract: every pillar below the bestseller bar must
	// surface at least one point. If the model graded low but returned fewer
	// points than weak pillars, synthesize one actionable point per uncovered
	// weak pillar from its own note — the scene-plan critic relies on the model
	// honoring this rule; the chapter critic adds a deterministic backstop so a
	// low score can NEVER ship zero suggestions again.
	weakPillars := map[string]int{}
	for _, pa := range result.PillarScores {
		if pa.Key != "" && pa.Score > 0 && pa.Score < strongPillarScore {
			weakPillars[pa.Key] = pa.Score
		}
	}
	covered := map[string]bool{}
	for _, s := range kept {
		if s.Pillar != "" {
			covered[s.Pillar] = true
		} else if cat := inferredPillarForCategory(s.Category); cat != "" {
			covered[cat] = true
		}
	}
	if len(kept) == 0 || len(weakPillars) > 0 {
		for pk, score := range weakPillars {
			if covered[pk] {
				continue
			}
			note := ""
			for _, pa := range result.PillarScores {
				if pa.Key == pk {
					note = strings.TrimSpace(pa.Note)
				}
			}
			en := normalizeLangEN(lang)
			title, action := fallbackPointTextEN(pk)
			if !en {
				title, action = fallbackPointTextES(pk)
			}
			detail := note
			if detail == "" {
				if en {
					detail = fmt.Sprintf("This pillar scored %d/100 against bestseller level with no concrete editorial point attached — every weakness below 90 needs an actionable fix.", score)
				} else {
					detail = fmt.Sprintf("Este pilar puntúa %d/100 contra nivel bestseller sin ningún punto editorial concreto adjunto — toda debilidad por debajo de 90 necesita una corrección accionable.", score)
				}
			}
			kept = append(kept, DraftedSuggestion{
				ID:                fmt.Sprintf("chpa_%d_fb", time.Now().Unix()),
				Category:          fallbackPointCategory(pk),
				Pillar:            pk,
				Title:             title,
				Detail:            detail,
				Action:            action,
				Status:            "pending",
				AppliesToChapters: []int{ch.Num},
			})
		}
	}
	if result.OverallScore < 0 || result.OverallScore > 100 {
		result.OverallScore = 0
	}
	finalIn := make([]FinalizableSuggestion, len(kept))
	for i, s := range kept {
		finalIn[i] = FinalizableSuggestion{Category: s.Category, Status: s.Status, Pillar: s.Pillar, Impact: s.ScoreImpact, Criteria: s.Criteria}
	}
	cleanPillars, finalOut := finalizePillarReport(result.PillarScores, finalIn)
	for i := range kept {
		kept[i].Pillar = finalOut[i].Pillar
		kept[i].Criteria = finalOut[i].Criteria
		kept[i].ScoreImpact = finalOut[i].Impact
		kept[i].Priority = finalOut[i].Priority
	}
	result.PillarScores = cleanPillars
	result.Summary = strings.TrimSpace(result.Summary)
	result.Suggestions = kept
	result.GeneratedAt = time.Now().Format(time.RFC3339)
	result.BasedOnStructure = structureLabel(cfg.Story.Structure, lang)
	result.ChapterNum = ch.Num
	result.DraftFingerprint = fingerprint
	ch.Analysis = &result
	refreshTropeIssuesWithConflict(ch, state.PendingWritingConflict)

	if logger != nil && len(result.Suggestions) == 0 {
		logger.WarnKey("log.chapter_analyze_no_suggestions", ch.Num, result.OverallScore)
	}

	// Auto-propose: immediately turn every advice-only chapter point into
	// concrete surgical interventions on beats/trope units, IN THE PIPELINE,
	// right after the critique — so the inline (Grammarly-style) highlights and
	// the suggestion cards show actionable fixes without extra clicks.
	// Best-effort: a failed proposal never fails the analysis itself; the author
	// can still trigger it manually from the critique card.
	for _, s := range result.Suggestions {
		if len(s.Interventions) > 0 || strings.TrimSpace(s.ID) == "" {
			continue
		}
		if ctx.Err() != nil {
			break
		}
		if logger != nil {
			logger.InfoKey("log.chapter_analyze_auto_proposal", draftTruncate(s.Title, 60))
		}
		if _, err := ProposeChapterInterventions(ctx, apiCfg, cfg, state, settingsForPrompt, chapterNum, s.ID, progressPath, logger); err != nil {
			logger.WarnKey("log.chapter_analyze_auto_propose_failed", ch.Num, s.ID)
		}
	}
	return nil
}

// renderPreviousPillarChecklist formats the previous chapter pass's graded
// rubric for embedding in the editorial prompt — the same iterative-scoring
// contract renderPreviousChecklistBlock provides for reports that implement
// OpenCritiqueReport, but reading the persisted PillarScores directly (the
// drafted-chapter report stores its checklist there). Returns "" when no
// prior grades exist.
func renderPreviousPillarChecklist(pillars []PillarAssessment, scope string) string {
	var b strings.Builder
	printed := false
	for _, key := range analysisPillarKeys {
		for i := range pillars {
			p := pillars[i]
			if p.Key != key || len(p.Checklist) == 0 {
				continue
			}
			if !printed {
				fmt.Fprintf(&b, "\nPREVIOUS EDITORIAL PASS ON %s — your own rubric grades, per sub-criterion. Re-grade EVERY item below against the CURRENT material: where the author applied corrections, the affected items MUST rise (and their suggestions close); never lower an item without stating why in its pillar note; keep items you cannot see changed in the material.\n", strings.ToUpper(scope))
				printed = true
			}
			names := make([]string, 0, len(p.Checklist))
			for _, c := range p.Checklist {
				if c.Level < 0 {
					names = append(names, c.Key+"=ungraded")
				} else {
					names = append(names, fmt.Sprintf("%s=%d", c.Key, c.Level))
				}
			}
			fmt.Fprintf(&b, "- %s (previous derived score %d/100): %s\n", key, p.Score, strings.Join(names, ", "))
		}
	}
	if !printed {
		return ""
	}
	b.WriteString("\n")
	return b.String()
}

// inferredPillarForCategory maps a suggestion category to the pillar it most
// plausibly serves when the model omitted the explicit "pillar" field — used
// only by the weak-pillar coverage backstop above.
func inferredPillarForCategory(cat string) string {
	switch cat {
	case "hook":
		return "reader_profile"
	case "emotion":
		return "tone"
	case "structure", "pacing", "scene_economy", "stakes":
		return "story_structure"
	default:
		return ""
	}
}

// fallbackPointCategory picks the rubric category for a synthesized point.
func fallbackPointCategory(pillarKey string) string {
	switch pillarKey {
	case "reader_profile":
		return "hook"
	case "tone":
		return "emotion"
	case "story_structure":
		return "structure"
	default:
		return "other"
	}
}

// fallbackPointTextEN returns (title, action) for a synthesized EN point.
func fallbackPointTextEN(pillarKey string) (string, string) {
	switch pillarKey {
	case "reader_profile":
		return "Deliver the promise this chapter made to the ideal reader",
			"Sharpen the genre payoff of this chapter: identify the flat passages where the reader's expectation goes unpaid and rewrite those beats so the promise lands explicitly."
	case "inspirational_pieces":
		return "Raise this chapter to the comp titles' standard",
			"Compare the weakest scene against the comp titles' benchmark moment and lift its hook, tension curve and emotional register with targeted micro-fixes."
	case "tone":
		return "Sustain and escalate the intended tone across the chapter",
			"Find the passages where the intended tone breaks or flattens and adjust diction, pacing and interiority there so the tonal arc escalates toward the chapter-ending hook."
	default:
		return "Make this chapter advance the framework beat it is assigned",
			"Tighten the chapter's structure: cut or merge dead-zone beats, move late payoffs earlier if rushed, and strengthen the closing hook so the act chain never slackens."
	}
}

// fallbackPointTextES returns (title, action) for a synthesized ES point.
func fallbackPointTextES(pillarKey string) (string, string) {
	switch pillarKey {
	case "reader_profile":
		return "Cumple la promesa que este capítulo hizo al lector ideal",
			"Afila el pago de género del capítulo: identifica los pasajes planos donde la expectativa del lector queda insatisfecha y reescribe esos beats para que la promesa aterrice explícitamente."
	case "inspirational_pieces":
		return "Eleva este capítulo al estándar de las obras de referencia",
			"Compara la escena más débil con el momento de referencia de las obras comp y sube su gancho, curva de tensión y registro emocional con micro-correcciones puntuales."
	case "tone":
		return "Sostén y escala el tono previsto a lo largo del capítulo",
			"Encuentra los pasajes donde el tono se rompe o se aplana y ajusta dicción, ritmo e interioridad allí para que el arco tonal escale hacia el gancho final."
	default:
		return "Haz que este capítulo avance el beat del marco que tiene asignado",
			"Ajusta la estructura del capítulo: corta o fusiona beats de zona muerta, adelanta pagos tardíos si van apresurados y refuerza el gancho de cierre para que la cadena del acto nunca se afloje."
	}
}
