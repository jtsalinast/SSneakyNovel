package story

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"showmethestory/internal/config"
	"showmethestory/internal/i18n"
	"showmethestory/internal/llm"
	"showmethestory/internal/prose"
	"showmethestory/internal/sse"
	"strings"
	"time"
)

func preferUserValue(userVal, fallback string) string {
	if userVal != "" {
		return userVal
	}
	return fallback
}

var (
	chapterMetaStartEN     = regexp.MustCompile(`(?i)^(?:chapter\s+\d+|part\s+\d+)`)
	chapterMetaPreambleEN  = regexp.MustCompile(`(?i)^(here\s+(?:is|are)|below\s+is|the\s+following\s+is|revised|full)\b.*(?:chapter|text|prose|content|version)`)
	chapterMetaParenNumRE  = regexp.MustCompile(`(?i)^\(\s*(?:chapter|part)\s+\d+[^)]*\)$`)
	chapterMetaRevisionRE  = regexp.MustCompile(`(?i)^.{0,10}(revised|modified|polished|rewritten).{0,5}(chapter|part)\s*\d+`)
	chapterMetaAsFollowsRE = regexp.MustCompile(`(?i)^.{0,10}(complete|full)?\s*(text|content|body).{0,5}(as follows|below)[:.:.\s]*$`)
)

// stripChapterMetaProse trims common AI-emitted chapter framing lines from prose boundaries.
// ponytail: line-based heuristics only; won't catch inline meta. Upgrade: model instructions + structured output.
func stripChapterMetaProse(content string, lang string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return content
	}
	lines := strings.Split(content, "\n")
	for len(lines) > 0 && isChapterMetaLine(strings.TrimSpace(lines[0]), lang) {
		lines = lines[1:]
	}
	for len(lines) > 0 && isChapterMetaLine(strings.TrimSpace(lines[len(lines)-1]), lang) {
		lines = lines[:len(lines)-1]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func isChapterMetaLine(line string, lang string) bool {
	if line == "" {
		return false
	}
	exact := []string{
		"End of chapter", "To be continued", "The End", "(完)", "(End)", "(Fin)",
	}
	for _, s := range exact {
		if line == s || strings.HasPrefix(line, s+".") {
			return true
		}
	}
	if chapterMetaStartEN.MatchString(line) {
		return true
	}
	if chapterMetaParenNumRE.MatchString(line) {
		return true
	}
	if chapterMetaPreambleEN.MatchString(line) {
		return true
	}
	if chapterMetaRevisionRE.MatchString(line) {
		return true
	}
	if chapterMetaAsFollowsRE.MatchString(line) {
		return true
	}
	return false
}

func formatWritingPOVBlock(pov, lang string) string {
	pov = strings.TrimSpace(pov)
	if pov == "" {
		return ""
	}
	return "[Narrative POV] " + pov
}

func formatExtraWritingConstraintsBlock(constraints, lang string) string {
	constraints = strings.TrimSpace(constraints)
	if constraints == "" {
		return ""
	}
	return "[Extra writing constraints (fact-check reconciliation)]\n" + constraints
}

func GenerateChapterAction(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, progressPath string, settings *ProjectSettings, skills []Skill, logger *sse.LogBroadcaster) error {
	if err := SyncPendingKnowledge(ctx, apiCfg, cfg, state, settings, progressPath, logger); err != nil {
		return err
	}
	if err := llm.ValidateConfig(apiCfg); err != nil {
		return err
	}
	if state.Phase != "writing" {
		return fmt.Errorf("not currently in the writing stage")
	}

	if state.CurrentChapterIndex >= len(state.Chapters) {
		return fmt.Errorf("haschaptersectionalready")
	}

	i := state.CurrentChapterIndex
	if err := EnsureNarrativeCheckpoints(ctx, apiCfg, cfg, state, progressPath, logger); err != nil {
		return err
	}
	ch := &state.Chapters[i]

	if ch.Status == StatusAccepted {
		return fmt.Errorf("# %d chapteralready, pleasewhenfrontchaptersectionorheavyplaceadvancedegree", ch.Num)
	}

	ch.Status = StatusWriting
	if err := SaveProgress(progressPath, state); err != nil {
		return err
	}

	logger.InfoKey("log.chapter_start", ch.Num, ch.Title)

	// writefront: thischapterbigifalreadyandwrite out (such asbigarrangemeetbutfrontalreadyrecognize),
	// firstmostsmallbigagainmove, by passhourbigwrite outshieldinside.
	if i > 0 {
		logger.StepInfo(1, 6, "checking this chapter's outline against the current story state...")
		revised, err := checkOutlineConsistency(ctx, apiCfg, cfg, state, i, logger)
		if err != nil {
			logger.WarnKey("log.outline_check_failed", err)
		} else if revised {
			if err := SaveProgress(progressPath, state); err != nil {
				return err
			}
			logger.InfoKey("log.outline_auto_revised")
		} else {
			logger.InfoKey("log.outline_consistent")
		}
	}

	if len(state.Foreshadows) > 0 {
		RunForeshadowOutlineCheckAndSave(ctx, apiCfg, cfg, state, progressPath, logger)
	}

	maxFactCheckRetries := 3
	factSkills := ResolveSkills(skills, cfg.SkillConfig, SkillScopeChapterFactCheck, cfg.Language)
	factCtx := llm.WithPromptAddon(ctx, FormatSkillsContent(factSkills))
	if len(factSkills) > 0 {
		names := make([]string, len(factSkills))
		for i, s := range factSkills {
			names[i] = s.Name
		}
		logger.InfoKey("log.skills_activated", strings.Join(names, ", "))
	}
	extraConstraints := ""
	var accumulatedIssues []string

	for attempt := 0; attempt <= maxFactCheckRetries; attempt++ {
		if ctx.Err() != nil {
			return fmt.Errorf("alreadywithdraw")
		}
		logger.StepInfo(2, 6, "inthoughtandwrite...")
		content, err := generateChapterContentWithLengthControl(ctx, apiCfg, cfg, state, i, settings, extraConstraints, logger)
		if err != nil {
			return err
		}
		if content == "" {
			return fmt.Errorf("lifeorquiltwithdraw")
		}
		ch.Content = content
		logger.InfoKey("log.prose_done", prose.CountProseUnits(content))

		logger.StepInfo(3, 6, "inthischapterwant to...")
		summary := generateChapterSummaryWithRetryLog(ctx, apiCfg, cfg, content, logger)
		if summary == "" {
			return fmt.Errorf("summary generation failed or was canceled")
		}
		ch.Summary = summary
		logger.InfoKey("log.summary_done")

		logger.StepInfo(4, 6, "intothischapteradvancetravel...")
		historySummary := buildHistorySummary(state, i)
		factCheckResult := generateChapterFactCheckWithRetryLog(factCtx, apiCfg, cfg, state, i, content, historySummary, logger)

		failed, issues := parseFactCheckResult(factCheckResult)
		if failed {
			accumulatedIssues = mergeUniqueIssues(accumulatedIssues, splitFactCheckIssues(issues))
			if attempt < maxFactCheckRetries {
				logger.WarnKey("log.factcheck_retry", ch.Num, attempt+1)
				logger.WarnKey("log.factcheck_details", issues)
				continue
			}

			logger.WarnKey("log.factcheck_max_retries")
			analysis, err := analyzeWritingConflict(ctx, apiCfg, cfg, state, i, content, accumulatedIssues, logger)
			if err != nil {
				logger.WarnKey("log.conflict_analyze_failed", err)
				break
			}

			if analysis.Reconcilable && strings.TrimSpace(analysis.ExtraConstraints) != "" {
				logger.InfoKey("log.conflict_retry")
				extraConstraints = strings.TrimSpace(analysis.ExtraConstraints)
				content, err = generateChapterContentWithLengthControl(ctx, apiCfg, cfg, state, i, settings, extraConstraints, logger)
				if err != nil {
					return err
				}
				if content == "" {
					return fmt.Errorf("lifeorquiltwithdraw")
				}
				ch.Content = content
				summary = generateChapterSummaryWithRetryLog(ctx, apiCfg, cfg, content, logger)
				if summary == "" {
					return fmt.Errorf("summary generation failed or was canceled")
				}
				ch.Summary = summary
				factCheckResult = generateChapterFactCheckWithRetryLog(factCtx, apiCfg, cfg, state, i, content, historySummary, logger)
				failed, issues = parseFactCheckResult(factCheckResult)
				if failed {
					accumulatedIssues = mergeUniqueIssues(accumulatedIssues, splitFactCheckIssues(issues))
				} else {
					logger.InfoKey("log.factcheck_constraint_pass")
					break
				}
			}

			conflict := buildWritingConflict(state, i, accumulatedIssues, analysis)
			lang := cfg.Language
			conflict.SuggestedActions = ensureConflictActions(conflict.SuggestedActions, lang)
			state.PendingWritingConflict = conflict
			if err := SaveProgress(progressPath, state); err != nil {
				return err
			}
			logger.WritingConflict(conflict)
			return &WritingConflictError{Conflict: conflict}
		}
		logger.InfoKey("log.factcheck_pass")
		break
	}

	state.PendingWritingConflict = nil

	if len(state.Foreshadows) > 0 {
		logger.StepInfo(5, 6, "inmorenewposture...")
		syncForeshadowsAfterChapter(ctx, apiCfg, cfg, state, i, progressPath, logger)
	}

	logger.StepInfo(6, 6, "inprotectmemory...")
	syncMemoryAfterChapter(ctx, apiCfg, cfg, state, i, progressPath, logger)
	// Memory sync commits a copied chapter slice, so refresh the chapter reference.
	ch = &state.Chapters[i]

	SaveChapterMarkdown(filepath.Dir(progressPath), *ch, state.Title)

	ch.Status = StatusReview
	state.CurrentChapterIndex = i
	if err := SaveProgress(progressPath, state); err != nil {
		return err
	}

	logger.SuccessKey("log.chapter_write_complete", ch.Num)
	return nil
}

// parseFactCheckResult untiefruit.
// firstuntie JSON middle result character-countparagraph, untiehourretreatforcharacter-count.
func parseFactCheckResult(raw string) (failed bool, issues string) {
	cleaned := cleanJSONResponse(raw)
	var resp struct {
		Result string   `json:"result"`
		Issues []string `json:"issues"`
	}
	if jsonStr := llm.ExtractJSON(cleaned); jsonStr != "" {
		if err := json.Unmarshal([]byte(jsonStr), &resp); err == nil && resp.Result != "" {
			return strings.EqualFold(strings.TrimSpace(resp.Result), "FAIL"), strings.Join(resp.Issues, "; ")
		}
	}
	// fallback: nountie JSON hourby character-count
	return strings.Contains(raw, "FAIL"), Truncate(raw, 300)
}

// checkOutlineConsistency writefrontbigone: toperfrontwant toandupperonechaptertail,
// thischapterbigisalreadyand(such asarrangemeetbutfrontalreadyrecognize).
// houruse AI give mostsmallthischapterbig(onlywhenfrontchapter), returnishairlife.
func checkOutlineConsistency(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, idx int, logger *sse.LogBroadcaster) (bool, error) {
	ch := &state.Chapters[idx]
	if strings.TrimSpace(ch.Outline) == "" {
		return false, nil
	}

	lang := cfg.Language
	prevEnding := ""
	if idx > 0 && state.Chapters[idx-1].Content != "" {
		if tail := tailAtParagraph(state.Chapters[idx-1].Content, prevTailMaxRunes); tail != "" {
			if i18n.NormalizeLanguage(lang) == i18n.LangEN {
				prevEnding = "[Previous chapter ending]\n" + tail + "\n\n"
			} else {
				prevEnding = "[upperonechaptertailplain]\n" + tail + "\n\n"
			}
		}
	}

	userPrompt := config.RenderPrompt(cfg.Prompts.OutlineConsistencyCheck, map[string]string{
		"ChapterNum":     fmt.Sprintf("%d", ch.Num),
		"ChapterTitle":   ch.Title,
		"ChapterOutline": ch.Outline,
		"HistorySummary": buildHistorySummaryForLang(state, idx, lang),
		"PreviousEnding": prevEnding,
	})
	systemPrompt := i18n.SystemPromptFor(lang, "outline_editor_brief_json")

	rawResp := llm.CallAPIWithRetryLog(ctx, apiCfg, systemPrompt, userPrompt, logger)
	if rawResp == "" {
		return false, fmt.Errorf("API call failed or was canceled")
	}

	var resp struct {
		Conflict       bool     `json:"conflict"`
		Issues         []string `json:"issues"`
		RevisedOutline string   `json:"revised_outline"`
	}
	jsonStr := llm.ExtractJSON(cleanJSONResponse(rawResp))
	if jsonStr == "" {
		return false, fmt.Errorf("unable to parse check result")
	}
	if err := json.Unmarshal([]byte(jsonStr), &resp); err != nil {
		return false, fmt.Errorf("failed to parse check result JSON: %w", err)
	}

	if !resp.Conflict || strings.TrimSpace(resp.RevisedOutline) == "" {
		return false, nil
	}

	logger.WarnKey("log.outline_conflict", ch.Num, strings.Join(resp.Issues, "; "))
	ch.Outline = strings.TrimSpace(resp.RevisedOutline)
	return true, nil
}

// ReviseChapterAction targets the "frontier chapter" (a chapter in review/writing state).
// makeusemostsmallword(plain), andinwant tohourlater pending chaptersectionbig.
func ReviseChapterAction(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, progressPath, feedback string, settings *ProjectSettings, logger *sse.LogBroadcaster) error {
	if err := llm.ValidateConfig(apiCfg); err != nil {
		return err
	}
	if state.Phase != "writing" {
		return fmt.Errorf("not currently in the writing stage")
	}

	chapterIdx := state.CurrentChapterIndex
	if chapterIdx < 0 || chapterIdx >= len(state.Chapters) {
		return fmt.Errorf("chaptersectioncordcross")
	}

	ch := &state.Chapters[chapterIdx]
	if ch.Status != StatusReview && ch.Status != StatusWriting {
		return fmt.Errorf("frontier chapter is not in review/writing state")
	}

	logger.InfoKey("log.chapter_modifying", ch.Num, ch.Title)

	logger.StepInfo(1, 3, "applying feedback revision...")
	revisedContent, err := reviseChapterContentStream(ctx, apiCfg, cfg, state, chapterIdx, feedback, settings, logger)
	if err != nil {
		return fmt.Errorf("chapter revision: %w", err)
	}
	ch.Content = revisedContent
	logger.InfoKey("log.prose_revised", prose.CountProseUnits(revisedContent))

	logger.StepInfo(2, 3, "regenerating summary...")
	summary := generateChapterSummaryWithRetryLog(ctx, apiCfg, cfg, ch.Content, logger)
	if summary == "" {
		return fmt.Errorf("summary generation failed or was canceled")
	}
	ch.Summary = summary
	logger.InfoKey("log.summary_done")

	SaveChapterMarkdown(filepath.Dir(progressPath), *ch, state.Title)

	if chapterIdx+1 < len(state.Chapters) {
		logger.StepInfo(3, 3, "revising subsequent chapter outlines...")
		if err := reviseSubsequentOutlines(ctx, apiCfg, cfg, state, chapterIdx, feedback); err != nil {
			logger.WarnKey("log.subsequent_outline_failed", err)
		} else {
			logger.InfoKey("log.subsequent_outline_done")
		}
	}

	ch.Status = StatusReview
	if err := SaveProgress(progressPath, state); err != nil {
		return err
	}

	if len(state.Foreshadows) > 0 {
		syncForeshadowsAfterChapter(ctx, apiCfg, cfg, state, chapterIdx, progressPath, logger)
		if err := SaveProgress(progressPath, state); err != nil {
			return err
		}
	}

	syncMemoryAfterChapter(ctx, apiCfg, cfg, state, chapterIdx, progressPath, logger)

	logger.SuccessKey("log.chapter_revised")
	return nil
}

// ReviseSpecificChapterAction tofingercompileNo. chaptersectiondomostsmall(includingalreadychaptersection).
// onlythechapterandwant to, not tentacleitshechaptersection, bigoradvancedegreefinger.
func ReviseSpecificChapterAction(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, progressPath string, chapterNum int, feedback string, settings *ProjectSettings, logger *sse.LogBroadcaster) error {
	if err := llm.ValidateConfig(apiCfg); err != nil {
		return err
	}
	if strings.TrimSpace(feedback) == "" {
		return fmt.Errorf("fewintentsee")
	}

	chapterIdx := -1
	for i, ch := range state.Chapters {
		if ch.Num == chapterNum {
			chapterIdx = i
			break
		}
	}
	if chapterIdx == -1 {
		return fmt.Errorf("chapter %d does not exist", chapterNum)
	}

	ch := &state.Chapters[chapterIdx]
	if ch.Content == "" {
		return fmt.Errorf("chapter %d has no generated content yet and cannot be revised (generate it first)", chapterNum)
	}
	if ch.Status == StatusWriting {
		return fmt.Errorf("chapter #%d is being written, please wait", chapterNum)
	}

	logger.InfoKey("log.chapter_specific_revising_long", ch.Num, ch.Title)

	logger.StepInfo(1, 2, "applying feedback revision...")
	revisedContent, err := reviseChapterContentStream(ctx, apiCfg, cfg, state, chapterIdx, feedback, settings, logger)
	if err != nil {
		return fmt.Errorf("chapter revision: %w", err)
	}
	ch.Content = revisedContent
	logger.InfoKey("log.prose_specific_revised", prose.CountProseUnits(revisedContent))

	logger.StepInfo(2, 2, "regenerating summary...")
	summary := generateChapterSummaryWithRetryLog(ctx, apiCfg, cfg, ch.Content, logger)
	if summary == "" {
		return fmt.Errorf("summary generation failed or was canceled")
	}
	ch.Summary = summary

	SaveChapterMarkdown(filepath.Dir(progressPath), *ch, state.Title)

	if err := SaveProgress(progressPath, state); err != nil {
		return err
	}

	if len(state.Foreshadows) > 0 {
		syncForeshadowsAfterChapter(ctx, apiCfg, cfg, state, chapterIdx, progressPath, logger)
		if err := SaveProgress(progressPath, state); err != nil {
			return err
		}
	}

	syncMemoryAfterChapter(ctx, apiCfg, cfg, state, chapterIdx, progressPath, logger)

	logger.SuccessKey("log.chapter_specific_done", ch.Num)
	return nil
}

func ConfirmChapterAction(state *Progress, progressPath string) error {
	original := state
	next := *state
	next.Chapters = append([]ChapterState(nil), state.Chapters...)
	next.NarrativeCheckpoints = append([]NarrativeCheckpoint(nil), state.NarrativeCheckpoints...)
	state = &next
	if state.Phase != "writing" {
		return fmt.Errorf("not currently in the writing stage")
	}

	chapterIdx := state.CurrentChapterIndex
	if chapterIdx < 0 || chapterIdx >= len(state.Chapters) {
		return fmt.Errorf("chaptersectioncordcross")
	}

	ch := &state.Chapters[chapterIdx]
	if ch.Status != StatusReview {
		return fmt.Errorf("whenfrontchaptersectionnot inposture, no")
	}

	ch.Status = StatusAccepted
	ch.KnowledgeTracked = true
	state.CurrentChapterIndex = chapterIdx + 1
	if err := SaveProgress(progressPath, state); err != nil {
		return err
	}
	*original = *state
	return nil
}

func generateChapterContentStream(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, idx int, settings *ProjectSettings, extraWritingConstraints string, logger *sse.LogBroadcaster) (string, error) {
	ch := state.Chapters[idx]
	lang := cfg.Language

	historySummary := buildHistorySummaryForLang(state, idx, lang)

	snapshot := state.StoryConfigSnapshot
	if snapshot == nil {
		snapshot = &cfg.Story
	}

	foreshadowContext := formatActiveForeshadowsForChapterLang(state.Foreshadows, ch.Num, lang)

	characterContext, worldviewContext := buildChapterSettingsContexts(settings, ch, lang)
	outlineConstraints := buildOutlineConstraintsForLang(state, idx, lang)
	memoryContext := buildMemoryForLang(state, idx, lang)

	minLen, maxLen := calcChapterLengthRange(snapshot.TargetWordsPerChapter)
	targetWords := snapshot.TargetWordsPerChapter

	userPrompt := config.RenderPrompt(cfg.Prompts.ChapterWriting, map[string]string{
		"Title":              preferUserValue(cfg.Story.Title, state.Title),
		"ChapterNum":         fmt.Sprintf("%d", ch.Num),
		"CorePrompt":         state.CorePrompt,
		"StorySynopsis":      ChapterSynopsis(cfg, state, ch.Num),
		"HistorySummary":     historySummary,
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
		"Foreshadows":        foreshadowContext,
		"Memory":             memoryContext,
		"OutlineConstraints": outlineConstraints,
	})
	userPrompt = finalizeChapterWritingPrompt(cfg.Prompts.ChapterWriting, userPrompt, minLen, maxLen, targetWords, lang)
	if np := NovelParametersBlock(cfg); np != "" {
		userPrompt += "\n\n" + np
	}
	if block := formatExtraWritingConstraintsBlock(extraWritingConstraints, lang); block != "" {
		userPrompt += "\n\n" + block
	}

	systemPrompt := state.CorePrompt
	if systemPrompt == "" {
		systemPrompt = i18n.SystemPromptFor(lang, "author_default")
	}

	onChunk := func(chunk string) {
		logger.ContentChunk(idx, chunk)
	}

	// knowfrontclearempty(heavy/moveconnectwritehourinside)
	logger.StreamStart(idx)
	content, err := llm.CallAPIStream(ctx, apiCfg, systemPrompt, userPrompt, onChunk)
	if err != nil {
		return "", err
	}
	return stripChapterMetaProse(content, lang), nil
}

func generateChapterContentStreamWithRetryLog(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, idx int, settings *ProjectSettings, extraWritingConstraints string, logger *sse.LogBroadcaster) string {
	retryCount := 0
	for {
		if ctx.Err() != nil {
			return ""
		}
		content, err := generateChapterContentStream(ctx, apiCfg, cfg, state, idx, settings, extraWritingConstraints, logger)
		if err == nil && content != "" {
			return content
		}
		if llm.IsFatalAPIError(err) {
			logger.ErrorKey("log.fatal_no_retry", err)
			return ""
		}

		retryCount++
		waitTime := llm.RetryWaitTime(retryCount)
		logger.WarnKey("log.content_gen_retry", err, retryCount, waitTime)
		select {
		case <-time.After(time.Duration(waitTime) * time.Second):
		case <-ctx.Done():
			return ""
		}
	}
}

func generateChapterSummary(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, content string) (string, error) {
	userPrompt := config.RenderPrompt(cfg.Prompts.ChapterSummary, map[string]string{
		"ChapterContent": content,
	})

	systemPrompt := i18n.SystemPromptFor(cfg.Language, "summary_analyst")
	return llm.CallAPI(ctx, apiCfg, systemPrompt, userPrompt)
}

func generateChapterSummaryWithRetryLog(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, content string, logger *sse.LogBroadcaster) string {
	retryCount := 0
	for {
		if ctx.Err() != nil {
			return ""
		}
		summary, err := generateChapterSummary(ctx, apiCfg, cfg, content)
		if err == nil && summary != "" {
			return summary
		}
		if llm.IsFatalAPIError(err) {
			logger.ErrorKey("log.fatal_no_retry", err)
			return ""
		}

		retryCount++
		waitTime := llm.RetryWaitTime(retryCount)
		logger.WarnKey("log.summary_retry", err, retryCount, waitTime)
		select {
		case <-time.After(time.Duration(waitTime) * time.Second):
		case <-ctx.Done():
			return ""
		}
	}
}

func generateChapterFactCheck(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, idx int, content string, historySummary string) (string, error) {
	ch := state.Chapters[idx]
	lang := cfg.Language
	outlineConstraints := buildOutlineConstraintsForLang(state, idx, lang)
	memoryContext := buildMemoryForLang(state, idx, lang, content)

	userPrompt := config.RenderPrompt(cfg.Prompts.FactCheck, map[string]string{
		"ChapterContent":     content,
		"HistorySummary":     historySummary,
		"CorePrompt":         "",
		"ChapterOutline":     ch.Outline,
		"OutlineConstraints": outlineConstraints,
		"Memory":             memoryContext,
	})
	systemPrompt := i18n.SystemPromptFor(lang, "fact_checker_json")
	return llm.CallAPI(ctx, apiCfg, systemPrompt, userPrompt)
}

func generateChapterFactCheckWithRetryLog(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, idx int, content string, historySummary string, logger *sse.LogBroadcaster) string {
	retryCount := 0
	for {
		if ctx.Err() != nil {
			return ""
		}
		result, err := generateChapterFactCheck(ctx, apiCfg, cfg, state, idx, content, historySummary)
		if err == nil && result != "" {
			return result
		}
		if llm.IsFatalAPIError(err) {
			logger.ErrorKey("log.fatal_no_retry", err)
			return ""
		}

		retryCount++
		waitTime := llm.RetryWaitTime(retryCount)
		logger.WarnKey("log.factcheck_api_retry", err, retryCount, waitTime)
		select {
		case <-time.After(time.Duration(waitTime) * time.Second):
		case <-ctx.Done():
			return ""
		}
	}
}

// quoteLineRegexp intentseemiddle usetravel: by '> ' openhead(markdown useblock).
// useinfrontplainlaterdotstrike"use"by , frontmoveBAmiddlecharacter-countby '> ' frontenterintentseeloseenter.
var quoteLineRegexp = regexp.MustCompile(`(?m)^[ \t]*>[ \t]?(.+?)\s*$`)

// errSegmentFallback fallback parsing (e.g. template usage failed, AI output paragraph numbering inconsistent),
// usesquareshouldreturnretreattochapter.
var errSegmentFallback = errors.New("segment revision unavailable, fallback to full chapter revision")

// extractQuotedSentences fromintentseemiddlewithdrawby '> ' openhead usetravel.
// returngoheavyinsurefollowingorder usesentencerowtable, byandgooffusetravellater "intentsee".
// ifhasusetravel, return nil andplain feedback.
func extractQuotedSentences(feedback string) (quotes []string, cleanFeedback string) {
	matches := quoteLineRegexp.FindAllStringSubmatch(feedback, -1)
	if len(matches) == 0 {
		return nil, feedback
	}
	seen := make(map[string]bool)
	for _, m := range matches {
		q := strings.TrimSpace(m[1])
		if q == "" || seen[q] {
			continue
		}
		seen[q] = true
		quotes = append(quotes, q)
	}
	if len(quotes) == 0 {
		return nil, feedback
	}
	cleanFeedback = strings.TrimSpace(quoteLineRegexp.ReplaceAllString(feedback, ""))
	return quotes, cleanFeedback
}

// findParagraphsContaining inchaptersectionmiddletobagoneusesentence paragraph.
// paragraphpassagefirstby doubletravelcutdivide; ifplainnot emptytravelthenby billtravelcutdivide. sep formakeuse divide,
// heavygrouphourwhiskeruseonedivide, thenassociationwritechapter travelgrid.
// ponytail: substring match on naive paragraph split; first hit per quote; miss → errSegmentFallback / full-chapter revise.
func findParagraphsContaining(content string, quotes []string) (matchedIdx []int, paragraphs []string, sep string, ok bool) {
	sep = "\n\n"
	paragraphs = strings.Split(content, sep)
	if len(paragraphs) <= 1 && strings.Contains(content, "\n") {
		sep = "\n"
		paragraphs = strings.Split(content, sep)
	}
	matchedSet := make(map[int]bool)
	for _, q := range quotes {
		found := -1
		for i, p := range paragraphs {
			if strings.Contains(p, q) {
				found = i
				break
			}
		}
		if found == -1 {
			return nil, nil, "", false
		}
		matchedSet[found] = true
	}
	for i := range paragraphs {
		if matchedSet[i] {
			matchedIdx = append(matchedIdx, i)
		}
	}
	return matchedIdx, paragraphs, sep, true
}

// trimEmptyEnds goexceptcutpiecetail emptywhiteparagraph(onlyemptywhitecharacter-count yuanplain).
func trimEmptyEnds(paras []string) []string {
	start, end := 0, len(paras)
	for start < end && strings.TrimSpace(paras[start]) == "" {
		start++
	}
	for end > start && strings.TrimSpace(paras[end-1]) == "" {
		end--
	}
	return paras[start:end]
}

// reviseChapterSegment tochaptersectionmiddlebagusesentence paragraphdopartmostsmall.
// onlyheavywriteparagraph, itsplaininsurestay. returnnew chapter.
// if template usage fails, or the AI output paragraph numbers are inconsistent with the expected count, return errSegmentFallback.
func reviseChapterSegment(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, chapterIdx int, quotes []string, cleanFeedback string, settings *ProjectSettings, logger *sse.LogBroadcaster) (string, error) {
	ch := state.Chapters[chapterIdx]
	lang := cfg.Language

	matchedIdx, paragraphs, sep, ok := findParagraphsContaining(ch.Content, quotes)
	if !ok {
		return "", errSegmentFallback
	}
	matchedParas := make([]string, 0, len(matchedIdx))
	for _, i := range matchedIdx {
		matchedParas = append(matchedParas, paragraphs[i])
	}
	segmentOriginal := strings.Join(matchedParas, "\n\n")
	quotedText := strings.Join(quotes, "\n")

	feedbackForAI := cleanFeedback
	if strings.TrimSpace(feedbackForAI) == "" {
		feedbackForAI = i18n.SystemPromptFor(lang, "segment_revision_default_feedback")
	}

	historySummary := buildHistorySummaryForLang(state, chapterIdx, lang)
	contextChapter := ch
	contextChapter.Outline += "\n" + feedbackForAI
	characterContext, worldviewContext := buildChapterSettingsContexts(settings, contextChapter, lang)

	userPrompt := config.RenderPrompt(cfg.Prompts.ChapterSegmentRevision, map[string]string{
		"ChapterNum":       fmt.Sprintf("%d", ch.Num),
		"ChapterTitle":     ch.Title,
		"CorePrompt":       state.CorePrompt,
		"HistorySummary":   historySummary,
		"WritingStyle":     cfg.Story.WritingStyle,
		"WritingPOV":       cfg.Story.WritingPOV,
		"CharacterContext": characterContext,
		"WorldviewContext": worldviewContext,
		"QuotedText":       quotedText,
		"SegmentOriginal":  segmentOriginal,
		"UserFeedback":     feedbackForAI,
	})
	userPrompt = appendIfMissingPlaceholder(cfg.Prompts.ChapterSegmentRevision, userPrompt, "{{.UserFeedback}}", feedbackForAI)
	userPrompt += factProtection(state, ch.Num, lang)

	systemPrompt := state.CorePrompt
	if systemPrompt == "" {
		systemPrompt = i18n.SystemPromptFor(lang, "author_default")
	}
	systemPrompt += i18n.SystemPromptFor(lang, "chapter_revision_suffix")

	rawResp := llm.CallAPIWithRetryLog(ctx, apiCfg, systemPrompt, userPrompt, logger)
	if rawResp == "" {
		return "", fmt.Errorf("partial revision API call failed or was canceled")
	}
	newSegment := stripChapterMetaProse(rawResp, lang)

	newParas := trimEmptyEnds(strings.Split(newSegment, "\n\n"))
	if len(newParas) != len(matchedParas) {
		return "", errSegmentFallback
	}
	out := make([]string, len(paragraphs))
	copy(out, paragraphs)
	for k, i := range matchedIdx {
		out[i] = newParas[k]
	}
	return strings.Join(out, sep), nil
}

// reviseChapterContentStream inplaindomostsmall().
func reviseChapterContentStream(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, chapterIdx int, userFeedback string, settings *ProjectSettings, logger *sse.LogBroadcaster) (string, error) {
	// partdividepay: useinintentseemiddleuse '> ' useplainpieceparagraphhour,
	// onlyheavywriteusesentenceinparagraph, itsplaininsurestay; thenreturnretreattochapter.
	if quotes, cleanFeedback := extractQuotedSentences(userFeedback); len(quotes) > 0 {
		logger.InfoKey("log.chapter_segment_revising", len(quotes))
		newContent, err := reviseChapterSegment(ctx, apiCfg, cfg, state, chapterIdx, quotes, cleanFeedback, settings, logger)
		if err == nil {
			return newContent, nil
		}
		if !errors.Is(err, errSegmentFallback) {
			return "", err
		}
		logger.InfoKey("log.chapter_segment_fallback")
	}

	ch := state.Chapters[chapterIdx]
	lang := cfg.Language

	historySummary := buildHistorySummaryForLang(state, chapterIdx, lang)
	contextChapter := ch
	contextChapter.Outline += "\n" + userFeedback
	characterContext, worldviewContext := buildChapterSettingsContexts(settings, contextChapter, lang)

	userPrompt := config.RenderPrompt(cfg.Prompts.ChapterRevision, map[string]string{
		"ChapterNum":       fmt.Sprintf("%d", ch.Num),
		"ChapterTitle":     ch.Title,
		"CorePrompt":       state.CorePrompt,
		"HistorySummary":   historySummary,
		"WritingStyle":     cfg.Story.WritingStyle,
		"WritingPOV":       cfg.Story.WritingPOV,
		"CharacterContext": characterContext,
		"WorldviewContext": worldviewContext,
		"OriginalContent":  ch.Content,
		"UserFeedback":     userFeedback,
	})
	userPrompt = appendIfMissingPlaceholder(cfg.Prompts.ChapterRevision, userPrompt, "{{.UserFeedback}}", userFeedback)
	userPrompt += factProtection(state, ch.Num, lang)

	systemPrompt := state.CorePrompt
	if systemPrompt == "" {
		systemPrompt = i18n.SystemPromptFor(lang, "author_default")
	}
	systemPrompt += i18n.SystemPromptFor(lang, "chapter_revision_suffix")

	onChunk := func(chunk string) {
		logger.ContentChunk(chapterIdx, chunk)
	}

	logger.StreamStart(chapterIdx)
	content, err := llm.CallAPIStream(ctx, apiCfg, systemPrompt, userPrompt, onChunk)
	if err != nil {
		return "", err
	}
	return stripChapterMetaProse(content, lang), nil
}

func reviseSubsequentOutlines(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, currentIdx int, userFeedback string) error {
	lang := cfg.Language
	en := i18n.NormalizeLanguage(lang) == i18n.LangEN

	subsequentChapters := ""
	for i := currentIdx + 1; i < len(state.Chapters); i++ {
		ch := state.Chapters[i]
		if ch.Status != StatusAccepted {
			subsequentChapters += formatChapterLine(ch.Num, ch.Title, ch.Outline, lang)
		}
	}
	if subsequentChapters == "" {
		return nil
	}

	lockedChapters := ""
	for i := 0; i <= currentIdx; i++ {
		ch := state.Chapters[i]
		if en {
			lockedChapters += fmt.Sprintf("Chapter %d \"%s\" (summary): %s\n", ch.Num, ch.Title, ch.Summary)
		} else {
			lockedChapters += fmt.Sprintf("#%dchapter<%s>(want to): %s\n", ch.Num, ch.Title, ch.Summary)
		}
	}

	var feedbackWrap string
	if en {
		feedbackWrap = fmt.Sprintf("The user gave revision feedback on chapter %d: %s\nOnly adjust later chapter outlines if this feedback affects downstream plot. If it is just wording detail, return the outlines verbatim.", state.Chapters[currentIdx].Num, userFeedback)
	} else {
		feedbackWrap = fmt.Sprintf("The user gave feedback on chapter #%d: %s\nOnly adjust the subsequent chapter outlines if this feedback affects later plot developments; if it is a wording-level tweak, return the outlines unchanged.", state.Chapters[currentIdx].Num, userFeedback)
	}

	userPrompt := config.RenderPrompt(cfg.Prompts.OutlineRevision, map[string]string{
		"CurrentOutline": subsequentChapters,
		"UserFeedback":   feedbackWrap,
		"LockedChapters": lockedChapters,
	})

	systemPrompt := i18n.SystemPromptFor(lang, "outline_editor_locked_json")

	rawResp := llm.CallAPIWithRetry(ctx, apiCfg, systemPrompt, userPrompt)
	if rawResp == "" {
		return fmt.Errorf("API call failed or was canceled")
	}
	rawResp = cleanJSONResponse(rawResp)

	var resp OutlineResponse
	if err := json.Unmarshal([]byte(rawResp), &resp); err != nil {
		return fmt.Errorf("untiebigJSON: %w", err)
	}

	for _, newCh := range resp.Chapters {
		for i, existingCh := range state.Chapters {
			if existingCh.Num == newCh.Num && existingCh.Status != StatusAccepted {
				state.Chapters[i].Title = newCh.Title
				state.Chapters[i].Outline = newCh.Outline
			}
		}
	}

	return nil
}

// futureOutlineWindow enterlaterchaptersectionbig windowmouthbigsmall(chapternumber)
const futureOutlineWindow = 10

// that omit a supported placeholder.
func appendIfMissingPlaceholder(template, rendered, placeholder, block string) string {
	if strings.TrimSpace(block) == "" || strings.Contains(template, placeholder) {
		return rendered
	}
	return rendered + "\n\n" + strings.TrimSpace(block)
}

func buildHistorySummary(state *Progress, idx int) string {
	return buildHistorySummaryForLang(state, idx, i18n.LangEN)
}

const (
	prevTailMaxRunes = 800  // enterupperonechaptertailpartplain mostbigcharacter-countnumber
	openingMaxRunes  = 1000 // connecthourwithdrawthischapteropenheadpieceparagraph mostbigcharacter-countnumber
)

// tailAtParagraph withdraw content tailapprox maxRunes character-count, directionlatertotoparagraphpassageedge, fromsentenceopen.
func tailAtParagraph(content string, maxRunes int) string {
	trimmed := strings.TrimSpace(content)
	runes := []rune(trimmed)
	if len(runes) <= maxRunes {
		return trimmed
	}
	tail := string(runes[len(runes)-maxRunes:])
	if i := strings.IndexByte(tail, '\n'); i >= 0 && i+1 < len(tail) {
		tail = tail[i+1:]
	}
	return strings.TrimSpace(tail)
}

// splitChapterOpening BAchaptersectioncutdivideforopenheadpieceparagraphandpartdivide, cutdotdirectionfronttotoparagraphpassageedge.
// rest foremptytablechapterallcalculateopenhead(chaptersectionshort).
func splitChapterOpening(content string, maxRunes int) (opening, rest string) {
	runes := []rune(content)
	if len(runes) <= maxRunes {
		return content, ""
	}
	cut := maxRunes
	for i := maxRunes; i > 0; i-- {
		if runes[i-1] == '\n' {
			cut = i
			break
		}
	}
	return string(runes[:cut]), string(runes[cut:])
}

// SmoothTransitionsAction batchmeasurealreadychaptersectionof connect:
// chapterBAupperonechaptertailpartandthischapteropenheadinteractgive AI disconnect, onlyinconnectlifehardhourmostsmallheavywritethischapteropenheadpieceparagraph.
// eachonechapteri.e.passageplate, canalong withhourwithdrawandnot alreadypartdivide.
func SmoothTransitionsAction(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, progressPath string, logger *sse.LogBroadcaster) error {
	if err := llm.ValidateConfig(apiCfg); err != nil {
		return err
	}

	var targets []int
	for i := 1; i < len(state.Chapters); i++ {
		if state.Chapters[i].Status == StatusAccepted && state.Chapters[i].Content != "" &&
			state.Chapters[i-1].Status == StatusAccepted && state.Chapters[i-1].Content != "" {
			targets = append(targets, i)
		}
	}
	if len(targets) == 0 {
		return fmt.Errorf("hascan chaptersection(want totofew alreadychaptersection)")
	}

	logger.InfoKey("log.smooth_start", len(targets))
	optimized := 0
	for n, idx := range targets {
		if ctx.Err() != nil {
			return fmt.Errorf("alreadywithdraw")
		}
		ch := &state.Chapters[idx]
		logger.StepInfo(n+1, len(targets), fmt.Sprintf("in# %d chapter<%s> connect...", ch.Num, ch.Title))

		prevTail := tailAtParagraph(state.Chapters[idx-1].Content, prevTailMaxRunes)
		opening, rest := splitChapterOpening(ch.Content, openingMaxRunes)

		userPrompt := config.RenderPrompt(cfg.Prompts.TransitionSmoothing, map[string]string{
			"ChapterNum":     fmt.Sprintf("%d", ch.Num),
			"ChapterTitle":   ch.Title,
			"ChapterOutline": ch.Outline,
			"PrevTail":       prevTail,
			"Opening":        opening,
		})
		systemPrompt := i18n.SystemPromptFor(cfg.Language, "transition_editor")
		userPrompt += factProtection(state, ch.Num, cfg.Language)

		resp := llm.CallAPIWithRetryLog(ctx, apiCfg, systemPrompt, userPrompt, logger)
		if resp == "" {
			return fmt.Errorf("continuity check call for chapter %d failed or was canceled", ch.Num)
		}
		revised := strings.TrimSpace(resp)

		head := revised
		if len([]rune(head)) > 30 {
			head = string([]rune(head)[:30])
		}
		if revised == "" || strings.Contains(head, "NO_CHANGE") {
			logger.InfoKey("log.smooth_natural", ch.Num)
			continue
		}

		if rest == "" {
			ch.Content = revised
		} else {
			ch.Content = revised + "\n\n" + strings.TrimLeft(rest, "\n")
		}
		SaveChapterMarkdown(filepath.Dir(progressPath), *ch, state.Title)
		ch.KnowledgeTracked = true
		if err := SaveProgress(progressPath, state); err != nil {
			return err
		}
		optimized++
		logger.InfoKey("log.smooth_optimized", ch.Num)
	}

	logger.SuccessKey("log.smooth_done", len(targets), optimized)
	return nil
}

func PolishChapterAction(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, chapterIdx int, skills []Skill, progressPath string, logger *sse.LogBroadcaster) error {
	if chapterIdx < 0 || chapterIdx >= len(state.Chapters) {
		return fmt.Errorf("chaptersectioncordcross")
	}

	ch := &state.Chapters[chapterIdx]
	if ch.Content == "" {
		return fmt.Errorf("chaptersectioninsideforempty, no")
	}

	skillsContent := FormatSkillsContent(skills)
	if skillsContent == "" {
		return fmt.Errorf("hasenableuse can, pleasefirstincanpageenableuse")
	}

	var userPrompt string
	if i18n.NormalizeLanguage(cfg.Language) == i18n.LangEN {
		userPrompt = fmt.Sprintf(`Polish the chapter below according to the rules. Output the full revised chapter prose. Do not add chapter titles, numbers, "End of chapter", or any other meta or explanatory text.

## Polish rules

%s

## Chapter to polish

%s`, skillsContent, ch.Content)
	} else {
		userPrompt = fmt.Sprintf(`Please remove the following AI-flavored phrasing from the chapter text below, then output only the revised text. Do not include the chapter title, chapter number, "this chapter" meta-references or any explanations. 

## then

%s

## wait

%s`, skillsContent, ch.Content)
	}

	systemPrompt := i18n.SystemPromptFor(cfg.Language, "polish_editor")
	userPrompt += factProtection(state, ch.Num, cfg.Language)

	onChunk := func(chunk string) {
		logger.ContentChunk(chapterIdx, chunk)
	}

	logger.StreamStart(chapterIdx)
	result, err := llm.CallAPIStream(ctx, apiCfg, systemPrompt, userPrompt, onChunk)
	if err != nil {
		return fmt.Errorf(": %w", err)
	}

	ch.Content = stripChapterMetaProse(result, cfg.Language)
	ch.KnowledgeTracked = true
	ch.Status = StatusReview

	SaveChapterMarkdown(filepath.Dir(progressPath), *ch, state.Title)

	if err := SaveProgress(progressPath, state); err != nil {
		return fmt.Errorf("insuredepositadvancedegree: %w", err)
	}

	return nil
}

// calcMemoryMaxTokens calculates the memory token budget based on total estimated word count.
func calcMemoryMaxTokens(chapterCount, targetWordsPerChapter int) int {
	totalWords := chapterCount * targetWordsPerChapter
	tokens := totalWords / 10
	if tokens < 2000 {
		tokens = 2000
	}
	if tokens > 20000 {
		tokens = 20000
	}
	return tokens
}

// nextMemoryID returns the next available memory entry ID.
func nextMemoryID(entries []MemoryEntry) int {
	maxID := 0
	for _, m := range entries {
		if m.ID > maxID {
			maxID = m.ID
		}
	}
	return maxID + 1
}

func syncMemoryAfterChapter(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, idx int, progressPath string, logger *sse.LogBroadcaster) {
	if err := SyncChapterMemory(ctx, apiCfg, cfg, state, idx, progressPath, logger); err != nil {
		logger.WarnKey("log.knowledge_failed", err)
	}
}

type memoryNewEntry struct {
	Content  string `json:"content"`
	Category string `json:"category"`
	Position int    `json:"position"`
}

type memoryUpdateEntry struct {
	ID     int    `json:"id"`
	Action string `json:"action"`
}

type memoryUpdateResult struct {
	NewMemories []memoryNewEntry    `json:"new_memories"`
	Updates     []memoryUpdateEntry `json:"updates"`
}

// parseMemoryUpdateResult parses the AI response for memory update.
func parseMemoryUpdateResult(result string) ([]memoryNewEntry, []memoryUpdateEntry, error) {
	cleaned := cleanJSONResponse(result)
	var parsed memoryUpdateResult
	if err := json.Unmarshal([]byte(cleaned), &parsed); err != nil {
		return nil, nil, err
	}
	return parsed.NewMemories, parsed.Updates, nil
}
