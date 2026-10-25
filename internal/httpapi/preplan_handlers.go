package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"showmethestory/internal/config"
	"showmethestory/internal/story"
)

// —— Pre-planning: book-level plan above the outline batches ——
//
// PostPrePlanGenerate asks the LLM for a whole-book plan derived from the
// configured Story structure (three-act, hero's journey, six-part, ...):
//   - recommended total chapter count
//   - long-term direction
//   - one card per act with a suggested chapter count for its batch and a
//     ready-to-use "Batch synopsis" text.
//
// The result is stored in Progress.PrePlanning (persisted in progress.json)
// and served by GetPrePlan so the Outline tab can render it above the batch
// planning card. Like the Theme & Motif generators it does NOT require a
// story idea — it is seeded from every novel parameter already entered.

type prePlanActJSON struct {
	Name              string `json:"name"`
	Summary           string `json:"summary"`
	SuggestedChapters int    `json:"suggested_chapters"`
	BatchSynopsis     string `json:"batch_synopsis"`
}

func (h *Handlers) PostPrePlanGenerate(w http.ResponseWriter, r *http.Request) {
	if !h.ensureProject(w, r) {
		return
	}
	if !h.tryStartTask() {
		h.writeErrorReq(w, r, http.StatusConflict, "task_running_wait")
		return
	}
	go func() {
		defer h.endTask()
		h.logger.InfoKey("log.preplan_generating")
		err := h.runPrePlanGenerate(h.taskCtx)
		if err != nil {
			if h.taskCtx.Err() == nil {
				h.logger.ErrorKey("log.preplan_generate_failed", err)
			}
			return
		}
		h.logger.SuccessKey("log.preplan_generate_done")
		h.broadcastProgress()
	}()
	h.writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}

// GetPrePlan returns the stored pre-planning (null when never generated),
// enriched with `act_analyses` so consumers (e.g. the Writing tab's act-critic
// steps inside ChapterSidebarList) can render per-act critique state directly
// from this endpoint — previously that state only lived in /api/progress and
// was lost on a plain page refresh if the progress snapshot lagged behind.
func (h *Handlers) GetPrePlan(w http.ResponseWriter, r *http.Request) {
	if !h.ensureProject(w, r) {
		return
	}
	pp := h.state.PrePlanning
	if pp == nil {
		h.writeJSON(w, http.StatusOK, nil)
		return
	}
	// Fill start_ch/end_ch on the served acts from their persisted
	// suggested_chapters (cumulative walk over the chapter list). Older saves
	// predate these fields and left them empty, which silently hid the
	// act-critique steps inside ChapterSidebarList after a plain F5 — the
	// buttons only appeared once "Refresh UI" re-saved the pre-plan with the
	// ranges computed. Computing here makes GET idempotent-safe: values that
	// are already set are preserved untouched.
	acts := make([]story.PrePlanningAct, len(pp.Acts))
	copy(acts, pp.Acts)
	{
		cursor := 1
		for i := range acts {
			if acts[i].StartCh <= 0 {
				acts[i].StartCh = cursor
			} else {
				cursor = acts[i].StartCh
			}
			n := acts[i].SuggestedChapters
			if n <= 0 {
				n = 1
			}
			end := cursor + n - 1
			if acts[i].EndCh <= 0 || acts[i].EndCh < acts[i].StartCh {
				acts[i].EndCh = end
			}
			cursor = acts[i].EndCh + 1
		}
	}
	resp := map[string]any{
		"generated_at":               pp.GeneratedAt,
		"based_on_structure":         pp.BasedOnStructure,
		"total_chapters":             pp.TotalChapters,
		"recommended_total_chapters": pp.TotalChapters, // legacy alias
		"long_term_direction":        pp.LongTermDirection,
		"acts":                       acts,
		// Act-critique state (keyed by 1-based act number). The Writing tab reads
		// this so the per-act steps in ChapterSidebarList survive a plain F5 even
		// when the progress snapshot has not been re-pulled yet.
		"act_analyses": h.state.ActAnalyses,
	}
	h.writeJSON(w, http.StatusOK, resp)
}

// DeletePrePlan clears the stored pre-planning card.
func (h *Handlers) DeletePrePlan(w http.ResponseWriter, r *http.Request) {
	if !h.ensureProject(w, r) {
		return
	}
	h.state.PrePlanning = nil
	if err := story.SaveProgress(h.progressPath, h.state); err != nil {
		h.writeErrorReq(w, r, http.StatusInternalServerError, "save_failed", err.Error())
		return
	}
	h.broadcastProgress()
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}

func (h *Handlers) runPrePlanGenerate(ctx context.Context) error {
	// Read the freshest on-disk config: inside a chat turn the live form values
	// were just PUT to disk, and the in-memory snapshot can lag (or be nil).
	sc := h.cfg.Story
	cfgLang := h.cfg.Language
	if h.cfgPath != "" {
		if cfg, err := config.LoadConfig(h.cfgPath); err == nil && cfg != nil {
			sc = cfg.Story
			cfgLang = cfg.Language
		}
	}
	return h.runPrePlanGenerateWith(ctx, sc, cfgLang)
}

func (h *Handlers) runPrePlanGenerateWith(ctx context.Context, sc config.StoryConfig, cfgLang string) error {
	h.logger.TaskStart("preplan_generate")
	structure := strings.TrimSpace(sc.Structure)
	if structure == "" {
		structure = config.DefaultStructureForLength(sc.StoryLength)
	}
	structLabel := structureLabel(structure, cfgLang)
	minCh, maxCh := config.SuggestedChaptersByLength(sc.StoryLength)

	var b strings.Builder
	b.WriteString("You are a senior novel architect. Based on the work parameters below, produce a book-level \"pre-planning\" that precedes any outline batch.\n")

	b.WriteString(storyParamsContext(&sc, cfgLang))
	b.WriteString("\n")
	fmt.Fprintf(&b, "Narrative structure framework (MUST drive the act breakdown): %s\nSuggested total-chapter range for this length: %d-%d\n", structLabel, minCh, maxCh)
	b.WriteString("Requirements:\n1) recommended_total_chapters: one concrete integer (usually inside the suggested range) considering structure, length and target words per chapter.\n2) long_term_direction: the book's cross-batch direction (3-6 sentences): protagonist arc, escalation path of the central conflict, ending direction.\n3) acts: split the book strictly according to the structure framework above (e.g. three-act = 3 acts; hero's journey condensed into 4-6 workable stages). Each act has: name, summary (one line about its duty), suggested_chapters (positive integers summing to roughly the total; each act must be <= 36 chapters because one outline batch covers at most 36), and batch_synopsis (a ready-to-paste synopsis for the \"Batch synopsis\" field: key events, escalation and the act-ending turn, 3-5 sentences).\n")
	b.WriteString("All text content must be written in the output language pinned above (Spanish if es, English if en).")

	switch lang := config.NormalizeOutputLanguage(sc.OutputLanguage); lang {
	case "en":
		b.WriteString(" Write everything in English.")
	case "es":
		b.WriteString(" Write everything in Spanish.")
	}
	b.WriteString(jsonRule(cfgLang, `{"recommended_total_chapters": 48, "long_term_direction": "...", "acts": [{"name": "...", "summary": "...", "suggested_chapters": 12, "batch_synopsis": "..."}]}`))

	var out struct {
		RecommendedTotalChapters int              `json:"recommended_total_chapters"`
		LongTermDirection        string           `json:"long_term_direction"`
		Acts                     []prePlanActJSON `json:"acts"`
	}
	if err := h.llmJSON(ctx, b.String(), &out); err != nil {
		return err
	}
	if len(out.Acts) == 0 {
		return errEmptyGeneration
	}

	pp := &story.PrePlanning{
		GeneratedAt:       time.Now().Format(time.RFC3339),
		BasedOnStructure:  structLabel,
		TotalChapters:     out.RecommendedTotalChapters,
		LongTermDirection: strings.TrimSpace(out.LongTermDirection),
	}
	// Sequentially assign chapter ranges from the suggested per-act counts so
	// each card shows exactly which chapters its batch should cover.
	next := 1
	sum := 0
	for _, a := range out.Acts {
		n := a.SuggestedChapters
		if n < 1 {
			n = 1
		}
		if n > 36 {
			n = 36
		}
		act := story.PrePlanningAct{
			Name:              strings.TrimSpace(a.Name),
			Summary:           strings.TrimSpace(a.Summary),
			SuggestedChapters: n,
			StartCh:           next,
			EndCh:             next + n - 1,
			BatchSynopsis:     strings.TrimSpace(a.BatchSynopsis),
		}
		next = act.EndCh + 1
		sum += n
		pp.Acts = append(pp.Acts, act)
	}
	// Keep the recommended total consistent with the acts: trust the LLM's
	// number when it is in the same ballpark as the act sum, otherwise fall
	// back to the sum (and clamp absurd values).
	if pp.TotalChapters <= 0 || pp.TotalChapters*2 < sum || pp.TotalChapters > sum*2 {
		pp.TotalChapters = sum
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	h.state.PrePlanning = pp
	if err := story.SaveProgress(h.progressPath, h.state); err != nil {
		return err
	}
	return nil
}
