package story

import (
	"context"
	"encoding/json"
	"fmt"
	"showmethestory/internal/config"
	"showmethestory/internal/i18n"
	"showmethestory/internal/llm"
	"showmethestory/internal/sse"
	"strings"
)

type WritingConflictError struct {
	Conflict *WritingConflict
}

func (e *WritingConflictError) Error() string {
	if e == nil || e.Conflict == nil {
		return "writing in progress"
	}
	return e.Conflict.Summary
}

type writingConflictAnalysis struct {
	Reconcilable     bool                   `json:"reconcilable"`
	Summary          string                 `json:"summary"`
	RootCause        string                 `json:"root_cause"`
	ExtraConstraints string                 `json:"extra_constraints"`
	SuggestedActions []ConflictActionOption `json:"suggested_actions"`
}

func analyzeWritingConflict(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, idx int, content string, failedIssues []string, logger *sse.LogBroadcaster) (*writingConflictAnalysis, error) {
	ch := state.Chapters[idx]
	lang := cfg.Language

	excerpt := content
	if runes := []rune(excerpt); len(runes) > 1200 {
		excerpt = string(runes[:600]) + "\n...\n" + string(runes[len(runes)-600:])
	}

	foreshadowBlock := formatActiveForeshadowsForChapterLang(state.Foreshadows, ch.Num, lang)
	outlineConstraints := buildOutlineConstraintsForLang(state, idx, lang)

	userPrompt := config.RenderPrompt(cfg.Prompts.WritingConflictAnalysis, map[string]string{
		"ChapterNum":         fmt.Sprintf("%d", ch.Num),
		"ChapterTitle":       ch.Title,
		"ChapterOutline":     ch.Outline,
		"HistorySummary":     buildHistorySummaryForLang(state, idx, lang),
		"OutlineConstraints": outlineConstraints,
		"Foreshadows":        foreshadowBlock,
		"FailedIssues":       strings.Join(failedIssues, "\n"),
		"ContentExcerpt":     excerpt,
	})

	systemPrompt := i18n.SystemPromptFor(lang, "writing_conflict_analyst_json")
	rawResp := llm.CallAPIWithRetryLog(ctx, apiCfg, systemPrompt, userPrompt, logger)
	if rawResp == "" {
		return nil, fmt.Errorf("API call failed or was canceled")
	}

	var analysis writingConflictAnalysis
	jsonStr := llm.ExtractJSON(cleanJSONResponse(rawResp))
	if jsonStr == "" {
		return nil, fmt.Errorf("writing segment split produced no result")
	}
	if err := json.Unmarshal([]byte(jsonStr), &analysis); err != nil {
		return nil, fmt.Errorf("parse writing segment split JSON: %w", err)
	}
	analysis.SuggestedActions = ensureConflictActions(analysis.SuggestedActions, lang)
	return &analysis, nil
}

func ensureConflictActions(actions []ConflictActionOption, lang string) []ConflictActionOption {
	en := i18n.NormalizeLanguage(lang) == i18n.LangEN
	byID := map[string]ConflictActionOption{}
	for _, a := range actions {
		if a.ID != "" {
			byID[a.ID] = a
		}
	}
	defaults := []ConflictActionOption{
		{ID: "edit_outline", Label: "Edit chapter outline", Description: "Adjust this or later chapter outlines on the Outline page"},
		{ID: "adjust_foreshadow", Label: "Adjust foreshadows", Description: "Change plant/payoff chapters or descriptions on the Foreshadows page, or abandon unworkable foreshadows"},
		{ID: "retry", Label: "Retry after edits", Description: "Regenerate this chapter after you have edited the outline or foreshadows"},
		{ID: "force_review", Label: "Keep draft for review", Description: "Accept the current draft and review it manually"},
	}
	_ = en
	out := make([]ConflictActionOption, 0, len(defaults))
	for _, d := range defaults {
		if a, ok := byID[d.ID]; ok {
			if a.Label == "" {
				a.Label = d.Label
			}
			if a.Description == "" {
				a.Description = d.Description
			}
			out = append(out, a)
		} else if d.ID == "retry" || d.ID == "force_review" || d.ID == "edit_outline" || d.ID == "adjust_foreshadow" {
			out = append(out, d)
		}
	}
	return out
}

func buildWritingConflict(state *Progress, idx int, failedIssues []string, analysis *writingConflictAnalysis) *WritingConflict {
	ch := state.Chapters[idx]
	summary := "multitimes, squaredirection"
	rootCause := "other"
	reconcilable := false
	actions := ensureConflictActions(nil, i18n.LangEN)
	if analysis != nil {
		if strings.TrimSpace(analysis.Summary) != "" {
			summary = strings.TrimSpace(analysis.Summary)
		}
		rootCause = analysis.RootCause
		reconcilable = analysis.Reconcilable
		if len(analysis.SuggestedActions) > 0 {
			actions = analysis.SuggestedActions
		}
	}
	return &WritingConflict{
		ChapterIndex:     idx,
		ChapterNum:       ch.Num,
		ChapterTitle:     ch.Title,
		Issues:           failedIssues,
		Summary:          summary,
		RootCause:        rootCause,
		Reconcilable:     reconcilable,
		SuggestedActions: actions,
	}
}

func mergeUniqueIssues(issues ...[]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, list := range issues {
		for _, item := range list {
			item = strings.TrimSpace(item)
			if item == "" || seen[item] {
				continue
			}
			seen[item] = true
			out = append(out, item)
		}
	}
	return out
}

func splitFactCheckIssues(issues string) []string {
	if strings.TrimSpace(issues) == "" {
		return nil
	}
	parts := strings.Split(issues, "; ")
	if len(parts) == 1 {
		parts = strings.Split(issues, ";")
	}
	return mergeUniqueIssues(parts)
}

// ResolveForceReviewIndex picks the chapter to promote writing→review:
// pending conflict chapter if set, otherwise the writing frontier chapter.
func ResolveForceReviewIndex(state *Progress) (int, error) {
	if state == nil {
		return -1, fmt.Errorf("noadvancedegreeposture")
	}
	if c := state.PendingWritingConflict; c != nil {
		if c.ChapterIndex < 0 || c.ChapterIndex >= len(state.Chapters) {
			return -1, fmt.Errorf("chaptersectioncordno")
		}
		return c.ChapterIndex, nil
	}
	i := state.CurrentChapterIndex
	if i >= 0 && i < len(state.Chapters) && state.Chapters[i].Status == StatusWriting {
		return i, nil
	}
	return -1, fmt.Errorf("there is already a confirmed chapter ready to advance into the writing stage")
}

// PromoteWritingToReview moves the chapter at idx to review and clears any pending conflict.
func PromoteWritingToReview(state *Progress, idx int) error {
	if state == nil || idx < 0 || idx >= len(state.Chapters) {
		return fmt.Errorf("chaptersectioncordno")
	}
	state.Chapters[idx].Status = StatusReview
	state.PendingWritingConflict = nil
	return nil
}
