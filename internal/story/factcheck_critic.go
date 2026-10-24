package story

// Evolved chapter fact-check — the "bestseller critic" applied to prose.
//
// The classic fact-checker only answered PASS/FAIL plus a list of issues, and
// every FAIL burned a full-chapter regeneration retry. This evolution gives
// the reviewer three things the outline bestseller critic already has:
//
//  1. Story awareness: it reviews chapter N knowing it sits inside Act X of
//     the book (from the pre-planning act cards), so pacing/structure verdicts
//     are judged against where the chapter SHOULD be in the arc.
//  2. Scene granularity: it receives the chapter's beat sheet (scene plan) and
//     must attribute every issue to a concrete scene via an exact quote from
//     the prose.
//  3. Repair routing: it decides whether each problem is fixable by rewriting
//     ONE scene (scope "scene") or requires touching the whole chapter
//     (scope "chapter"). Scene-scoped issues are repaired surgically through
//     rewriteSceneTrope before ever regenerating the chapter, so voice, tense
//     and word budgets of the healthy scenes stay byte-identical.

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"showmethestory/internal/config"
	"showmethestory/internal/i18n"
	"showmethestory/internal/llm"
	"sort"
	"strings"

	"showmethestory/internal/sse"
)

const maxCriticSceneRepairs = 4

// CriticIssue is one finding of the evolved fact-check critic.
type CriticIssue struct {
	Type    string `json:"type"`    // continuity | premature_event | re_enactment | identity | timeline | other
	Scope   string `json:"scope"`   // "scene" | "chapter"
	SceneID int    `json:"scene_id"` // planned scene ID (0 when unknown / chapter-scope)
	Quote   string `json:"quote"`   // short EXACT excerpt from the prose carrying the problem
	Issue   string `json:"issue"`   // what contradicts what, concretely
	Fix     string `json:"fix"`     // concrete guidance for the repair pass
}

// CriticVerdict is the parsed JSON answer of the chapter critic.
type CriticVerdict struct {
	Result  string        `json:"result"` // PASS | FAIL
	Scope   string        `json:"scope"`  // overall repair scope ("scene" | "chapter")
	Issues  []CriticIssue `json:"issues"`
	Summary string        `json:"summary"`
}

// actForChapter locates the act card covering chapter num in the book's
// pre-planning. Returns the 1-based act number, its name and whether found.
func actForChapter(state *Progress, num int) (int, string, bool) {
	if state == nil || state.PrePlanning == nil {
		return 0, "", false
	}
	for i, act := range state.PrePlanning.Acts {
		start, end := act.StartCh, act.EndCh
		if start == 0 && end == 0 {
			continue // no chapter mapping on this card
		}
		if start == 0 {
			start = 1
		}
		if end == 0 {
			end = start + max(1, act.SuggestedChapters) - 1
		}
		if num >= start && num <= end {
			return i + 1, strings.TrimSpace(act.Name), true
		}
	}
	return 0, "", false
}

// buildCriticSceneMapBlock renders the chapter's beat sheet with per-scene
// paragraph ranges and subscene labels so the critic can attribute issues.
func buildCriticSceneMapBlock(ch ChapterState, lang string) string {
	en := i18n.NormalizeLanguage(lang) == i18n.LangEN
	if ch.ScenePlan == nil || len(ch.ScenePlan.Scenes) == 0 {
		if en {
			return "[Chapter structure]\nThis chapter was written without a scene plan — treat it as one continuous text.\n\n"
		}
		return "[Estructura del capítulo]\nEste capítulo se escribió sin plan de escenas — trátalo como un texto continuo.\n\n"
	}
	var sb strings.Builder
	if en {
		sb.WriteString("[Chapter structure — scenes and their subscenes]\n")
		sb.WriteString("Issues that live inside ONE scene must be reported with scope=\"scene\", that scene's id, and a verbatim quote taken from its text below.\n")
	} else {
		sb.WriteString("[Estructura del capítulo — escenas y sus subescenas]\n")
		sb.WriteString("Los problemas que viven dentro de UNA escena deben reportarse con scope=\"scene\", el id de esa escena y una cita textual tomada de su texto abajo.\n")
	}
	for _, sc := range ch.ScenePlan.Scenes {
		title := sceneTitleOf(sc, lang)
		unitDescs := make([]string, 0, len(sc.TropeUnits))
		for _, u := range sc.TropeUnits {
			unitDescs = append(unitDescs, fmt.Sprintf("%d:%s(%dw)", u.Index+1, u.Label, u.Words))
		}
		unitsLine := ""
		if len(unitDescs) > 0 {
			if en {
				unitsLine = fmt.Sprintf(" — subscenes: %s", strings.Join(unitDescs, " → "))
			} else {
				unitsLine = fmt.Sprintf(" — subescenas: %s", strings.Join(unitDescs, " → "))
			}
		}
		fmt.Fprintf(&sb, "- Scene id=%d \"%s\" (~%d words)%s\n", sc.ID, title, sc.WordsBudget, unitsLine)
		if d := strings.TrimSpace(sc.Description); d != "" {
			fmt.Fprintf(&sb, "  %s\n", Truncate(d, 200))
		}
	}
	sb.WriteString("\n")
	return sb.String()
}

// buildCriticActBlock tells the critic which act of the book this chapter is.
func buildCriticActBlock(state *Progress, ch ChapterState, lang string) string {
	en := i18n.NormalizeLanguage(lang) == i18n.LangEN
	total := len(state.Chapters)
	num := ch.Num
	if actNo, actName, ok := actForChapter(state, num); ok {
		if en {
			return fmt.Sprintf("[Where this chapter sits]\nChapter %d of %d, inside ACT %d (%s) of the book's planned structure. Judge arc/pacing problems against what this act is supposed to accomplish.\n\n", num, total, actNo, actName)
		}
		return fmt.Sprintf("[Dónde está este capítulo]\nCapítulo %d de %d, dentro del ACTO %d (%s) de la estructura planificada. Evalúa los problemas de arco/ritmo contra lo que este acto debe cumplir.\n\n", num, total, actNo, actName)
	}
	if en {
		return fmt.Sprintf("[Where this chapter sits]\nChapter %d of %d in the book.\n\n", num, total)
	}
	return fmt.Sprintf("[Dónde está este capítulo]\nCapítulo %d de %d del libro.\n\n", num, total)
}

// buildChapterCriticPrompt assembles the user prompt for the evolved critic.
func buildChapterCriticPrompt(cfg *config.Config, state *Progress, idx int, content, historySummary, lang string) string {
	ch := state.Chapters[idx]
	en := i18n.NormalizeLanguage(lang) == i18n.LangEN
	outlineConstraints := buildArcConstraintsForReview(state, idx, lang)
	memoryContext := buildMemoryForLang(state, idx, lang, content)
	actBlock := buildCriticActBlock(state, ch, lang)
	sceneBlock := buildCriticSceneMapBlock(ch, lang)

	var sb strings.Builder
	if en {
		sb.WriteString("You are a world-famous literary editor whose lists have produced multiple global bestsellers, running the final consistency-and-quality pass on ONE drafted chapter of a novel in progress.\n\n")
		sb.WriteString("First, factual contradictions against the established story:\n")
		sb.WriteString("1. Character names or honorifics inconsistent with prior text.\n")
		sb.WriteString("2. Timeline contradictions (e.g. previous text ended at night, this chapter inexplicably reverts to morning of the same day).\n")
		sb.WriteString("3. Facts that directly contradict established events (a dead character reappearing without explanation, a destroyed object intact again).\n")
		sb.WriteString("4. Character abilities or identity clashing with established setting.\n")
		sb.WriteString("5. Premature introduction of characters, first meetings or events the outline reserves for later chapters.\n")
		sb.WriteString("6. One-time events already played out in prior chapters being re-enacted as new.\n\n")
		sb.WriteString("Then, bestseller-level craft checks limited to THIS chapter: a continuity break between two scenes of the chapter, a scene that contradicts its own beat-sheet assignment, or an internal contradiction (a character knowing something they cannot know yet). Pure style, pacing taste or scene-length preferences are NOT issues — do not report them.\n\n")
		sb.WriteString(actBlock)
		sb.WriteString("[Story-so-far]\n" + historySummary + "\n\n")
		sb.WriteString("[Chapter outline]\n" + ch.Outline + "\n\n")
		sb.WriteString(outlineConstraints)
		sb.WriteString(sceneBlock)
		sb.WriteString(memoryContext)
		sb.WriteString("[Chapter under review]\n" + content + "\n\n")
		sb.WriteString("Rules:\n- New information neither the story-so-far nor the outline mentions is not a contradiction.\n- Only solid objective contradictions warrant FAIL. When in doubt, PASS.\n- For every issue decide its repair SCOPE: \"scene\" when rewriting the single offending scene (or one subscene inside it) fixes it; \"chapter\" only when the problem is structural and spans several scenes (wrong act placement, duplicated arc beats across the chapter, POV drift through the whole text).\n- A scene-scope issue MUST carry scene_id (from the chapter structure above) and a verbatim quote of up to ~25 words copied exactly from the chapter text where the problem lives.\n- Deduplicate by root event: several angles on the same conflict count as ONE issue.\n\nReturn JSON only (no other text):\n{" + `"result": "PASS", "scope": "none", "issues": [], "summary": "one line"` + "}\nor\n{" + `"result": "FAIL", "scope": "scene", "issues": [{"type": "continuity|premature_event|re_enactment|identity|timeline|craft", "scope": "scene", "scene_id": 2, "quote": "verbatim excerpt…", "issue": "what contradicts what", "fix": "concrete repair guidance"}], "summary": "one line"` + "}\nUse scope \"chapter\" (and scene_id 0, empty quote) only for structural problems no single-scene rewrite can fix.")
		return sb.String()
	}
	sb.WriteString("Eres un editor literario de fama mundial cuyos listados han producido múltiples superventas, ejecutando la pasada final de consistencia y calidad sobre UN capítulo redactado de una novela en curso.\n\n")
	sb.WriteString("Primero, contradicciones fácticas contra la historia establecida:\n")
	sb.WriteString("1. Nombres o títulos de personajes inconsistentes con el texto previo.\n")
	sb.WriteString("2. Contradicciones de línea de tiempo (p. ej. el texto anterior terminó de noche y este capítulo vuelve inexplicablemente a la mañana del mismo día).\n")
	sb.WriteString("3. Hechos que contradicen eventos establecidos (un personaje muerto reaparece sin explicación, un objeto destruido vuelve intacto).\n")
	sb.WriteString("4. Habilidades o identidad de personajes chocando con el mundo establecido.\n")
	sb.WriteString("5. Introducción prematura de personajes, primeros encuentros o eventos que el outline reserva para capítulos posteriores.\n")
	sb.WriteString("6. Eventos únicos ya ocurridos en capítulos previos re-representados como nuevos.\n\n")
	sb.WriteString("Luego, chequeos de oficio a nivel superventa limitados a ESTE capítulo: una ruptura de continuidad entre dos escenas del capítulo, una escena que contradice su propia asignación del beat sheet, o una contradicción interna (un personaje sabe algo que aún no puede saber). El estilo puro, el gusto de ritmo o preferencias de largo de escena NO son problemas — no los reportes.\n\n")
	sb.WriteString(actBlock)
	sb.WriteString("[Historia hasta ahora]\n" + historySummary + "\n\n")
	sb.WriteString("[Outline del capítulo]\n" + ch.Outline + "\n\n")
	sb.WriteString(outlineConstraints)
	sb.WriteString(sceneBlock)
	sb.WriteString(memoryContext)
	sb.WriteString("[Capítulo bajo revisión]\n" + content + "\n\n")
	sb.WriteString("Reglas:\n- Información nueva que ni la historia ni el outline mencionan no es una contradicción.\n- Solo contradicciones objetivas sólidas ameritan FAIL. Ante la duda, PASS.\n- Para cada problema decide su ALCANCE de reparación: \"scene\" cuando reescribir la única escena ofensora (o una subescena dentro de ella) lo resuelve; \"chapter\" solo cuando el problema es estructural y abarca varias escenas (ubicación de acto equivocada, latidos de arco duplicados a lo largo del capítulo, deriva de POV en todo el texto).\n- Un problema de alcance escena DEBE llevar scene_id (de la estructura arriba) y una cita textual de hasta ~25 palabras copiada exactamente del capítulo donde vive el problema.\n- Deduplica por evento raíz: varios ángulos del mismo conflicto cuentan como UN problema.\n\nDevuelve solo JSON (sin otro texto):\n{" + `"result": "PASS", "scope": "none", "issues": [], "summary": "una línea"` + "}\no\n{" + `"result": "FAIL", "scope": "scene", "issues": [{"type": "continuity|premature_event|re_enactment|identity|timeline|craft", "scope": "scene", "scene_id": 2, "quote": "extracto textual…", "issue": "qué contradice a qué", "fix": "guía concreta de reparación"}], "summary": "una línea"` + "}\nUsa alcance \"chapter\" (y scene_id 0, cita vacía) solo para problemas estructurales que ninguna reescena de una escena puede arreglar.")
	return sb.String()
}

// parseCriticVerdict parses the critic JSON. ok=false means the response is
// unusable and the caller should fall back to the classic parser.
func parseCriticVerdict(raw string) (v CriticVerdict, ok bool) {
	cleaned := cleanJSONResponse(raw)
	jsonStr := llm.ExtractJSON(cleaned)
	if jsonStr == "" {
		return CriticVerdict{}, false
	}
	var resp CriticVerdict
	if err := json.Unmarshal([]byte(jsonStr), &resp); err != nil {
		return CriticVerdict{}, false
	}
	verdict := strings.ToUpper(strings.TrimSpace(resp.Result))
	if verdict == "" {
		return CriticVerdict{}, false
	}
	resp.Result = verdict
	for i := range resp.Issues {
		resp.Issues[i].Scope = strings.ToLower(strings.TrimSpace(resp.Issues[i].Scope))
		resp.Issues[i].Quote = strings.TrimSpace(resp.Issues[i].Quote)
		resp.Issues[i].Issue = strings.TrimSpace(resp.Issues[i].Issue)
		resp.Issues[i].Fix = strings.TrimSpace(resp.Issues[i].Fix)
	}
	return resp, true
}

// legacyCriticFallback turns a critic verdict without structured issues into
// the classic (failed, issuesText) pair using the keyword scanner.
func legacyCriticFallback(raw string) (bool, string) {
	return parseFactCheckResult(raw)
}

// locateTropeUnitInContent finds the (scenePos, tropeIdx) owning a quoted
// excerpt inside the current chapter content. Matching is tolerant: exact
// substring first, then the ~6-word-window fuzzy match used elsewhere for
// model quotes. Returns (-1,-1,false) when nothing matches.
func locateTropeUnitInContent(ch *ChapterState, content, quote string) (int, int, bool) {
	if ch.ScenePlan == nil || strings.TrimSpace(quote) == "" {
		return -1, -1, false
	}
	q := strings.TrimSpace(quote)
	paras, sep := paragraphUnits(content)
	for pos := range ch.ScenePlan.Scenes {
		lo, hi := sceneSpanFromBlocks(ch, pos, paras)
		if hi <= lo || lo < 0 || hi > len(paras) {
			continue
		}
		sceneText := strings.Join(paras[lo:hi], sep)
		sc := &ch.ScenePlan.Scenes[pos]
		units := sc.TropeUnits
		if len(units) == 0 {
			tropesOfScene(sceneText, sc)
			units = sc.TropeUnits
		}
		for _, u := range units {
			uLo, uHi := u.ParaStart, u.ParaEnd
			if uHi <= uLo || uHi > hi {
				continue
			}
			unitText := strings.Join(paras[uLo:min(uHi, len(paras))], sep)
			if strings.Contains(unitText, q) || containsWordWindow(q, unitText) {
				return pos, u.Index, true
			}
		}
		// Quote inside the scene but not matched to a specific unit → target the
		// opening unit; the rewrite prompt still receives the whole scene.
		if strings.Contains(sceneText, q) || containsWordWindow(q, sceneText) {
			return pos, 0, true
		}
	}
	return -1, -1, false
}

var criticSceneRefRe = regexp.MustCompile(`(?i)\bscene\s*(?:id\s*)?(\d+)`)

// scenePosFromIssue resolves a scene position from the issue's scene_id, or
// from a "scene N" mention in the issue text as fallback.
func scenePosFromIssue(plan *ScenePlan, iss CriticIssue) int {
	if plan == nil {
		return -1
	}
	if iss.SceneID > 0 {
		for pos := range plan.Scenes {
			if plan.Scenes[pos].ID == iss.SceneID {
				return pos
			}
		}
	}
	if m := criticSceneRefRe.FindStringSubmatch(iss.Issue + " " + iss.Fix); m != nil {
		var n int
		fmt.Sscanf(m[1], "%d", &n)
		if n >= 1 && n <= len(plan.Scenes) {
			return n - 1
		}
	}
	return -1
}

// repairChapterWithCritic runs the evolved critic over a freshly generated
// chapter draft and repairs scene-scoped issues surgically (subscene level
// when the quote locates a unit, whole-scene level otherwise). It returns the
// possibly-repaired content plus the leftover issues that still require the
// classic full-chapter retry loop (empty slice = chapter passes).
func repairChapterWithCritic(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, idx int, content string, historySummary string, settings *ProjectSettings, logger *sse.LogBroadcaster) (string, []string) {
	userPrompt := buildChapterCriticPrompt(cfg, state, idx, content, historySummary, cfg.Language)
	systemPrompt := i18n.SystemPromptFor(cfg.Language, "bestseller_chapter_critic_json")
	var raw string
	if logger != nil {
		raw = llm.CallAPIWithRetryLog(ctx, apiCfg, systemPrompt, userPrompt, logger)
	} else {
		raw = llm.CallAPIWithRetry(ctx, apiCfg, systemPrompt, userPrompt)
	}
	if ctx.Err() != nil || strings.TrimSpace(raw) == "" {
		failed, issues := legacyCriticFallback(raw)
		if failed {
			return content, splitFactCheckIssues(issues)
		}
		return content, nil
	}
	verdict, ok := parseCriticVerdict(raw)
	if !ok {
		failed, issues := legacyCriticFallback(raw)
		if failed {
			return content, splitFactCheckIssues(issues)
		}
		return content, nil
	}
	if verdict.Result != "FAIL" || len(verdict.Issues) == 0 {
		if logger != nil {
			logger.InfoKey("log.factcheck_pass")
		}
		return content, nil
	}

	ch := &state.Chapters[idx]
	current := content
	var sceneTargets [][2]int
	var chapterIssues []string
	for _, iss := range verdict.Issues {
		line := strings.TrimSpace(iss.Issue)
		if line == "" {
			continue
		}
		if iss.Fix != "" {
			line += " — " + iss.Fix
		}
		scopeScene := iss.Scope == "scene" && iss.Quote != ""
		if iss.Scope != "scene" && iss.Scope != "chapter" {
			// Unstructured verdict: trust the overall scope field.
			scopeScene = verdict.Scope == "scene" && iss.Quote != ""
		}
		if !scopeScene {
			chapterIssues = append(chapterIssues, line)
			continue
		}
		pos, tropeIdx, located := locateTropeUnitInContent(ch, current, iss.Quote)
		if !located {
			pos = scenePosFromIssue(ch.ScenePlan, iss)
			tropeIdx = 0
			located = pos >= 0
		}
		if !located {
			chapterIssues = append(chapterIssues, line)
			continue
		}
		sceneTargets = append(sceneTargets, [2]int{pos, tropeIdx})
		if logger != nil {
			logger.WarnKey("log.critic_scene_issue", ch.Num, pos+1, tropeIdx+1, Truncate(line, 160))
		}
	}

	// Surgical repairs: dedupe targets, cap the number, rewrite each once.
	seen := map[[2]int]bool{}
	repaired, skipped := 0, 0
	targets := make([][2]int, 0, len(sceneTargets))
	for _, t := range sceneTargets {
		if !seen[t] && len(targets) < maxCriticSceneRepairs {
			seen[t] = true
			targets = append(targets, t)
		}
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i][0] < targets[j][0] })
	for _, t := range targets {
		if ctx.Err() != nil {
			break
		}
		fb := enFB(i18n.NormalizeLanguage(cfg.Language) == i18n.LangEN, "Consistency fix required in this unit: ", "Corrección de consistencia requerida en esta unidad: ")
		if len(verdict.Issues) > 0 {
			// Feed the most relevant issue text to the repair pass.
			for _, iss := range verdict.Issues {
				if iss.Quote == "" {
					continue
				}
				pos, ti, located := locateTropeUnitInContent(ch, current, iss.Quote)
				if located && pos == t[0] && ti == t[1] && strings.TrimSpace(iss.Fix) != "" {
					fb += iss.Fix
					break
				}
			}
		}
		newContent, err := rewriteSceneTrope(ctx, apiCfg, cfg, state, idx, settings, t[0], t[1], fb, logger)
		if err != nil {
			skipped++
			continue
		}
		current = newContent
		ch.Content = current
		repaired++
	}
	if repaired > 0 && logger != nil {
		logger.SuccessKey("log.critic_repairs_done", ch.Num, repaired, skipped)
	}
	if len(chapterIssues) == 0 && repaired > 0 {
		// Re-run the critic once on the repaired draft to confirm the surgical
		// fixes resolved everything; any leftover goes to the classic loop.
		leftover := recheckCriticOnce(ctx, apiCfg, cfg, state, idx, current, historySummary, logger)
		return current, leftover
	}
	return current, chapterIssues
}

// recheckCriticOnce asks the critic for a second opinion after surgical
// repairs. Returns remaining issue lines (empty = pass).
func recheckCriticOnce(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, idx int, content, historySummary string, logger *sse.LogBroadcaster) []string {
	if ctx.Err() != nil {
		return nil
	}
	userPrompt := buildChapterCriticPrompt(cfg, state, idx, content, historySummary, cfg.Language)
	systemPrompt := i18n.SystemPromptFor(cfg.Language, "bestseller_chapter_critic_json")
	var raw string
	if logger != nil {
		raw = llm.CallAPIWithRetryLog(ctx, apiCfg, systemPrompt, userPrompt, logger)
	} else {
		raw = llm.CallAPIWithRetry(ctx, apiCfg, systemPrompt, userPrompt)
	}
	verdict, ok := parseCriticVerdict(raw)
	if !ok {
		failed, issues := legacyCriticFallback(raw)
		if failed {
			return splitFactCheckIssues(issues)
		}
		return nil
	}
	if verdict.Result != "FAIL" {
		return nil
	}
	var out []string
	for _, iss := range verdict.Issues {
		if s := strings.TrimSpace(iss.Issue); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func enFB(en bool, enText, esText string) string {
	if en {
		return enText
	}
	return esText
}
