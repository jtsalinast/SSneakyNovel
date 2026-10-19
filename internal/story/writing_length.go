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
	chapterGenMaxLengthAttempts   = 1 // initial draft + one rewrite, then compress/expand

	chapterLengthSoftOverflowAbsolute = 200 // soft keep: within ±200 units of a hard bound
	chapterLengthSoftOverflowPercent  = 0   // hard percentage band: no extra tolerance
	chapterLengthAdjustMaxOverRatio   = 120 // skip compress when actual > max * 1.2
	// Expand only drafts that are at least ~75% of the minimum (≈67.5% of the
	// configured target). Much shorter drafts come from truncated API output;
	// "expanding" them invents filler instead of regenerating the scene.
	chapterLengthAdjustMinUnderRatio = 75
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
		return fmt.Sprintf("IMPORTANT: The previous draft was only %d words, below the %d–%d word range. Expand with concrete scene, action, and dialogue while staying inside this chapter's outline.", actual, minLen, maxLen)
	}
	return fmt.Sprintf("IMPORTANTE: El borrador anterior tenía solo %d palabras, por debajo del rango de %d–%d. Amplía con escenas concretas, acción y diálogo sin salirte del outline de este capítulo.", actual, minLen, maxLen)
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

func adjustChapterLength(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, content string, minLen, maxLen int, logger *sse.LogBroadcaster) (string, error) {
	actual := prose.CountProseUnits(content)
	lang := cfg.Language
	var userPrompt string
	if actual > maxLen {
		if i18n.NormalizeLanguage(lang) == i18n.LangEN {
			userPrompt = fmt.Sprintf(`You are a novel editor. The chapter below is %d words but must be %d–%d words.
Compress without changing plot beats, character actions, or key dialogue. Cut redundant description and repeated narration; do not remove core events from this chapter's outline.
Output ONLY the full revised chapter prose.

[Chapter text]
%s`, actual, minLen, maxLen, content)
		} else {
			userPrompt = fmt.Sprintf(`Eres un editor de novelas. El capítulo siguiente tiene %d palabras pero debe tener entre %d y %d palabras.
Comprime sin alterar los beats de la trama, las acciones de los personajes ni los diálogos clave. Elimina descripción redundante y narración repetida; no borres eventos centrales del outline de este capítulo.
Devuelve SOLO el texto revisado del capítulo.

[Texto del capítulo]
%s`, actual, minLen, maxLen, content)
		}
	} else {
		if i18n.NormalizeLanguage(lang) == i18n.LangEN {
			userPrompt = fmt.Sprintf(`You are a novel editor. The chapter below is %d words but must be %d–%d words.
Expand with concrete scene, sensory detail, and dialogue without adding new plot beats beyond this chapter's outline.
Output ONLY the full revised chapter prose.

[Chapter text]
%s`, actual, minLen, maxLen, content)
		} else {
			userPrompt = fmt.Sprintf(`Eres un editor de novelas. El capítulo siguiente tiene %d palabras pero debe tener entre %d y %d palabras.
Amplía con escenas concretas, detalle sensorial y diálogo sin añadir nuevos beats de trama más allá del outline de este capítulo.
Devuelve SOLO el texto revisado del capítulo.

[Texto del capítulo]
%s`, actual, minLen, maxLen, content)
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

func recoverChapterLengthBest(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, bestContent string, minLen, maxLen, target int, bestScore int, logger *sse.LogBroadcaster) (string, error) {
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
	adjusted, err := adjustChapterLength(ctx, apiCfg, cfg, bestContent, minLen, maxLen, logger)
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
		content := generateChapterContentStreamWithRetryLog(ctx, apiCfg, cfg, state, idx, settings, constraints, logger)
		if content == "" {
			return "", fmt.Errorf("no hay borrador recuperable para el capítulo")
		}
		actualLen := prose.CountProseUnits(content)
		maybeUpdateBestDraft(&bestContent, &bestScore, content, minLen, maxLen, target)
		if chapterLengthInRange(content, minLen, maxLen) {
			return content, nil
		}
		if attempt < chapterGenMaxLengthAttempts {
			if logger != nil {
				logger.WarnKey("log.chapter_length_retry", actualLen, minLen, maxLen, attempt+1)
			}
			lengthFeedback = formatChapterLengthRetryFeedback(actualLen, minLen, maxLen, lang)
			continue
		}
	}

	return recoverChapterLengthBest(ctx, apiCfg, cfg, bestContent, minLen, maxLen, target, bestScore, logger)
}
