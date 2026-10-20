package story

import (
	"context"
	"fmt"
	"showmethestory/internal/config"
	"showmethestory/internal/i18n"
	"showmethestory/internal/llm"
	"showmethestory/internal/prose"
	"showmethestory/internal/sse"
	"strings"
)

const (
	// A chapter is accepted only between 90% and 150% of the configured
	// target words per chapter (Novel parameters).
	chapterLengthAcceptMinPercent = 90
	chapterLengthAcceptMaxPercent = 150
	chapterGenMaxLengthAttempts   = 2 // initial draft + two rewrites, then compress/expand

	chapterLengthSoftOverflowAbsolute = 200 // soft keep: within ±200 units of a hard bound
	chapterLengthSoftOverflowPercent  = 0   // hard percentage band: no extra tolerance
	chapterLengthAdjustMaxOverRatio   = 120 // skip compress when actual > max * 1.2
	// Expand drafts down to ~40% of the minimum (≈36% of the configured
	// target). The old 75% floor silently accepted short chapters: a 2600-word
	// draft against a 4000-word target (min 3600) sat just under the ratio and
	// was kept as-is without any expand attempt. A partial draft is still much
	// closer to the user's word goal than nothing, and the expand prompt now
	// demands developing the outline's own beats rather than inventing filler.
	chapterLengthAdjustMinUnderRatio = 40

	// Truncated-draft recovery: when the writer call hits the output cap
	// (finish_reason=length), the returned text is by construction only the
	// FIRST scenes of the chapter (e.g. 1200 words of a 4000-word plan). The
	// whole-chapter length gate must NOT be applied to such a fragment — it
	// "never passes" no matter how many times the model rewrites, because the
	// cap cuts every attempt at roughly the same spot. Instead the fragment
	// is continued scene by scene until the prose stops being cut off.
	chapterGenMaxContinuations = 5
	// Minimum usable tail before trying to anchor a continuation on it.
	chapterContinueMinTailWords = 80
)

const proseLengthOutOfRangeBase = 1_000_000

// calcChapterLengthRange returns acceptable chapter prose length bounds in
// WORDS (prose units): [90%, 150%] of the configured target words per chapter.
func calcChapterLengthRange(targetWordsPerChapter int) (minLen, maxLen int) {
	if targetWordsPerChapter < 1 {
		targetWordsPerChapter = 2500
	}
	minLen = targetWordsPerChapter * chapterLengthAcceptMinPercent / 100
	if minLen < 1 {
		minLen = 1
	}
	maxLen = targetWordsPerChapter * chapterLengthAcceptMaxPercent / 100
	return minLen, maxLen
}

// resolveChapterTargetWords returns the word target actually used for a
// chapter and where it came from (project snapshot vs live config), so logs
// never silently fall back to a different value than the user configured.
func resolveChapterTargetWords(state *Progress, cfg *config.Config) (target int, source string) {
	if state != nil && state.StoryConfigSnapshot != nil && state.StoryConfigSnapshot.TargetWordsPerChapter > 0 {
		return state.StoryConfigSnapshot.TargetWordsPerChapter, "snapshot"
	}
	if cfg != nil && cfg.Story.TargetWordsPerChapter > 0 {
		return cfg.Story.TargetWordsPerChapter, "config"
	}
	return 2500, "default"
}

func chapterLengthInRange(content string, minLen, maxLen int) bool {
	n := prose.CountProseUnits(content)
	return n >= minLen && n <= maxLen
}

// proseLengthScore ranks drafts: lower is better. In-range drafts beat any out-of-range;
// in-range ties break by distance to target; out-of-range by distance to nearest bound.
func proseLengthScore(actual, minLen, maxLen, target int) int {
	if actual >= minLen && actual <= maxLen {
		diff := actual - target
		if diff < 0 {
			diff = -diff
		}
		return diff
	}
	if actual > maxLen {
		return proseLengthOutOfRangeBase + (actual - maxLen)
	}
	return proseLengthOutOfRangeBase + (minLen - actual)
}

func softLengthTolerance(boundary int) int {
	tol := chapterLengthSoftOverflowAbsolute
	if pct := boundary * chapterLengthSoftOverflowPercent / 100; pct > tol {
		tol = pct
	}
	return tol
}

func isSoftLengthDeviation(actual, minLen, maxLen int) bool {
	if actual > maxLen {
		return actual-maxLen <= softLengthTolerance(maxLen)
	}
	if actual < minLen {
		return minLen-actual <= softLengthTolerance(minLen)
	}
	return false
}

func shouldAttemptLengthAdjust(actual, minLen, maxLen int) bool {
	if actual >= minLen && actual <= maxLen {
		return false
	}
	if isSoftLengthDeviation(actual, minLen, maxLen) {
		return false
	}
	if actual > maxLen {
		return actual <= maxLen*chapterLengthAdjustMaxOverRatio/100
	}
	return actual >= minLen*chapterLengthAdjustMinUnderRatio/100
}

func maybeUpdateBestDraft(bestContent *string, bestScore *int, content string, minLen, maxLen, target int) {
	score := proseLengthScore(prose.CountProseUnits(content), minLen, maxLen, target)
	if *bestContent == "" || score < *bestScore {
		*bestContent = content
		*bestScore = score
	}
}

func formatChapterLengthRequirementBlock(minLen, maxLen, target int, lang string) string {
	if i18n.NormalizeLanguage(lang) == i18n.LangEN {
		return fmt.Sprintf("Chapter prose must be %d–%d WORDS (target %d; accepted range is 90%%–150%% of the configured words per chapter). Below %d or above %d words is unacceptable — stay inside this chapter's outline only.", minLen, maxLen, target, minLen, maxLen)
	}
	return fmt.Sprintf("La prosa del capítulo debe tener entre %d y %d PALABRAS (objetivo: %d; se acepta del 90%% al 150%% de las palabras por capítulo configuradas). Por debajo de %d o por encima de %d palabras no es aceptable: quédate dentro del outline de este capítulo.", minLen, maxLen, target, minLen, maxLen)
}

func formatChapterLengthRetryFeedback(actual, minLen, maxLen int, lang string) string {
	if actual > maxLen {
		if i18n.NormalizeLanguage(lang) == i18n.LangEN {
			return fmt.Sprintf("IMPORTANT: The previous draft was %d words, exceeding the %d–%d word limit. Regenerate this chapter within the limit. Do not advance into later chapters; compress redundant description and keep only this chapter's outline beats.", actual, minLen, maxLen)
		}
		return fmt.Sprintf("IMPORTANTE: El borrador anterior tenía %d palabras y superaba el límite de %d–%d. Regenera el capítulo dentro del límite; no avances a capítulos posteriores, comprime la descripción redundante y conserva solo los beats del outline de este capítulo.", actual, minLen, maxLen)
	}
	if i18n.NormalizeLanguage(lang) == i18n.LangEN {
		return fmt.Sprintf("IMPORTANT: The previous draft was only %d words, below the %d–%d word range. Regenerate the FULL chapter from scratch at target length: develop EVERY beat of this chapter's outline into a complete scene with concrete action, sensory detail and dialogue — never summarize a beat in one sentence. Do not shorten or skip beats, and do not advance into later chapters' events.", actual, minLen, maxLen)
	}
	return fmt.Sprintf("IMPORTANTE: El borrador anterior tenía solo %d palabras, por debajo del rango de %d–%d. Regenera el capítulo COMPLETO desde cero con la longitud objetivo: desarrolla CADA beat del outline de este capítulo en una escena completa con acción concreta, detalle sensorial y diálogo; nunca resumas un beat en una frase. No acortes ni omitas beats, y no avances a eventos de capítulos posteriores.", actual, minLen, maxLen)
}

func mergeWritingConstraints(a, b string) string {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	return a + "\n\n" + b
}

func finalizeChapterWritingPrompt(template, rendered string, minLen, maxLen, target int, lang string) string {
	if !strings.Contains(template, "{{.TargetWordsMin}}") {
		rendered += "\n\n" + formatChapterLengthRequirementBlock(minLen, maxLen, target, lang)
	}
	return rendered
}

// scenePlanBudgetLines renders the approved beat sheet as a compact checklist
// for length-adjustment prompts: "1. title (~700 words — 600 written)". The
// per-scene actual counts let the expansion pass target the scenes that fell
// short of their budget instead of padding uniformly. Returns "" when there is
// no plan or no usable budgets.
func scenePlanBudgetLines(plan *ScenePlan, content string, lang string) string {
	if plan == nil || len(plan.Scenes) == 0 {
		return ""
	}
	counts := perSceneWordCounts(plan, content)
	en := i18n.NormalizeLanguage(lang) == i18n.LangEN
	var b strings.Builder
	for i, sc := range plan.Scenes {
		title := strings.TrimSpace(sc.Title)
		if title == "" {
			if en {
				title = fmt.Sprintf("Scene %d", i+1)
			} else {
				title = fmt.Sprintf("Escena %d", i+1)
			}
		}
		switch {
		case counts != nil && counts[i] > 0 && en:
			fmt.Fprintf(&b, "%d. %s (~%d words budget — %d written)\n", i+1, title, sc.WordsBudget, counts[i])
		case counts != nil && counts[i] > 0:
			fmt.Fprintf(&b, "%d. %s (presupuesto ~%d palabras — %d escritas)\n", i+1, title, sc.WordsBudget, counts[i])
		case en:
			fmt.Fprintf(&b, "%d. %s (~%d words)\n", i+1, title, sc.WordsBudget)
		default:
			fmt.Fprintf(&b, "%d. %s (~%d palabras)\n", i+1, title, sc.WordsBudget)
		}
	}
	if en {
		return "[Scene plan budgets]\n" + b.String()
	}
	return "[Presupuesto del plan de escenas]\n" + b.String()
}

// perSceneWordCounts heuristically splits the chapter draft across the planned
// scenes by locating each scene's summary/title head fragment in the prose and
// counting words between consecutive anchors. Returns nil when fewer than half
// of the anchors can be located (the split would be unreliable).
func perSceneWordCounts(plan *ScenePlan, content string) []int {
	lower := strings.ToLower(content)
	anchors := make([]int, len(plan.Scenes))
	found := 0
	last := -1
	for i, sc := range plan.Scenes {
		frag := sceneAnchorFragment(sc)
		pos := -1
		if frag != "" {
			pos = strings.Index(lower, frag)
		}
		if pos < 0 && last >= 0 {
			// Anchor missing: keep ordering monotonic by reusing the previous one.
			pos = last
		}
		if pos < 0 {
			anchors[i] = -1
			continue
		}
		if pos <= last {
			pos = last // enforce non-decreasing anchors
		}
		anchors[i] = pos
		last = pos
		found++
	}
	if found*2 < len(plan.Scenes) {
		return nil
	}
	end := len(content)
	counts := make([]int, len(plan.Scenes))
	for i := range anchors {
		if anchors[i] < 0 {
			counts[i] = 0
			continue
		}
		stop := end
		for j := i + 1; j < len(anchors); j++ {
			if anchors[j] > anchors[i] {
				stop = anchors[j]
				break
			}
		}
		counts[i] = prose.CountProseUnits(content[anchors[i]:stop])
	}
	return counts
}

// sceneAnchorFragment picks a short lowercase head fragment (first ~5 words)
// from the scene title or summary to locate it inside the draft prose.
func sceneAnchorFragment(sc Scene) string {
	src := strings.TrimSpace(sc.Title)
	if src == "" {
		src = strings.TrimSpace(sc.Description)
	}
	words := strings.FieldsFunc(strings.ToLower(src), func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == ',' || r == ':' || r == '.' || r == ';'
	})
	if len(words) == 0 {
		return ""
	}
	if len(words) > 5 {
		words = words[:5]
	}
	return strings.Join(words, " ")
}

func adjustChapterLength(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, content string, minLen, maxLen int, logger *sse.LogBroadcaster, scenePlanBlock string) (string, error) {
	actual := prose.CountProseUnits(content)
	lang := cfg.Language
	if scenePlanBlock != "" {
		scenePlanBlock = "\n\n" + scenePlanBlock + "\n"
	}
	var userPrompt string
	if actual > maxLen {
		if i18n.NormalizeLanguage(lang) == i18n.LangEN {
			userPrompt = fmt.Sprintf(`You are a novel editor. The chapter below is %d words but must be %d–%d words.
Compress without changing plot beats, character actions, or key dialogue. Cut redundant description and repeated narration; do not remove core events from this chapter's outline.%s
Output ONLY the full revised chapter prose.

[Chapter text]
%s`, actual, minLen, maxLen, scenePlanBlock, content)
		} else {
			userPrompt = fmt.Sprintf(`Eres un editor de novelas. El capítulo siguiente tiene %d palabras pero debe tener entre %d y %d palabras.
Comprime sin alterar los beats de la trama, las acciones de los personajes ni los diálogos clave. Elimina descripción redundante y narración repetida; no borres eventos centrales del outline de este capítulo.%s
Devuelve SOLO el texto revisado del capítulo.

[Texto del capítulo]
%s`, actual, minLen, maxLen, scenePlanBlock, content)
		}
	} else {
		if i18n.NormalizeLanguage(lang) == i18n.LangEN {
			userPrompt = fmt.Sprintf(`You are a novel editor. The chapter below is %d words but must be %d–%d words.
Expand it to reach the lower bound by DEEPENING the existing scenes only: add concrete action, sensory detail, character interiority and dialogue exchanges at the moments the prose currently rushes or summarizes. Develop every scene already present into its fullest version. Do NOT invent new plot events, new characters, or events from other chapters; do not pad with repeated narration.%s
If a scene budget list is shown above, prioritize the scenes whose written count is furthest below their budget — bring EACH scene up to its budget rather than padding uniformly.
Output ONLY the full revised chapter prose.

[Chapter text]
%s`, actual, minLen, maxLen, scenePlanBlock, content)
		} else {
			userPrompt = fmt.Sprintf(`Eres un editor de novelas. El capítulo siguiente tiene %d palabras pero debe tener entre %d y %d palabras.
Amplíalo hasta alcanzar el límite inferior PROFUNDIZANDO solo las escenas ya existentes: añade acción concreta, detalle sensorial, vida interior de los personajes e intercambios de diálogo en los momentos que la prosa acelera o resume. Desarrolla cada escena presente hasta su versión más completa. NO inventes nuevos eventos de trama, nuevos personajes, ni eventos de otros capítulos; no rellenes con narración repetida.%s
Si arriba aparece una lista de presupuestos por escena, prioriza las escenas cuyo conteo escrito esté más lejos de su presupuesto: lleva CADA escena a su presupuesto en lugar de rellenar de forma uniforme.
Devuelve SOLO el texto revisado del capítulo.

[Texto del capítulo]
%s`, actual, minLen, maxLen, scenePlanBlock, content)
		}
	}

	systemPrompt := i18n.SystemPromptFor(lang, "author_default")
	var raw string
	if logger != nil {
		raw = llm.CallAPIWithRetryLog(ctx, apiCfg, systemPrompt, userPrompt, logger)
	} else {
		raw = llm.CallAPIWithRetry(ctx, apiCfg, systemPrompt, userPrompt)
	}
	if raw == "" {
		return "", fmt.Errorf("el capítulo no produjo texto (API vacía)")
	}
	return stripChapterMetaProse(raw, lang), nil
}

func recoverChapterLengthBest(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, idx int, bestContent string, minLen, maxLen, target int, bestScore int, logger *sse.LogBroadcaster) (string, error) {
	if bestContent == "" {
		return "", fmt.Errorf("no hay borrador recuperable para el capítulo")
	}
	actual := prose.CountProseUnits(bestContent)
	if chapterLengthInRange(bestContent, minLen, maxLen) {
		return bestContent, nil
	}
	if isSoftLengthDeviation(actual, minLen, maxLen) {
		if logger != nil {
			logger.WarnKey("log.chapter_length_soft_keep", actual, minLen, maxLen)
			logger.WarnKey("log.chapter_length_off_range", actual, minLen, maxLen)
		}
		return bestContent, nil
	}
	if !shouldAttemptLengthAdjust(actual, minLen, maxLen) {
		if logger != nil {
			logger.WarnKey("log.chapter_length_skip_adjust", actual, minLen, maxLen)
			logger.WarnKey("log.chapter_length_off_range", actual, minLen, maxLen)
		}
		return bestContent, nil
	}
	if logger != nil {
		logger.WarnKey("log.chapter_length_adjust", actual, minLen, maxLen)
	}
	scenePlanBlock := ""
if state != nil && idx >= 0 && idx < len(state.Chapters) {
scenePlanBlock = scenePlanBudgetLines(state.Chapters[idx].ScenePlan, bestContent, cfg.Language)
}
adjusted, err := adjustChapterLength(ctx, apiCfg, cfg, bestContent, minLen, maxLen, logger, scenePlanBlock)
	if err != nil || adjusted == "" {
		if logger != nil {
			logger.WarnKey("log.chapter_length_adjust_failed", err)
			logger.WarnKey("log.chapter_length_off_range", actual, minLen, maxLen)
		}
		return bestContent, nil
	}
	adjustedLen := prose.CountProseUnits(adjusted)
	if chapterLengthInRange(adjusted, minLen, maxLen) {
		return adjusted, nil
	}
	adjustedScore := proseLengthScore(adjustedLen, minLen, maxLen, target)
	if adjustedScore >= bestScore {
		if logger != nil {
			logger.WarnKey("log.chapter_length_adjust_reverted", adjustedLen, actual, minLen, maxLen)
			logger.WarnKey("log.chapter_length_off_range", actual, minLen, maxLen)
		}
		return bestContent, nil
	}
	if logger != nil {
		logger.WarnKey("log.chapter_length_off_range", adjustedLen, minLen, maxLen)
	}
	return adjusted, nil
}

// enforceRevisionLength guards feedback-driven rewrites. Models habitually
// compress prose when "applying feedback" (a 4000-word chapter came back as
// 1868 words and was accepted silently). If the revision drops below the hard
// minimum band, retry once with an explicit keep-everything instruction; if it
// still undershoots, revert to whichever draft is longest so the user's word
// target is never quietly lost. Returns the content plus whether the final
// text is in the acceptance band.
func enforceRevisionLength(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, original, revised, feedback string, minLen, maxLen int, logger *sse.LogBroadcaster, rerun func(string) (string, error)) (string, bool, error) {
	originalLen := prose.CountProseUnits(original)
	revisedLen := prose.CountProseUnits(revised)
	if revisedLen >= minLen && revisedLen <= maxLen {
		return revised, true, nil
	}
	// A revision that is shorter than the hard minimum AND shorter than the
	// original by more than the soft tolerance destroyed content; everything
	// else (longer-but-still-short drafts, minor trims of already-short
	// chapters) is left alone rather than ping-ponging the model.
	destruction := revisedLen < minLen && originalLen >= minLen && !isSoftLengthDeviation(revisedLen, minLen, maxLen)
	if !destruction {
		return revised, false, nil
	}
	if logger != nil {
		logger.WarnKey("log.chapter_revision_short_retry", revisedLen, minLen, maxLen)
	}
	keepFeedback := feedback + "\n\n[LENGTH GUARD] Your previous rewrite produced only " + fmt.Sprintf("%d", revisedLen) + " words; the chapter must stay within " + fmt.Sprintf("%d-%d", minLen, maxLen) + " words. Rewrite again applying ONLY the requested changes while keeping every other scene, paragraph, dialogue and description of the original text intact. Do not summarize or compress anything else."
	again, err := rerun(keepFeedback)
	if err != nil {
		return "", false, err
	}
	againLen := prose.CountProseUnits(again)
	if againLen > revisedLen {
		revised, revisedLen = again, againLen
	}
	if revisedLen >= minLen && revisedLen <= maxLen {
		return revised, true, nil
	}
	if revisedLen < originalLen && originalLen >= minLen {
		if logger != nil {
			logger.WarnKey("log.chapter_revision_reverted", revisedLen, originalLen, minLen, maxLen)
		}
		return original, false, nil
	}
	if logger != nil {
		logger.WarnKey("log.chapter_length_off_range", revisedLen, minLen, maxLen)
	}
	return revised, false, nil
}

func generateChapterContentWithLengthControl(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, idx int, settings *ProjectSettings, extraWritingConstraints string, logger *sse.LogBroadcaster) (string, error) {
	target, source := resolveChapterTargetWords(state, cfg)
	minLen, maxLen := calcChapterLengthRange(target)
	lang := cfg.Language
	if logger != nil {
		logger.InfoKey("log.chapter_length_target", state.Chapters[idx].Num, target, minLen, maxLen, source)
	}
	lengthFeedback := ""
	var bestContent string
	bestScore := int(^uint(0) >> 1)

	for attempt := 0; attempt <= chapterGenMaxLengthAttempts; attempt++ {
		if ctx.Err() != nil {
			return "", fmt.Errorf("generación cancelada")
		}
		constraints := mergeWritingConstraints(extraWritingConstraints, lengthFeedback)
		content, truncated := generateChapterContentStreamWithRetryLog(ctx, apiCfg, cfg, state, idx, settings, constraints, logger)
		if content == "" {
			return "", fmt.Errorf("no hay borrador recuperable para el capítulo")
		}
		// Output-cap truncation means the draft is only the FIRST scenes of the
		// chapter. The whole-chapter length gate can never pass such a fragment
		// (every regeneration gets cut at roughly the same spot), so continue
		// the chapter scene by scene instead of rewriting it from scratch.
		if truncated {
			if logger != nil {
				logger.WarnKey("log.chapter_length_retry_truncated", prose.CountProseUnits(content), minLen, maxLen, attempt+1)
			}
			if cont, contOK := continueTruncatedChapter(ctx, apiCfg, cfg, state, idx, settings, constraints, content, logger); contOK {
				maybeUpdateBestDraft(&bestContent, &bestScore, cont, minLen, maxLen, target)
				if chapterLengthInRange(cont, minLen, maxLen) || !isLikelyTruncatedFragment(cont, minLen, maxLen, target) {
					return cont, nil
				}
				content = cont
			}
		}
		actualLen := prose.CountProseUnits(content)
		maybeUpdateBestDraft(&bestContent, &bestScore, content, minLen, maxLen, target)
		if chapterLengthInRange(content, minLen, maxLen) {
			return content, nil
		}
		if attempt < chapterGenMaxLengthAttempts {
			if logger != nil && !truncated {
				logger.WarnKey("log.chapter_length_retry", actualLen, minLen, maxLen, attempt+1)
			}
			lengthFeedback = formatChapterLengthRetryFeedback(actualLen, minLen, maxLen, lang)
			continue
		}
	}

	return recoverChapterLengthBest(ctx, apiCfg, cfg, state, idx, bestContent, minLen, maxLen, target, bestScore, logger)
}

// isLikelyTruncatedFragment reports whether a draft still looks like a partial
// chapter produced by an output-cap cut: far below the minimum bound and with
// no scene plan to justify its size. Used after continuation passes to decide
// whether another continuation round is worthwhile.
func isLikelyTruncatedFragment(content string, minLen, maxLen, target int) bool {
	n := prose.CountProseUnits(content)
	return n < minLen*chapterLengthAdjustMinUnderRatio/100 && n < target
}

// remainingScenesFromPlan uses the approved beat sheet to find which scenes
// still have to be written: for each planned scene it locates the scene's
// anchor fragment inside the partial draft; scenes whose anchor is absent were
// never reached before the output cap cut the generation. Returns all scenes
// (except the first) when anchoring is unreliable — continuing one scene late
// merely repeats material the editor passes can compress, while missing whole
// scenes silently shortens every chapter.
func remainingScenesFromPlan(plan *ScenePlan, content string) []Scene {
	if plan == nil || len(plan.Scenes) < 2 {
		return nil
	}
	lower := strings.ToLower(content)
	var remaining []Scene
	missing := 0
	for i, sc := range plan.Scenes {
		frag := sceneAnchorFragment(sc)
		found := frag != "" && strings.Contains(lower, frag)
		if found || i == 0 {
			continue
		}
		missing++
		remaining = append(remaining, sc)
	}
	if missing == 0 {
		return nil // draft already covers every planned scene
	}
	if missing*3 > len(plan.Scenes)*2 {
		// Anchors mostly missing → unreliable split; assume everything after
		// the opening scene still needs writing.
		return plan.Scenes[1:]
	}
	return remaining
}

// formatSceneContinuationBlock builds the [Continuation] instruction appended
// to the full chapter-writing prompt so the model keeps the existing prose and
// writes ONLY the listed remaining scenes toward their word budgets.
func formatSceneContinuationBlock(doneWords, remainingBudget int, remaining []Scene, lang string) string {
	en := i18n.NormalizeLanguage(lang) == i18n.LangEN
	var sb strings.Builder
	if en {
		fmt.Fprintf(&sb, "\n\n[CONTINUATION — do NOT rewrite or repeat what is already written]\nThe chapter above was cut off by the output limit after ~%d words. It is INCOMPLETE: continue it RIGHT WHERE IT STOPS, in the same voice, tense and point of view, writing ONLY the remaining scenes below as full dramatized scenes (concrete action, sensory detail, dialogue) until each reaches its word budget.\nDo NOT restate earlier events, do NOT summarize the missing scenes, do NOT add events outside this chapter's outline.\n", doneWords)
	} else {
		fmt.Fprintf(&sb, "\n\n[CONTINUACIÓN — NO reescribas ni repitas lo que ya está escrito]\nEl capítulo anterior fue cortado por el límite de salida tras ~%d palabras. Está INCOMPLETO: continúalo EXACTAMENTE donde se corta, con la misma voz, tiempo verbal y punto de vista, escribiendo SOLO las escenas restantes listadas abajo como escenas completas y dramatizadas (acción concreta, detalle sensorial, diálogo) hasta que cada una alcance su presupuesto de palabras.\nNo resumas eventos previos, no resumas las escenas faltantes, no agregues eventos fuera del outline de este capítulo.\n", doneWords)
	}
	for _, sc := range remaining {
		title := strings.TrimSpace(sc.Title)
		if title == "" {
			title = fmt.Sprintf("Scene %d", sc.ID)
		}
		if en {
			fmt.Fprintf(&sb, "- Scene %d (%d words): %s — %s\n", sc.ID, sc.WordsBudget, title, sc.Description)
		} else {
			fmt.Fprintf(&sb, "- Escena %d (%d palabras): %s — %s\n", sc.ID, sc.WordsBudget, title, sc.Description)
		}
	}
	if en {
		fmt.Fprintf(&sb, "The remaining scenes need approximately %d words in total. Output ONLY the new continuation prose (start mid-flow, no recap, no title, no meta comments).", remainingBudget)
	} else {
		fmt.Fprintf(&sb, "Las escenas restantes necesitan aproximadamente %d palabras en total. Devuelve SOLO la prosa nueva de la continuación (empieza en medio de la acción, sin recap, sin título, sin comentarios meta).", remainingBudget)
	}
	return sb.String()
}

// contentOverlapRatio estimates how much of "segment" is already present in
// "existing": normalized unique lines of the segment that appear (as
// substrings) in the existing draft. Used to reject continuation rounds where
// the model restates earlier scenes instead of writing new ones.
func contentOverlapRatio(existing, segment string) float64 {
	normalize := func(s string) string {
		s = strings.ToLower(strings.TrimSpace(s))
		var b strings.Builder
		prevSpace := true
		for _, r := range s {
			switch r {
			case ' ', '\t', '\n', '\r':
				if !prevSpace {
					b.WriteByte(' ')
					prevSpace = true
				}
			default:
				b.WriteRune(r)
				prevSpace = false
			}
		}
		return strings.TrimSpace(b.String())
	}
	lowExisting := normalize(existing)
	seen := map[string]bool{}
	total, dup := 0, 0
	for _, line := range strings.Split(segment, "\n") {
		norm := normalize(line)
		if len([]rune(norm)) < 30 {
			continue // short lines (scene breaks, dialogue tags) are not evidence
		}
		if seen[norm] {
			continue
		}
		seen[norm] = true
		total++
		if strings.Contains(lowExisting, norm) {
			dup++
		}
	}
	if total == 0 {
		return 0
	}
	return float64(dup) / float64(total)
}

// continueTruncatedChapter extends a partial draft (cut by finish_reason=length)
// scene by scene until the prose is no longer being cut off. Each round calls
// the writer with the FULL original chapter context plus a [Continuation]
// block listing the scenes still missing per the approved beat sheet, and
// appends the returned segment. Returns the joined text and whether the final
func continueTruncatedChapter(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, idx int, settings *ProjectSettings, extraWritingConstraints string, partial string, logger *sse.LogBroadcaster) (string, bool) {
	content := partial
	plan := state.Chapters[idx].ScenePlan
	target, _ := resolveChapterTargetWords(state, cfg)
	constraints := extraWritingConstraints
	userPrompt, systemPrompt := buildChapterWritingPrompt(cfg, state, idx, settings, &constraints)

	for round := 0; round < chapterGenMaxContinuations; round++ {
		if ctx.Err() != nil {
			break
		}
		remaining := remainingScenesFromPlan(plan, content)
		remainingBudget := 0
		for _, sc := range remaining {
			remainingBudget += sc.WordsBudget
		}
		if remainingBudget <= 0 {
			if plan != nil && len(plan.Scenes) >= 2 && remainingScenesFromPlan(plan, content) == nil {
				break // every planned scene is already drafted — nothing to continue
			}
			remainingBudget = target - prose.CountProseUnits(content)
		}
		if remainingBudget < 150 {
			break // nothing meaningful left to write
		}
		prompt := userPrompt + formatSceneContinuationBlock(prose.CountProseUnits(content), remainingBudget, remaining, cfg.Language)
		logger.StreamStart(idx)
		result, err := llm.CallAPIStreamMessages(ctx, apiCfg, []llm.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: prompt},
		}, func(chunk string) { logger.ContentChunk(idx, chunk) })
		truncatedAgain := err == nil && result.FinishReason == "length"
		raw := result.Content
		if raw == "" && truncatedAgain {
			// Defensive: an empty capped completion cannot extend the draft.
			break
		}
		if err != nil && !(llm.IsOutputTruncation(err) && strings.TrimSpace(raw) != "") {
			if logger != nil {
				logger.WarnKey("log.chapter_length_continue_failed", err, round+1)
			}
			break
		}
		segment := stripChapterMetaProse(raw, cfg.Language)
		if prose.CountProseUnits(segment) < chapterContinueMinTailWords {
			break // degenerate/empty continuation: stop rather than loop
		}
		// Guard against the model restating already-written scenes: if the new
		// segment is mostly a copy of the existing draft (≥60% of its unique
		// lines already present), appending it would duplicate whole scenes
		// and inflate the word count without adding story. Stop instead.
		if contentOverlapRatio(content, segment) >= 0.6 {
			if logger != nil {
				logger.WarnKey("log.chapter_length_continue_duplicate", round+1)
			}
			break
		}
		content = strings.TrimRight(content, "\n") + "\n\n" + segment
		if logger != nil {
			logger.InfoKey("log.chapter_length_continue", prose.CountProseUnits(content), round+1)
		}
		if err == nil && !truncatedAgain {
			return content, true // clean finish: the rest of the chapter got written
		}
	}
	return content, false
}
