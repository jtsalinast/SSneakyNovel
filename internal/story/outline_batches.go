package story

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime/debug"
	"showmethestory/internal/config"
	"showmethestory/internal/i18n"
	"showmethestory/internal/sse"
	"strings"
	"sync"
	"time"
)

// OutlineBatch binds an author's synopsis to exactly one range of chapters.
type OutlineBatch struct {
	ID                 int    `json:"id"`
	Revision           int    `json:"revision"`
	StartCh            int    `json:"start_ch"`
	EndCh              int    `json:"end_ch"`
	Synopsis           string `json:"synopsis"`
	EndingIntent       string `json:"ending_intent,omitempty"`
	EndingStyle        string `json:"ending_style,omitempty"`
	EndingRequirements string `json:"ending_requirements,omitempty"`
	PlannedFinal       bool   `json:"planned_final,omitempty"`
}

type OutlineBatchRequest struct {
	ChapterCount       int    `json:"chapter_count"`
	Synopsis           string `json:"outline_synopsis"`
	LongTermDirection  string `json:"long_term_direction"`
	Mode               string `json:"mode"`
	BatchID            int    `json:"batch_id"`
	EndingIntent       string `json:"ending_intent"`
	EndingStyle        string `json:"ending_style"`
	EndingRequirements string `json:"ending_requirements"`
	ConfirmContinue    bool   `json:"confirm_continue"`
}

// ValidateOutlineBatch never mutates the project. Call again inside the task.
func ValidateOutlineBatch(state *Progress, req OutlineBatchRequest, lang string) error {
	if err := validateEnding(state, req, lang); err != nil {
		return err
	}
	key := ""
	switch {
	case strings.TrimSpace(req.Synopsis) == "":
		key = "batch_synopsis_required"
	case req.ChapterCount < 1 || req.ChapterCount > 36:
		key = "batch_count_invalid"
	case req.Mode != "" && req.Mode != "append" && req.Mode != "replace_last":
		key = "batch_mode_invalid"
	case state.BookStatus == BookStatusCompleted:
		key = "batch_book_completed"
	}
	if key != "" {
		return fmt.Errorf("%s", i18n.T(lang, key))
	}
	for _, ch := range state.Chapters {
		if ch.Status == StatusWriting || ch.Status == StatusReview {
			return fmt.Errorf("%s", i18n.T(lang, "batch_chapter_busy"))
		}
	}
	if req.Mode == "replace_last" {
		if len(state.OutlineBatches) == 0 {
			return fmt.Errorf("%s", i18n.T(lang, "batch_replace_invalid"))
		}
		b := state.OutlineBatches[len(state.OutlineBatches)-1]
		count := 0
		for _, ch := range state.Chapters {
			if ch.Num > b.EndCh {
				return fmt.Errorf("%s", i18n.T(lang, "batch_replace_invalid"))
			}
			if ch.Num >= b.StartCh && ch.Num <= b.EndCh {
				count++
				if ch.Status != StatusPending || ch.Content != "" {
					return fmt.Errorf("%s", i18n.T(lang, "batch_replace_invalid"))
				}
			}
		}
		if b.ID != req.BatchID || count != b.EndCh-b.StartCh+1 {
			return fmt.Errorf("%s", i18n.T(lang, "batch_replace_invalid"))
		}
	}
	return nil
}

// maxEndingRequirementsRunes bounds the user-supplied ending requirements that
// are injected verbatim into the batch prompt. An unbounded paste (e.g. a whole
// chapter or an accidental huge text) used to blow up the prompt until the API
// rejected it, and combined with retries this could freeze the machine.
const maxEndingRequirementsRunes = 4000

func truncateToRunes(s string, limit int) string {
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return strings.TrimSpace(string(r[:limit]))
}

func GenerateOutlineBatch(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, settings *ProjectSettings, req OutlineBatchRequest, path string, logger *sse.LogBroadcaster, wg ...*sync.WaitGroup) (err error) {
	// A panic during generation must not leave the HTTP task lock held forever
	// (the UI would show the task as "running" indefinitely). Recover, log the
	// stack for diagnosis, and surface a normal error instead.
	defer func() {
		if rec := recover(); rec != nil {
			devlog := fmt.Sprintf("%v\n%s", rec, debug.Stack())
			if logger != nil {
				logger.ErrorKey("log.outline_batch_panic", fmt.Sprint(rec))
			}
			fmt.Printf("[panic] GenerateOutlineBatch: %s\n", devlog)
			err = fmt.Errorf("internal error while generating the outline batch: %v", rec)
		}
	}()
	if err := ValidateOutlineBatch(state, req, cfg.Language); err != nil {
		return err
	}
	req.EndingRequirements = truncateToRunes(strings.TrimSpace(req.EndingRequirements), maxEndingRequirementsRunes)
	if err := EnsureNarrativeCheckpoints(ctx, apiCfg, cfg, state, path, logger); err != nil {
		return err
	}
	next := *state
	next.Chapters = append([]ChapterState(nil), state.Chapters...)
	next.OutlineBatches = append([]OutlineBatch(nil), state.OutlineBatches...)
	id := 1
	revision := 1
	for _, b := range next.OutlineBatches {
		if b.ID >= id {
			id = b.ID + 1
		}
	}
	if req.Mode == "replace_last" {
		b := next.OutlineBatches[len(next.OutlineBatches)-1]
		id = b.ID
		revision = b.Revision + 1
		next.OutlineBatches = next.OutlineBatches[:len(next.OutlineBatches)-1]
		kept := make([]ChapterState, 0, len(next.Chapters))
		for _, ch := range next.Chapters {
			if ch.Num < b.StartCh {
				kept = append(kept, ch)
			}
		}
		next.Chapters = kept
	}
	start := 1
	for _, ch := range next.Chapters {
		if ch.Num >= start {
			start = ch.Num + 1
		}
	}
	synopsis := strings.TrimSpace(req.Synopsis)
	previous := BuildPlanningHistory(&next, synopsis+"\n"+req.LongTermDirection, cfg.Language)
	selectedSettings, _ := retrieveSettings(settings, synopsis+"\n"+req.LongTermDirection, settingsContextRunes)
	template := cfg.Prompts.ContinuationOutlineGeneration
	// Always add the explicit scope, including for previously saved custom templates.
	template += batchScopeTemplate(cfg.Language)
	template += endingPrompt(OutlineBatch{EndCh: start + req.ChapterCount - 1, EndingIntent: req.EndingIntent, EndingStyle: req.EndingStyle, EndingRequirements: req.EndingRequirements, PlannedFinal: req.EndingIntent == "final" || req.EndingIntent == "sequel"}, 0, cfg.Language)
	if np := NovelParametersBlock(cfg); np != "" {
		template += "\n\n" + np
	}
	data := map[string]string{
		"Title": preferUserValue(cfg.Story.Title, state.Title), "StoryType": cfg.Story.Type,
		"CorePrompt": state.CorePrompt, "StorySynopsis": synopsis, "OutlineSynopsis": synopsis,
		"WritingStyle": cfg.Story.WritingStyle, "WritingPOV": cfg.Story.WritingPOV,
		"ExistingOutline": previous, "NewChapterCount": fmt.Sprint(req.ChapterCount),
		"StartNum": fmt.Sprint(start), "EndNum": fmt.Sprint(start + req.ChapterCount - 1),
		"UserRequirements": synopsis, "LongTermDirection": strings.TrimSpace(req.LongTermDirection),
	}
	needsTitle := start == 1 && preferUserValue(cfg.Story.Title, state.Title) == ""
	var chapters []OutlineChapter
	var generatedTitle string
	for attempt := 0; attempt < 3; attempt++ {
		resp, err := generateOutlineChaptersOnly(ctx, apiCfg, cfg, selectedSettings, template, data, logger)
		if err != nil {
			return err
		}
		chapters = resp.Chapters
		generatedTitle = strings.TrimSpace(resp.Title)
		valid := len(chapters) == req.ChapterCount && (!needsTitle || generatedTitle != "")
		for i, ch := range chapters {
			if ch.Num != start+i {
				valid = false
			}
		}
		if valid {
			break
		}
		if attempt == 2 {
			return fmt.Errorf("%s", i18n.T(cfg.Language, "batch_response_invalid"))
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, ch := range chapters {
		next.Chapters = append(next.Chapters, chapterStateFromOutline(ch, StatusPending))
	}
	for i := range next.OutlineBatches {
		next.OutlineBatches[i].PlannedFinal = false
	}
	next.OutlineBatches = append(next.OutlineBatches, OutlineBatch{ID: id, Revision: revision, StartCh: start, EndCh: start + req.ChapterCount - 1, Synopsis: synopsis, EndingIntent: req.EndingIntent, EndingStyle: req.EndingStyle, EndingRequirements: strings.TrimSpace(req.EndingRequirements), PlannedFinal: req.EndingIntent == "final" || req.EndingIntent == "sequel"})
	next.Title = preferUserValue(cfg.Story.Title, state.Title)
	if start == 1 && next.Title == "" {
		next.Title = generatedTitle
	}
	next.LongTermDirection = strings.TrimSpace(req.LongTermDirection)
	next.Phase = "writing"
	next.BookStatus = BookStatusActive
	if err := SaveProgress(path, &next); err != nil {
		return err
	}
	*state = next
	// Post-process consistency checks (foreshadow + character) run AFTER the
	// batch is saved and reported as done, with a short detached context.
	// Previously they ran inline: two extra LLM calls delayed task completion
	// by minutes and, when the API stalled, made the whole generation appear
	// to hang even though the outlines were already written.
	//
	// The goroutine only *computes* the reports (read-only LLM calls); it does
	// not mutate the shared state nor call SaveProgress. Persisting from a
	// detached background task raced with the user's next action — corrupting
	// rollback journals and writing files into directories being deleted.
	// Instead, the resulting reports are stashed on the live state under the
	// storage mutex and persisted by the next normal save path.
	// Snapshot the state under the storage mutex so the goroutine owns a
	// private copy; other readers never touch it.
	var snapshot Progress
	progressStorageMu.Lock()
	errClone := cloneProgressJSON(&next, &snapshot)
	progressStorageMu.Unlock()
	if errClone != nil {
		logger.WarnKey("log.outline_postprocess_skipped", errClone)
		return nil
	}
	var postWG *sync.WaitGroup
	if len(wg) > 0 {
		postWG = wg[0]
	}
	if postWG != nil {
		postWG.Add(1)
	}
	go func() {
		if postWG != nil {
			defer postWG.Done()
		}
		defer func() {
			if r := recover(); r != nil {
				logger.WarnKey("log.outline_postprocess_panic", fmt.Errorf("%v", r))
				debug.PrintStack()
			}
		}()
		pctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		fs, cr := computeOutlinePostProcessReports(pctx, apiCfg, cfg, &snapshot, settings, logger)
		if fs == nil && cr == nil {
			return
		}
		publishOutlinePostProcessReports(path, fs, cr, logger)
	}()
	return nil
}

// publishOutlinePostProcessReports merges freshly computed consistency reports
// into the persisted progress file. It re-reads from disk (under the storage
// mutex shared with Load/SaveProgress) instead of mutating the live in-memory
// state, so a background task can never race — or corrupt — whatever the user
// is doing next. If the file cannot be read or saved, the reports are simply
// dropped: they are advisory and will be regenerated on the next check.
func publishOutlinePostProcessReports(progressPath string, fs *ForeshadowOutlineReport, cr *OutlineCharacterReport, logger *sse.LogBroadcaster) {
	progressStorageMu.Lock()
	defer progressStorageMu.Unlock()
	current, err := loadProgressUnlocked(progressPath)
	if err != nil {
		logger.WarnKey("log.outline_postprocess_skipped", err)
		return
	}
	if current == nil {
		return
	}
	if fs != nil {
		current.LastForeshadowOutlineReport = fs
	}
	if cr != nil {
		current.LastOutlineCharacterReport = cr
	}
	if err := saveProgressUnlocked(progressPath, current); err != nil {
		logger.WarnKey("log.outline_postprocess_skipped", err)
	}
}

// computeOutlinePostProcessReports runs the two read-only consistency checks
// (foreshadow placement and unregistered characters) against a private copy of
// the state. It never mutates the copy's persisted fields nor saves anything;
// callers decide how to publish the results. Errors are logged but tolerated:
// a failing check simply yields a nil report.
func computeOutlinePostProcessReports(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, stateCopy *Progress, settings *ProjectSettings, logger *sse.LogBroadcaster) (*ForeshadowOutlineReport, *OutlineCharacterReport) {
	var fs *ForeshadowOutlineReport
	var cr *OutlineCharacterReport
	if report, err := CheckForeshadowOutlineConsistency(ctx, apiCfg, cfg, stateCopy, logger); err != nil {
		logger.WarnKey("log.outline_postprocess_skipped", err)
	} else {
		fs = report
	}
	if ctx.Err() != nil {
		return fs, cr
	}
	if report, err := CheckOutlineCharacterConsistency(ctx, apiCfg, cfg, stateCopy, settings, logger); err != nil {
		logger.WarnKey("log.outline_postprocess_skipped", err)
	} else {
		cr = report
	}
	return fs, cr
}

// cloneProgressJSON makes a deep copy of Progress through its JSON encoding,
// so background tasks can own their snapshot without racing the live state.
func cloneProgressJSON(src, dst *Progress) error {
	data, err := json.Marshal(src)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}
