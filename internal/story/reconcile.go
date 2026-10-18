package story

import (
	"context"
	"encoding/json"
	"fmt"
	"showmethestory/internal/config"
	"showmethestory/internal/i18n"
	"showmethestory/internal/llm"
	"showmethestory/internal/sse"
)

type ReconciliationResult struct {
	Type         string `json:"type"`
	WritingStyle string `json:"writing_style"`
	WritingPOV   string `json:"writing_pov"`
	Explanation  string `json:"explanation"`
}

func ReconcileSettingsAction(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress,
	newSettings config.StoryConfig, settings *ProjectSettings, progressPath string, cfgPath string, logger *sse.LogBroadcaster) error {

	logger.StepInfo(1, 3, "Reconciliando capítulos existentes con la nueva configuración...")
	lang := cfg.Language
	if err := EnsureNarrativeCheckpoints(ctx, apiCfg, cfg, state, progressPath, logger); err != nil {
		return err
	}
	acceptedSummaries := BuildPlanningHistory(state, newSettings.Type+"\n"+newSettings.WritingStyle+"\n"+newSettings.WritingPOV, lang)

	userPrompt := config.RenderPrompt(cfg.Prompts.SettingsReconciliation, map[string]string{
		"NewType":           newSettings.Type,
		"NewWritingStyle":   newSettings.WritingStyle,
		"NewWritingPOV":     newSettings.WritingPOV,
		"ExistingSummaries": acceptedSummaries,
	})

	systemPrompt := i18n.SystemPromptFor(lang, "consistency_reviewer_json")

	rawResp := llm.CallAPIWithRetry(ctx, apiCfg, systemPrompt, userPrompt)
	if rawResp == "" {
		return fmt.Errorf("API call failed or was canceled")
	}
	rawResp = cleanJSONResponse(rawResp)

	var result ReconciliationResult
	if err := json.Unmarshal([]byte(rawResp), &result); err != nil {
		return fmt.Errorf("failed to parse reconciliation result JSON: %w\nraw response: %s", err, rawResp)
	}

	logger.StepInfo(2, 3, "Updating affected chapters...")

	adjustedStory := storyConfigFromReconciliation(result, newSettings)
	conflicts := CollectStoryConfigConflicts(newSettings, adjustedStory, "reconcile", result.Explanation)
	pendingPath := PendingConfigChangesPath(progressPath)

	cfg.Story = newSettings
	SyncProgressMetaFromStory(state, newSettings)
	snapshot := newSettings
	state.StoryConfigSnapshot = &snapshot

	hasPending := false
	for _, ch := range state.Chapters {
		if ch.Status == StatusPending {
			hasPending = true
			break
		}
	}

	if hasPending {
		logger.StepInfo(3, 3, "regenerating pending chapter outlines from the new settings...")
		if err := regeneratePendingOutlines(ctx, apiCfg, cfg, state, progressPath, cfgPath, logger); err != nil {
			logger.WarnKey("log.reconcile_pending_outline_failed", err)
		}
	}

	if err := config.SaveConfig(cfgPath, cfg); err != nil {
		return fmt.Errorf("insuredepositplace: %w", err)
	}

	if err := SaveProgress(progressPath, state); err != nil {
		return fmt.Errorf("guardar progreso: %w", err)
	}

	if err := appendPendingChanges(pendingPath, conflicts, logger); err != nil {
		return err
	}

	runOutlinePostProcessChecks(ctx, apiCfg, cfg, state, settings, progressPath, logger)

	logger.SuccessKey("log.reconcile_done_explain" + result.Explanation)

	changedFields := []string{}
	for _, c := range conflicts {
		changedFields = append(changedFields, c.Field)
	}

	logger.SettingsReconciled(map[string]interface{}{
		"explanation":    result.Explanation,
		"changed_fields": changedFields,
	})

	return nil
}

func regeneratePendingOutlines(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, progressPath, cfgPath string, logger *sse.LogBroadcaster) error {
	lang := cfg.Language
	en := i18n.NormalizeLanguage(lang) == i18n.LangEN

	pendingChapters := ""
	for _, ch := range state.Chapters {
		if ch.Status == StatusPending {
			pendingChapters += formatChapterLine(ch.Num, ch.Title, ch.Outline, lang)
		}
	}

	lockedChapters := BuildPlanningHistory(state, pendingChapters, lang)

	var feedback string
	if en {
		feedback = fmt.Sprintf("Story settings updated to: type=%s, writing_style=%s, writing_pov=%s. Adjust the pending chapter outlines so they stay consistent with the new settings and the existing chapters.",
			cfg.Story.Type, cfg.Story.WritingStyle, cfg.Story.WritingPOV)
	} else {
		feedback = fmt.Sprintf("Updated: genre=%s, writing_style=%s, pov=%s. Please adjust the new outline accordingly so it stays consistent with the existing chapters. ",
			cfg.Story.Type, cfg.Story.WritingStyle, cfg.Story.WritingPOV)
	}

	userPrompt := config.RenderPrompt(cfg.Prompts.OutlineRevision, map[string]string{
		"CurrentOutline": pendingChapters,
		"UserFeedback":   feedback,
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

	return applyOutlineRevision(cfg, state, resp, "outline_revision", PendingConfigChangesPath(progressPath), cfgPath, logger)
}
