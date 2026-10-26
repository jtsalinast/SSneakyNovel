package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"showmethestory/internal/story"
)

// PostOutlineAnalyze runs the bestseller-editor pass over the FULL novel
// outline in one background task. The prompt weighs the Ideal reader profile,
// Inspirational pieces, Tone and Story structure (plus theme/motif, style and
// the rest of the novel parameters), and the report is persisted on Progress
// (state.OutlineAnalysis) so the Outline page can render it point by point.
func (h *Handlers) PostOutlineAnalyze(w http.ResponseWriter, r *http.Request) {
	if !h.ensureProject(w, r) {
		return
	}
	if len(h.state.Chapters) == 0 {
		h.writeErrorReq(w, r, http.StatusBadRequest, "outline_empty")
		return
	}
	if !h.tryStartTask() {
		h.writeErrorReq(w, r, http.StatusConflict, "task_running_wait")
		return
	}
	go func() {
		defer h.endTask()
		// tryStartTask wires a fresh token tracker into h.taskCtx; the LLM call
		// inside AnalyzeFullOutline only reports prompt/completion usage to the
		// frontend badge when that tracker is attached to the context it uses.
		ctx := h.ensureTaskTokens(h.taskCtx)
		h.logger.TaskStart("outline_analyze")
		err := story.AnalyzeFullOutline(ctx, h.apiCfg, h.cfg, h.state, h.settings, h.logger)
		if err == nil {
			err = story.SaveProgress(h.progressPath, h.state)
		}
		if err != nil {
			if ctx.Err() != nil {
				h.logger.WarnKey("log.outline_analyze_cancelled")
			} else {
				h.logger.ErrorKey("log.outline_analyze_failed", err)
			}
			h.logger.TaskEnd("outline_analyze", false)
			return
		}
		h.logger.SuccessKey("log.outline_analyze_done", len(h.state.OutlineAnalysis.Suggestions))
		h.logger.TaskEnd("outline_analyze", true)
		h.broadcastProgress()
	}()
	h.writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}

// PostOutlineAnalysisApply applies ONE analysis point: every replacement
// recommendation attached to that suggestion is written into the matching
// chapters (pending/writing/review only; accepted chapters are never touched)
// and the suggestion is marked applied. Body: {"id":"oa_..."}.
func (h *Handlers) PostOutlineAnalysisApply(w http.ResponseWriter, r *http.Request) {
	if !h.ensureProject(w, r) {
		return
	}
	if h.rejectIfTaskRunning(w, r) {
		return
	}
	if h.state.OutlineAnalysis == nil {
		h.writeErrorReq(w, r, http.StatusNotFound, "no_outline_analysis")
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.ID) == "" {
		h.writeErrorReq(w, r, http.StatusBadRequest, "missing_suggestion_id")
		return
	}
	changed, err := story.ApplyOutlineSuggestion(h.state, strings.TrimSpace(body.ID), h.progressPath)
	if err != nil {
		h.writeErrorReq(w, r, http.StatusConflict, "apply_failed", err)
		return
	}
	// The applied batch may have reshaped chapter casts; register any brand-new
	// characters the editor invented so the character sheet stays consistent
	// with the outline (best-effort: a settings-save failure must not undo the
	// outline changes that were already persisted).
	if h.settings != nil {
		for _, s := range h.state.OutlineAnalysis.Suggestions {
			if s.ID != strings.TrimSpace(body.ID) {
				continue
			}
			if added, regErr := story.RegisterCastCharacters(h.settings, s.Recommendations, h.settingsPath); regErr == nil && added > 0 {
				h.logger.SettingsUpdated()
			}
			break
		}
	}
	h.broadcastProgress()
	h.writeJSON(w, http.StatusOK, map[string]any{
		"status":   "applied",
		"id":       body.ID,
		"chapters": changed,
		"progress": story.SnapshotProgress(h.state),
	})
}

// PostOutlineAnalysisEditRecommendation persists an edit the user made by hand
// in the "Replace: Outline" textarea of a recommendation before applying it.
// Body: {"id":"oa_...","chapter_num":7,"new_outline":"..."}.
func (h *Handlers) PostOutlineAnalysisEditRecommendation(w http.ResponseWriter, r *http.Request) {
	if !h.ensureProject(w, r) {
		return
	}
	if h.state.OutlineAnalysis == nil {
		h.writeErrorReq(w, r, http.StatusNotFound, "no_outline_analysis")
		return
	}
	var body struct {
		ID         string `json:"id"`
		ChapterNum int    `json:"chapter_num"`
		NewOutline string `json:"new_outline"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.ID) == "" {
		h.writeErrorReq(w, r, http.StatusBadRequest, "missing_suggestion_id")
		return
	}
	rep := h.state.OutlineAnalysis
	idx := -1
	for i := range rep.Suggestions {
		if rep.Suggestions[i].ID == body.ID {
			idx = i
			break
		}
	}
	if idx == -1 {
		h.writeErrorReq(w, r, http.StatusNotFound, "suggestion_not_found")
		return
	}
	recs := rep.Suggestions[idx].Recommendations
	found := false
	for j := range recs {
		if recs[j].Present && recs[j].ChapterNum == body.ChapterNum {
			recs[j].NewOutline = body.NewOutline
			found = true
			break
		}
	}
	if !found {
		h.writeErrorReq(w, r, http.StatusNotFound, fmt.Sprintf("recommendation_not_found_ch%d", body.ChapterNum))
		return
	}
	if err := story.SaveProgress(h.progressPath, h.state); err != nil {
		h.writeErrorReq(w, r, http.StatusInternalServerError, "save_failed", err)
		return
	}
	h.broadcastProgress()
	h.writeJSON(w, http.StatusOK, map[string]any{
		"status":   "updated",
		"progress": story.SnapshotProgress(h.state),
	})
}

// PostOutlineAnalysisPropose handles a point WITHOUT automatic replacements:
// it asks the editor to generate concrete outline-level suggestions (what to
// add/change and in which chapter) as ready-to-apply recommendations, then
// returns the updated progress so the frontend can show them with an Apply
// button. Body: {"id":"oa_..."}. Runs as a normal task so token usage is
// tracked and StopTask can cancel it.
func (h *Handlers) PostOutlineAnalysisPropose(w http.ResponseWriter, r *http.Request) {
	if !h.ensureProject(w, r) {
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.ID) == "" {
		h.writeErrorReq(w, r, http.StatusBadRequest, "missing_suggestion_id")
		return
	}
	if h.state.OutlineAnalysis == nil {
		h.writeErrorReq(w, r, http.StatusNotFound, "no_outline_analysis")
		return
	}
	if !h.tryStartTask() {
		h.writeErrorReq(w, r, http.StatusConflict, "task_running_wait")
		return
	}
	defer h.endTask()
	ctx := h.ensureTaskTokens(h.taskCtx)
	h.logger.TaskStart("outline_analyze_propose")
	s, err := story.ProposeOutlineReplacements(ctx, h.apiCfg, h.cfg, h.state, h.settings, strings.TrimSpace(body.ID), h.progressPath, h.settingsPath, h.logger)
	if err != nil {
		h.logger.ErrorKey("log.outline_analyze_failed", err)
		h.logger.TaskEnd("outline_analyze_propose", false)
		status := http.StatusInternalServerError
		code := "propose_failed"
		switch err.Error() {
		case "no_outline_analysis", "suggestion_not_found":
			status = http.StatusNotFound
			code = "suggestion_not_found"
		case "suggestion_already_applied":
			status = http.StatusConflict
		}
		h.writeErrorReq(w, r, status, code, err)
		return
	}
	h.logger.SuccessKey("log.outline_analyze_proposed", len(s.Recommendations))
	h.logger.TaskEnd("outline_analyze_propose", true)
	h.broadcastProgress()
	h.writeJSON(w, http.StatusOK, map[string]any{
		"status":   "proposed",
		"id":       s.ID,
		"progress": story.SnapshotProgress(h.state),
	})
}

// DeleteOutlineAnalysis clears the stored editorial report.
func (h *Handlers) DeleteOutlineAnalysis(w http.ResponseWriter, r *http.Request) {
	if !h.ensureProject(w, r) || h.rejectIfTaskRunning(w, r) {
		return
	}
	h.state.OutlineAnalysis = nil
	if err := story.SaveProgress(h.progressPath, h.state); err != nil {
		h.writeErrorReq(w, r, http.StatusInternalServerError, "save_failed", err)
		return
	}
	h.broadcastProgress()
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}
