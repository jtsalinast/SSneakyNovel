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
		return fmt.Errorf("todos los capítulos ya fueron escritos")
	}

	i := state.CurrentChapterIndex
	if err := EnsureNarrativeCheckpoints(ctx, apiCfg, cfg, state, progressPath, logger); err != nil {
		return err
	}
	ch := &state.Chapters[i]

	if ch.Status == StatusAccepted {
		return fmt.Errorf("el capítulo #%d ya está aceptado; ve al capítulo actual o reinicia el avance", ch.Num)
	}

	ch.Status = StatusWriting
	if err := SaveProgress(progressPath, state); err != nil {
		return err
	}

	logger.InfoKey("log.chapter_start", ch.Num, ch.Title)

	// Pre-check: if this chapter's outline contradicts what has already been
	// written (e.g. re-staging a meeting that already happened), revise it
	// first — with safeguards against degraded overwrites — before writing.
	if i > 0 {
		logger.StepInfo(1, 6, "Checking this chapter's outline against the current story state...")
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
	// Issues fixed via the reconcilable-constraint retry path. When the chapter
	// finally passes with them applied, they become this chapter's continuity
	// notes (persisted by ConfirmChapterAction) so later chapters/plannings
	// treat them as hard "do not repeat" constraints.
	var resolvedIssues []string

	for attempt := 0; attempt <= maxFactCheckRetries; attempt++ {
		if ctx.Err() != nil {
			return fmt.Errorf("generación cancelada")
		}
		logger.StepInfo(2, 6, "Escribiendo la prosa del capítulo...")
		content, err := generateChapterContentWithLengthControl(ctx, apiCfg, cfg, state, i, settings, extraConstraints, logger)
		if err != nil {
			return err
		}
		if content == "" {
			return fmt.Errorf("la generación de prosa falló o fue cancelada")
		}
		ch.Content = content
		logger.InfoKey("log.prose_done", prose.CountProseUnits(content))

		logger.StepInfo(3, 6, "Resumiendo este capítulo para el historial...")
		summary := generateChapterSummaryWithRetryLog(ctx, apiCfg, cfg, content, logger)
		if summary == "" {
			return fmt.Errorf("summary generation failed or was canceled")
		}
		ch.Summary = summary
		logger.InfoKey("log.summary_done")

		logger.StepInfo(4, 6, "Verifying this chapter against established facts...")
		historySummary := buildHistorySummary(state, i)
		factCheckResult := generateChapterFactCheckWithRetryLog(factCtx, apiCfg, cfg, state, i, content, historySummary, logger)

		failed, issues := parseFactCheckResult(factCheckResult)
		if failed {
			accumulatedIssues = dedupeArcIssuesWithCast(mergeUniqueIssues(accumulatedIssues, splitFactCheckIssues(issues)), state, i)
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
					return fmt.Errorf("la generación de prosa falló o fue cancelada")
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
					accumulatedIssues = dedupeArcIssuesWithCast(mergeUniqueIssues(accumulatedIssues, splitFactCheckIssues(issues)), state, i)
				} else {
					logger.InfoKey("log.factcheck_constraint_pass")
					resolvedIssues = mergeUniqueIssues(nil, accumulatedIssues)
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
	// Persist the continuity notes on the in-progress chapter so they survive
	// even if the user edits/confirms through another path. ConfirmChapterAction
	// keeps them when moving the chapter to accepted.
	if len(resolvedIssues) > 0 {
		notes := []string{}
		for _, issue := range resolvedIssues {
			if n := strings.TrimSpace(issue); n != "" {
				notes = append(notes, Truncate(n, 200))
			}
		}
		ch.ContinuityNotes = notes[:min(len(notes), 6)]
	}

	if len(state.Foreshadows) > 0 {
		logger.StepInfo(5, 6, "Updating foreshadow tracking...")
		syncForeshadowsAfterChapter(ctx, apiCfg, cfg, state, i, progressPath, logger)
	}

	logger.StepInfo(6, 6, "Syncing narrative memory...")
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

// factVerdictFailRe matches an explicit FAIL verdict in JSON form ("result":
// "FAIL") or in plain text (FAIL as a standalone word). The old substring scan
// treated any occurrence of "FAIL" — including inside issue descriptions like
// "the plan does not fail anywhere" or a PASS verdict explaining what did NOT
// fail — as a failed check, triggering needless full-chapter regenerations.
var factVerdictFailRe = regexp.MustCompile(`(?i)"result"\s*:\s*"?\s*FAIL\b|\bFAIL\b`)

// factVerdictPassRe detects an explicit PASS verdict used as a tie-breaker when
// both words appear in free-form prose.
var factVerdictPassRe = regexp.MustCompile(`(?i)\bPASS\b`)

// parseFactCheckResult parses the fact-check verdict.
// It first tries to read the JSON result/issues fields; on failure it falls
// back to strict keyword scanning that only treats a standalone FAIL token
// (or the JSON verdict field) as a failed check.
func parseFactCheckResult(raw string) (failed bool, issues string) {
	cleaned := cleanJSONResponse(raw)
	var resp struct {
		Result string   `json:"result"`
		Issues []string `json:"issues"`
	}
	if jsonStr := llm.ExtractJSON(cleaned); jsonStr != "" {
		if err := json.Unmarshal([]byte(jsonStr), &resp); err == nil && strings.TrimSpace(resp.Result) != "" {
			verdict := strings.ToUpper(strings.TrimSpace(resp.Result))
			failed = strings.HasPrefix(verdict, "FAIL")
			return failed, strings.Join(resp.Issues, "; ")
		}
	}
	// fallback: the response is not valid JSON. Scan for a standalone FAIL
	// verdict token; if the text also carries a standalone PASS token, prefer
	// PASS (models often write "PASS — no issues" but mention failures only
	// hypothetically). Issue list, when failed, is the truncated raw text.
	hasFail := factVerdictFailRe.FindString(raw) != ""
	hasPass := factVerdictPassRe.FindString(raw) != ""
	failed = hasFail && !hasPass
	if failed {
		return true, Truncate(raw, 300)
	}
	return false, ""
}

// checkOutlineConsistency verifies the upcoming chapter's outline does not
// contradict what has already been written (e.g. restaging an event that
// already happened). It asks the AI for a minimal revision of THIS chapter's
// outline only when earlier chapters changed it; returns whether it revised.
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
				prevEnding = "[Final del capítulo anterior]\n" + tail + "\n\n"
			}
		}
	}

	// The same outline also contradicts the FUTURE arc when it stages debuts
	// or one-time events assigned to later chapters — exactly the premature-
	// introduction failures the fact-checker keeps reporting after the prose
	// is already written. Feed the hard debut ban into the pre-check so the
	// outline gets fixed before writing instead of burning fact-check retries.
	en := i18n.NormalizeLanguage(lang) == i18n.LangEN
	futureArc := ""
	eventLines := buildPrematureEventLines(state, idx, lang)
	if len(eventLines) > 0 {
		if en {
			futureArc = "[One-time events reserved for LATER chapters — this outline must not stage them]\n" + strings.Join(eventLines, "") + "\n\n"
		} else {
			futureArc = "[Eventos únicos reservados para capítulos POSTERIORES — este outline no debe representarlos]\n" + strings.Join(eventLines, "") + "\n\n"
		}
	}
	hardDebut := buildHardDebutBlock(state, idx, lang)
	if hardDebut != "" {
		futureArc += hardDebut
	}
	// No structured future data at all → there is nothing the checker can
	// verify against. A vague "may conflict with later chapters" prompt made it
	// hallucinate conflicts (e.g. flagging a mere mention of a character whose
	// debut is planned later even though the outline never stages the meeting).
	// In that case pass the check deterministically: zero API cost, zero noise.
	if eventLines == nil && hardDebut == "" {
		return false, nil
	}

	// The checker's own template must carry {{.FutureArc}}: RenderPrompt drops
	// data keys absent from the template, so a customized copy without the token
	// would silently disable the future channel (premature debuts) and leave the
	// model with only the past-checklist framing — half-blind behaviour that
	// makes it report conflicts about events nobody actually staged.
	userPrompt := config.RenderPrompt(withTokenFallback(cfg.Prompts.OutlineConsistencyCheck, "{{.FutureArc}}"), map[string]string{
		"ChapterNum":     fmt.Sprintf("%d", ch.Num),
		"ChapterTitle":   ch.Title,
		"ChapterOutline": ch.Outline,
		"HistorySummary": buildHistorySummaryForLang(state, idx, lang),
		"PreviousEnding": prevEnding,
		"FutureArc":      futureArc,
	})
	userPrompt = appendIfMissingPlaceholder(cfg.Prompts.OutlineConsistencyCheck, userPrompt, "{{.FutureArc}}", futureArc)
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

	revised := strings.TrimSpace(resp.RevisedOutline)
	// Guard against the checker "revising" the outline into a generic
	// summary: an accepted rewrite must preserve the original chapter's
	// substance (length and named entities). Otherwise we would silently
	// degrade the detailed outline into plot drift that every later
	// generation inherits. When in doubt, keep the human-approved outline.
	oldRunes := []rune(ch.Outline)
	newRunes := []rune(revised)
	if len(newRunes) < len(oldRunes)/2 {
		logger.WarnKey("log.outline_revision_rejected", ch.Num)
		return false, nil
	}
	if droppedNamedEntities(ch.Outline, revised) {
		logger.WarnKey("log.outline_revision_rejected", ch.Num)
		return false, nil
	}

	logger.WarnKey("log.outline_conflict", ch.Num, strings.Join(resp.Issues, "; "))
	ch.Outline = revised
	return true, nil
}

// entityPattern matches capitalized multi-token names (character/place
// entities) commonly used in Latin-script outlines. Single common words are
// filtered through the stoplist below; CJK text yields no matches, which
// droppedNamedEntities treats as "nothing to compare".
var entityPattern = regexp.MustCompile(`[A-ZÀ-ÖØ-Þ][\p{L}'’-]*(?:[ \t]+[A-ZÀ-ÖØ-Þ][\p{L}'’-]*)*`)

var entityStoplist = map[string]bool{
	"The": true, "A": true, "An": true, "He": true, "She": true, "It": true,
	"They": true, "We": true, "In": true, "On": true, "At": true, "But": true,
	"And": true, "Or": true, "If": true, "When": true, "While": true, "After": true,
	"Before": true, "Meanwhile": true, "However": true, "Then": true, "Finally": true,
	"Chapter": true, "His": true, "Her": true, "Their": true, "This": true, "That": true,
	"There": true, "Here": true, "As": true, "No": true, "Yes": true, "Not": true,
}

// entitySet extracts candidate proper-name entities from text.
func entitySet(text string) map[string]bool {
	set := map[string]bool{}
	for _, m := range entityPattern.FindAllString(text, -1) {
		m = strings.TrimSpace(m)
		if m == "" || entityStoplist[m] || len([]rune(m)) < 3 {
			continue
		}
		set[strings.ToLower(m)] = true
	}
	return set
}

// droppedNamedEntities reports whether the revised outline loses named
// entities that were present in the original outline.
func droppedNamedEntities(original, revised string) bool {
	orig := entitySet(original)
	if len(orig) == 0 {
		return false // nothing to compare (non-Latin script or nameless outline)
	}
	rev := entitySet(revised)
	for e := range orig {
		if !rev[e] {
			return true
		}
	}
	return false
}

// ReviseChapterAction targets the "frontier chapter" (a chapter in review/writing state).
// Usa la menor cantidad de palabras posible y no toca tramas de capítulos pendientes posteriores.
func ReviseChapterAction(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, progressPath, feedback string, settings *ProjectSettings, logger *sse.LogBroadcaster) error {
	if err := llm.ValidateConfig(apiCfg); err != nil {
		return err
	}
	if state.Phase != "writing" {
		return fmt.Errorf("not currently in the writing stage")
	}

	chapterIdx := state.CurrentChapterIndex
	if chapterIdx < 0 || chapterIdx >= len(state.Chapters) {
		return fmt.Errorf("índice de capítulo fuera de rango")
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
	// Length guard: feedback rewrites habitually compress the prose (4000-word
	// chapters came back as ~1800 words and were accepted silently). Retry with
	// an explicit keep-everything instruction, then revert to the original if
	// the rewrite still destroys content.
	origContent := ch.Content
	targetWords, _ := resolveChapterTargetWords(state, cfg)
	minLen, maxLen := calcChapterLengthRange(targetWords)
	revisedContent, _, err = enforceRevisionLength(ctx, apiCfg, cfg, origContent, revisedContent, feedback, minLen, maxLen, logger,
		func(guardedFeedback string) (string, error) {
			return reviseChapterContentStream(ctx, apiCfg, cfg, state, chapterIdx, guardedFeedback, settings, logger)
		})
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

// ReviseSpecificChapterAction revisa un capítulo por número (incluidos los ya aceptados).
// Solo afecta a ese capítulo; no toca otros capítulos, tramas ni el cursor de avance.
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
	// Same length guard as ReviseChapterAction: never let a feedback rewrite
	// silently compress the chapter far below the configured word band.
	origContent := ch.Content
	targetWords, _ := resolveChapterTargetWords(state, cfg)
	minLen, maxLen := calcChapterLengthRange(targetWords)
	revisedContent, _, err = enforceRevisionLength(ctx, apiCfg, cfg, origContent, revisedContent, feedback, minLen, maxLen, logger,
		func(guardedFeedback string) (string, error) {
			return reviseChapterContentStream(ctx, apiCfg, cfg, state, chapterIdx, guardedFeedback, settings, logger)
		})
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
		return fmt.Errorf("índice de capítulo fuera de rango")
	}

	ch := &state.Chapters[chapterIdx]
	if ch.Status != StatusReview {
		return fmt.Errorf("el capítulo actual no está en estado de revisión")
	}

	ch.Status = StatusAccepted
	ch.KnowledgeTracked = true
	// The fact-check issues that this chapter had to overcome are exactly the
	// continuity pitfalls later chapters tend to repeat (a character knowing
	// something they shouldn't yet, a location reached too early…). Record them
	// on the chapter so outline generation and writing treat them as hard
	// constraints. Only set once; manual edits after acceptance keep the notes.
	if len(ch.ContinuityNotes) == 0 && state.PendingWritingConflict != nil &&
		state.PendingWritingConflict.ChapterIndex == chapterIdx {
		notes := []string{}
		for _, issue := range state.PendingWritingConflict.Issues {
			if n := strings.TrimSpace(issue); n != "" {
				notes = append(notes, Truncate(n, 200))
			}
		}
		if len(notes) > 0 {
			next.Chapters[chapterIdx].ContinuityNotes = notes[:min(len(notes), 6)]
			ch = &state.Chapters[chapterIdx]
		}
	}
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

	targetWords, _ := resolveChapterTargetWords(state, cfg)

	foreshadowContext := formatActiveForeshadowsForChapterLang(state.Foreshadows, ch.Num, lang)

	characterContext, worldviewContext := buildChapterSettingsContexts(settings, ch, lang)
	outlineConstraints := buildOutlineConstraintsForLang(state, idx, lang)
	memoryContext := buildMemoryForLang(state, idx, lang)
	voiceSamples := buildVoiceSamplesForLang(state, idx, settings, lang)

	minLen, maxLen := calcChapterLengthRange(targetWords)

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
		"VoiceSamples":       voiceSamples,
		"OutlineConstraints": outlineConstraints,
	})
	userPrompt = finalizeChapterWritingPrompt(cfg.Prompts.ChapterWriting, userPrompt, minLen, maxLen, targetWords, lang)
	// Customized templates that omit {{.PreviousEnding}} would silently lose the
	// verbatim tail of the previous chapter (seamless continuation). Re-append it
	// so continuity never depends on the template author remembering the token.
	userPrompt = appendIfMissingPlaceholder(cfg.Prompts.ChapterWriting, userPrompt, "{{.PreviousEnding}}", buildPreviousChapterTailForLang(state, idx, lang))
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

	// Stream the prose live to the UI while generating.
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
		if err == nil && strings.TrimSpace(content) != "" {
			return content
		}
		if err == nil {
			err = fmt.Errorf("la API devolvió una respuesta vacía")
		}
		if llm.IsFatalAPIError(err) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			logger.ErrorKey("log.fatal_no_retry", err)
			return ""
		}
		retryCount++
		if retryCount > llm.MaxAPIRetries {
			logger.ErrorKey("log.max_retries_exceeded", fmt.Errorf("se agotaron los %d reintentos de generación del capítulo %d", llm.MaxAPIRetries, idx+1))
			return ""
		}

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
		if err == nil && strings.TrimSpace(summary) != "" {
			return summary
		}
		if err == nil {
			err = fmt.Errorf("la API devolvió una respuesta vacía")
		}
		if llm.IsFatalAPIError(err) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			logger.ErrorKey("log.fatal_no_retry", err)
			return ""
		}
		retryCount++
		if retryCount > llm.MaxAPIRetries {
			logger.ErrorKey("log.max_retries_exceeded", fmt.Errorf("se agotaron los %d reintentos de resumen del capítulo", llm.MaxAPIRetries))
			return ""
		}

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
	// Reviewers get the FULL arc block (upcoming outlines included): unlike
	// the writer, they must judge the prose against what later chapters plan,
	// so they can flag leaked events and name their destination chapter.
	outlineConstraints := buildArcConstraintsForReview(state, idx, lang)
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
		if err == nil && strings.TrimSpace(result) != "" {
			return result
		}
		if err == nil {
			err = fmt.Errorf("la API devolvió una respuesta vacía")
		}
		if llm.IsFatalAPIError(err) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			logger.ErrorKey("log.fatal_no_retry", err)
			return ""
		}
		retryCount++
		if retryCount > llm.MaxAPIRetries {
			logger.ErrorKey("log.max_retries_exceeded", fmt.Errorf("se agotaron los %d reintentos de verificación de hechos del capítulo %d", llm.MaxAPIRetries, idx+1))
			return ""
		}

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
// Uses the plain-text quote marker ">" at line starts to locate the quoted passage in the current text.
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

// findParagraphsContaining localiza los párrafos del capítulo que contienen una cita.
// Los párrafos se separan primero por doble salto de línea; si no hay, por salto simple. sep indica
// el separador usado; al reagrupar se reconstruye el texto del capítulo tal cual.
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

// trimEmptyEnds removes leading/trailing blank paragraphs (whitespace-only lines).
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

// reviseChapterSegment revisa solo los párrafos citados del capítulo.
// Reescribe únicamente esos párrafos; el resto permanece intacto. Devuelve el capítulo nuevo.
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
func reviseChapterContentStream(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, chapterIdx int, userFeedback string, settings *ProjectSettings, logger *sse.LogBroadcaster, extraContext ...string) (string, error) {
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
	// extraContext carries hard sequencing constraints (debut ban list,
	// reserved one-time events) into feedback-driven rewrites; without them a
	// "remove the premature Edmund scene" revision risks replacing it with an
	// equally premature substitute, since the ChapterRevision template has no
	// OutlineConstraints placeholder of its own.
	for _, ec := range extraContext {
		if strings.TrimSpace(ec) != "" {
			userPrompt += "\n\n" + strings.TrimSpace(ec)
		}
	}
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
		return fmt.Errorf("parse JSON: %w", err)
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

// futureOutlineWindow es el tamaño de la ventana de capítulos futuros (en número de capítulos)
const futureOutlineWindow = 10

// that omit a supported placeholder.
func appendIfMissingPlaceholder(template, rendered, placeholder, block string) string {
	if strings.TrimSpace(block) == "" || strings.Contains(template, placeholder) {
		return rendered
	}
	return rendered + "\n\n" + strings.TrimSpace(block)
}

// withTokenFallback guarantees a data key actually reaches the model even when
// the user's customized template dropped the {{...}} token: RenderPrompt only
// substitutes tokens present in the template, so silently-missing tokens used
// to disable whole context channels (future arc, feedback). Appending the token
// to the template lets RenderPrompt place it; appendIfMissingPlaceholder then
// sees it as present and does nothing further.
func withTokenFallback(template, token string) string {
	if strings.Contains(template, token) {
		return template
	}
	return template + "\n\n" + token
}

func buildHistorySummary(state *Progress, idx int) string {
	return buildHistorySummaryForLang(state, idx, i18n.LangEN)
}

const (
	prevTailMaxRunes = 800  // máximo de caracteres del fragmento del final del capítulo anterior
	openingMaxRunes  = 1000 // max runes taken from the opening paragraphs of the chapter when linking
)

// tailAtParagraph devuelve el final de content con ~maxRunes caracteres, alineado a límite de párrafo.
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

// splitChapterOpening divide el capítulo en su párrafo de apertura y el resto; el corte queda en
// el límite del primer párrafo. rest puede contener todo el capítulo si este es corto.
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

// SmoothTransitionsAction revisa en lote las uniones entre capítulos escritos:
// pasa a la IA el final del capítulo anterior y la apertura del actual; solo reescribe la primera
// frase del capítulo actual cuando la conexión es dura. Cada capítulo se procesa secuencialmente
// y los cambios pueden revertirse porque se guardan por separado.
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
		return fmt.Errorf("no hay suficientes capítulos consecutivos ya escritos para suavizar transiciones")
	}

	logger.InfoKey("log.smooth_start", len(targets))
	optimized := 0
	for n, idx := range targets {
		if ctx.Err() != nil {
			return fmt.Errorf("generación cancelada")
		}
		ch := &state.Chapters[idx]
		logger.StepInfo(n+1, len(targets), fmt.Sprintf("Suavizando transicion del capitulo #%d <%s>...", ch.Num, ch.Title))

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
		return fmt.Errorf("índice de capítulo fuera de rango")
	}

	ch := &state.Chapters[chapterIdx]
	if ch.Content == "" {
		return fmt.Errorf("el capítulo está vacío")
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
		return fmt.Errorf("guardar progreso: %w", err)
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
