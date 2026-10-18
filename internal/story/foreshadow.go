package story

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"showmethestory/internal/config"
	"showmethestory/internal/i18n"
	"showmethestory/internal/llm"
	"showmethestory/internal/sse"
	"strings"
	"time"
)

type ForeshadowSuggestion struct {
	Name          string `json:"name"`
	Description   string `json:"description"`
	PlantChapter  int    `json:"plant_chapter"`
	TargetChapter int    `json:"target_chapter"`
}

type ForeshadowPlanResponse struct {
	Foreshadows []ForeshadowSuggestion `json:"foreshadows"`
}

type ForeshadowUpdateItem struct {
	ID         int    `json:"id"`
	Status     string `json:"status"`
	Event      string `json:"event"`
	Resolution string `json:"resolution"`
}

type ForeshadowUpdateResponse struct {
	Updates []ForeshadowUpdateItem `json:"updates"`
}

func SuggestForeshadows(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, logger *sse.LogBroadcaster) ([]ForeshadowSuggestion, error) {
	lang := cfg.Language
	outline := BuildPlanningHistory(state, state.LongTermDirection, lang)

	userPrompt := config.RenderPrompt(cfg.Prompts.ForeshadowPlanning, map[string]string{
		"Title":         state.Title,
		"CorePrompt":    state.CorePrompt,
		"StorySynopsis": state.LongTermDirection,
		"Outline":       outline,
	})

	systemPrompt := i18n.SystemPromptFor(lang, "narrative_architect_json")

	rawResp := llm.CallAPIWithRetryLog(ctx, apiCfg, systemPrompt, userPrompt, logger)
	if rawResp == "" {
		return nil, fmt.Errorf("API call failed or was canceled")
	}
	rawResp = cleanJSONResponse(rawResp)

	var resp ForeshadowPlanResponse
	if err := json.Unmarshal([]byte(rawResp), &resp); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}

	logger.InfoKey("log.foreshadow_plan_parsed", len(resp.Foreshadows))
	return resp.Foreshadows, nil
}

func UpdateForeshadows(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, chapterIdx int, logger *sse.LogBroadcaster) error {
	ch := state.Chapters[chapterIdx]
	lang := cfg.Language

	foreshadowsText := formatForeshadowsForPromptLang(state.Foreshadows, lang)
	if foreshadowsText == "no" || foreshadowsText == "(none)" {
		return nil
	}

	historySummary := buildHistorySummaryForLang(state, chapterIdx, lang)

	userPrompt := config.RenderPrompt(cfg.Prompts.ForeshadowUpdate, map[string]string{
		"Title":          state.Title,
		"ChapterNum":     fmt.Sprintf("%d", ch.Num),
		"ChapterTitle":   ch.Title,
		"ChapterContent": ch.Content,
		"HistorySummary": historySummary,
		"Foreshadows":    foreshadowsText,
	})

	systemPrompt := i18n.SystemPromptFor(lang, "foreshadow_tracker_json")

	rawResp := llm.CallAPIWithRetryLog(ctx, apiCfg, systemPrompt, userPrompt, logger)
	if rawResp == "" {
		return fmt.Errorf("API call failed or was canceled")
	}
	rawResp = cleanJSONResponse(rawResp)

	var resp ForeshadowUpdateResponse
	if err := json.Unmarshal([]byte(rawResp), &resp); err != nil {
		return fmt.Errorf("parse update JSON: %w", err)
	}

	applyForeshadowUpdates(state, resp.Updates, ch.Num)
	logger.InfoKey("log.foreshadow_status_updated", len(resp.Updates))
	return nil
}

func applyForeshadowUpdates(state *Progress, updates []ForeshadowUpdateItem, chapterNum int) {
	updateMap := make(map[int]ForeshadowUpdateItem)
	for _, u := range updates {
		updateMap[u.ID] = u
	}

	for i := range state.Foreshadows {
		fs := &state.Foreshadows[i]
		u, ok := updateMap[fs.ID]
		if !ok {
			continue
		}

		if u.Event != "" {
			fs.Events = append(fs.Events, ForeshadowEvent{
				Chapter: chapterNum,
				Note:    u.Event,
			})
		}

		if u.Status != "" {
			fs.Status = ForeshadowStatus(u.Status)
		}

		if u.Resolution != "" {
			fs.Resolution = u.Resolution
		}
	}
}

func BuildForeshadowWarnings(state *Progress) string {
	if len(state.Foreshadows) == 0 {
		return ""
	}

	currentChapter := state.CurrentChapterIndex + 1
	var warnings []string

	for _, fs := range state.Foreshadows {
		if fs.Status == ForeshadowResolved || fs.Status == ForeshadowAbandoned {
			continue
		}
		if fs.TargetChapter > 0 && currentChapter > fs.TargetChapter+3 {
			warnings = append(warnings, fmt.Sprintf(" #%d \"%s\" ya superó el capítulo de resolución #%d", fs.ID, fs.Name, fs.TargetChapter))
		}
	}

	if len(warnings) == 0 {
		return ""
	}

	return "⚠️ Hilos sin resolver: " + strings.Join(warnings, "; ")
}

func NextForeshadowID(foreshadows []Foreshadow) int {
	maxID := 0
	for _, fs := range foreshadows {
		if fs.ID > maxID {
			maxID = fs.ID
		}
	}
	return maxID + 1
}

func ForeshadowStatusLabel(status ForeshadowStatus) string {
	switch status {
	case ForeshadowPlanted:
		return "plantado"
	case ForeshadowProgressing:
		return "en progreso"
	case ForeshadowResolved:
		return "resuelto"
	case ForeshadowAbandoned:
		return "abandonado"
	default:
		return string(status)
	}
}

func maxChapterNum(state *Progress) int {
	maxNum := 0
	for _, ch := range state.Chapters {
		if ch.Num > maxNum {
			maxNum = ch.Num
		}
	}
	for _, fs := range state.Foreshadows {
		if fs.PlantChapter > maxNum {
			maxNum = fs.PlantChapter
		}
		if fs.TargetChapter > maxNum {
			maxNum = fs.TargetChapter
		}
		for _, ev := range fs.Events {
			if ev.Chapter > maxNum {
				maxNum = ev.Chapter
			}
		}
	}
	return maxNum
}

// BuildForeshadowRoadmapMarkdown lifeusereadread roadthreadimage Markdown.
func BuildForeshadowRoadmapMarkdown(state *Progress) string {
	title := state.Title
	if title == "" {
		title = "not yetlife-fatesmallsay"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# roadthreadimage — <%s>\n\n", title))
	sb.WriteString(fmt.Sprintf("> morenewhour: %s\n\n", time.Now().Format("2006-01-02 15:04:05")))

	if len(state.Foreshadows) == 0 {
		sb.WriteString("whenfrontno. \n")
		return sb.String()
	}

	active, resolved, abandoned := 0, 0, 0
	for _, fs := range state.Foreshadows {
		switch fs.Status {
		case ForeshadowPlanted, ForeshadowProgressing:
			active++
		case ForeshadowResolved:
			resolved++
		case ForeshadowAbandoned:
			abandoned++
		}
	}

	sb.WriteString("## Resumen\n\n")
	sb.WriteString(fmt.Sprintf("- total **%d** | activos **%d** | resueltos **%d** | abandonados **%d**\n\n", len(state.Foreshadows), active, resolved, abandoned))

	if warn := BuildForeshadowWarnings(state); warn != "" {
		sb.WriteString("## Hilos sin resolver\n\n")
		sb.WriteString(strings.TrimPrefix(warn, "⚠️ Hilos sin resolver: "))
		sb.WriteString("\n\n")
	}

	maxCh := maxChapterNum(state)
	if maxCh > 0 {
		sb.WriteString("## Por capítulo\n\n")
		for chNum := 1; chNum <= maxCh; chNum++ {
			var lines []string
			for _, fs := range state.Foreshadows {
				if fs.PlantChapter == chNum {
					lines = append(lines, fmt.Sprintf("- 🔵 **#%d %s** — plantado (%s)", fs.ID, fs.Name, ForeshadowStatusLabel(fs.Status)))
				}
				if fs.TargetChapter == chNum {
					lines = append(lines, fmt.Sprintf("- 🎯 **#%d %s** — resolución (%s)", fs.ID, fs.Name, ForeshadowStatusLabel(fs.Status)))
				}
				for _, ev := range fs.Events {
					if ev.Chapter == chNum {
						lines = append(lines, fmt.Sprintf("- 📌 **#%d %s** — %s", fs.ID, fs.Name, ev.Note))
					}
				}
			}
			if len(lines) == 0 {
				continue
			}
			sb.WriteString(fmt.Sprintf("### Capítulo #%d\n\n", chNum))
			sb.WriteString(strings.Join(lines, "\n"))
			sb.WriteString("\n\n")
		}
	}

	sb.WriteString("## Detalle de hilos\n\n")
	for _, fs := range state.Foreshadows {
		sb.WriteString(fmt.Sprintf("### #%d %s [%s]\n\n", fs.ID, fs.Name, ForeshadowStatusLabel(fs.Status)))
		sb.WriteString(fmt.Sprintf("%s\n\n", fs.Description))
		sb.WriteString(fmt.Sprintf("- Plantado en: capítulo ** #%d**\n", fs.PlantChapter))
		if fs.TargetChapter > 0 {
			sb.WriteString(fmt.Sprintf("- Resolución prevista: capítulo **#%d**\n", fs.TargetChapter))
		}
		if len(fs.Events) > 0 {
			sb.WriteString("- Progreso:\n")
			for _, ev := range fs.Events {
				sb.WriteString(fmt.Sprintf("  - Capítulo #%d: %s\n", ev.Chapter, ev.Note))
			}
		}
		if fs.Resolution != "" {
			sb.WriteString(fmt.Sprintf("- Resolución: %s\n", fs.Resolution))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// SaveForeshadowRoadmap escribe el mapa de presagios en el archivo Foreshadows.md del proyecto.
func SaveForeshadowRoadmap(projectDir string, state *Progress) error {
	content := BuildForeshadowRoadmapMarkdown(state)
	return os.WriteFile(ForeshadowRoadmapPath(projectDir), []byte(content), 0644)
}

// syncForeshadowsAfterChapter actualiza el estado de los hilos tras un capítulo y reescribe el mapa de presagios.
func syncForeshadowsAfterChapter(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, chapterIdx int, progressPath string, logger *sse.LogBroadcaster) {
	if len(state.Foreshadows) == 0 {
		return
	}
	if err := UpdateForeshadows(ctx, apiCfg, cfg, state, chapterIdx, logger); err != nil {
		logger.WarnKey("log.foreshadow_sync_failed", err)
		return
	}
	active, resolved := 0, 0
	for _, fs := range state.Foreshadows {
		switch fs.Status {
		case ForeshadowPlanted, ForeshadowProgressing:
			active++
		case ForeshadowResolved:
			resolved++
		}
	}
	logger.InfoKey("log.foreshadow_sync_summary", active, resolved)
	if err := SaveForeshadowRoadmap(filepath.Dir(progressPath), state); err != nil {
		logger.WarnKey("log.foreshadow_roadmap_save_failed", err)
	}
	if warn := BuildForeshadowWarnings(state); warn != "" {
		logger.Warn(warn)
	}
}

func buildFullOutlineText(state *Progress, lang string) string {
	return BuildPlanningHistory(state, formatForeshadowsForPromptLang(state.Foreshadows, lang), lang)
}

func buildAcceptedSummariesText(state *Progress, lang string) string {
	return buildHistorySummaryForLang(state, len(state.Chapters), lang)
}

func CheckForeshadowOutlineConsistency(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, logger *sse.LogBroadcaster) (*ForeshadowOutlineReport, error) {
	if len(state.Foreshadows) == 0 {
		return &ForeshadowOutlineReport{Summary: "no"}, nil
	}

	lang := cfg.Language
	userPrompt := config.RenderPrompt(cfg.Prompts.ForeshadowOutlineConsistency, map[string]string{
		"Title":             preferUserValue(cfg.Story.Title, state.Title),
		"Outline":           buildFullOutlineText(state, lang),
		"Foreshadows":       formatForeshadowsForPromptLang(state.Foreshadows, lang),
		"AcceptedSummaries": buildAcceptedSummariesText(state, lang),
	})
	systemPrompt := i18n.SystemPromptFor(lang, "foreshadow_outline_checker_json")

	rawResp := llm.CallAPIWithRetryLog(ctx, apiCfg, systemPrompt, userPrompt, logger)
	if rawResp == "" {
		return nil, fmt.Errorf("API call failed or was canceled")
	}

	var report ForeshadowOutlineReport
	jsonStr := llm.ExtractJSON(cleanJSONResponse(rawResp))
	if jsonStr == "" {
		return nil, fmt.Errorf("no batch result parsed")
	}
	if err := json.Unmarshal([]byte(jsonStr), &report); err != nil {
		return nil, fmt.Errorf("parse batch JSON: %w", err)
	}
	return &report, nil
}

func applyForeshadowOutlineReport(state *Progress, report *ForeshadowOutlineReport) {
	if report == nil {
		return
	}
	state.LastForeshadowOutlineReport = report
}

func RunForeshadowOutlineCheckAndSave(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, progressPath string, logger *sse.LogBroadcaster) error {
	if len(state.Foreshadows) == 0 {
		return nil
	}
	report, err := CheckForeshadowOutlineConsistency(ctx, apiCfg, cfg, state, logger)
	if err != nil {
		logger.WarnKey("log.foreshadow_outline_check_failed", err)
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	before := state.LastForeshadowOutlineReport
	applyForeshadowOutlineReport(state, report)
	if err := SaveProgress(progressPath, state); err != nil {
		state.LastForeshadowOutlineReport = before
		logger.ErrorKey("log.foreshadow_outline_report_save_failed", err)
		return err
	}
	if report.HasConflicts {
		logger.ForeshadowOutlineConflicts(report)
	} else {
		logger.InfoKey("log.foreshadow_outline_check_pass")
	}
	return nil
}
