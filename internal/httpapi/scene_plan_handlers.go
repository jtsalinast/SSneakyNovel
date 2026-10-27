package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"

	"showmethestory/internal/story"
)

// —— Scene plan (editable chapter beat sheet) ——

// resolveScenePlanChapter parses {num} and locates the chapter in state.
func (h *Handlers) resolveScenePlanChapter(w http.ResponseWriter, r *http.Request) (int, bool) {
	var num int
	if _, err := fmt.Sscanf(r.PathValue("num"), "%d", &num); err != nil {
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_chapter_num")
		return 0, false
	}
	if story.FindChapterIdx(h.state, num) < 0 {
		h.writeErrorReq(w, r, http.StatusNotFound, "chapter_n_not_found", num)
		return 0, false
	}
	return num, true
}

// GetScenePlan returns the current scene plan of a chapter plus the auto
// switch, so the UI can render the editable table (or an empty state).
func (h *Handlers) GetScenePlan(w http.ResponseWriter, r *http.Request) {
	num, ok := h.resolveScenePlanChapter(w, r)
	if !ok {
		return
	}
	plan, _, err := story.GetScenePlan(h.state, num)
	if err != nil {
		h.writeErrorReq(w, r, http.StatusInternalServerError, "scene_plan_load_failed", err)
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"chapter_num":     num,
		"plan":            plan,
		"auto_scene_plan": h.state.AutoScenePlan,
	})
}

// PutScenePlan replaces the chapter's scene list with the author's edits
// (add/remove/reorder scenes, tweak word budgets). Any edit un-approves the
// plan; the author must approve it again before writing resumes.
func (h *Handlers) PutScenePlan(w http.ResponseWriter, r *http.Request) {
	if h.isTaskRunning() {
		h.writeErrorReq(w, r, http.StatusConflict, "task_running_wait")
		return
	}
	num, ok := h.resolveScenePlanChapter(w, r)
	if !ok {
		return
	}
	var body struct {
		Scenes      []story.Scene `json:"scenes"`
		KeepBudgets *bool         `json:"keep_budgets,omitempty"`
		Approve     bool          `json:"approve,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	keep := true // author edits are authoritative by default
	if body.KeepBudgets != nil {
		keep = *body.KeepBudgets
	}
	plan, err := story.SetScenePlanScenes(h.state, h.cfg, num, body.Scenes, keep, h.progressPath)
	if err != nil {
		h.writeErrorReq(w, r, http.StatusBadRequest, "scene_plan_save_failed", err)
		return
	}
	if body.Approve {
		plan, err = story.ApproveScenePlan(h.state, num, h.progressPath)
		if err != nil {
			h.writeErrorReq(w, r, http.StatusBadRequest, "scene_plan_approve_failed", err)
			return
		}
	}
	h.broadcastProgress()
	h.writeJSON(w, http.StatusOK, plan)
}

// PostScenePlanApprove marks the plan approved so chapter generation may run.
func (h *Handlers) PostScenePlanApprove(w http.ResponseWriter, r *http.Request) {
	if h.isTaskRunning() {
		h.writeErrorReq(w, r, http.StatusConflict, "task_running_wait")
		return
	}
	num, ok := h.resolveScenePlanChapter(w, r)
	if !ok {
		return
	}
	plan, err := story.ApproveScenePlan(h.state, num, h.progressPath)
	if err != nil {
		h.writeErrorReq(w, r, http.StatusBadRequest, "scene_plan_approve_failed", err)
		return
	}
	h.logger.InfoKey("log.scene_plan_approved", num)
	h.broadcastProgress()
	h.writeJSON(w, http.StatusOK, plan)
}

// PostScenePlanRegenerate asks the LLM for a fresh plan, optionally guided by
// free-text feedback from the author ("merge scenes 2-3", "give more room to
// the climax"...). Runs as a task so the cancel button works and no other
// task races against it.
func (h *Handlers) PostScenePlanRegenerate(w http.ResponseWriter, r *http.Request) {
	if !h.tryStartTask() {
		h.writeErrorReq(w, r, http.StatusConflict, "task_running_wait")
		return
	}
	num, ok := h.resolveScenePlanChapter(w, r)
	if !ok {
		h.endTask()
		return
	}
	var body struct {
		Feedback string `json:"feedback,omitempty"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	go func() {
		defer h.endTask()
		h.logger.TaskStart("scene_plan_generation")
		ctx := h.activateSkills(h.taskCtx, story.SkillScopeChapterGenerate, true)
		plan, err := story.RegenerateScenePlan(ctx, h.apiCfg, h.cfg, h.state, num, body.Feedback, h.progressPath, h.logger)
		if err != nil {
			if ctx.Err() != nil {
				h.logger.WarnKey("log.scene_plan_cancelled")
			} else {
				h.logger.ErrorKey("log.scene_plan_failed", err)
			}
			h.logger.TaskEnd("scene_plan_generation", false)
			h.broadcastProgress()
			return
		}
		h.logger.SuccessKey("log.scene_plan_ready", num, len(plan.Scenes), plan.TargetWords)
		h.logger.TaskEnd("scene_plan_generation", true)
		h.broadcastProgress()
	}()

	h.writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}

// PutAutoScenePlan toggles project-wide automatic plan approval: when on, the
// plan is generated and accepted without pausing for review, and chapter
// writing continues immediately.
func (h *Handlers) PutAutoScenePlan(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Auto bool `json:"auto"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if err := story.SetAutoScenePlan(h.state, body.Auto, h.progressPath); err != nil {
		h.writeErrorReq(w, r, http.StatusInternalServerError, "save_failed", err)
		return
	}
	if body.Auto {
		h.logger.InfoKey("log.auto_scene_plan_on")
	} else {
		h.logger.InfoKey("log.auto_scene_plan_off")
	}
	h.broadcastProgress()
	h.writeJSON(w, http.StatusOK, map[string]bool{"auto_scene_plan": h.state.AutoScenePlan})
}
