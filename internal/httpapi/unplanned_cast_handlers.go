package httpapi

import (
	"net/http"

	"showmethestory/internal/story"
)

// PostUnplannedCastRegister scans the chapter's draft for characters that were
// never planned in any cast of the outline, judges their relevance with one
// cheap LLM call and registers the plot-relevant ones in that chapter's cast.
// Runs synchronously under a task lock so it cannot race against generation;
// the HTTP response carries the registration report.
func (h *Handlers) PostUnplannedCastRegister(w http.ResponseWriter, r *http.Request) {
	if !h.tryStartTask() {
		h.writeErrorReq(w, r, http.StatusConflict, "task_running_wait")
		return
	}
	defer h.endTask()

	num, ok := h.resolveScenePlanChapter(w, r)
	if !ok {
		return
	}
	idx := story.FindChapterIdx(h.state, num)

	h.logger.TaskStart("unplanned_cast_scan")
	res, err := story.RegisterUnplannedCastAction(h.taskCtx, h.apiCfg, h.cfg, h.state, idx, h.progressPath, h.logger)
	if err != nil {
		h.logger.TaskEnd("unplanned_cast_scan", false)
		h.broadcastProgress()
		h.writeErrorReq(w, r, http.StatusBadGateway, "unplanned_cast_failed", err)
		return
	}
	h.logger.TaskEnd("unplanned_cast_scan", true)
	h.broadcastProgress()
	h.writeJSON(w, http.StatusOK, res)
}
