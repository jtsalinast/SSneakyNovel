package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"showmethestory/internal/story"
)

// ---------------------------------------------------------------------------
// Scene-plan bestseller critique handlers — siblings of the outline analysis
// ones (outline_analysis_handlers.go), aimed at the pending per-chapter beat
// sheets. Advice-only first pass; concrete scene replacements are drafted on
// demand via "Propose", applied explicitly via "Apply".
// ---------------------------------------------------------------------------

// PostScenePlanAnalyze runs the bestseller-editor pass over all PENDING
// chapter scene plans in one background task; report persisted on
// state.ScenePlanAnalysis so the Writing page / plan editor can render it.
func (h *Handlers) PostScenePlanAnalyze(w http.ResponseWriter, r *http.Request) {
	if !h.ensureProject(w, r) {
		return
	}
	planned := 0
	for _, ch := range h.state.Chapters {
		if ch.Status != story.StatusAccepted && ch.ScenePlan != nil && len(ch.ScenePlan.Scenes) > 0 {
			planned++
		}
	}
	if planned == 0 {
		h.writeErrorReq(w, r, http.StatusBadRequest, "scene_plans_empty")
		return
	}
	if !h.tryStartTask() {
		h.writeErrorReq(w, r, http.StatusConflict, "task_running_wait")
		return
	}
	go func() {
		defer h.endTask()
		ctx := h.ensureTaskTokens(h.taskCtx)
		h.logger.TaskStart("sceneplan_analyze")
		err := story.AnalyzePendingScenePlans(ctx, h.apiCfg, h.cfg, h.state, h.settings, h.logger)
		if err == nil {
			err = story.SaveProgress(h.progressPath, h.state)
		}
		if err != nil {
			if ctx.Err() != nil {
				h.logger.WarnKey("log.sceneplan_analyze_cancelled")
			} else {
				h.logger.ErrorKey("log.sceneplan_analyze_failed", err)
			}
			h.logger.TaskEnd("sceneplan_analyze", false)
			return
		}
		h.logger.SuccessKey("log.sceneplan_analyze_done", len(h.state.ScenePlanAnalysis.Suggestions))
		h.logger.TaskEnd("sceneplan_analyze", true)
		h.broadcastProgress()
	}()
	h.writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}

// PostScenePlanAnalysisPropose asks the editor to draft concrete per-scene
// replacements for one advice-only point. Body: {"id":"spa_..."}.
func (h *Handlers) PostScenePlanAnalysisPropose(w http.ResponseWriter, r *http.Request) {
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
	if h.state.ScenePlanAnalysis == nil {
		h.writeErrorReq(w, r, http.StatusNotFound, "no_scene_plan_analysis")
		return
	}
	if !h.tryStartTask() {
		h.writeErrorReq(w, r, http.StatusConflict, "task_running_wait")
		return
	}
	defer h.endTask()
	ctx := h.ensureTaskTokens(h.taskCtx)
	h.logger.TaskStart("sceneplan_analyze_propose")
	s, err := story.ProposeScenePlanReplacements(ctx, h.apiCfg, h.cfg, h.state, h.settings, strings.TrimSpace(body.ID), h.progressPath, h.logger)
	if err != nil {
		h.logger.ErrorKey("log.sceneplan_analyze_failed", err)
		h.logger.TaskEnd("sceneplan_analyze_propose", false)
		status := http.StatusInternalServerError
		code := "propose_failed"
		switch err.Error() {
		case "no_scene_plan_analysis", "suggestion_not_found":
			status = http.StatusNotFound
			code = "suggestion_not_found"
		case "suggestion_already_applied":
			status = http.StatusConflict
		}
		h.writeErrorReq(w, r, status, code, err)
		return
	}
	h.logger.SuccessKey("log.sceneplan_analyze_proposed", len(s.Recommendations))
	h.logger.TaskEnd("sceneplan_analyze_propose", true)
	h.broadcastProgress()
	h.writeJSON(w, http.StatusOK, map[string]any{
		"status":   "proposed",
		"id":       s.ID,
		"progress": story.ProgressView(h.state),
	})
}

// PostScenePlanAnalysisApply applies every proposed replacement of one
// scene-plan point to the live beat sheets (non-accepted chapters only).
// Body: {"id":"spa_..."}.
func (h *Handlers) PostScenePlanAnalysisApply(w http.ResponseWriter, r *http.Request) {
	if !h.ensureProject(w, r) {
		return
	}
	if h.rejectIfTaskRunning(w, r) {
		return
	}
	if h.state.ScenePlanAnalysis == nil {
		h.writeErrorReq(w, r, http.StatusNotFound, "no_scene_plan_analysis")
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.ID) == "" {
		h.writeErrorReq(w, r, http.StatusBadRequest, "missing_suggestion_id")
		return
	}
	changed, err := story.ApplyScenePlanSuggestion(h.state, strings.TrimSpace(body.ID), h.progressPath)
	if err != nil {
		h.writeErrorReq(w, r, http.StatusConflict, "apply_failed", err)
		return
	}
	h.logger.SuccessKey("log.sceneplan_analysis_applied", changed)
	h.broadcastProgress()
	h.writeJSON(w, http.StatusOK, map[string]any{
		"status":   "applied",
		"id":       body.ID,
		"scenes":   changed,
		"progress": story.ProgressView(h.state),
	})
}

// PostScenePlanAnalysisEditRecommendation persists a manual edit made in one
// accordion field of a proposed scene replacement before applying it.
// Body: {"id":"spa_...","chapter_num":7,"scene_id":2,"field":"new_description","value":"..."}.
func (h *Handlers) PostScenePlanAnalysisEditRecommendation(w http.ResponseWriter, r *http.Request) {
	if !h.ensureProject(w, r) {
		return
	}
	if h.state.ScenePlanAnalysis == nil {
		h.writeErrorReq(w, r, http.StatusNotFound, "no_scene_plan_analysis")
		return
	}
	var body struct {
		ID             string `json:"id"`
		ChapterNum     int    `json:"chapter_num"`
		SceneID        int    `json:"scene_id"`
		Field          string `json:"field"`
		Value          string `json:"value"`
		MergeIntoScene int    `json:"merge_into_scene_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.ID) == "" {
		h.writeErrorReq(w, r, http.StatusBadRequest, "missing_suggestion_id")
		return
	}
	if err := story.EditScenePlanRecommendation(h.state, body.ID, body.ChapterNum, body.SceneID, body.Field, body.Value, body.MergeIntoScene, h.progressPath); err != nil {
		status := http.StatusInternalServerError
		code := "edit_failed"
		switch err.Error() {
		case "suggestion_not_found":
			status = http.StatusNotFound
			code = "suggestion_not_found"
		case "recommendation_not_found":
			status = http.StatusNotFound
			code = "recommendation_not_found"
		case "unknown_field":
			status = http.StatusBadRequest
			code = "unknown_field"
		}
		h.writeErrorReq(w, r, status, code, err)
		return
	}
	h.broadcastProgress()
	h.writeJSON(w, http.StatusOK, map[string]any{
		"status":   "updated",
		"progress": story.ProgressView(h.state),
	})
}

// DeleteScenePlanAnalysis clears the stored scene-plan editorial report.
func (h *Handlers) DeleteScenePlanAnalysis(w http.ResponseWriter, r *http.Request) {
	if !h.ensureProject(w, r) || h.rejectIfTaskRunning(w, r) {
		return
	}
	h.state.ScenePlanAnalysis = nil
	if err := story.SaveProgress(h.progressPath, h.state); err != nil {
		h.writeErrorReq(w, r, http.StatusInternalServerError, "save_failed", err)
		return
	}
	h.logger.InfoKey("log.sceneplan_analysis_cleared")
	h.broadcastProgress()
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}

// ---------------------------------------------------------------------------
// Act bestseller critique handlers — triggered automatically by the writing
// pipeline when an act's last chapter is confirmed, and re-runnable here.
// ---------------------------------------------------------------------------

// actAnalyzeTarget resolves {"act_no":N} or ?act=N against the pre-planning.
func (h *Handlers) actAnalyzeTarget(w http.ResponseWriter, r *http.Request, bodyActNo int) (int, bool) {
	actNo := bodyActNo
	if actNo <= 0 {
		if v := r.URL.Query().Get("act"); v != "" {
			actNo, _ = strconv.Atoi(v)
		}
	}
	if actNo <= 0 {
		// Default to the most recently completed act when the UI omits it.
		if len(h.state.ActAnalyses) > 0 {
			best := 0
			for k := range h.state.ActAnalyses {
				if k > best {
					best = k
				}
			}
			actNo = best
		}
	}
	if h.state.PrePlanning == nil || actNo < 1 || actNo > len(h.state.PrePlanning.Acts) {
		h.writeErrorReq(w, r, http.StatusNotFound, "act_not_found")
		return 0, false
	}
	return actNo, true
}

// PostActAnalyze runs the bestseller-editor pass over ONE pre-planning act
// whose chapters have been drafted. Body: {"act_no":2} (optional).
func (h *Handlers) PostActAnalyze(w http.ResponseWriter, r *http.Request) {
	if !h.ensureProject(w, r) {
		return
	}
	var body struct {
		ActNo int `json:"act_no"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	actNo, ok := h.actAnalyzeTarget(w, r, body.ActNo)
	if !ok {
		return
	}
	if !h.tryStartTask() {
		h.writeErrorReq(w, r, http.StatusConflict, "task_running_wait")
		return
	}
	go func() {
		defer h.endTask()
		ctx := h.ensureTaskTokens(h.taskCtx)
		h.logger.TaskStart("act_analyze")
		err := story.AnalyzeFinishedAct(ctx, h.apiCfg, h.cfg, h.state, h.settings, actNo, h.progressPath, h.logger)
		if err == nil {
			err = story.SaveProgress(h.progressPath, h.state)
		}
		if err != nil {
			if ctx.Err() != nil {
				h.logger.WarnKey("log.act_analyze_cancelled")
			} else {
				h.logger.ErrorKey("log.act_analyze_failed", err)
			}
			h.logger.TaskEnd("act_analyze", false)
			return
		}
		h.logger.SuccessKey("log.act_analyze_done", len(h.state.ActAnalyses[actNo].Suggestions))
		h.logger.TaskEnd("act_analyze", true)
		h.broadcastProgress()
	}()
	h.writeJSON(w, http.StatusAccepted, map[string]any{"status": "started", "act_no": actNo})
}

// PostActInterventionsPropose drafts small trope interventions for one
// advice-only act point. Body: {"act_no":2,"id":"acta_..."}.
func (h *Handlers) PostActInterventionsPropose(w http.ResponseWriter, r *http.Request) {
	if !h.ensureProject(w, r) {
		return
	}
	var body struct {
		ActNo int    `json:"act_no"`
		ID    string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.ID) == "" {
		h.writeErrorReq(w, r, http.StatusBadRequest, "missing_suggestion_id")
		return
	}
	actNo, ok := h.actAnalyzeTarget(w, r, body.ActNo)
	if !ok {
		return
	}
	if h.state.ActAnalyses == nil || h.state.ActAnalyses[actNo] == nil {
		h.writeErrorReq(w, r, http.StatusNotFound, "no_act_analysis")
		return
	}
	if !h.tryStartTask() {
		h.writeErrorReq(w, r, http.StatusConflict, "task_running_wait")
		return
	}
	defer h.endTask()
	ctx := h.ensureTaskTokens(h.taskCtx)
	h.logger.TaskStart("act_analyze_propose")
	s, err := story.ProposeActInterventions(ctx, h.apiCfg, h.cfg, h.state, h.settings, actNo, strings.TrimSpace(body.ID), h.progressPath, h.logger)
	if err != nil {
		h.logger.ErrorKey("log.act_analyze_failed", err)
		h.logger.TaskEnd("act_analyze_propose", false)
		status := http.StatusInternalServerError
		code := "propose_failed"
		switch err.Error() {
		case "no_act_analysis", "suggestion_not_found":
			status = http.StatusNotFound
			code = "suggestion_not_found"
		case "suggestion_already_applied":
			status = http.StatusConflict
		}
		h.writeErrorReq(w, r, status, code, err)
		return
	}
	h.logger.SuccessKey("log.act_analyze_proposed", len(s.Interventions))
	h.logger.TaskEnd("act_analyze_propose", true)
	h.broadcastProgress()
	h.writeJSON(w, http.StatusOK, map[string]any{
		"status":   "proposed",
		"id":       s.ID,
		"progress": story.ProgressView(h.state),
	})
}

// PostActInterventionsApply executes every pending trope intervention of one
// act suggestion through the surgical rewrite channel. Body:
// {"act_no":2,"id":"acta_..."}.
func (h *Handlers) PostActInterventionsApply(w http.ResponseWriter, r *http.Request) {
	if !h.ensureProject(w, r) {
		return
	}
	var body struct {
		ActNo int    `json:"act_no"`
		ID    string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.ID) == "" {
		h.writeErrorReq(w, r, http.StatusBadRequest, "missing_suggestion_id")
		return
	}
	actNo, ok := h.actAnalyzeTarget(w, r, body.ActNo)
	if !ok {
		return
	}
	if h.state.ActAnalyses == nil || h.state.ActAnalyses[actNo] == nil {
		h.writeErrorReq(w, r, http.StatusNotFound, "no_act_analysis")
		return
	}
	if !h.tryStartTask() {
		h.writeErrorReq(w, r, http.StatusConflict, "task_running_wait")
		return
	}
	defer h.endTask()
	ctx := h.ensureTaskTokens(h.taskCtx)
	h.logger.TaskStart("act_interventions_apply")
	applied, err := story.ApplyActInterventions(ctx, h.apiCfg, h.cfg, h.state, h.settings, actNo, strings.TrimSpace(body.ID), h.progressPath, h.logger)
	if err != nil {
		h.logger.TaskEnd("act_interventions_apply", false)
		status := http.StatusInternalServerError
		code := "apply_failed"
		switch err.Error() {
		case "no_act_analysis", "suggestion_not_found":
			status = http.StatusNotFound
		case "no_pending_interventions":
			status = http.StatusConflict
		}
		h.writeErrorReq(w, r, status, code, err)
		return
	}
	h.logger.TaskEnd("act_interventions_apply", true)
	h.broadcastProgress()
	h.writeJSON(w, http.StatusOK, map[string]any{
		"status":      "applied",
		"id":          body.ID,
		"interventions": applied,
		"progress":    story.ProgressView(h.state),
	})
}

// DeleteActAnalysis clears one stored act report (?act=N) or all of them.
func (h *Handlers) DeleteActAnalysis(w http.ResponseWriter, r *http.Request) {
	if !h.ensureProject(w, r) || h.rejectIfTaskRunning(w, r) {
		return
	}
	if v := r.URL.Query().Get("act"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_chapter_num", v)
			return
		}
		delete(h.state.ActAnalyses, n)
	} else {
		h.state.ActAnalyses = nil
	}
	if err := story.SaveProgress(h.progressPath, h.state); err != nil {
		h.writeErrorReq(w, r, http.StatusInternalServerError, "save_failed", err)
		return
	}
	h.logger.InfoKey("log.act_analysis_cleared")
	h.broadcastProgress()
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}

// ---------------------------------------------------------------------------
// Chapter bestseller critique handlers — same editorial frame as the outline
// pass, scoped to ONE drafted chapter, with trope-unit interventions.
// ---------------------------------------------------------------------------

// chapterAnalyzeTarget resolves {num} from the URL to a real chapter index.
func (h *Handlers) chapterAnalyzeTarget(w http.ResponseWriter, r *http.Request) (int, bool) {
	numStr := r.PathValue("num")
	num, err := strconv.Atoi(numStr)
	if err != nil {
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_chapter_num", numStr)
		return 0, false
	}
	for i := range h.state.Chapters {
		if h.state.Chapters[i].Num == num {
			return num, true
		}
	}
	h.writeErrorReq(w, r, http.StatusNotFound, "chapter_not_found")
	return 0, false
}

// PostChapterAnalyze runs the bestseller-editor pass over ONE drafted
// chapter; report persisted on that chapter's Analysis field.
func (h *Handlers) PostChapterAnalyze(w http.ResponseWriter, r *http.Request) {
	if !h.ensureProject(w, r) {
		return
	}
	num, ok := h.chapterAnalyzeTarget(w, r)
	if !ok {
		return
	}
	if !h.tryStartTask() {
		h.writeErrorReq(w, r, http.StatusConflict, "task_running_wait")
		return
	}
	go func() {
		defer h.endTask()
		ctx := h.ensureTaskTokens(h.taskCtx)
		h.logger.TaskStart("chapter_analyze")
		err := story.AnalyzeDraftedChapter(ctx, h.apiCfg, h.cfg, h.state, h.settings, num, h.progressPath, h.logger)
		if err == nil {
			err = story.SaveProgress(h.progressPath, h.state)
		}
		if err != nil {
			if ctx.Err() != nil {
				h.logger.WarnKey("log.chapter_analyze_cancelled")
			} else {
				h.logger.ErrorKey("log.chapter_analyze_failed", err)
			}
			h.logger.TaskEnd("chapter_analyze", false)
			return
		}
		sugCount := 0
		for i := range h.state.Chapters {
			if h.state.Chapters[i].Num == num && h.state.Chapters[i].Analysis != nil {
				sugCount = len(h.state.Chapters[i].Analysis.Suggestions)
			}
		}
		h.logger.SuccessKey("log.chapter_analyze_done", sugCount)
		h.logger.TaskEnd("chapter_analyze", true)
		h.broadcastProgress()
	}()
	h.writeJSON(w, http.StatusAccepted, map[string]any{"status": "started", "chapter_num": num})
}

// PostChapterInterventionsPropose drafts concrete trope interventions for one
// advice-only chapter point. Body: {"id":"chpa_..."}.
func (h *Handlers) PostChapterInterventionsPropose(w http.ResponseWriter, r *http.Request) {
	if !h.ensureProject(w, r) {
		return
	}
	num, ok := h.chapterAnalyzeTarget(w, r)
	if !ok {
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.ID) == "" {
		h.writeErrorReq(w, r, http.StatusBadRequest, "missing_suggestion_id")
		return
	}
	if !h.tryStartTask() {
		h.writeErrorReq(w, r, http.StatusConflict, "task_running_wait")
		return
	}
	defer h.endTask()
	ctx := h.ensureTaskTokens(h.taskCtx)
	h.logger.TaskStart("chapter_analyze_propose")
	s, err := story.ProposeChapterInterventions(ctx, h.apiCfg, h.cfg, h.state, h.settings, num, strings.TrimSpace(body.ID), h.progressPath, h.logger)
	if err != nil {
		h.logger.ErrorKey("log.chapter_analyze_failed", err)
		h.logger.TaskEnd("chapter_analyze_propose", false)
		status := http.StatusInternalServerError
		code := "propose_failed"
		switch err.Error() {
		case "no_chapter_analysis", "suggestion_not_found":
			status = http.StatusNotFound
			code = "suggestion_not_found"
		case "suggestion_already_applied":
			status = http.StatusConflict
		}
		h.writeErrorReq(w, r, status, code, err)
		return
	}
	h.logger.SuccessKey("log.chapter_analyze_proposed", len(s.Interventions))
	h.logger.TaskEnd("chapter_analyze_propose", true)
	h.broadcastProgress()
	h.writeJSON(w, http.StatusOK, map[string]any{
		"status":   "proposed",
		"id":       s.ID,
		"progress": story.ProgressView(h.state),
	})
}

// PostChapterInterventionsApply executes every pending trope intervention of
// one chapter suggestion. Body: {"id":"chpa_..."}.
func (h *Handlers) PostChapterInterventionsApply(w http.ResponseWriter, r *http.Request) {
	if !h.ensureProject(w, r) {
		return
	}
	num, ok := h.chapterAnalyzeTarget(w, r)
	if !ok {
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.ID) == "" {
		h.writeErrorReq(w, r, http.StatusBadRequest, "missing_suggestion_id")
		return
	}
	if !h.tryStartTask() {
		h.writeErrorReq(w, r, http.StatusConflict, "task_running_wait")
		return
	}
	defer h.endTask()
	ctx := h.ensureTaskTokens(h.taskCtx)
	h.logger.TaskStart("chapter_interventions_apply")
	applied, err := story.ApplyChapterInterventions(ctx, h.apiCfg, h.cfg, h.state, h.settings, num, strings.TrimSpace(body.ID), h.progressPath, h.logger)
	if err != nil {
		h.logger.TaskEnd("chapter_interventions_apply", false)
		status := http.StatusInternalServerError
		code := "apply_failed"
		switch err.Error() {
		case "no_chapter_analysis", "suggestion_not_found":
			status = http.StatusNotFound
		case "no_pending_interventions":
			status = http.StatusConflict
		}
		h.writeErrorReq(w, r, status, code, err)
		return
	}
	h.logger.TaskEnd("chapter_interventions_apply", true)
	h.broadcastProgress()
	h.writeJSON(w, http.StatusOK, map[string]any{
		"status":        "applied",
		"id":            body.ID,
		"interventions": applied,
		"progress":      story.ProgressView(h.state),
	})
}

// DeleteChapterAnalysis clears one chapter's stored editorial report.
func (h *Handlers) DeleteChapterAnalysis(w http.ResponseWriter, r *http.Request) {
	if !h.ensureProject(w, r) || h.rejectIfTaskRunning(w, r) {
		return
	}
	num, ok := h.chapterAnalyzeTarget(w, r)
	if !ok {
		return
	}
	for i := range h.state.Chapters {
		if h.state.Chapters[i].Num == num {
			h.state.Chapters[i].Analysis = nil
		}
	}
	if err := story.SaveProgress(h.progressPath, h.state); err != nil {
		h.writeErrorReq(w, r, http.StatusInternalServerError, "save_failed", err)
		return
	}
	h.broadcastProgress()
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}

// maybeAnalyzeCompletedAct launches the act bestseller-critique in the
// background when confirming chapter num closed a pre-planning act. Called
// from the confirm paths right after ConfirmChapterAction succeeds — but NOT
// while a task is already running (the auto-write loop owns the pipeline for
// the whole batch): in that case the critique is deferred and triggered by
// maybeAnalyzeCompletedActIfIdle at the end of the batch. Fire-and-forget:
// failures only log; the author can re-run POST /api/acts/analyze.
func (h *Handlers) maybeAnalyzeCompletedAct(num int) {
	if h.isTaskRunning() {
		return // deferred: PostAutoWriteStop / the batch tail trigger it
	}
	h.startActAnalysisForChapter(num)
}

// maybeAnalyzeCompletedActIfIdle checks every act whose last chapter has
// been confirmed since the previous run and launches the critique for the
// first one lacking a report. Used at the tail of the auto-write batch so a
// completed act never waits un-analyzed.
func (h *Handlers) maybeAnalyzeCompletedActIfIdle() {
	if h.state.PrePlanning == nil || h.isTaskRunning() {
		return
	}
	for i := range h.state.Chapters {
		ch := h.state.Chapters[i]
		if ch.Status != story.StatusAccepted {
			continue
		}
		actNo, ok := story.CompletedActForChapter(h.state, ch.Num)
		if !ok {
			continue
		}
		if h.state.ActAnalyses != nil && h.state.ActAnalyses[actNo] != nil {
			continue // already critiqued this act's current draft
		}
		h.startActAnalysis(actNo)
		return
	}
}

func (h *Handlers) startActAnalysisForChapter(num int) {
	actNo, ok := story.CompletedActForChapter(h.state, num)
	if !ok {
		return
	}
	h.startActAnalysis(actNo)
}

func (h *Handlers) startActAnalysis(actNo int) {
	if !h.tryStartTask() {
		return // another task owns the pipeline; skip silently
	}
	go func() {
		defer h.endTask()
		ctx := h.ensureTaskTokens(h.taskCtx)
		h.logger.TaskStart("act_analyze")
		err := story.AnalyzeFinishedAct(ctx, h.apiCfg, h.cfg, h.state, h.settings, actNo, h.progressPath, h.logger)
		if err == nil {
			err = story.SaveProgress(h.progressPath, h.state)
		}
		if err != nil {
			if ctx.Err() != nil {
				h.logger.WarnKey("log.act_analyze_cancelled")
			} else {
				h.logger.WarnKey("log.act_analyze_failed", err)
			}
			h.logger.TaskEnd("act_analyze", false)
			return
		}
		h.logger.SuccessKey("log.act_analyze_done", len(h.state.ActAnalyses[actNo].Suggestions))
		h.logger.TaskEnd("act_analyze", true)
		h.broadcastProgress()
	}()
}
