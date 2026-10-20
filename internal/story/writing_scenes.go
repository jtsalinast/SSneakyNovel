package story

import (
	"context"
	"fmt"
	"strings"

	"showmethestory/internal/config"
	"showmethestory/internal/i18n"
	"showmethestory/internal/llm"
	"showmethestory/internal/prose"
	"showmethestory/internal/sse"
)

// Scene-by-scene chapter writing.
//
// When a chapter carries an approved beat sheet (ScenePlan), the writer no
// longer improvises the whole chapter in one call: each scene gets its own
// streaming LLM call, its output is measured against the scene's word budget
// (±10% band), and only after that verification (plus up to three controlled
// retries when the band is missed) does the loop move on to the next scene.
// Every step — target band, generation, count check, retry, per-scene verdict
// and the final whole-chapter 90%–180% verification — is emitted to the chat
// log so the user can follow the process live.
//
// Chapters without a usable plan fall back to the legacy one-shot pipeline in
// writing_length.go (generateChapterContentWithLengthControl).

// sceneWritingContextBlock renders the shared background blocks (synopsis,
// story-so-far recap, previous ending, characters, worldview, foreshadows,
// memory, voice samples, arc constraints, novel parameters) from the same
// template used by the one-shot path, minus the scene-plan block. Reusing
// the user template keeps custom prompt edits effective in the sequential
// path too; any leftover {{.Scene*}} tokens are dropped afterwards.
func sceneWritingContextBlock(cfg *config.Config, state *Progress, idx int, settings *ProjectSettings) string {
	ch := state.Chapters[idx]
	lang := cfg.Language
	targetWords, _ := resolveChapterTargetWords(state, cfg)
	minLen, maxLen := calcChapterLengthRange(targetWords)
	characterContext, worldviewContext := buildChapterSettingsContexts(settings, ch, lang)

	rendered := config.RenderPrompt(withTokenFallback(cfg.Prompts.ChapterWriting, "{{.ScenePlan}}"), map[string]string{
		"Title":              preferUserValue(cfg.Story.Title, state.Title),
		"ChapterNum":         fmt.Sprintf("%d", ch.Num),
		"CorePrompt":         state.CorePrompt,
		"StorySynopsis":      ChapterSynopsis(cfg, state, ch.Num),
		"HistorySummary":     buildHistorySummaryForLang(state, idx, lang),
		"PreviousEnding":     buildPreviousChapterTailForLang(state, idx, lang),
		"ChapterTitle":       ch.Title,
		"ChapterOutline":     ch.Outline,
		"WritingStyle":       cfg.Story.WritingStyle,
		"WritingPOV":         cfg.Story.WritingPOV,
		"CharacterContext":   characterContext,
		"WorldviewContext":   worldviewContext,
		"TargetWords":        fmt.Sprintf("%d", targetWords),
		"TargetWordsMin":     fmt.Sprintf("%d", minLen),
		"TargetWordsMax":     fmt.Sprintf("%d", maxLen),
		"Foreshadows":        formatActiveForeshadowsForChapterLang(state.Foreshadows, ch.Num, lang),
		"Memory":             buildMemoryForLang(state, idx, lang),
		"VoiceSamples":       buildVoiceSamplesForLang(state, idx, settings, lang),
		"OutlineConstraints": buildOutlineConstraintsForLang(state, idx, lang),
		"ScenePlan":          renderScenePlanBlock(ch.ScenePlan, lang),
	})
	// Drop every placeholder the default template knows about that we did NOT
	// fill above (custom templates may add their own unknown tokens, which are
	// left untouched), then tidy blank-line runs created by empty values.
	for _, tok := range chapterTemplateTokens {
		rendered = strings.ReplaceAll(rendered, "{{."+tok+"}}", "")
	}
	for strings.Contains(rendered, "\n\n\n") {
		rendered = strings.ReplaceAll(rendered, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(rendered)
}

// chapterTemplateTokens lists every data key buildChapterWritingPrompt feeds
// to the ChapterWriting template; sceneWritingContextBlock strips leftovers.
var chapterTemplateTokens = []string{
	"Title", "ChapterNum", "CorePrompt", "StorySynopsis", "HistorySummary",
	"PreviousEnding", "ChapterTitle", "ChapterOutline", "WritingStyle",
	"WritingPOV", "CharacterContext", "WorldviewContext", "TargetWords",
	"TargetWordsMin", "TargetWordsMax", "Foreshadows", "Memory",
	"VoiceSamples", "OutlineConstraints", "ScenePlan",
}

// scenePlanChecklistBlock renders the full beat sheet as context for every
// scene call so the model knows where this scene sits inside the chapter.
func scenePlanChecklistBlock(plan *ScenePlan, lang string, chapterTargetTotal int) string {
	en := i18n.NormalizeLanguage(lang) == i18n.LangEN
	var sb strings.Builder
	if en {
		sb.WriteString("[Full chapter beat sheet — write ONLY the assigned scene below]\n")
	} else {
		fmt.Fprintf(&sb, "[Beat sheet completo del capítulo — escribe SOLO la escena asignada abajo]\n")
	}
	cum := 0
	denominator := plan.TargetWords
	if denominator <= 0 {
		denominator = chapterTargetTotal
	}
	for _, s := range plan.Scenes {
		cum += s.WordsBudget
		pct := 0
		if denominator > 0 {
			pct = cum * 100 / denominator
		}
		if en {
			fmt.Fprintf(&sb, "%d. [%d words · ends ≈%d%%] %s: %s\n", s.ID, s.WordsBudget, pct, s.Title, s.Description)
		} else {
			fmt.Fprintf(&sb, "%d. [%d palabras · termina ≈%d%%] %s: %s\n", s.ID, s.WordsBudget, pct, s.Title, s.Description)
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

// buildSceneWritingPrompt assembles the user prompt for ONE scene call:
// shared chapter context + beat sheet + already-written scenes (verbatim tail
// for continuity) + the assigned scene with its hard ±band word budget.
func buildSceneWritingPrompt(cfg *config.Config, state *Progress, idx int, settings *ProjectSettings, plan *ScenePlan, pos int, written string, extraConstraints, retryFeedback string, chapterTargetTotal int) (userPrompt, systemPrompt string) {
	lang := cfg.Language
	sc := plan.Scenes[pos]
	en := i18n.NormalizeLanguage(lang) == i18n.LangEN
	bandMin, bandMax := calcSceneLengthRange(sc.WordsBudget)

	title := strings.TrimSpace(sc.Title)
	if title == "" {
		if en {
			title = fmt.Sprintf("Scene %d", sc.ID)
		} else {
			title = fmt.Sprintf("Escena %d", sc.ID)
		}
	}

	var sb strings.Builder
	if ctxBlock := sceneWritingContextBlock(cfg, state, idx, settings); ctxBlock != "" {
		sb.WriteString(ctxBlock)
		sb.WriteString("\n\n")
	}
	sb.WriteString(scenePlanChecklistBlock(plan, lang, chapterTargetTotal))
	sb.WriteString("\n\n")

	writtenWords := prose.CountProseUnits(written)
	targetPct := 0
	if sc.WordsBudget > 0 && chapterTargetTotal > 0 {
		targetPct = sc.WordsBudget * 100 / chapterTargetTotal
	}
	lastScene := pos == len(plan.Scenes)-1
	firstScene := writtenWords == 0
	if en {
		// Natural-language request up front, in the style that word-count
		// benchmarks follow ("Write a ~N-word scene based on this outline…").
		fmt.Fprintf(&sb, "Write scene %d of %d of this chapter: an approximately %d-word fully dramatized scene", pos+1, len(plan.Scenes), sc.WordsBudget)
		if targetPct > 0 {
			fmt.Fprintf(&sb, " (about %d%% of the chapter)", targetPct)
		}
		sb.WriteString(", in prose.\n\n")
		fmt.Fprintf(&sb, "Outline for this scene — %s:\n%s\n\n", title, strings.TrimSpace(sc.Description))
		if sc.PoV != "" {
			fmt.Fprintf(&sb, "Point of view: %s\n\n", sc.PoV)
		}
		if firstScene && strings.TrimSpace(buildPreviousChapterTailForLang(state, idx, lang)) != "" {
			sb.WriteString("Open the chapter flowing naturally from the end of the previous chapter shown above.\n\n")
		}
		if lastScene {
			sb.WriteString("This is the last scene: close the chapter on the hook the outline implies.\n\n")
		}
		if writtenWords > 0 {
			sb.WriteString("Here are the scenes already written in this chapter. Continue seamlessly from the very end of this text — match its voice and tense, and never repeat or rewrite it:\n\n")
			sb.WriteString(strings.TrimSpace(written))
			sb.WriteString("\n\n")
			sb.WriteString("Now write only the new scene, picking up right where that text leaves off.\n\n")
		}
		sb.WriteString("A few ground rules:\n")
		sb.WriteString("- Play the scene out moment to moment — concrete action, sensory detail, dialogue, the character's inner life. Don't summarize it and don't skip ahead past its beats.\n")
		fmt.Fprintf(&sb, "- Keep the length within roughly %d–%d words: under %d it reads like a summary, over %d it drags. Before you finish, check how long your draft is and adjust it toward %d words.\n", bandMin, bandMax, bandMin, bandMax, sc.WordsBudget)
		sb.WriteString("- Write only this scene, nothing else — no other scene from the plan above.")
		if lastScene {
			sb.WriteString(" End it with a clean cliffhanger or emotional turn that closes the chapter.")
		}
		sb.WriteString("\nReturn only the finished scene prose itself — no title, numbering, recap, divider or note about what you wrote.")
	} else {
		fmt.Fprintf(&sb, "Escribe la escena %d de %d de este capítulo: una escena completamente dramatizada de aproximadamente %d palabras", pos+1, len(plan.Scenes), sc.WordsBudget)
		if targetPct > 0 {
			fmt.Fprintf(&sb, " (cerca del %d%% del capítulo)", targetPct)
		}
		sb.WriteString(", en prosa.\n\n")
		fmt.Fprintf(&sb, "Argumento de esta escena — %s:\n%s\n\n", title, strings.TrimSpace(sc.Description))
		if sc.PoV != "" {
			fmt.Fprintf(&sb, "Punto de vista: %s\n\n", sc.PoV)
		}
		if firstScene && strings.TrimSpace(buildPreviousChapterTailForLang(state, idx, lang)) != "" {
			sb.WriteString("Abre el capítulo fluyendo con naturalidad desde el final del capítulo anterior mostrado arriba.\n\n")
		}
		if lastScene {
			sb.WriteString("Esta es la última escena: cierra el capítulo con el gancho que sugiere el argumento.\n\n")
		}
		if writtenWords > 0 {
			sb.WriteString("Estas son las escenas ya escritas de este capítulo. Continúa sin costuras justo desde el final de este texto — iguala su voz y su tiempo verbal, y nunca lo repitas ni lo reescribas:\n\n")
			sb.WriteString(strings.TrimSpace(written))
			sb.WriteString("\n\n")
			sb.WriteString("Ahora escribe solo la escena nueva, retomando exactamente donde se queda ese texto.\n\n")
		}
		sb.WriteString("Algunas reglas básicas:\n")
		sb.WriteString("- Desarrolla la escena momento a momento: acción concreta, detalle sensorial, diálogo y vida interior del personaje. No la resumes ni saltes más allá de sus beats.\n")
		fmt.Fprintf(&sb, "- Mantén la extensión entre aproximadamente %d y %d palabras: por debajo de %d se lee como resumen; por encima de %d se arrastra. Antes de terminar, revisa cuánto mediste y ajústalo hacia las %d palabras.\n", bandMin, bandMax, bandMin, bandMax, sc.WordsBudget)
		sb.WriteString("- Escribe solo esta escena, nada más — ninguna otra escena del plan de arriba.")
		if lastScene {
			sb.WriteString(" Ciérrala con un gancho o giro emocional que remate el capítulo.")
		}
		sb.WriteString("\nDevuelve únicamente la prosa final de la escena — sin título, numeración, resumen, separador ni comentario alguno sobre lo escrito.")
	}

	userPrompt = sb.String()
	if np := NovelParametersBlock(cfg); np != "" {
		userPrompt += "\n\n" + np
	}
	extra := strings.TrimSpace(extraConstraints)
	if block := formatExtraWritingConstraintsBlock(extra, lang); block != "" {
		userPrompt += "\n\n" + block
	}
	if retryFeedback != "" {
		userPrompt += "\n\n" + retryFeedback
	}

	systemPrompt = state.CorePrompt
	if systemPrompt == "" {
		systemPrompt = i18n.SystemPromptFor(lang, "author_default")
	}
	return userPrompt, systemPrompt
}

// streamSceneCall streams one scene generation to the UI and returns the
// cleaned prose plus whether the output-cap truncated it.
func streamSceneCall(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, idx int, userPrompt, systemPrompt string, logger *sse.LogBroadcaster) (string, bool) {
	if logger != nil {
		logger.StreamStart(idx)
	}
	emit := func(chunk string) {
		if logger != nil {
			logger.ContentChunk(idx, chunk)
		}
	}
	result, err := llm.CallAPIStreamMessages(ctx, apiCfg, []llm.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}, emit)
	truncated := (err == nil && result.FinishReason == "length") || llm.IsOutputTruncation(err)
	text := stripChapterMetaProse(result.Content, cfg.Language)
	if strings.TrimSpace(text) == "" {
		return "", truncated
	}
	return text, truncated
}

// generateChapterScenesSequentially writes the chapter scene by scene,
// verifying each scene against its ±10% word-budget band (up to three
// controlled retries per scene) before continuing, then verifies the joined
// chapter against the whole-chapter 90%–180% band. It reports ok=false when
// the chapter has no usable plan or the pass could not produce any text, so
// the caller falls back to the legacy one-shot pipeline.
func generateChapterScenesSequentially(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, idx int, settings *ProjectSettings, extraConstraints string, target, minLen, maxLen int, logger *sse.LogBroadcaster) (content string, ok bool) {
	ch := &state.Chapters[idx]
	plan := ch.ScenePlan
	lang := cfg.Language
	en := i18n.NormalizeLanguage(lang) == i18n.LangEN
	if plan == nil || len(plan.Scenes) < 2 {
		return "", false // one-shot fallback handles single-scene/unplanned chapters
	}
	usable := 0
	for _, sc := range plan.Scenes {
		if sc.WordsBudget >= 1 {
			usable++
		}
	}
	if usable == 0 {
		return "", false
	}

	if logger != nil {
		logger.InfoKey("log.scene_seq_start", ch.Num, len(plan.Scenes), target)
	}

	var parts []string
	written := ""
	// paraAcc tracks how many content paragraphs every accepted scene so far
	// occupies, so each Scene can record its EXACT absolute paragraph span
	// (ParaStart/ParaEnd) instead of relying on proportional budget guesses.
	// syncSceneBlocks prefers these spans; when they are fresh the viewer
	// groups story-blocks per trope inside each scene and renders the scene
	// chip between scenes.
	paraAcc := 0
	for pos, sc := range plan.Scenes {
		if ctx.Err() != nil {
			break
		}
		bandMin, bandMax := calcSceneLengthRange(sc.WordsBudget)
		title := strings.TrimSpace(sc.Title)
		if title == "" {
			if en {
				title = fmt.Sprintf("Scene %d", sc.ID)
			} else {
				title = fmt.Sprintf("Escena %d", sc.ID)
			}
		}
		if bandMin <= 0 {
			// Degenerate budget: nothing to enforce — write it with the plan's
			// share of the target so the scene still gets a real instruction.
			bandMin, bandMax = calcSceneLengthRange(maxOf(1, target/len(plan.Scenes)))
		}
		if logger != nil {
			logger.InfoKey("log.scene_gen_start", pos+1, len(plan.Scenes), title, sc.WordsBudget, bandMin, bandMax)
		}

		retryFeedback := ""
		var best string
		bestDist := -1 // word-distance-to-budget of the closest attempt so far
		wrote := false
		// attemptsLeft is the shared pool of controlled retries for THIS scene:
		// one initial generation plus up to sceneBudgetMaxRetries (3) retries,
		// whether a retry was triggered by an out-of-band draft or by an empty
		// call (API failure). The loop only moves to the next scene once the
		// band is hit, the pool is exhausted, or the scene cannot be written.
		attemptsLeft := sceneBudgetMaxRetries
		for try := 0; ; try++ {
			userPrompt, systemPrompt := buildSceneWritingPrompt(cfg, state, idx, settings, plan, pos, written, extraConstraints, retryFeedback, target)
			text, truncated := streamSceneCall(ctx, apiCfg, cfg, idx, userPrompt, systemPrompt, logger)
			if text == "" {
				if logger != nil {
					logger.WarnKey("log.scene_gen_failed", pos+1, title, try+1)
				}
				if attemptsLeft <= 0 {
					break
				}
				attemptsLeft--
				if logger != nil {
					logger.WarnKey("log.scene_call_retry", pos+1, len(plan.Scenes), title, try+1, attemptsLeft)
				}
				continue
			}
			n := prose.CountProseUnits(text)
			dist := n - sc.WordsBudget
			if dist < 0 {
				dist = -dist
			}
			// Track the closest attempt to the budget up front, so this draft is
			// a fallback candidate even when the loop later exits through an
			// unexpected path (e.g. context cancellation mid-retry).
			if bestDist < 0 || dist < bestDist {
				bestDist, best = dist, text
			}
			if truncated {
				if logger != nil {
					logger.WarnKey("log.scene_truncated", pos+1, title, n)
				}
			}
			if n >= bandMin && n <= bandMax {
				if logger != nil {
					logger.InfoKey("log.scene_count_ok", pos+1, title, n, sc.WordsBudget, bandMin, bandMax)
				}
				wrote = true
				break
			}
			if attemptsLeft > 0 {
				attemptsLeft--
				var need string
				if n < bandMin {
					need = fmt.Sprintf("write MORE (at least %d more words)", maxOf(1, bandMin-n))
				} else {
					need = fmt.Sprintf("cut at least %d words", n-bandMax)
				}
				if logger != nil {
					logger.WarnKey("log.scene_count_retry", pos+1, len(plan.Scenes), title, n, sc.WordsBudget, bandMin, bandMax, need, sceneBudgetMaxRetries-attemptsLeft, sceneBudgetMaxRetries)
				}
				retryFeedback = formatSceneLengthRetryFeedback(title, n, bandMin, bandMax, try+1, sceneBudgetMaxRetries, lang)
				continue
			}
			if logger != nil {
				logger.WarnKey("log.scene_count_keep_best", pos+1, title, prose.CountProseUnits(best), sc.WordsBudget, bandMin, bandMax, sceneBudgetMaxRetries)
			}
			wrote = true
			break
		}
		if !wrote {
			if best != "" {
				wrote = true // keep closest attempt even if the loop exited oddly
			} else {
				continue // skip this scene entirely; later verification judges the total
			}
		}
		if best == "" {
			continue
		}
		best = stripChapterMetaProse(best, lang)
		if strings.TrimSpace(best) == "" {
			continue
		}
		parts = append(parts, best)
		written = strings.Join(parts, "\n\n")
		// Record this scene's EXACT absolute paragraph span over the joined
		// content (paraAcc accumulates accepted-scene paragraph counts). This is
		// what lets syncSceneBlocks group story-blocks per trope inside each
		// scene without guessing from word budgets. Meta lines were stripped
		// above so the span matches what actually lands in the chapter.
		bestParas, _ := splitBlockParagraphs(best)
		plan.Scenes[pos].ParaStart = paraAcc
		plan.Scenes[pos].ParaEnd = paraAcc + len(bestParas)
		paraAcc += len(bestParas)
		// Segment the accepted scene into trope units and record their
		// paragraph ranges + block spans on the plan. This is what lets a
		// later critique (fact-check or bestseller chapter critic) repair one
		// trope unit instead of the whole scene or chapter.
		if sc.TropeUnits == nil || len(sc.TropeUnits) == 0 {
			units := tropesOfScene(best, &plan.Scenes[pos])
			if logger != nil && len(units) > 1 {
				labels := make([]string, len(units))
				for i, u := range units {
					labels[i] = string(u.Label)
				}
				logger.InfoKey("log.scene_tropes", pos+1, title, len(units), strings.Join(labels, " → "))
			}
		}
		if logger != nil {
			logger.InfoKey("log.scene_done", pos+1, len(plan.Scenes), prose.CountProseUnits(written), title)
		}
	}

	if len(parts) == 0 {
		if logger != nil {
			logger.WarnKey("log.scene_seq_fallback")
		}
		return "", false
	}
	content = strings.Join(parts, "\n\n")

	// recordFinalSceneSpans re-derives every scene's EXACT paragraph span and
	// trope units over the FINAL joined content. It must run after any length
	// adjustment too: when a compression/expansion pass changes the paragraph
	// count, stale ParaStart/ParaEnd fail recordedSceneParaSpans' tiling check
	// and the viewer silently drops back to one-block-per-paragraph — losing
	// both the per-trope grouping and the scene chip.
	recordFinalSceneSpans := func(finalContent string) {
		finalParas, finalSep := splitBlockParagraphs(finalContent)
		if len(finalParas) == 0 {
			return
		}
		// Fast path: the accepted spans still tile the final content exactly
		// (no length adjustment changed the paragraph count). Only refresh the
		// trope segmentation of each scene.
		if paraAcc > 0 && paraAcc == len(finalParas) {
			stillTiles := true
			for pos := range plan.Scenes {
				sc := &plan.Scenes[pos]
				if sc.ParaEnd <= 0 || sc.ParaStart >= sc.ParaEnd || sc.ParaEnd > len(finalParas) {
					stillTiles = false
					break
				}
				if pos > 0 && sc.ParaStart != plan.Scenes[pos-1].ParaEnd {
					stillTiles = false
					break
				}
			}
			if stillTiles {
				for pos := range plan.Scenes {
					sc := &plan.Scenes[pos]
					if sc.ParaEnd <= 0 {
						continue
					}
					segBeats, segRanges := splitTropesFromParas(finalParas[sc.ParaStart:sc.ParaEnd], "", finalSep)
					applyTropeSegmentation(sc, segBeats, segRanges)
				}
				return
			}
		}
		// Slow path: a compression/expansion pass rewrote paragraphs, or the
		// joined content has extra/fewer paragraphs than paraAcc counted
		// (blank-line runs, single-newline separators, meta lines). Re-anchor
		// each accepted scene part inside the final paragraph list by fuzzy
		// head/tail matching so ParaStart/ParaEnd stay exact instead of going
		// stale. Scenes that cannot be located keep their old bookkeeping; the
		// budget fallback in sceneParaSpans then covers them safely.
		type loc struct{ lo, hi int }
		locs := make([]loc, len(plan.Scenes))
		cum := 0
		found := 0
		for pos, part := range parts {
			if pos >= len(plan.Scenes) {
				break
			}
			partParas, _ := splitBlockParagraphs(part)
			if len(partParas) == 0 {
				continue
			}
			n := locatePartSpan(finalParas, cum, partParas)
			if n <= 0 {
				continue
			}
			locs[pos] = loc{cum, cum + n}
			cum += n
			found++
		}
		if found == 0 || cum == 0 {
			return
		}
		// The last located scene absorbs every remaining paragraph so the
		// spans tile the whole content (trailing blank artifacts included).
		if cum < len(finalParas) {
			for pos := len(locs) - 1; pos >= 0; pos-- {
				if locs[pos].hi > locs[pos].lo {
					locs[pos].hi = len(finalParas)
					break
				}
			}
		}
		for pos := range plan.Scenes {
			sc := &plan.Scenes[pos]
			l := locs[pos]
			if l.hi <= l.lo || l.hi > len(finalParas) {
				continue
			}
			sc.ParaStart = l.lo
			sc.ParaEnd = l.hi
			segBeats, segRanges := splitTropesFromParas(finalParas[l.lo:l.hi], "", finalSep)
			applyTropeSegmentation(sc, segBeats, segRanges)
		}
	}

	// Final whole-chapter verification (90%–180% of the configured target).
	total := prose.CountProseUnits(content)
	if logger != nil {
		logger.InfoKey("log.chapter_total_check", total, minLen, maxLen)
	}
	if total >= minLen && total <= maxLen {
		if logger != nil {
			logger.SuccessKey("log.chapter_total_ok", total, minLen, maxLen)
		}
		recordFinalSceneSpans(content)
		return content, true
	}
	if logger != nil {
		logger.WarnKey("log.chapter_total_adjust", total, minLen, maxLen)
	}
	var bestScore string
	score := int(^uint(0) >> 1)
	maybeUpdateBestDraft(&bestScore, &score, content, minLen, maxLen, target)
	adjusted, err := recoverChapterLengthBest(ctx, apiCfg, cfg, state, idx, content, minLen, maxLen, target, score, logger)
	if err == nil && adjusted != "" {
		maybeUpdateBestDraft(&bestScore, &score, adjusted, minLen, maxLen, target)
	}
	recordFinalSceneSpans(bestScore)
	final := prose.CountProseUnits(bestScore)
	if final >= minLen && final <= maxLen {
		if logger != nil {
			logger.SuccessKey("log.chapter_total_ok", final, minLen, maxLen)
		}
	} else if logger != nil {
		logger.WarnKey("log.chapter_total_final", final, minLen, maxLen)
	}
	return bestScore, true
}

func maxOf(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// locatePartSpan finds how many paragraphs of finalParas (searching forward
// from hint) correspond to one accepted scene part after a whole-chapter
// length adjustment. It matches on paragraph "shape" — the first/last few
// words of each paragraph — so compressions that reword sentences in place
// still align, while inserted/deleted paragraphs shift the window. Returns 0
// when no plausible run is found (caller keeps stale bookkeeping and lets the
// budget fallback take over).
func locatePartSpan(finalParas []string, hint int, partParas []string) int {
	n := len(partParas)
	if n == 0 || hint >= len(finalParas) {
		return 0
	}
	key := func(p string) string {
		fields := strings.Fields(strings.ToLower(p))
		if len(fields) == 0 {
			return ""
		}
		head := strings.Join(fields[:min(3, len(fields))], " ")
		tail := strings.Join(fields[max(0, len(fields)-3):], " ")
		return head + "|" + tail
	}
	partKeys := make([]string, n)
	for i, p := range partParas {
		partKeys[i] = key(p)
	}
	scoreAt := func(start int) int {
		score := 0
		for i := 0; i < n && start+i < len(finalParas); i++ {
			k := key(finalParas[start+i])
			if k == partKeys[i] {
				score += 2
			} else if k != "" && partKeys[i] != "" && (strings.HasPrefix(k, partKeys[i][:min(len(partKeys[i]), 8)]) || strings.Contains(k, partKeys[i])) {
				score++
			}
		}
		return score
	}
	best, bestScore := 0, 0
	limit := len(finalParas) - n
	if limit < hint {
		limit = hint
	}
	for s := hint; s <= limit; s++ {
		sc := scoreAt(s)
		if sc > bestScore {
			bestScore, best = sc, s
		}
		// Exact match: stop early, no need to scan further.
		if bestScore == 2*n {
			break
		}
	}
	if bestScore < n { // less than half the paragraphs matched → unreliable
		return 0
	}
	// The located span may extend past the part's own paragraph count when the
	// adjuster merged text; clamp it so consecutive parts keep tiling.
	end := best + n
	if end > len(finalParas) {
		end = len(finalParas)
	}
	return end - hint
}
