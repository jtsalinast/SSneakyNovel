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
func scenePlanChecklistBlock(plan *ScenePlan, lang string) string {
	en := i18n.NormalizeLanguage(lang) == i18n.LangEN
	var sb strings.Builder
	if en {
		sb.WriteString("[Full chapter beat sheet — write ONLY the assigned scene below]\n")
	} else {
		fmt.Fprintf(&sb, "[Beat sheet completo del capítulo — escribe SOLO la escena asignada abajo]\n")
	}
	cum := 0
	for _, s := range plan.Scenes {
		cum += s.WordsBudget
		pct := 0
		if plan.TargetWords > 0 {
			pct = cum * 100 / plan.TargetWords
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
func buildSceneWritingPrompt(cfg *config.Config, state *Progress, idx int, settings *ProjectSettings, plan *ScenePlan, pos int, written string, extraConstraints, retryFeedback string) (userPrompt, systemPrompt string) {
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
	sb.WriteString(scenePlanChecklistBlock(plan, lang))
	sb.WriteString("\n\n")

	writtenWords := prose.CountProseUnits(written)
	if en {
		fmt.Fprintf(&sb, "[TASK — WRITE SCENE %d OF %d ONLY]\n", pos+1, len(plan.Scenes))
		sb.WriteString("Write ONLY the following scene as a FULL dramatized scene (concrete action, sensory detail, dialogue, character interiority). Do not summarize it, do not write any other scene, and do not advance past this scene's beats.\n\n")
		fmt.Fprintf(&sb, "Assigned scene — %s (%d words budget, accepted range %d–%d words):\n%s\n\n", title, sc.WordsBudget, bandMin, bandMax, strings.TrimSpace(sc.Description))
		if sc.PoV != "" {
			fmt.Fprintf(&sb, "Point of view for this scene: %s\n\n", sc.PoV)
		}
		if writtenWords > 0 {
			sb.WriteString("[Already-written scenes of THIS chapter — continue seamlessly from the END of this text; never repeat or rewrite it]\n")
			sb.WriteString(strings.TrimSpace(written))
			sb.WriteString("\n\n")
		} else if strings.TrimSpace(buildPreviousChapterTailForLang(state, idx, lang)) != "" {
			sb.WriteString("The chapter's opening must flow naturally from the previous chapter's ending shown above.\n\n")
		}
		if pos == len(plan.Scenes)-1 {
			sb.WriteString("This is the LAST scene: close the chapter on the hook/cliffhanger implied by the outline.\n\n")
		}
		fmt.Fprintf(&sb, "Output exactly %d–%d words of prose (hard length requirement for this scene). Output ONLY the scene prose — no titles, numbering, recaps, dividers or meta lines.", bandMin, bandMax)
	} else {
		fmt.Fprintf(&sb, "[TAREA — ESCRIBE SOLO LA ESCENA %d DE %d]\n", pos+1, len(plan.Scenes))
		sb.WriteString("Escribe ÚNICAMENTE la siguiente escena como una escena completa dramatizada (acción concreta, detalle sensorial, diálogo, vida interior). No la resumas, no escribas ninguna otra escena y no avances más allá de los beats de esta escena.\n\n")
		fmt.Fprintf(&sb, "Escena asignada — %s (presupuesto %d palabras, rango aceptado %d–%d palabras):\n%s\n\n", title, sc.WordsBudget, bandMin, bandMax, strings.TrimSpace(sc.Description))
		if sc.PoV != "" {
			fmt.Fprintf(&sb, "Punto de vista de esta escena: %s\n\n", sc.PoV)
		}
		if writtenWords > 0 {
			sb.WriteString("[Escenas ya escritas de ESTE capítulo — continúa sin costuras desde el FINAL de este texto; nunca lo repitas ni lo reescribas]\n")
			sb.WriteString(strings.TrimSpace(written))
			sb.WriteString("\n\n")
		} else if strings.TrimSpace(buildPreviousChapterTailForLang(state, idx, lang)) != "" {
			sb.WriteString("La apertura del capítulo debe fluir naturalmente desde el final del capítulo anterior mostrado arriba.\n\n")
		}
		if pos == len(plan.Scenes)-1 {
			sb.WriteString("Esta es la ÚLTIMA escena: cierra el capítulo con el gancho que implica el outline.\n\n")
		}
		fmt.Fprintf(&sb, "Devuelve exactamente %d–%d palabras de prosa (requisito de longitud estricto de esta escena). Devuelve SOLO la prosa de la escena — sin títulos, numeración, resúmenes, separadores ni comentarios meta.", bandMin, bandMax)
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
			userPrompt, systemPrompt := buildSceneWritingPrompt(cfg, state, idx, settings, plan, pos, written, extraConstraints, retryFeedback)
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
		parts = append(parts, best)
		written = strings.Join(parts, "\n\n")
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

	// Final whole-chapter verification (90%–180% of the configured target).
	total := prose.CountProseUnits(content)
	if logger != nil {
		logger.InfoKey("log.chapter_total_check", total, minLen, maxLen)
	}
	if total >= minLen && total <= maxLen {
		if logger != nil {
			logger.SuccessKey("log.chapter_total_ok", total, minLen, maxLen)
		}
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
