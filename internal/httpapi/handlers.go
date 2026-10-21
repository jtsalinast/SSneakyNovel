package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"showmethestory/internal/agent"
	"showmethestory/internal/config"
	"showmethestory/internal/devlog"
	"showmethestory/internal/fsutil"
	"showmethestory/internal/i18n"
	"showmethestory/internal/llm"
	"showmethestory/internal/prose"
	"showmethestory/internal/sse"
	"showmethestory/internal/story"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Handlers struct {
	apiCfg     *config.APIConfig
	apiCfgPath string
	logger     *sse.LogBroadcaster
	version    string

	// Project management
	progDir     string
	projectName string
	projectMu   sync.RWMutex

	// Per-project state (updated on switchProject)
	cfg             *config.Config
	cfgPath         string
	state           *story.Progress
	progressPath    string
	settings        *story.ProjectSettings
	settingsPath    string
	skills          []story.Skill
	sessionsDir     string
	postprocess     *story.PostProcessState
	postprocessPath string

	// Task management
	taskMu             sync.Mutex
	taskRunning        bool
	activeWork         int
	taskCtx            context.Context
	generationID       uint64 // bumped on every task start/stop; see tryStartTask
	taskCancel         context.CancelFunc
	manualStop         bool // set by PostTaskStop; distinguishes a user Stop from the MaxTaskDuration deadline (see TaskStoppedByUser)
	taskTokens         *llm.TaskTokenUsage
	knowledgeAtStart   string
	forceKnowledgeSync bool
	skipKnowledgeSync  bool
	autoConfirm        bool // modo auto: avanza al siguiente capítulo tras aceptar uno

	lastChatMessage   string             // guarda el último mensaje de chat enviado, para reintentos
	lastReconcileBody config.StoryConfig // guarda la última solicitud recibida
}

func NewHandlers(apiCfg *config.APIConfig, apiCfgPath string, logger *sse.LogBroadcaster, progDir string, version string) *Handlers {
	return &Handlers{
		apiCfg:      apiCfg,
		apiCfgPath:  apiCfgPath,
		logger:      logger,
		version:     version,
		progDir:     progDir,
		cfg:         config.DefaultConfig(),
		state:       &story.Progress{Phase: "writing", BookStatus: story.BookStatusActive},
		settings:    &story.ProjectSettings{},
		postprocess: story.NewProofreadState(),
	}
}

func (h *Handlers) storysDir() string {
	return filepath.Join(h.progDir, "storys")
}

// projectDir returns the current project's directory (empty if no project selected).
func (h *Handlers) projectDir() string {
	h.projectMu.RLock()
	defer h.projectMu.RUnlock()
	if h.projectName == "" {
		return h.progDir
	}
	return filepath.Join(h.progDir, "storys", h.projectName)
}

// switchProject loads all project-specific data for the given project name.
func (h *Handlers) switchProject(name string) error {
	if !validProjectName(name) {
		return fmt.Errorf("invalid project name")
	}
	h.projectMu.Lock()
	defer h.projectMu.Unlock()

	projectDir := filepath.Join(h.progDir, "storys", name)
	if info, err := os.Stat(projectDir); err != nil || !info.IsDir() {
		return fmt.Errorf("archivo no encontrado: %s", name)
	}
	if err := ensureProjectCompatible(projectDir); err != nil {
		return err
	}

	configPath := filepath.Join(projectDir, "config.json")
	progressPath := filepath.Join(projectDir, "progress.json")
	settingsPath := filepath.Join(projectDir, "settings.json")
	sessionsDir := filepath.Join(projectDir, "sessions")
	os.MkdirAll(sessionsDir, 0755)

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("itemplace: %w", err)
	}

	state, err := story.LoadProgress(progressPath)
	if err != nil {
		return fmt.Errorf("leer archivo: %w", err)
	}
	if state == nil {
		state = &story.Progress{Phase: "writing", BookStatus: story.BookStatusActive}
	}

	settings, err := story.LoadProjectSettings(settingsPath)
	if err != nil {
		return fmt.Errorf("item: %w", err)
	}

	skills := story.LoadAllSkills(cfg, h.progDir)

	postprocessPath := filepath.Join(projectDir, "postprocess.json")
	postprocess, err := story.LoadPostProcess(postprocessPath)
	if err != nil {
		return fmt.Errorf("posture: %w", err)
	}

	h.projectName = name
	h.cfg = cfg
	h.cfgPath = configPath
	h.state = state
	h.progressPath = progressPath
	h.settings = settings
	h.settingsPath = settingsPath
	h.skills = skills
	h.sessionsDir = sessionsDir
	h.postprocessPath = postprocessPath
	h.postprocess = postprocess

	fmt.Printf(" [system] Switched to project: %s (%s)\n", name, projectDir)
	return nil
}

// ensureProject returns true if a project is selected, otherwise writes an error response.
func (h *Handlers) ensureProject(w http.ResponseWriter, r *http.Request) bool {
	h.projectMu.RLock()
	defer h.projectMu.RUnlock()
	if h.projectName == "" {
		h.writeErrorReq(w, r, http.StatusBadRequest, "select_project_first")
		return false
	}
	return true
}

// writeErrorReq writes a JSON error response, picking message language from the request.
func (h *Handlers) writeErrorReq(w http.ResponseWriter, r *http.Request, code int, key string, args ...any) {
	lang := i18n.FromRequest(r)
	response := map[string]interface{}{"error": i18n.T(lang, key, args...)}
	for _, arg := range args {
		if err, ok := arg.(error); ok {
			if saveErr, ok := fsutil.AsSaveError(err); ok {
				response["code"] = "storage_save_failed"
				response["storage_error"] = map[string]interface{}{
					"file":               filepath.Base(saveErr.Path),
					"path":               saveErr.Path,
					"stage":              saveErr.Stage,
					"original_preserved": saveErr.OriginalPreserved,
					"backup_path":        saveErr.BackupPath,
					"detail":             saveErr.Error(),
				}
				break
			}
		}
	}
	h.writeJSON(w, code, response)
}

func (h *Handlers) writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

// newTaskTokenTracker builds the fresh per-task tracker and publishes it under
// taskMu. tryStartTask calls it *before* acquiring its own lock (Go mutexes are
// not reentrant), so the returned tracker is what /api/status snapshots and
// what every context derived from h.taskCtx accumulates into.
func (h *Handlers) newTaskTokenTracker() *llm.TaskTokenUsage {
	usage := llm.NewTaskTokenUsage(h.logger)
	h.taskMu.Lock()
	h.taskTokens = usage
	h.taskMu.Unlock()
	return usage
}

func (h *Handlers) tryStartTask() bool {
	// Fresh tracker first (its own short taskMu section): the previous run's
	// tracker is orphaned once its context is cancelled, so /api/status must
	// snapshot the new one.
	tokens := h.newTaskTokenTracker()
	h.taskMu.Lock()
	defer h.taskMu.Unlock()
	if h.taskRunning || h.activeWork > 0 {
		devlog.Log("tryStartTask rejected running=%v activeWork=%d", h.taskRunning, h.activeWork)
		return false
	}
	h.taskRunning = true
	h.knowledgeAtStart = h.knowledgeVersion()
	h.forceKnowledgeSync = false
	h.skipKnowledgeSync = false
	h.activeWork = 1
	// Hard wall-clock ceiling per task (llm.NewTaskContext): without it a
	// persistently failing API endpoint could keep retrying forever and freeze
	// the machine. Manual stop via taskCancel still works as before.
	ctx, cancel := llm.NewTaskContext(context.Background())
	// Publish h.taskCtx WITH the tracker already attached: goroutines snapshot
	// h.taskCtx right after tryStartTask returns, and any LLM call made through a
	// context lacking a TaskTokenUsage reports no counts at all — the frontend
	// badge stayed empty for those generations.
	h.taskCtx = llm.AttachTaskTokens(ctx, tokens)
	h.taskCancel = cancel
	h.manualStop = false
	h.generationID++
	devlog.Log("tryStartTask ok activeWork=1")
	return true
}

// TaskStoppedByUser reports whether the CURRENT task's context was cancelled by
// an explicit Stop (PostTaskStop) rather than by hitting MaxTaskDuration. The
// two conditions used to be indistinguishable at the story layer — both surface
// as a done context with DeadlineExceeded/Canceled cause — which made every
// deadline abort look like a user cancellation in the log ("Chapter writing
// aborted" even when it was really the 30-min ceiling). Handlers classify
// errors with this instead of relying on context.Cause alone.
func (h *Handlers) TaskStoppedByUser() bool {
	h.taskMu.Lock()
	defer h.taskMu.Unlock()
	return h.manualStop
}

// generationID identifies the current task run. endTask bumps it so any
// background goroutine that captured an older id knows its context was
// cancelled by task teardown (normal completion or stop) rather than by a
// real error — used to suppress spurious "API use: context canceled"
// logs when a chat turn's lock is released while child work finished first.

func (h *Handlers) endTask() {
	h.taskMu.Lock()
	h.activeWork--
	cancelled := false
	if h.activeWork <= 0 {
		h.taskMu.Unlock()
		if !h.skipKnowledgeSync && h.cfg != nil && h.state != nil && h.taskCtx != nil && h.taskCtx.Err() == nil && (h.forceKnowledgeSync || h.knowledgeAtStart != h.knowledgeVersion()) {
			h.logger.TaskStart("knowledge_sync")
			err := story.SyncPendingKnowledge(h.taskCtx, h.apiCfg, h.cfg, h.state, h.settings, h.progressPath, h.logger)
			if err != nil {
				h.logger.WarnKey("log.knowledge_failed", err)
			}
			h.logger.TaskEnd("knowledge_sync", err == nil)
			h.broadcastProgress()
		}
		h.taskMu.Lock()
		h.activeWork = 0
		h.taskRunning = false
		if h.taskCancel != nil {
			h.taskCancel()
			h.taskCancel = nil
			cancelled = true
		}
		h.generationID++
	}
	aw, running := h.activeWork, h.taskRunning
	h.taskMu.Unlock()
	devlog.Log("endTask activeWork=%d running=%v cancelled=%v", aw, running, cancelled)
}

// startChildWork alivemakecountnumber(usein Agent ), not new context
func (h *Handlers) startChildWork() bool {
	h.taskMu.Lock()
	defer h.taskMu.Unlock()
	if !h.taskRunning {
		devlog.Log("startChildWork rejected taskRunning=false")
		return false
	}
	h.activeWork++
	devlog.Log("startChildWork ok activeWork=%d", h.activeWork)
	return true
}

// currentTaskTokens returns the tracker owned by the currently running task
// (set by tryStartTask). Child work — e.g. a detached section generation
// started from a chat turn — must accumulate its LLM tokens into this same
// tracker: /api/status snapshots it and the SSE token_usage events feed the
// frontend badge from it. A child that builds its own fresh tracker instead
// makes the badge show nothing even though generation is running fine.
func (h *Handlers) currentTaskTokens() *llm.TaskTokenUsage {
	h.taskMu.Lock()
	defer h.taskMu.Unlock()
	return h.taskTokens
}

// ensureTaskTokens returns ctx with a token tracker attached, re-using the
// current task's tracker when one exists. Used as a safety net by detached
// runners so their LLM calls are always counted somewhere visible.
func (h *Handlers) ensureTaskTokens(ctx context.Context) context.Context {
	if llm.TaskTokensFromContext(ctx) != nil {
		return ctx
	}
	usage := h.currentTaskTokens()
	return llm.AttachTaskTokens(ctx, usage)
}

func (h *Handlers) isTaskRunning() bool {
	h.taskMu.Lock()
	defer h.taskMu.Unlock()
	return h.taskRunning || h.activeWork > 0
}

func (h *Handlers) activeWorkCount() int {
	h.taskMu.Lock()
	defer h.taskMu.Unlock()
	return h.activeWork
}

// rejectIfTaskRunning in AI travelexpectcompileclassplease, defendintentoutsideinteract.
// return true tablealreadywriteenter 409 noiseshould, usesquareshouldconnect return.
func (h *Handlers) rejectIfTaskRunning(w http.ResponseWriter, r *http.Request) bool {
	if h.isTaskRunning() {
		h.writeErrorReq(w, r, http.StatusConflict, "task_running_locked")
		return true
	}
	return false
}

func (h *Handlers) isAutoConfirmOn() bool {
	h.taskMu.Lock()
	defer h.taskMu.Unlock()
	return h.autoConfirm
}

func knowledgeSyncMustStop(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	if ctx != nil && ctx.Err() != nil {
		return true
	}
	_, saveFailed := fsutil.AsSaveError(err)
	return saveFailed
}

func (h *Handlers) GetAutoConfirm(w http.ResponseWriter, r *http.Request) {
	h.writeJSON(w, http.StatusOK, map[string]bool{"enabled": h.isAutoConfirmOn()})
}

// PutAutoConfirm cutmove, travelexpect-toocanalong withhouropenclose.
func (h *Handlers) PutAutoConfirm(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	h.taskMu.Lock()
	h.autoConfirm = req.Enabled
	h.taskMu.Unlock()

	if req.Enabled {
		h.logger.InfoKey("log.autoconfirm_on")
	} else {
		h.logger.InfoKey("log.autoconfirm_off")
	}
	h.writeJSON(w, http.StatusOK, map[string]bool{"enabled": req.Enabled})
}

func (h *Handlers) PostTaskStop(w http.ResponseWriter, r *http.Request) {
	h.taskMu.Lock()
	if !h.taskRunning {
		h.taskMu.Unlock()
		h.writeErrorReq(w, r, http.StatusBadRequest, "no_task_running")
		return
	}
	if h.taskCancel != nil {
		h.manualStop = true
		h.taskCancel()
	}
	devlog.Log("task_stop requested activeWork=%d current=%s", h.activeWork, h.logger.CurrentTask())
	h.taskMu.Unlock()
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "stopping"})
}

func (h *Handlers) GetAPIConfig(w http.ResponseWriter, r *http.Request) {
	h.writeJSON(w, http.StatusOK, h.apiCfg)
}

func (h *Handlers) PutAPIConfig(w http.ResponseWriter, r *http.Request) {
	if h.rejectIfTaskRunning(w, r) {
		return
	}
	var newCfg config.APIConfig
	if err := json.NewDecoder(r.Body).Decode(&newCfg); err != nil {
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	if newCfg.HTTPTimeoutSeconds <= 0 {
		newCfg.HTTPTimeoutSeconds = config.DefaultHTTPTimeoutSeconds
	}
	llm.EnsureContextBudget(&newCfg)

	data, err := json.MarshalIndent(newCfg, "", "  ")
	if err != nil {
		h.writeErrorReq(w, r, http.StatusInternalServerError, "serialize_api_config_failed", err.Error())
		return
	}
	if err := fsutil.WriteFileAtomic(h.apiCfgPath, data); err != nil {
		h.writeErrorReq(w, r, http.StatusInternalServerError, "save_api_config_failed", err)
		return
	}

	h.apiCfg = &newCfg
	h.writeJSON(w, http.StatusOK, h.apiCfg)
}

func (h *Handlers) PostAPITest(w http.ResponseWriter, r *http.Request) {
	if h.rejectIfTaskRunning(w, r) {
		return
	}
	var testCfg config.APIConfig
	if err := json.NewDecoder(r.Body).Decode(&testCfg); err != nil {
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if err := llm.ValidateConfig(&testCfg); err != nil {
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	resp, err := llm.CallAPIMessages(ctx, &testCfg, []llm.Message{
		{Role: "user", Content: "Hi"},
	})
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			h.writeErrorReq(w, r, http.StatusGatewayTimeout, "api_test_timeout")
			return
		}
		h.writeErrorReq(w, r, http.StatusBadGateway, "api_test_failed", err.Error())
		return
	}

	result := map[string]interface{}{
		"success": true,
		"message": "connectconnect",
		"model":   testCfg.Model,
	}
	if len(resp) > 100 {
		result["sample"] = resp[:100] + "..."
	} else {
		result["sample"] = resp
	}
	h.writeJSON(w, http.StatusOK, result)
}

func (h *Handlers) GetConfig(w http.ResponseWriter, r *http.Request) {
	h.writeJSON(w, http.StatusOK, h.cfg)
}

func (h *Handlers) PutConfig(w http.ResponseWriter, r *http.Request) {
	if h.rejectIfTaskRunning(w, r) {
		return
	}
	var newCfg config.Config
	if err := json.NewDecoder(r.Body).Decode(&newCfg); err != nil {
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	if newCfg.Story.TargetWordsPerChapter <= 0 {
		// Use the preset tied to the selected story length (light novel,
		// anthology, flash...) instead of a one-size-fits-all default.
		if preset := config.DefaultTargetWordsForLength(newCfg.Story.StoryLength); preset > 0 {
			newCfg.Story.TargetWordsPerChapter = preset
		} else {
			newCfg.Story.TargetWordsPerChapter = 2500
		}
	}
	newCfg.Language = i18n.NormalizeLanguage(newCfg.Language)
	if newCfg.Language == "" {
		newCfg.Language = h.cfg.Language
	}
	newCfg.ProjectFormatVersion = config.ProjectFormatVersion
	newCfg.CreatedWithVersion = h.cfg.CreatedWithVersion
	newCfg.Prompts.ApplyDefaults(newCfg.Language)

	data, err := json.MarshalIndent(newCfg, "", "  ")
	if err != nil {
		h.writeErrorReq(w, r, http.StatusInternalServerError, "serialize_config_failed", err.Error())
		return
	}
	if err := fsutil.WriteFileAtomic(h.cfgPath, data); err != nil {
		h.writeErrorReq(w, r, http.StatusInternalServerError, "save_config_failed", err)
		return
	}

	h.cfg = &newCfg
	h.writeJSON(w, http.StatusOK, h.cfg)
}

func (h *Handlers) GetPendingConfigChanges(w http.ResponseWriter, r *http.Request) {
	if !h.ensureProject(w, r) {
		return
	}
	pending, err := story.LoadPendingConfigChanges(story.PendingConfigChangesPath(h.progressPath))
	if err != nil {
		h.writeErrorReq(w, r, http.StatusInternalServerError, "load_pending_config_failed", err.Error())
		return
	}
	h.writeJSON(w, http.StatusOK, pending)
}

func (h *Handlers) PostApplyConfigChanges(w http.ResponseWriter, r *http.Request) {
	if !h.ensureProject(w, r) {
		return
	}
	if h.rejectIfTaskRunning(w, r) {
		return
	}
	var body struct {
		Fields []string `json:"fields"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Fields) == 0 {
		h.writeErrorReq(w, r, http.StatusBadRequest, "missing_fields")
		return
	}

	pendingPath := story.PendingConfigChangesPath(h.progressPath)
	pending, err := story.LoadPendingConfigChanges(pendingPath)
	if err != nil {
		h.writeErrorReq(w, r, http.StatusInternalServerError, "load_pending_config_failed", err.Error())
		return
	}
	if len(pending.Changes) == 0 {
		h.writeErrorReq(w, r, http.StatusBadRequest, "no_pending_changes")
		return
	}

	story.ApplySelectedPendingChanges(h.cfg, h.state, pending, body.Fields)

	if err := config.SaveConfig(h.cfgPath, h.cfg); err != nil {
		h.writeErrorReq(w, r, http.StatusInternalServerError, "save_config_failed", err)
		return
	}
	if err := story.SaveProgress(h.progressPath, h.state); err != nil {
		h.writeErrorReq(w, r, http.StatusInternalServerError, "save_progress_failed", err)
		return
	}
	if err := story.RemovePendingFields(pendingPath, body.Fields...); err != nil {
		h.writeErrorReq(w, r, http.StatusInternalServerError, "save_pending_config_failed", err)
		return
	}

	h.logger.SettingsUpdated()
	h.broadcastProgress()
	h.writeJSON(w, http.StatusOK, h.cfg)
}

func (h *Handlers) DeletePendingConfigChanges(w http.ResponseWriter, r *http.Request) {
	if !h.ensureProject(w, r) {
		return
	}
	if h.rejectIfTaskRunning(w, r) {
		return
	}
	if err := story.SavePendingConfigChanges(story.PendingConfigChangesPath(h.progressPath), &story.PendingConfigChanges{}); err != nil {
		h.writeErrorReq(w, r, http.StatusInternalServerError, "delete_pending_config_failed", err.Error())
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}

func (h *Handlers) GetProgress(w http.ResponseWriter, r *http.Request) {
	h.writeJSON(w, http.StatusOK, story.ProgressView(h.state))
}

// GetChapterContent returns one chapter with full prose content.
func (h *Handlers) GetChapterContent(w http.ResponseWriter, r *http.Request) {
	numStr := r.PathValue("num")
	var num int
	if _, err := fmt.Sscanf(numStr, "%d", &num); err != nil {
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_chapter_num")
		return
	}
	idx := story.FindChapterIdx(h.state, num)
	if idx < 0 {
		h.writeErrorReq(w, r, http.StatusNotFound, "chapter_n_not_found", num)
		return
	}
	ch := h.state.Chapters[idx]
	if ch.Content != "" {
		ch.WordCount = prose.CountProseUnits(ch.Content)
		ch.ContentRev = fmt.Sprintf("%x", story.HashContent(ch.Content))
	}
	h.writeJSON(w, http.StatusOK, ch)
}

// —— story.Block compile ——

// parseChapterBlockIDs extracts {num} and {id} path values.
func parseChapterBlockIDs(r *http.Request) (num, id int, err error) {
	if _, err = fmt.Sscanf(r.PathValue("num"), "%d", &num); err != nil {
		return
	}
	_, err = fmt.Sscanf(r.PathValue("id"), "%d", &id)
	return
}

// blockEditChapter locates the chapter, runs edit, saves, and responds with
// the updated chapter (incl. blocks).
func (h *Handlers) blockEditChapter(w http.ResponseWriter, r *http.Request, num int, edit func(ch *story.ChapterState) error) {
	idx := story.FindChapterIdx(h.state, num)
	if idx < 0 {
		h.writeErrorReq(w, r, http.StatusNotFound, "chapter_n_not_found", num)
		return
	}
	ch := &h.state.Chapters[idx]
	before := *ch
	before.Blocks = append([]story.Block(nil), ch.Blocks...)
	if len(ch.Blocks) == 0 && ch.Content != "" {
		story.SyncChapterBlocks(ch)
	}
	if err := edit(ch); err != nil {
		if err == story.ErrBlockNotFound {
			h.writeErrorReq(w, r, http.StatusNotFound, "block_not_found")
		} else {
			h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_json", err.Error())
		}
		return
	}
	opts := story.FactEditOptions{ContentRev: r.Header.Get("X-Content-Rev"), ConfirmFactImpact: r.Header.Get("X-Confirm-Fact-Impact") == "true"}
	if err := story.ValidateFactEdit(h.state, before, *ch, opts, i18n.FromRequest(r)); err != nil {
		*ch = before
		h.writeErrorReq(w, r, http.StatusConflict, "invalid_json", err)
		return
	}
	ch.KnowledgeTracked = true
	if err := story.SaveProgress(h.progressPath, h.state); err != nil {
		*ch = before
		h.writeErrorReq(w, r, http.StatusInternalServerError, "save_progress_failed", err)
		return
	}
	story.SaveChapterMarkdown(h.projectDir(), *ch, h.state.Title)
	h.broadcastProgress()
	resp := *ch
	if resp.Content != "" {
		resp.WordCount = prose.CountProseUnits(resp.Content)
		resp.ContentRev = fmt.Sprintf("%x", story.HashContent(resp.Content))
	}
	h.writeJSON(w, http.StatusOK, resp)
	h.startKnowledgeSync()
}

func (h *Handlers) PutChapterBlock(w http.ResponseWriter, r *http.Request) {
	if h.rejectIfTaskRunning(w, r) {
		return
	}
	num, id, err := parseChapterBlockIDs(r)
	if err != nil {
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_chapter_num")
		return
	}
	var body struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Text) == "" {
		h.writeErrorReq(w, r, http.StatusBadRequest, "block_text_required")
		return
	}
	h.blockEditChapter(w, r, num, func(ch *story.ChapterState) error {
		return story.UpdateBlock(ch, id, body.Text)
	})
}

func (h *Handlers) DeleteChapterBlock(w http.ResponseWriter, r *http.Request) {
	if h.rejectIfTaskRunning(w, r) {
		return
	}
	num, id, err := parseChapterBlockIDs(r)
	if err != nil {
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_chapter_num")
		return
	}
	h.blockEditChapter(w, r, num, func(ch *story.ChapterState) error {
		return story.DeleteBlock(ch, id)
	})
}

func (h *Handlers) PostChapterBlockInsert(w http.ResponseWriter, r *http.Request) {
	if h.rejectIfTaskRunning(w, r) {
		return
	}
	numStr := r.PathValue("num")
	var num int
	if _, err := fmt.Sscanf(numStr, "%d", &num); err != nil {
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_chapter_num")
		return
	}
	var body struct {
		AfterID int    `json:"after_id"`
		Text    string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Text) == "" {
		h.writeErrorReq(w, r, http.StatusBadRequest, "block_text_required")
		return
	}
	h.blockEditChapter(w, r, num, func(ch *story.ChapterState) error {
		_, err := story.InsertBlockAfter(ch, body.AfterID, body.Text)
		return err
	})
}

// PostChapterBlockRevise runs an async AI revision scoped to one block.
func (h *Handlers) PostChapterBlockRevise(w http.ResponseWriter, r *http.Request) {
	num, id, err := parseChapterBlockIDs(r)
	if err != nil {
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_chapter_num")
		return
	}

	var body struct {
		Feedback string `json:"feedback"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Feedback) == "" {
		h.writeErrorReq(w, r, http.StatusBadRequest, "missing_feedback")
		return
	}

	idx := story.FindChapterIdx(h.state, num)
	if idx < 0 {
		h.writeErrorReq(w, r, http.StatusNotFound, "chapter_n_not_found", num)
		return
	}
	if len(h.state.Chapters[idx].Blocks) == 0 && h.state.Chapters[idx].Content != "" {
		story.SyncChapterBlocks(&h.state.Chapters[idx])
	}
	if story.FindBlockIdx(&h.state.Chapters[idx], id) < 0 {
		h.writeErrorReq(w, r, http.StatusNotFound, "block_not_found")
		return
	}
	before := h.state.Chapters[idx]
	after := before
	after.Blocks = append([]story.Block(nil), before.Blocks...)
	after.Blocks[story.FindBlockIdx(&after, id)].Text += "\n[revision]"
	opts := story.FactEditOptions{ContentRev: r.Header.Get("X-Content-Rev"), ConfirmFactImpact: r.Header.Get("X-Confirm-Fact-Impact") == "true"}
	if err := story.ValidateFactEdit(h.state, before, after, opts, i18n.FromRequest(r)); err != nil {
		h.writeErrorReq(w, r, http.StatusConflict, "invalid_json", err)
		return
	}

	if !h.tryStartTask() {
		h.writeErrorReq(w, r, http.StatusConflict, "task_running_wait")
		return
	}

	go func() {
		defer h.endTask()
		h.logger.TaskStart("block_revision")
		ctx := h.activateSkills(h.taskCtx, story.SkillScopeChapterRevise, true)

		h.logger.InfoKey("log.block_revising", num, id)
		err := story.ReviseBlockAction(ctx, h.apiCfg, h.cfg, h.state, h.progressPath, num, id, body.Feedback, h.settings, h.logger)
		if err != nil {
			if ctx.Err() != nil {
				h.logger.WarnKey("log.chapter_revise_cancelled")
			} else {
				h.logger.ErrorKey("log.chapter_revise_failed", err)
			}
			h.logger.TaskEnd("block_revision", false)
			return
		}

		h.logger.TaskEnd("block_revision", true)
		h.broadcastProgress()
	}()

	h.writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}

// GetBookExport streams the whole book as plain text.
func (h *Handlers) GetBookExport(w http.ResponseWriter, r *http.Request) {
	title := h.state.Title
	if h.cfg != nil && h.cfg.Story.Title != "" {
		title = h.cfg.Story.Title
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "# %s", title)
	for _, ch := range h.state.Chapters {
		if ch.Content == "" {
			continue
		}
		fmt.Fprintf(w, "\n\n## Chapter %d: %s\n\n%s", ch.Num, ch.Title, ch.Content)
	}
}

func (h *Handlers) DeleteProgress(w http.ResponseWriter, r *http.Request) {
	if h.isTaskRunning() {
		h.writeErrorReq(w, r, http.StatusConflict, "reset_progress_locked")
		return
	}

	if err := story.ResetProgressFiles(h.progressPath); err != nil {
		h.writeErrorReq(w, r, http.StatusInternalServerError, "delete_progress_failed", err.Error())
		return
	}

	h.state = &story.Progress{Phase: "outline"}
	h.writeJSON(w, http.StatusOK, story.ProgressView(h.state))
}

func (h *Handlers) PostOutlineConfirm(w http.ResponseWriter, r *http.Request) {
	if h.isTaskRunning() {
		h.writeErrorReq(w, r, http.StatusConflict, "task_running_wait")
		return
	}

	if h.state.Phase != "outline" {
		h.writeErrorReq(w, r, http.StatusBadRequest, "phase_not_outline")
		return
	}

	if len(h.state.Chapters) == 0 {
		h.writeErrorReq(w, r, http.StatusBadRequest, "outline_empty")
		return
	}

	if err := story.ConfirmOutlineAction(h.state, h.progressPath, h.cfg.Story); err != nil {
		h.writeErrorReq(w, r, http.StatusInternalServerError, "outline_confirm_failed", err)
		return
	}

	h.logger.SuccessKey("log.outline_confirmed")
	h.writeJSON(w, http.StatusOK, story.ProgressView(h.state))
}

func (h *Handlers) PostOutlineRevise(w http.ResponseWriter, r *http.Request) {
	if !h.tryStartTask() {
		h.writeErrorReq(w, r, http.StatusConflict, "task_running_wait")
		return
	}

	var body struct {
		Feedback string `json:"feedback"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Feedback == "" {
		h.endTask()
		h.writeErrorReq(w, r, http.StatusBadRequest, "missing_feedback")
		return
	}

	go func() {
		defer h.endTask()
		h.logger.TaskStart("outline_revision")
		ctx := h.activateSkills(h.taskCtx, story.SkillScopeOutlineRevise, true)

		h.logger.InfoKey("log.outline_revising")
		err := story.ReviseOutlineAction(ctx, h.apiCfg, h.cfg, h.state, h.settings, h.progressPath, h.cfgPath, body.Feedback, h.logger)

		if err != nil {
			if ctx.Err() != nil {
				h.logger.WarnKey("log.outline_revise_cancelled")
				h.logger.TaskEnd("outline_revision", false)
			} else {
				h.logger.ErrorKey("log.outline_revise_failed", err)
				h.logger.TaskEnd("outline_revision", false)
			}
			return
		}

		h.logger.SuccessKey("log.outline_revised")
		h.logger.TaskEnd("outline_revision", true)
		h.broadcastProgress()
	}()

	h.writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}

func (h *Handlers) PostOutlineCharactersConfirm(w http.ResponseWriter, r *http.Request) {
	if !h.ensureProject(w, r) {
		return
	}
	if h.rejectIfTaskRunning(w, r) {
		return
	}

	var req struct {
		Characters []story.OutlineCharacterSuggestion `json:"characters"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if len(req.Characters) == 0 {
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_json", "characters required")
		return
	}

	if h.settings == nil {
		h.settings = &story.ProjectSettings{}
	}

	existing := story.RegisteredCharacterNameSet(h.settings)
	for _, item := range req.Characters {
		name := strings.TrimSpace(story.StripNameMarks(item.Name))
		if name == "" || existing[name] {
			continue
		}
		notes := strings.TrimSpace(item.Description)
		if role := strings.TrimSpace(item.Role); role != "" {
			if notes != "" {
				notes += "; "
			}
			notes += role
		}
		h.settings.Characters = append(h.settings.Characters, story.Character{
			ID:         h.settings.NextCharacterID(),
			Name:       name,
			Background: notes,
			Notes:      fmt.Sprintf("times: #%dchapter", item.ChapterNum),
		})
		existing[name] = true
	}

	if err := story.SaveProjectSettings(h.settingsPath, h.settings); err != nil {
		h.writeErrorReq(w, r, http.StatusInternalServerError, "save_failed", err)
		return
	}

	if h.state.LastOutlineCharacterReport != nil {
		h.state.LastOutlineCharacterReport.HasSuggestions = false
		h.state.LastOutlineCharacterReport.Suggestions = nil
		h.state.LastOutlineCharacterReport.Summary = "alreadyandhorn"
		_ = story.SaveProgress(h.progressPath, h.state)
	}

	h.logger.SettingsUpdated()
	h.writeJSON(w, http.StatusOK, h.settings.Characters)
}

// asScenePlanPending reports whether err is the scene-plan review gate pause.
func asScenePlanPending(err error) (*story.ScenePlanPendingError, bool) {
	var spErr *story.ScenePlanPendingError
	if errors.As(err, &spErr) {
		return spErr, true
	}
	return nil, false
}

func (h *Handlers) PostChapterGenerate(w http.ResponseWriter, r *http.Request) {
	if !h.tryStartTask() {
		h.writeErrorReq(w, r, http.StatusConflict, "task_running_wait")
		return
	}

	go func() {
		defer h.endTask()
		h.logger.TaskStart("chapter_generation")
		ctx := h.activateSkills(h.taskCtx, story.SkillScopeChapterGenerate, true)
		success := true

		for {
			chIdx := h.state.CurrentChapterIndex
			chTitle := ""
			if chIdx < len(h.state.Chapters) {
				chTitle = h.state.Chapters[chIdx].Title
			}

			h.logger.InfoKey("log.chapter_writing", chIdx+1)
			err := story.GenerateChapterAction(ctx, h.apiCfg, h.cfg, h.state, h.progressPath, h.settings, h.skills, h.logger)

			if err != nil {
				// Three distinct outcomes, previously conflated into a single
				// "Chapter writing cancelled" log that made every stop look like a
				// user cancel and hid the fact that prose was checkpointed:
				//  - explicit Stop (taskCtx cancelled): genuine cancellation; any
				//    finished prose was already checkpointed by the abort helpers.
				//  - task deadline hit (context.DeadlineExceeded): NOT a user cancel —
				//    report it as an abort with saved progress.
				//  - summary-step failure after expensive prose: draft resumable via
				//    POST /api/chapters/{n}/resume.
				switch {
				case h.TaskStoppedByUser():
					// Explicit Stop pressed by the user — never mislabel it as the
					// MaxTaskDuration deadline: endTask also cancels the task context on
					// normal exit, so context.Cause alone cannot tell them apart.
					h.logger.WarnKey("log.chapter_write_cancelled")
				case errors.Is(context.Cause(ctx), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded):
					h.logger.WarnKey("log.chapter_write_aborted_deadline")
				case story.IsSummaryStepFailed(err):
					// Draft checkpoint + explanatory warn already logged inside
					// abortChapterWithDraftCheckpoint; keep the batch stopped but do
					// not claim the work was lost. The wrapped cause carries the
					// precise step failure (cancelled/timeout/api_error/…).
					h.logger.WarnKey("log.chapter_write_aborted", err.Error())
				case ctx.Err() != nil:
					h.logger.WarnKey("log.chapter_write_cancelled")
				default:
					var wcErr *story.WritingConflictError
					if errors.As(err, &wcErr) {
						h.logger.WarnKey("log.chapter_write_conflict_pause")
					} else if _, ok := asScenePlanPending(err); ok {
						// Scene-plan review gate: not an error — the plan was
						// generated and awaits approval (GenerateChapterAction
						// already logged it and persisted the pause marker in
						// state.PendingScenePlanChapter). Stop the batch cleanly;
						// the UI auto-opens the plan editor from progress.
					} else {
						h.logger.ErrorKey("log.chapter_write_failed", err)
					}
				}
			} else {
				h.logger.SuccessKey("log.chapter_write_done", chIdx+1, chTitle)
			}
			h.broadcastProgress()
			if err != nil {
				h.logger.TaskEnd("chapter_generation", false)
				return
			}

			// move: movethischapterandlifeloweronechapter; closeopencloselaterinthischapterhourstop
			if !h.isAutoConfirmOn() {
				break
			}
			if err := story.ConfirmChapterAction(h.state, h.progressPath); err != nil {
				h.logger.WarnKey("log.chapter_autoconfirm_failed", err)
				break
			}
			h.logger.SuccessKey("log.chapter_autoconfirmed", chIdx+1, chTitle)
			// Bestseller act critic: when this confirmed chapter closes a
			// pre-planning act, queue the act analysis (it only launches if
			// no other task owns the pipeline; the batch tail retries).
			if chIdx < len(h.state.Chapters) {
				h.maybeAnalyzeCompletedAct(h.state.Chapters[chIdx].Num)
			}
			if err := story.SyncPendingKnowledge(ctx, h.apiCfg, h.cfg, h.state, h.settings, h.progressPath, h.logger); err != nil {
				h.logger.WarnKey("log.knowledge_failed", err)
				if knowledgeSyncMustStop(ctx, err) {
					success = false
					break
				}
			}
			h.broadcastProgress()

			if h.state.CurrentChapterIndex >= len(h.state.Chapters) {
				h.logger.SuccessKey("log.all_chapters_done")
				break
			}
			if ctx.Err() != nil {
				h.logger.WarnKey("log.autowrite_cancelled")
				break
			}
		}

		h.logger.TaskEnd("chapter_generation", success)
		h.broadcastProgress()
		// Late trigger for acts whose last chapter was auto-confirmed while
		// this batch owned the pipeline (maybeAnalyzeCompletedAct deferred).
		if success {
			h.maybeAnalyzeCompletedActIfIdle()
		}
	}()

	h.writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}

func (h *Handlers) GetChapterConflict(w http.ResponseWriter, r *http.Request) {
	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"conflict": h.state.PendingWritingConflict,
	})
}

func (h *Handlers) PostChapterConflictResolve(w http.ResponseWriter, r *http.Request) {
	if h.isTaskRunning() {
		h.writeErrorReq(w, r, http.StatusConflict, "task_running_wait")
		return
	}

	var body struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Action == "" {
		h.writeErrorReq(w, r, http.StatusBadRequest, "missing_action")
		return
	}

	switch body.Action {
	case "force_review", "dismiss":
		// dismiss ≡ force_review: keep draft in review so UI stays recoverable
		idx, err := story.ResolveForceReviewIndex(h.state)
		if err != nil {
			if h.state.PendingWritingConflict != nil {
				h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_conflict_chapter_idx")
			} else {
				h.writeErrorReq(w, r, http.StatusBadRequest, "writing_conflict_none")
			}
			return
		}
		ch := &h.state.Chapters[idx]
		if err := story.PromoteWritingToReview(h.state, idx); err != nil {
			h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_conflict_chapter_idx")
			return
		}
		if err := story.SaveProgress(h.progressPath, h.state); err != nil {
			h.writeErrorReq(w, r, http.StatusInternalServerError, "save_progress_failed", err)
			return
		}
		h.logger.SuccessKey("log.chapter_kept_review", ch.Num)
		h.broadcastProgress()
		h.writeJSON(w, http.StatusOK, story.ProgressView(h.state))
	case "retry":
		if h.state.PendingWritingConflict == nil {
			h.writeErrorReq(w, r, http.StatusBadRequest, "writing_conflict_none")
			return
		}
		h.state.PendingWritingConflict = nil
		if err := story.SaveProgress(h.progressPath, h.state); err != nil {
			h.writeErrorReq(w, r, http.StatusInternalServerError, "save_progress_failed", err)
			return
		}
		h.broadcastProgress()
		h.writeJSON(w, http.StatusAccepted, map[string]string{"status": "retry"})
	case "revise_from_feedback":
		if h.state.PendingWritingConflict == nil {
			h.writeErrorReq(w, r, http.StatusBadRequest, "writing_conflict_none")
			return
		}
		if strings.TrimSpace(h.state.Chapters[h.state.PendingWritingConflict.ChapterIndex].Content) == "" {
			h.writeErrorReq(w, r, http.StatusBadRequest, "conflict_chapter_no_draft")
			return
		}
		if !h.tryStartTask() {
			h.writeErrorReq(w, r, http.StatusConflict, "task_running_wait")
			return
		}
		go func() {
			defer h.endTask()
			h.logger.TaskStart("chapter_revision")
			ctx := h.activateSkills(h.taskCtx, story.SkillScopeChapterRevise, true)
			err := story.ReviseFromConflictAction(ctx, h.apiCfg, h.cfg, h.state, h.progressPath, h.settings, h.skills, h.logger)
			if err != nil {
				var wcErr *story.WritingConflictError
				switch {
				case ctx.Err() != nil:
					h.logger.WarnKey("log.chapter_revise_cancelled")
				case errors.As(err, &wcErr):
					h.logger.WarnKey("log.chapter_write_conflict_pause")
				default:
					h.logger.ErrorKey("log.chapter_revise_failed", err)
				}
				h.logger.TaskEnd("chapter_revision", false)
				h.broadcastProgress()
				return
			}
			h.logger.TaskEnd("chapter_revision", true)
			h.broadcastProgress()
		}()
		h.writeJSON(w, http.StatusAccepted, map[string]string{"status": "revising"})
	case "surgical_trope":
		// Repair ONLY the trope unit owning the first locatable issue — the rest
		// of the chapter stays untouched. Then re-run the fact-check pass so a
		// clean draft leaves review-ready state and a still-failing one raises a
		// fresh conflict with updated targets.
		c := h.state.PendingWritingConflict
		if c == nil {
			h.writeErrorReq(w, r, http.StatusBadRequest, "writing_conflict_none")
			return
		}
		idx := c.ChapterIndex
		target := [2]int{-1, -1}
		for _, t := range c.SceneTargets {
			if t[0] >= 0 {
				target = t
				break
			}
		}
		if target[0] < 0 || idx < 0 || idx >= len(h.state.Chapters) ||
			strings.TrimSpace(h.state.Chapters[idx].Content) == "" {
			h.writeErrorReq(w, r, http.StatusBadRequest, "conflict_no_surgical_target")
			return
		}
		feedback := strings.TrimSpace(c.Summary)
		var fbParts []string
		for i, iss := range c.Issues {
			if i < len(c.SceneTargets) && c.SceneTargets[i][0] == target[0] && c.SceneTargets[i][1] == target[1] {
				fbParts = append(fbParts, iss)
			}
		}
		if len(fbParts) == 0 {
			fbParts = c.Issues
		}
		if feedback != "" {
			fbParts = append([]string{feedback}, fbParts...)
		}
		joinedFB := strings.Join(fbParts, "\n")
		chNum := h.state.Chapters[idx].Num
		scTitle := ""
		if plan := h.state.Chapters[idx].ScenePlan; plan != nil && target[0] < len(plan.Scenes) {
			sc := plan.Scenes[target[0]]
			scTitle = sc.Title
			if scTitle == "" {
				scTitle = sc.Description
			}
		}
		h.state.PendingWritingConflict = nil
		if err := story.SaveProgress(h.progressPath, h.state); err != nil {
			h.writeErrorReq(w, r, http.StatusInternalServerError, "save_progress_failed", err)
			return
		}
		if !h.tryStartTask() {
			h.writeErrorReq(w, r, http.StatusConflict, "task_running_wait")
			return
		}
		go func() {
			defer h.endTask()
			h.logger.TaskStart("chapter_surgical_trope")
			h.logger.InfoKey("log.surgical_trope_start", chNum, scTitle, target[0]+1, target[1]+1)
			ctx := h.activateSkills(h.taskCtx, story.SkillScopeChapterFactCheck, true)
			newContent, err := story.SurgicalTropeRepair(ctx, h.apiCfg, h.cfg, h.state, h.progressPath, idx, target[0], target[1], joinedFB, h.settings, h.logger)
			if err != nil {
				if ctx.Err() != nil {
					h.logger.WarnKey("log.chapter_write_cancelled")
				} else {
					h.logger.ErrorKey("log.surgical_trope_failed", err)
				}
				h.logger.TaskEnd("chapter_surgical_trope", false)
				h.broadcastProgress()
				return
			}
			_ = newContent
			err = story.FactCheckChapterAction(ctx, h.apiCfg, h.cfg, h.state, h.progressPath, h.settings, h.skills, chNum, h.logger)
			if err != nil {
				var wcErr *story.WritingConflictError
				switch {
				case errors.As(err, &wcErr):
					h.logger.WarnKey("log.chapter_write_conflict_pause")
				case ctx.Err() != nil:
					h.logger.WarnKey("log.chapter_write_cancelled")
				default:
					h.logger.ErrorKey("log.chapter_factcheck_failed", err)
				}
				h.logger.TaskEnd("chapter_surgical_trope", false)
				h.broadcastProgress()
				return
			}
			h.logger.SuccessKey("log.surgical_trope_done", chNum)
			h.logger.TaskEnd("chapter_surgical_trope", true)
			h.broadcastProgress()
		}()
		h.writeJSON(w, http.StatusAccepted, map[string]string{"status": "surgical_repair"})
	default:
		h.writeErrorReq(w, r, http.StatusBadRequest, "unsupported_action", body.Action)
	}
}

func (h *Handlers) PostForeshadowOutlineCheck(w http.ResponseWriter, r *http.Request) {
	if !h.tryStartTask() {
		h.writeErrorReq(w, r, http.StatusConflict, "task_running_wait")
		return
	}
	if len(h.state.Foreshadows) == 0 {
		h.endTask()
		h.writeErrorReq(w, r, http.StatusBadRequest, "no_foreshadows_to_check")
		return
	}

	go h.runForeshadowOutlineCheck()

	h.writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}

// The caller owns tryStartTask's reservation before starting this goroutine.
func (h *Handlers) runForeshadowOutlineCheck() {
	defer h.endTask()
	h.logger.TaskStart("foreshadow_outline_check")
	ctx := h.activateSkills(h.taskCtx, story.SkillScopeForeshadowPlan, true)
	err := story.RunForeshadowOutlineCheckAndSave(ctx, h.apiCfg, h.cfg, h.state, h.progressPath, h.logger)
	h.logger.TaskEnd("foreshadow_outline_check", err == nil)
	h.broadcastProgress()
}

func (h *Handlers) PostChapterConfirm(w http.ResponseWriter, r *http.Request) {
	if h.isTaskRunning() {
		h.writeErrorReq(w, r, http.StatusConflict, "task_running_wait")
		return
	}

	if h.state.Phase != "writing" {
		h.writeErrorReq(w, r, http.StatusBadRequest, "phase_not_writing")
		return
	}

	if err := story.ConfirmChapterAction(h.state, h.progressPath); err != nil {
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	ch := h.state.Chapters[h.state.CurrentChapterIndex-1]
	h.logger.SuccessKey("log.chapter_confirmed", ch.Num)
	// Bestseller act critic: if this manual confirmation closes a
	// pre-planning act, launch the act analysis in the background.
	h.maybeAnalyzeCompletedAct(ch.Num)
	h.writeJSON(w, http.StatusOK, story.ProgressView(h.state))
	h.startKnowledgeSync()
}

// chapterRecoveryTarget resolves {num} from the URL and validates it is a real
// chapter index. Shared by the resume/fact-check/reset handlers.
func (h *Handlers) chapterRecoveryTarget(w http.ResponseWriter, r *http.Request) (int, bool) {
	numStr := r.PathValue("num")
	num, err := strconv.Atoi(numStr)
	if err != nil {
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_chapter_num", numStr)
		return 0, false
	}
	idx := -1
	for i := range h.state.Chapters {
		if h.state.Chapters[i].Num == num {
			idx = i
			break
		}
	}
	if idx == -1 {
		h.writeErrorReq(w, r, http.StatusNotFound, "chapter_not_found", num)
		return 0, false
	}
	return idx, true
}

// PostChapterResume re-runs ONLY the cheap post-prose steps (summary →
// critic/fact-check → memory) on the draft that an interrupted generation
// checkpointed (DraftSavedAt). The prose itself is never regenerated.
func (h *Handlers) PostChapterResume(w http.ResponseWriter, r *http.Request) {
	if !h.tryStartTask() {
		h.writeErrorReq(w, r, http.StatusConflict, "task_running_wait")
		return
	}
	idx, ok := h.chapterRecoveryTarget(w, r)
	if !ok {
		h.endTask()
		return
	}
	if h.state.Phase != "writing" {
		h.endTask()
		h.writeErrorReq(w, r, http.StatusBadRequest, "phase_not_writing")
		return
	}
	chNum := h.state.Chapters[idx].Num
	go func() {
		defer h.endTask()
		h.logger.TaskStart("chapter_resume")
		ctx := h.activateSkills(h.taskCtx, story.SkillScopeChapterGenerate, true)
		err := story.ResumeChapterAction(ctx, h.apiCfg, h.cfg, h.state, h.progressPath, h.settings, h.skills, chNum, h.logger)
		if err != nil {
			var wcErr *story.WritingConflictError
			switch {
			case errors.As(err, &wcErr):
				h.logger.WarnKey("log.chapter_write_conflict_pause")
			case h.TaskStoppedByUser():
				h.logger.WarnKey("log.chapter_write_cancelled")
			case story.IsSummaryStepFailed(err):
				// The resume itself hit the task deadline / stop mid-summary — the
				// draft stays resumable; say so instead of a generic failure.
				if errors.Is(err, context.DeadlineExceeded) || errors.Is(context.Cause(ctx), context.DeadlineExceeded) {
					h.logger.WarnKey("log.chapter_write_aborted_deadline")
				} else {
					h.logger.WarnKey("log.chapter_write_aborted", err.Error())
				}
			case ctx.Err() != nil:
				h.logger.WarnKey("log.chapter_write_cancelled")
			default:
				h.logger.ErrorKey("log.chapter_resume_failed", err)
			}
			h.logger.TaskEnd("chapter_resume", false)
			h.broadcastProgress()
			return
		}
		h.logger.TaskEnd("chapter_resume", true)
		h.broadcastProgress()
	}()
	h.writeJSON(w, http.StatusAccepted, map[string]string{"status": "resuming"})
}

// PostChapterFactCheck re-runs the critic/fact-check pass over the CURRENT
// draft of a chapter without regenerating its prose or summary.
func (h *Handlers) PostChapterFactCheck(w http.ResponseWriter, r *http.Request) {
	if !h.tryStartTask() {
		h.writeErrorReq(w, r, http.StatusConflict, "task_running_wait")
		return
	}
	idx, ok := h.chapterRecoveryTarget(w, r)
	if !ok {
		h.endTask()
		return
	}
	if h.state.Phase != "writing" {
		h.endTask()
		h.writeErrorReq(w, r, http.StatusBadRequest, "phase_not_writing")
		return
	}
	chNum := h.state.Chapters[idx].Num
	go func() {
		defer h.endTask()
		h.logger.TaskStart("chapter_factcheck")
		ctx := h.activateSkills(h.taskCtx, story.SkillScopeChapterFactCheck, true)
		err := story.FactCheckChapterAction(ctx, h.apiCfg, h.cfg, h.state, h.progressPath, h.settings, h.skills, chNum, h.logger)
		if err != nil {
			var wcErr *story.WritingConflictError
			switch {
			case errors.As(err, &wcErr):
				h.logger.WarnKey("log.chapter_write_conflict_pause")
			case h.TaskStoppedByUser():
				h.logger.WarnKey("log.chapter_write_cancelled")
			case ctx.Err() != nil:
				h.logger.WarnKey("log.chapter_write_aborted_deadline")
			default:
				h.logger.ErrorKey("log.chapter_factcheck_failed", err)
			}
			h.logger.TaskEnd("chapter_factcheck", false)
			h.broadcastProgress()
			return
		}
		h.logger.TaskEnd("chapter_factcheck", true)
		h.broadcastProgress()
	}()
	h.writeJSON(w, http.StatusAccepted, map[string]string{"status": "factchecking"})
}

// PostChapterReset discards the generated prose/summary/blocks of the CURRENT
// chapter so the next generation starts clean. Synchronous (no LLM calls).
func (h *Handlers) PostChapterReset(w http.ResponseWriter, r *http.Request) {
	if h.isTaskRunning() {
		h.writeErrorReq(w, r, http.StatusConflict, "task_running_wait")
		return
	}
	idx, ok := h.chapterRecoveryTarget(w, r)
	if !ok {
		return
	}
	if h.state.Phase != "writing" {
		h.writeErrorReq(w, r, http.StatusBadRequest, "phase_not_writing")
		return
	}
	chNum := h.state.Chapters[idx].Num
	if err := story.ResetChapterAction(h.state, h.progressPath, chNum, h.logger); err != nil {
		h.writeErrorReq(w, r, http.StatusBadRequest, "chapter_reset_failed", err.Error())
		return
	}
	h.broadcastProgress()
	h.writeJSON(w, http.StatusOK, story.ProgressView(h.state))
}

func (h *Handlers) PostChapterEdit(w http.ResponseWriter, r *http.Request) {
	if h.rejectIfTaskRunning(w, r) {
		return
	}

	var req story.EditChapterContentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if req.Operation == "" {
		h.writeErrorReq(w, r, http.StatusBadRequest, "chapter_edit_op_required")
		return
	}
	if req.NewText == "" && req.Operation != story.EditOpReplaceText {
		h.writeErrorReq(w, r, http.StatusBadRequest, "chapter_edit_text_required")
		return
	}

	totalLines, err := story.EditChapterContent(h.state, req)
	if err != nil {
		h.writeErrorReq(w, r, http.StatusBadRequest, "chapter_edit_failed", err.Error())
		return
	}

	if err := story.SaveProgress(h.progressPath, h.state); err != nil {
		h.writeErrorReq(w, r, http.StatusInternalServerError, "save_progress_failed", err)
		return
	}

	story.SaveChapterMarkdown(h.projectDir(), h.getChapterByNum(req.ChapterNum), "")
	h.startKnowledgeSync()
	h.broadcastProgress()
	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":     true,
		"total_lines": totalLines,
		"chapter":     h.getChapterByNum(req.ChapterNum),
	})
}

func (h *Handlers) getChapterByNum(num int) story.ChapterState {
	for _, ch := range h.state.Chapters {
		if ch.Num == num {
			return ch
		}
	}
	return story.ChapterState{}
}

func (h *Handlers) PostChapterRevise(w http.ResponseWriter, r *http.Request) {
	if !h.tryStartTask() {
		h.writeErrorReq(w, r, http.StatusConflict, "task_running_wait")
		return
	}

	var body chapterRevisionRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || (strings.TrimSpace(body.Feedback) == "" && len(body.WorldviewIDs) == 0) {
		h.endTask()
		h.writeErrorReq(w, r, http.StatusBadRequest, "missing_feedback")
		return
	}
	idx := h.state.CurrentChapterIndex
	if idx < 0 || idx >= len(h.state.Chapters) {
		h.endTask()
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_chapter_num")
		return
	}
	if !h.prepareSettingRevision(w, r, h.state.Chapters[idx].Num, &body) {
		h.endTask()
		return
	}

	go func() {
		defer h.endTask()
		h.logger.TaskStart("chapter_revision")
		ctx := h.activateSkills(h.taskCtx, story.SkillScopeChapterRevise, true)

		h.logger.InfoKey("log.chapter_revising")
		err := story.ReviseChapterAction(ctx, h.apiCfg, h.cfg, h.state, h.progressPath, body.Feedback, h.settings, h.logger)

		if err != nil {
			if ctx.Err() != nil {
				h.logger.WarnKey("log.chapter_revise_cancelled")
				h.logger.TaskEnd("chapter_revision", false)
			} else {
				h.logger.ErrorKey("log.chapter_revise_failed", err)
				h.logger.TaskEnd("chapter_revision", false)
			}
			return
		}

		h.logger.SuccessKey("log.chapter_revised")
		h.logger.TaskEnd("chapter_revision", true)
		h.broadcastProgress()
	}()

	h.writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}

// PostChapterReviseSpecific revisa un capítulo concreto por número (incluso aceptados);
// solo afecta a ese capítulo, no a otros capítulos ni a las tramas.
func (h *Handlers) PostChapterReviseSpecific(w http.ResponseWriter, r *http.Request) {
	numStr := r.PathValue("num")
	var num int
	if _, err := fmt.Sscanf(numStr, "%d", &num); err != nil {
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_chapter_num")
		return
	}

	if !h.tryStartTask() {
		h.writeErrorReq(w, r, http.StatusConflict, "task_running_wait")
		return
	}

	var body chapterRevisionRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || (strings.TrimSpace(body.Feedback) == "" && len(body.WorldviewIDs) == 0) {
		h.endTask()
		h.writeErrorReq(w, r, http.StatusBadRequest, "missing_feedback")
		return
	}
	if !h.prepareSettingRevision(w, r, num, &body) {
		h.endTask()
		return
	}

	go func() {
		defer h.endTask()
		h.logger.TaskStart("chapter_revision")
		ctx := h.activateSkills(h.taskCtx, story.SkillScopeChapterRevise, true)

		h.logger.InfoKey("log.chapter_specific_revising", num)
		err := story.ReviseSpecificChapterAction(ctx, h.apiCfg, h.cfg, h.state, h.progressPath, num, body.Feedback, h.settings, h.logger)

		if err != nil {
			if ctx.Err() != nil {
				h.logger.WarnKey("log.chapter_revise_cancelled")
			} else {
				h.logger.ErrorKey("log.chapter_revise_failed", err)
			}
			h.logger.TaskEnd("chapter_revision", false)
			return
		}

		h.logger.TaskEnd("chapter_revision", true)
		h.broadcastProgress()
	}()

	h.writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}

// PostChaptersSmoothTransitions revisa en lote las uniones entre capítulos escritos.
// Conecta el final del capítulo anterior con la apertura de este; escribe solo lo mínimo necesario para abrir el capítulo actual.
func (h *Handlers) PostChaptersSmoothTransitions(w http.ResponseWriter, r *http.Request) {
	if !h.ensureProject(w, r) {
		return
	}

	pairs := 0
	for i := 1; i < len(h.state.Chapters); i++ {
		if h.state.Chapters[i].Status == story.StatusAccepted && h.state.Chapters[i].Content != "" &&
			h.state.Chapters[i-1].Status == story.StatusAccepted && h.state.Chapters[i-1].Content != "" {
			pairs++
		}
	}
	if pairs == 0 {
		h.writeErrorReq(w, r, http.StatusBadRequest, "no_transitions_to_optimize")
		return
	}

	if !h.tryStartTask() {
		h.writeErrorReq(w, r, http.StatusConflict, "task_running_wait")
		return
	}

	go func() {
		defer h.endTask()
		h.logger.TaskStart("smooth_transitions")
		ctx := h.activateSkills(h.taskCtx, story.SkillScopeChapterRevise, true)

		err := story.SmoothTransitionsAction(ctx, h.apiCfg, h.cfg, h.state, h.progressPath, h.logger)
		if err != nil {
			if ctx.Err() != nil {
				h.logger.WarnKey("log.smooth_transitions_cancelled")
			} else {
				h.logger.ErrorKey("log.smooth_transitions_failed", err)
			}
			h.logger.TaskEnd("smooth_transitions", false)
			h.broadcastProgress()
			return
		}

		h.logger.TaskEnd("smooth_transitions", true)
		h.broadcastProgress()
	}()

	h.writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}

func (h *Handlers) DeleteChapter(w http.ResponseWriter, r *http.Request) {
	if h.isTaskRunning() {
		h.writeErrorReq(w, r, http.StatusConflict, "delete_chapter_locked")
		return
	}

	num, err := story.DeleteFrontierChapter(h.state, h.projectDir())
	if err != nil {
		switch err {
		case story.ErrNoChaptersToDelete:
			h.writeErrorReq(w, r, http.StatusBadRequest, "no_chapters_to_delete")
		case story.ErrWritingChapterCannotDelete:
			h.writeErrorReq(w, r, http.StatusConflict, "writing_chapter_cannot_delete")
		case story.ErrDeleteFrontierUnavailable:
			h.writeErrorReq(w, r, http.StatusConflict, "delete_frontier_unavailable")
		default:
			h.writeErrorReq(w, r, http.StatusInternalServerError, "save_progress_failed", err)
		}
		return
	}

	if err := story.SaveProgress(h.progressPath, h.state); err != nil {
		h.writeErrorReq(w, r, http.StatusInternalServerError, "save_progress_failed", err)
		return
	}

	h.logger.SuccessKey("log.chapter_deleted", num)
	h.writeJSON(w, http.StatusOK, story.ProgressView(h.state))
	h.startKnowledgeSync()
}

func (h *Handlers) DeleteOutline(w http.ResponseWriter, r *http.Request) {
	if h.isTaskRunning() {
		h.writeErrorReq(w, r, http.StatusConflict, "delete_outline_locked")
		return
	}

	for _, ch := range h.state.Chapters {
		if ch.Status == story.StatusWriting || ch.Status == story.StatusReview {
			h.writeErrorReq(w, r, http.StatusConflict, "writing_chapter_present_delete")
			return
		}
	}

	h.state.Title = ""
	h.state.CorePrompt = ""
	h.state.OutlineBatches = nil
	h.state.Chapters = nil
	h.state.StoryConfigSnapshot = nil
	h.state.CurrentChapterIndex = 0

	if err := story.SaveProgress(h.progressPath, h.state); err != nil {
		h.writeErrorReq(w, r, http.StatusInternalServerError, "save_progress_failed", err)
		return
	}

	h.logger.SuccessKey("log.outline_deleted")
	h.writeJSON(w, http.StatusOK, story.ProgressView(h.state))
	h.startKnowledgeSync()
}

func (h *Handlers) PutChapterOutline(w http.ResponseWriter, r *http.Request) {
	if !h.tryStartTask() {
		h.writeErrorReq(w, r, http.StatusConflict, "task_running_wait")
		return
	}
	background := false
	defer func() {
		if !background {
			h.endTask()
		}
	}()

	numStr := r.PathValue("num")
	var num int
	if _, err := fmt.Sscanf(numStr, "%d", &num); err != nil {
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_chapter_num")
		return
	}

	var body struct {
		Title      string                           `json:"title"`
		Outline    string                           `json:"outline"`
		Characters *[]story.OutlineChapterCharacter `json:"characters"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	before := append([]story.ChapterState(nil), h.state.Chapters...)
	if err := story.EditChapterOutline(h.state, num, body.Title, body.Outline, body.Characters); err != nil {
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	if err := story.SaveProgress(h.progressPath, h.state); err != nil {
		h.state.Chapters = before
		h.writeErrorReq(w, r, http.StatusInternalServerError, "save_progress_failed", err)
		return
	}

	h.logger.SuccessKey("log.chapter_outline_updated", num)
	h.writeJSON(w, http.StatusOK, story.ProgressView(h.state))
	if len(h.state.Foreshadows) > 0 {
		background = true
		go h.runForeshadowOutlineCheck()
	}
}

func (h *Handlers) PostSettingsReconcile(w http.ResponseWriter, r *http.Request) {
	if !h.tryStartTask() {
		h.writeErrorReq(w, r, http.StatusConflict, "task_running_wait")
		return
	}

	var body config.StoryConfig
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.endTask()
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	go func() {
		defer h.endTask()
		h.logger.TaskStart("settings_reconciliation")
		ctx := h.taskCtx

		h.logger.InfoKey("log.settings_reconciling")
		err := story.ReconcileSettingsAction(ctx, h.apiCfg, h.cfg, h.state, body, h.settings, h.progressPath, h.cfgPath, h.logger)

		if err != nil {
			if ctx.Err() != nil {
				h.logger.WarnKey("log.settings_reconcile_cancelled")
				h.logger.TaskEnd("settings_reconciliation", false)
			} else {
				h.logger.ErrorKey("log.settings_reconcile_failed", err)
				h.logger.TaskEnd("settings_reconciliation", false)
			}
			return
		}

		h.logger.SuccessKey("log.settings_reconcile_done")
		h.logger.TaskEnd("settings_reconciliation", true)
		h.broadcastProgress()
	}()

	h.writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}

func (h *Handlers) DeleteChaptersFrom(w http.ResponseWriter, r *http.Request) {
	if h.isTaskRunning() {
		h.writeErrorReq(w, r, http.StatusConflict, "delete_chapter_locked")
		return
	}

	numStr := r.PathValue("num")
	var num int
	if _, err := fmt.Sscanf(numStr, "%d", &num); err != nil {
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_chapter_num")
		return
	}

	startIdx := -1
	for i, ch := range h.state.Chapters {
		if ch.Num == num {
			startIdx = i
			break
		}
	}
	if startIdx == -1 {
		h.writeErrorReq(w, r, http.StatusNotFound, "chapter_n_not_found", num)
		return
	}

	for i := startIdx; i < len(h.state.Chapters); i++ {
		if h.state.Chapters[i].Status == story.StatusWriting {
			h.writeErrorReq(w, r, http.StatusConflict, "writing_range_has_writing")
			return
		}
	}

	deletedCount := len(h.state.Chapters) - startIdx

	for i := startIdx; i < len(h.state.Chapters); i++ {
		ch := &h.state.Chapters[i]
		fsutil.Delete(story.ChapterMarkdownPath(h.projectDir(), ch.Num))
		ch.Content = ""
		ch.Summary = ""
		ch.Status = story.StatusPending
	}

	if h.state.CurrentChapterIndex >= startIdx {
		h.state.CurrentChapterIndex = startIdx
	}

	if err := story.SaveProgress(h.progressPath, h.state); err != nil {
		h.writeErrorReq(w, r, http.StatusInternalServerError, "save_progress_failed", err)
		return
	}

	h.logger.SuccessKey("log.chapters_deleted_from", num, deletedCount)
	h.writeJSON(w, http.StatusOK, story.ProgressView(h.state))
	h.startKnowledgeSync()
}

func chapterProgress(chapters []story.ChapterState) (total, accepted int) {
	for _, ch := range chapters {
		if ch.Inherited {
			continue
		}
		total++
		if ch.Status == story.StatusAccepted {
			accepted++
		}
	}
	return
}

func (h *Handlers) broadcastProgress() {
	total, accepted := chapterProgress(h.state.Chapters)
	var pct float64
	if total > 0 {
		pct = float64(accepted) / float64(total) * 100
	}
	h.logger.ProgressUpdate(map[string]interface{}{
		"phase":             h.state.Phase,
		"title":             h.state.Title,
		"current_chapter":   h.state.CurrentChapterIndex,
		"total_chapters":    total,
		"accepted_chapters": accepted,
		"percent":           pct,
		"is_task_running":   h.isTaskRunning(),
	})
}

func (h *Handlers) GetVersion(w http.ResponseWriter, r *http.Request) {
	h.writeJSON(w, http.StatusOK, map[string]string{"version": h.version})
}

func (h *Handlers) GetStatus(w http.ResponseWriter, r *http.Request) {
	lang := i18n.LangEN
	if h.cfg != nil {
		lang = i18n.NormalizeLanguage(h.cfg.Language)
	}
	running := h.isTaskRunning()
	totalChapters, _ := chapterProgress(h.state.Chapters)
	resp := map[string]interface{}{
		"phase":            h.state.Phase,
		"title":            h.state.Title,
		"total_chapters":   totalChapters,
		"is_task_running":  running,
		"auto_confirm":     h.isAutoConfirmOn(),
		"project_language": lang,
	}
	if running {
		resp["active_work"] = h.activeWorkCount()
		if task := h.logger.CurrentTask(); task != "" {
			resp["current_task"] = task
		}
		if h.taskTokens != nil {
			prompt, completion := h.taskTokens.Snapshot()
			resp["token_usage"] = map[string]int{
				"prompt_tokens":     prompt,
				"completion_tokens": completion,
			}
		}
	}
	h.writeJSON(w, http.StatusOK, resp)
}

func (h *Handlers) GetForeshadows(w http.ResponseWriter, r *http.Request) {
	if h.state.Foreshadows == nil {
		h.writeJSON(w, http.StatusOK, []story.Foreshadow{})
		return
	}
	h.writeJSON(w, http.StatusOK, h.state.Foreshadows)
}

func (h *Handlers) GetForeshadowsRoadmap(w http.ResponseWriter, r *http.Request) {
	if !h.ensureProject(w, r) {
		return
	}
	markdown := story.BuildForeshadowRoadmapMarkdown(h.state)
	h.writeJSON(w, http.StatusOK, map[string]string{
		"markdown": markdown,
		"path":     story.ForeshadowRoadmapPath(h.projectDir()),
	})
}

func (h *Handlers) persistForeshadowRoadmap() {
	if err := story.SaveForeshadowRoadmap(h.projectDir(), h.state); err != nil {
		h.logger.WarnKey("log.foreshadow_roadmap_save_failed", err)
	}
}

func (h *Handlers) PostForeshadowsSuggest(w http.ResponseWriter, r *http.Request) {
	if !h.tryStartTask() {
		h.writeErrorReq(w, r, http.StatusConflict, "task_running_wait")
		return
	}

	if len(h.state.Chapters) == 0 {
		h.endTask()
		h.writeErrorReq(w, r, http.StatusBadRequest, "need_generate_outline_first")
		return
	}

	go func() {
		defer h.endTask()
		h.logger.TaskStart("foreshadow_suggest")
		ctx := h.activateSkills(h.taskCtx, story.SkillScopeForeshadowPlan, true)

		h.logger.InfoKey("log.foreshadow_suggesting")
		suggestions, err := story.SuggestForeshadows(ctx, h.apiCfg, h.cfg, h.state, h.logger)

		if err != nil {
			if ctx.Err() != nil {
				h.logger.WarnKey("log.foreshadow_suggest_cancelled")
				h.logger.TaskEnd("foreshadow_suggest", false)
			} else {
				h.logger.ErrorKey("log.foreshadow_suggest_failed", err)
				h.logger.TaskEnd("foreshadow_suggest", false)
			}
			return
		}

		h.logger.SuccessKey("log.foreshadow_suggest_done", len(suggestions))
		h.logger.TaskEnd("foreshadow_suggest", true)
		h.logger.ForeshadowSuggestions(suggestions)
	}()

	h.writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}

func (h *Handlers) PostForeshadow(w http.ResponseWriter, r *http.Request) {
	if h.rejectIfTaskRunning(w, r) {
		return
	}
	var req struct {
		Name          string `json:"name"`
		Description   string `json:"description"`
		PlantChapter  int    `json:"plant_chapter"`
		TargetChapter int    `json:"target_chapter"`
		TargetHorizon string `json:"target_horizon"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if req.Name == "" {
		h.writeErrorReq(w, r, http.StatusBadRequest, "foreshadow_name_required")
		return
	}
	if req.Description == "" {
		h.writeErrorReq(w, r, http.StatusBadRequest, "foreshadow_desc_required")
		return
	}

	fs := story.Foreshadow{
		ID:            story.NextForeshadowID(h.state.Foreshadows),
		Name:          req.Name,
		Description:   req.Description,
		PlantChapter:  req.PlantChapter,
		TargetChapter: req.TargetChapter,
		TargetHorizon: req.TargetHorizon,
		Status:        story.ForeshadowPlanted,
		Events:        []story.ForeshadowEvent{},
	}

	h.state.Foreshadows = append(h.state.Foreshadows, fs)

	if err := story.SaveProgress(h.progressPath, h.state); err != nil {
		h.writeErrorReq(w, r, http.StatusInternalServerError, "save_failed", err)
		return
	}

	h.persistForeshadowRoadmap()
	h.writeJSON(w, http.StatusOK, fs)
}

func (h *Handlers) PutForeshadow(w http.ResponseWriter, r *http.Request) {
	if h.rejectIfTaskRunning(w, r) {
		return
	}
	idStr := r.PathValue("id")
	var id int
	if _, err := fmt.Sscanf(idStr, "%d", &id); err != nil {
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_foreshadow_id")
		return
	}

	var req struct {
		Name          string                 `json:"name"`
		Description   string                 `json:"description"`
		PlantChapter  int                    `json:"plant_chapter"`
		TargetChapter int                    `json:"target_chapter"`
		TargetHorizon string                 `json:"target_horizon"`
		Status        story.ForeshadowStatus `json:"status"`
		Resolution    string                 `json:"resolution"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	idx := -1
	for i, fs := range h.state.Foreshadows {
		if fs.ID == id {
			idx = i
			break
		}
	}
	if idx == -1 {
		h.writeErrorReq(w, r, http.StatusNotFound, "foreshadow_not_found")
		return
	}

	fs := &h.state.Foreshadows[idx]
	if req.Name != "" {
		fs.Name = req.Name
	}
	if req.Description != "" {
		fs.Description = req.Description
	}
	if req.PlantChapter > 0 {
		fs.PlantChapter = req.PlantChapter
	}
	if req.TargetChapter > 0 {
		fs.TargetChapter = req.TargetChapter
	}
	if req.TargetHorizon != "" {
		fs.TargetHorizon = req.TargetHorizon
		fs.TargetChapter = 0
	}
	if req.Status != "" {
		fs.Status = req.Status
	}
	if req.Resolution != "" {
		fs.Resolution = req.Resolution
	}

	if err := story.SaveProgress(h.progressPath, h.state); err != nil {
		h.writeErrorReq(w, r, http.StatusInternalServerError, "save_failed", err)
		return
	}

	h.persistForeshadowRoadmap()
	h.writeJSON(w, http.StatusOK, fs)
}

func (h *Handlers) DeleteForeshadow(w http.ResponseWriter, r *http.Request) {
	if h.rejectIfTaskRunning(w, r) {
		return
	}
	idStr := r.PathValue("id")
	var id int
	if _, err := fmt.Sscanf(idStr, "%d", &id); err != nil {
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_foreshadow_id")
		return
	}

	idx := -1
	for i, fs := range h.state.Foreshadows {
		if fs.ID == id {
			idx = i
			break
		}
	}
	if idx == -1 {
		h.writeErrorReq(w, r, http.StatusNotFound, "foreshadow_not_found")
		return
	}

	h.state.Foreshadows = append(h.state.Foreshadows[:idx], h.state.Foreshadows[idx+1:]...)

	if err := story.SaveProgress(h.progressPath, h.state); err != nil {
		h.writeErrorReq(w, r, http.StatusInternalServerError, "save_failed", err)
		return
	}

	h.persistForeshadowRoadmap()
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *Handlers) PostForeshadowsConfirm(w http.ResponseWriter, r *http.Request) {
	if !h.tryStartTask() {
		h.writeErrorReq(w, r, http.StatusConflict, "task_running_wait")
		return
	}
	background := false
	defer func() {
		if !background {
			h.endTask()
		}
	}()
	var req struct {
		Foreshadows []story.Foreshadow `json:"foreshadows"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	for i := range req.Foreshadows {
		req.Foreshadows[i].ID = story.NextForeshadowID(h.state.Foreshadows) + i
		req.Foreshadows[i].Status = story.ForeshadowPlanted
		if req.Foreshadows[i].Events == nil {
			req.Foreshadows[i].Events = []story.ForeshadowEvent{}
		}
	}

	before := h.state.Foreshadows
	h.state.Foreshadows = append(h.state.Foreshadows, req.Foreshadows...)

	if err := story.SaveProgress(h.progressPath, h.state); err != nil {
		h.state.Foreshadows = before
		h.writeErrorReq(w, r, http.StatusInternalServerError, "save_failed", err)
		return
	}

	h.persistForeshadowRoadmap()
	h.broadcastProgress()
	h.writeJSON(w, http.StatusOK, h.state.Foreshadows)
	if len(h.state.Foreshadows) > 0 {
		background = true
		go h.runForeshadowOutlineCheck()
	}
}

// —— enterwaterthread handlers ——

// PostImportSplit this cutchapterbrowse(, no AI). not howinside.
func (h *Handlers) PostImportSplit(w http.ResponseWriter, r *http.Request) {
	if !h.ensureProject(w, r) {
		return
	}
	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Content) == "" {
		h.writeErrorReq(w, r, http.StatusBadRequest, "missing_content")
		return
	}
	chapters, _ := story.SplitImportContent(body.Content)
	h.writeJSON(w, http.StatusOK, map[string]any{"chapters": story.BuildImportPreview(chapters)})
}

// PostImportStart openenterwaterthread(): cutchapterpassageplate → yuandivide → chapterbig/want to(disconnectdotrun)→ dividevolumeremit.
func (h *Handlers) PostImportStart(w http.ResponseWriter, r *http.Request) {
	if !h.ensureProject(w, r) {
		return
	}
	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Content) == "" {
		h.writeErrorReq(w, r, http.StatusBadRequest, "missing_content")
		return
	}
	if len(h.state.Chapters) > 0 {
		h.writeErrorReq(w, r, http.StatusConflict, "import_project_not_empty")
		return
	}
	if !h.tryStartTask() {
		h.writeErrorReq(w, r, http.StatusConflict, "task_running_wait")
		return
	}
	go func() {
		defer h.endTask()
		h.logger.TaskStart("import_pipeline")
		ctx := h.activateSkills(h.taskCtx, story.SkillScopeImportAnalyze, true)
		err := story.ImportStartAction(ctx, h.apiCfg, h.cfg, h.state, h.settings, body.Content, h.progressPath, h.cfgPath, story.ImportStatePath(h.projectDir()), h.logger)
		h.finishImportTask(ctx, err)
	}()
	h.writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}

// PostImportResume fromdisconnectdotenterwaterthread().
func (h *Handlers) PostImportResume(w http.ResponseWriter, r *http.Request) {
	if !h.ensureProject(w, r) {
		return
	}
	if story.LoadImportState(story.ImportStatePath(h.projectDir())) == nil {
		h.writeErrorReq(w, r, http.StatusBadRequest, "import_nothing_to_resume")
		return
	}
	if !h.tryStartTask() {
		h.writeErrorReq(w, r, http.StatusConflict, "task_running_wait")
		return
	}
	go func() {
		defer h.endTask()
		h.logger.TaskStart("import_pipeline")
		ctx := h.activateSkills(h.taskCtx, story.SkillScopeImportAnalyze, true)
		err := story.ImportResumeAction(ctx, h.apiCfg, h.cfg, h.state, story.ImportStatePath(h.projectDir()), h.progressPath, h.cfgPath, h.logger)
		h.finishImportTask(ctx, err)
	}()
	h.writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}

func (h *Handlers) finishImportTask(ctx context.Context, err error) {
	if err != nil {
		if ctx.Err() != nil {
			h.logger.WarnKey("log.import_task_cancelled")
		} else {
			h.logger.ErrorKey("log.import_task_failed", err)
		}
		h.logger.TaskEnd("import_pipeline", false)
		h.broadcastProgress()
		return
	}
	h.logger.TaskEnd("import_pipeline", true)
	h.broadcastProgress()
}

// GetImportStatus enterdisconnectdotposture().
func (h *Handlers) GetImportStatus(w http.ResponseWriter, r *http.Request) {
	if !h.ensureProject(w, r) {
		return
	}
	st := story.LoadImportState(story.ImportStatePath(h.projectDir()))
	if st == nil {
		h.writeJSON(w, http.StatusOK, map[string]any{"active": false})
		return
	}
	h.writeJSON(w, http.StatusOK, st)
}

func (h *Handlers) PostOutlineGenerateContinuation(w http.ResponseWriter, r *http.Request) {
	if !h.tryStartTask() {
		h.writeErrorReq(w, r, http.StatusConflict, "task_running_wait")
		return
	}

	// The same batch endpoint handles initial planning, append and explicit last-batch replacement.
	if !story.ContinuationOutlineAllowed(h.state.Phase, len(h.state.Chapters)) {
		h.endTask()
		h.writeErrorReq(w, r, http.StatusBadRequest, "phase_not_outline")
		return
	}

	var body story.OutlineBatchRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.endTask()
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_request_body")
		return
	}
	if err := story.ValidateOutlineBatch(h.state, body, i18n.FromRequest(r)); err != nil {
		h.endTask()
		h.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	go func() {
		defer h.endTask()
		h.logger.TaskStart("continuation_outline")
		ctx := h.activateSkills(h.taskCtx, story.SkillScopeOutlineGenerate, true)

		h.logger.InfoKey("log.continuation_outline_generating")
		err := story.GenerateOutlineBatch(ctx, h.apiCfg, h.cfg, h.state, h.settings, body, h.progressPath, h.logger)

		if err != nil {
			if ctx.Err() != nil {
				h.logger.WarnKey("log.continuation_outline_cancelled")
				h.logger.TaskEnd("continuation_outline", false)
			} else {
				h.logger.ErrorKey("log.continuation_outline_failed", err)
				h.logger.TaskEnd("continuation_outline", false)
			}
			return
		}

		h.logger.SuccessKey("log.continuation_outline_done")
		h.logger.TaskEnd("continuation_outline", true)
		h.broadcastProgress()
	}()

	h.writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}

func (h *Handlers) SSEHandler(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := h.logger.Subscribe()
	defer h.logger.Unsubscribe(ch)

	ctx := r.Context()

	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return
			}
			_, err := w.Write(sse.Format(msg))
			if err != nil {
				return
			}
			flusher.Flush()
		case <-ctx.Done():
			return
		}
	}
}

func (h *Handlers) GetSettings(w http.ResponseWriter, r *http.Request) {
	view := *h.settings
	view.StoryChanges, view.StorySynced = nil, nil
	h.writeJSON(w, http.StatusOK, &view)
}

func (h *Handlers) PostCharacter(w http.ResponseWriter, r *http.Request) {
	if h.rejectIfTaskRunning(w, r) {
		return
	}
	var c story.Character
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if c.Name == "" {
		h.writeErrorReq(w, r, http.StatusBadRequest, "character_name_empty")
		return
	}

	c.ID = h.settings.NextCharacterID()
	h.settings.Characters = append(h.settings.Characters, c)

	if err := story.SaveProjectSettings(h.settingsPath, h.settings); err != nil {
		h.writeErrorReq(w, r, http.StatusInternalServerError, "save_failed", err)
		return
	}

	h.writeJSON(w, http.StatusOK, c)
}

func (h *Handlers) PutCharacter(w http.ResponseWriter, r *http.Request) {
	if h.rejectIfTaskRunning(w, r) {
		return
	}
	id := r.PathValue("id")

	var req story.Character
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	for i, c := range h.settings.Characters {
		if c.ID == id {
			if req.Name != "" {
				h.settings.Characters[i].Name = req.Name
			}
			if req.Age != "" {
				h.settings.Characters[i].Age = req.Age
			}
			if req.Appearance != "" {
				h.settings.Characters[i].Appearance = req.Appearance
			}
			if req.Personality != "" {
				h.settings.Characters[i].Personality = req.Personality
			}
			if req.Background != "" {
				h.settings.Characters[i].Background = req.Background
			}
			if req.Motivation != "" {
				h.settings.Characters[i].Motivation = req.Motivation
			}
			if req.Abilities != "" {
				h.settings.Characters[i].Abilities = req.Abilities
			}
			if req.Notes != "" {
				h.settings.Characters[i].Notes = req.Notes
			}
			if req.Goals != "" {
				h.settings.Characters[i].Goals = req.Goals
			}
			if req.Flaws != "" {
				h.settings.Characters[i].Flaws = req.Flaws
			}
			if req.Strengths != "" {
				h.settings.Characters[i].Strengths = req.Strengths
			}
			if req.Arc != "" {
				h.settings.Characters[i].Arc = req.Arc
			}
			if len(req.Extra) > 0 {
				if h.settings.Characters[i].Extra == nil {
					h.settings.Characters[i].Extra = map[string]string{}
				}
				for k, v := range req.Extra {
					if strings.TrimSpace(v) != "" {
						h.settings.Characters[i].Extra[k] = v
					}
				}
			}

			if err := story.SaveProjectSettings(h.settingsPath, h.settings); err != nil {
				h.writeErrorReq(w, r, http.StatusInternalServerError, "save_failed", err)
				return
			}

			h.writeJSON(w, http.StatusOK, h.settings.Characters[i])
			return
		}
	}

	h.writeErrorReq(w, r, http.StatusNotFound, "character_not_found")
}

func (h *Handlers) DeleteCharacter(w http.ResponseWriter, r *http.Request) {
	if h.rejectIfTaskRunning(w, r) {
		return
	}
	id := r.PathValue("id")

	for i, c := range h.settings.Characters {
		if c.ID == id {
			h.settings.Characters = append(h.settings.Characters[:i], h.settings.Characters[i+1:]...)
			if err := story.SaveProjectSettings(h.settingsPath, h.settings); err != nil {
				h.writeErrorReq(w, r, http.StatusInternalServerError, "save_failed", err)
				return
			}
			h.writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
			return
		}
	}

	h.writeErrorReq(w, r, http.StatusNotFound, "character_not_found")
}

func (h *Handlers) PostWorldview(w http.ResponseWriter, r *http.Request) {
	if h.rejectIfTaskRunning(w, r) {
		return
	}
	var wv story.WorldviewEntry
	if err := json.NewDecoder(r.Body).Decode(&wv); err != nil {
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	wv.Name, wv.Description = strings.TrimSpace(wv.Name), strings.TrimSpace(wv.Description)
	if wv.Name == "" || wv.Description == "" {
		h.writeErrorReq(w, r, http.StatusBadRequest, "worldview_field_empty")
		return
	}

	wv.ID = h.settings.NextWorldviewID()
	next := *h.settings
	next.Worldview = append(append([]story.WorldviewEntry(nil), h.settings.Worldview...), wv)

	if err := story.SaveProjectSettings(h.settingsPath, &next); err != nil {
		h.writeErrorReq(w, r, http.StatusInternalServerError, "save_failed", err)
		return
	}
	*h.settings = next
	h.logger.SettingsUpdated()

	h.writeJSON(w, http.StatusOK, wv)
}

func (h *Handlers) PutWorldview(w http.ResponseWriter, r *http.Request) {
	if h.rejectIfTaskRunning(w, r) {
		return
	}
	id := r.PathValue("id")

	var req story.WorldviewEntry
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	for i, wv := range h.settings.Worldview {
		if wv.ID == id {
			next := *h.settings
			next.Worldview = append([]story.WorldviewEntry(nil), h.settings.Worldview...)
			if req.Name != "" {
				next.Worldview[i].Name = req.Name
			}
			if req.Category != "" {
				next.Worldview[i].Category = req.Category
			}
			if req.Description != "" {
				next.Worldview[i].Description = req.Description
			}
			if req.Tags != "" {
				next.Worldview[i].Tags = req.Tags
			}

			if err := story.SaveProjectSettings(h.settingsPath, &next); err != nil {
				h.writeErrorReq(w, r, http.StatusInternalServerError, "save_failed", err)
				return
			}
			*h.settings = next
			h.logger.SettingsUpdated()

			h.writeJSON(w, http.StatusOK, h.settings.Worldview[i])
			return
		}
	}

	h.writeErrorReq(w, r, http.StatusNotFound, "worldview_not_found")
}

func (h *Handlers) DeleteWorldview(w http.ResponseWriter, r *http.Request) {
	if h.rejectIfTaskRunning(w, r) {
		return
	}
	id := r.PathValue("id")

	for i, wv := range h.settings.Worldview {
		if wv.ID == id {
			h.settings.Worldview = append(h.settings.Worldview[:i], h.settings.Worldview[i+1:]...)
			if err := story.SaveProjectSettings(h.settingsPath, h.settings); err != nil {
				h.writeErrorReq(w, r, http.StatusInternalServerError, "save_failed", err)
				return
			}
			h.writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
			return
		}
	}

	h.writeErrorReq(w, r, http.StatusNotFound, "worldview_not_found")
}

func (h *Handlers) PostOrganization(w http.ResponseWriter, r *http.Request) {
	if h.rejectIfTaskRunning(w, r) {
		return
	}
	var o story.Organization
	if err := json.NewDecoder(r.Body).Decode(&o); err != nil {
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if o.Name == "" {
		h.writeErrorReq(w, r, http.StatusBadRequest, "organization_name_empty")
		return
	}

	o.ID = h.settings.NextOrganizationID()
	h.settings.Organizations = append(h.settings.Organizations, o)

	if err := story.SaveProjectSettings(h.settingsPath, h.settings); err != nil {
		h.writeErrorReq(w, r, http.StatusInternalServerError, "save_failed", err)
		return
	}

	h.writeJSON(w, http.StatusOK, o)
}

func (h *Handlers) PutOrganization(w http.ResponseWriter, r *http.Request) {
	if h.rejectIfTaskRunning(w, r) {
		return
	}
	id := r.PathValue("id")

	var req story.Organization
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	for i, o := range h.settings.Organizations {
		if o.ID == id {
			if req.Name != "" {
				h.settings.Organizations[i].Name = req.Name
			}
			if req.Type != "" {
				h.settings.Organizations[i].Type = req.Type
			}
			if req.Description != "" {
				h.settings.Organizations[i].Description = req.Description
			}
			if req.Members != nil {
				h.settings.Organizations[i].Members = req.Members
			}

			if err := story.SaveProjectSettings(h.settingsPath, h.settings); err != nil {
				h.writeErrorReq(w, r, http.StatusInternalServerError, "save_failed", err)
				return
			}

			h.writeJSON(w, http.StatusOK, h.settings.Organizations[i])
			return
		}
	}

	h.writeErrorReq(w, r, http.StatusNotFound, "organization_not_found")
}

func (h *Handlers) DeleteOrganization(w http.ResponseWriter, r *http.Request) {
	if h.rejectIfTaskRunning(w, r) {
		return
	}
	id := r.PathValue("id")

	for i, o := range h.settings.Organizations {
		if o.ID == id {
			h.settings.Organizations = append(h.settings.Organizations[:i], h.settings.Organizations[i+1:]...)
			if err := story.SaveProjectSettings(h.settingsPath, h.settings); err != nil {
				h.writeErrorReq(w, r, http.StatusInternalServerError, "save_failed", err)
				return
			}
			h.writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
			return
		}
	}

	h.writeErrorReq(w, r, http.StatusNotFound, "organization_not_found")
}

func (h *Handlers) PostRelation(w http.ResponseWriter, r *http.Request) {
	if h.rejectIfTaskRunning(w, r) {
		return
	}
	var rel story.Relation
	if err := json.NewDecoder(r.Body).Decode(&rel); err != nil {
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if rel.SourceID == "" || rel.TargetID == "" {
		h.writeErrorReq(w, r, http.StatusBadRequest, "relation_endpoints_empty")
		return
	}

	rel.ID = h.settings.NextRelationID()
	h.settings.Relations = append(h.settings.Relations, rel)

	if err := story.SaveProjectSettings(h.settingsPath, h.settings); err != nil {
		h.writeErrorReq(w, r, http.StatusInternalServerError, "save_failed", err)
		return
	}

	h.writeJSON(w, http.StatusOK, rel)
}

func (h *Handlers) PutRelation(w http.ResponseWriter, r *http.Request) {
	if h.rejectIfTaskRunning(w, r) {
		return
	}
	id := r.PathValue("id")

	var req story.Relation
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	for i, rel := range h.settings.Relations {
		if rel.ID == id {
			if req.SourceID != "" {
				h.settings.Relations[i].SourceID = req.SourceID
			}
			if req.SourceType != "" {
				h.settings.Relations[i].SourceType = req.SourceType
			}
			if req.TargetID != "" {
				h.settings.Relations[i].TargetID = req.TargetID
			}
			if req.TargetType != "" {
				h.settings.Relations[i].TargetType = req.TargetType
			}
			if req.Label != "" {
				h.settings.Relations[i].Label = req.Label
			}

			if err := story.SaveProjectSettings(h.settingsPath, h.settings); err != nil {
				h.writeErrorReq(w, r, http.StatusInternalServerError, "save_failed", err)
				return
			}

			h.writeJSON(w, http.StatusOK, h.settings.Relations[i])
			return
		}
	}

	h.writeErrorReq(w, r, http.StatusNotFound, "relation_not_found")
}

func (h *Handlers) DeleteRelation(w http.ResponseWriter, r *http.Request) {
	if h.rejectIfTaskRunning(w, r) {
		return
	}
	id := r.PathValue("id")

	for i, rel := range h.settings.Relations {
		if rel.ID == id {
			h.settings.Relations = append(h.settings.Relations[:i], h.settings.Relations[i+1:]...)
			if err := story.SaveProjectSettings(h.settingsPath, h.settings); err != nil {
				h.writeErrorReq(w, r, http.StatusInternalServerError, "save_failed", err)
				return
			}
			h.writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
			return
		}
	}

	h.writeErrorReq(w, r, http.StatusNotFound, "relation_not_found")
}

func (h *Handlers) PostSettingsAIGenerate(w http.ResponseWriter, r *http.Request) {
	h.writeErrorReq(w, r, http.StatusGone, "settings_ai_generate_moved")
}

func (h *Handlers) PostSettingsPolish(w http.ResponseWriter, r *http.Request) {
	h.writeErrorReq(w, r, http.StatusGone, "settings_polish_moved")
}

func (h *Handlers) PostChapterPolish(w http.ResponseWriter, r *http.Request) {
	if !h.ensureProject(w, r) {
		return
	}

	polishSkills := story.ResolveSkills(h.skills, h.cfg.SkillConfig, story.SkillScopeChapterPolish, h.cfg.Language)
	if len(polishSkills) == 0 {
		h.writeErrorReq(w, r, http.StatusBadRequest, "need_polish_skill")
		return
	}

	var body struct {
		Num int `json:"num"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	chapterIdx := -1
	if body.Num > 0 {
		for i, ch := range h.state.Chapters {
			if ch.Num == body.Num {
				chapterIdx = i
				break
			}
		}
		if chapterIdx == -1 {
			h.writeErrorReq(w, r, http.StatusBadRequest, "chapter_not_found")
			return
		}
	} else {
		chapterIdx = h.state.CurrentChapterIndex
		if chapterIdx < 0 || chapterIdx >= len(h.state.Chapters) {
			h.writeErrorReq(w, r, http.StatusBadRequest, "chapter_num_required")
			return
		}
	}

	ch := h.state.Chapters[chapterIdx]
	if ch.Content == "" {
		h.writeErrorReq(w, r, http.StatusBadRequest, "chapter_content_empty")
		return
	}
	if ch.Status == story.StatusWriting {
		h.writeErrorReq(w, r, http.StatusBadRequest, "chapter_in_writing")
		return
	}

	if !h.tryStartTask() {
		h.writeErrorReq(w, r, http.StatusConflict, "task_running_wait")
		return
	}

	prevStatus := ch.Status
	idx := chapterIdx

	go func() {
		defer h.endTask()
		h.logger.TaskStart("chapter_polish")
		ctx := h.activateSkills(h.taskCtx, story.SkillScopeChapterPolish, false)

		err := story.PolishChapterAction(ctx, h.apiCfg, h.cfg, h.state, idx, polishSkills, h.progressPath, h.logger)
		if err != nil {
			if ctx.Err() != nil {
				h.logger.WarnKey("log.chapter_polish_cancelled")
			} else {
				h.logger.ErrorKey("log.chapter_polish_failed", err)
			}
			h.logger.TaskEnd("chapter_polish", false)
			return
		}

		if prevStatus == story.StatusAccepted {
			h.state.Chapters[idx].Status = story.StatusAccepted
			_ = story.SaveProgress(h.progressPath, h.state)
		}

		h.logger.TaskEnd("chapter_polish", true)
		h.broadcastProgress()
	}()

	h.writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}

func (h *Handlers) GetSkills(w http.ResponseWriter, r *http.Request) {
	h.writeJSON(w, http.StatusOK, h.skillViews())
}

func (h *Handlers) PutSkillToggle(w http.ResponseWriter, r *http.Request) {
	if h.rejectIfTaskRunning(w, r) {
		return
	}
	id := r.PathValue("id")

	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	found := false
	for _, s := range h.skills {
		if s.ID == id {
			status := validationStatus(s)
			if req.Enabled && (status == "failed" || status == "needs_optimization" || status == "validating") {
				h.writeErrorReq(w, r, http.StatusConflict, "skill_cannot_enable", status)
				return
			}
			found = true
			break
		}
	}
	if !found {
		h.writeErrorReq(w, r, http.StatusNotFound, "skill_not_found")
		return
	}

	if h.cfg.SkillConfig == nil {
		h.cfg.SkillConfig = &config.SkillConfig{EnabledSkills: make(map[string]bool)}
	}
	if h.cfg.SkillConfig.EnabledSkills == nil {
		h.cfg.SkillConfig.EnabledSkills = make(map[string]bool)
	}

	h.cfg.SkillConfig.EnabledSkills[id] = req.Enabled

	if err := config.SaveConfig(h.cfgPath, h.cfg); err != nil {
		h.writeErrorReq(w, r, http.StatusInternalServerError, "save_config_failed", err)
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]interface{}{"id": id, "enabled": req.Enabled})
}

func (h *Handlers) GetChatSessions(w http.ResponseWriter, r *http.Request) {
	idx, err := story.LoadChatSessions(h.sessionsDir)
	if err != nil {
		h.writeErrorReq(w, r, http.StatusInternalServerError, "load_session_list_failed", err.Error())
		return
	}
	if idx == nil {
		idx = &story.ChatSessionIndex{}
	}

	// clearemptyassociation: except msg_count == 0 andnot inwhenfrontassociationmiddle strip
	var cleaned []story.ChatSessionMeta
	for _, m := range idx.Sessions {
		if m.MsgCount == 0 {
			path := filepath.Join(story.ChatSessionsDir(h.sessionsDir), m.ID+".json")
			fsutil.Delete(path)
			continue
		}
		cleaned = append(cleaned, m)
	}
	idx.Sessions = cleaned

	h.writeJSON(w, http.StatusOK, idx)
}

func (h *Handlers) PostChatSession(w http.ResponseWriter, r *http.Request) {
	now := time.Now().Format(time.RFC3339)
	session := &story.ChatSession{
		ID:        story.GenerateSessionID(),
		Title:     "newassociation",
		Messages:  []story.ChatMessage{},
		CreatedAt: now,
		UpdatedAt: now,
	}

	// insuredepositassociationitembutnot entercord, etctimeshairhour story.SaveChatSession only thenentercord,
	// life 0 strip emptyassociation.
	dir := story.ChatSessionsDir(h.sessionsDir)
	os.MkdirAll(dir, 0755)
	path := filepath.Join(dir, session.ID+".json")
	data, _ := json.MarshalIndent(session, "", "  ")
	fsutil.WriteFileAtomic(path, data)

	h.writeJSON(w, http.StatusOK, session)
}

func (h *Handlers) GetChatSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	session, err := story.LoadChatSession(h.sessionsDir, id)
	if err != nil {
		h.writeErrorReq(w, r, http.StatusNotFound, "chat_session_not_found")
		return
	}

	h.writeJSON(w, http.StatusOK, session)
}

func (h *Handlers) DeleteChatSession(w http.ResponseWriter, r *http.Request) {
	if h.rejectIfTaskRunning(w, r) {
		return
	}
	id := r.PathValue("id")

	if err := story.DeleteChatSession(h.sessionsDir, id); err != nil {
		h.writeErrorReq(w, r, http.StatusInternalServerError, "delete_session_failed", err.Error())
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *Handlers) PostChatMessage(w http.ResponseWriter, r *http.Request) {
	if !h.tryStartTask() {
		h.writeErrorReq(w, r, http.StatusConflict, "task_running_wait")
		return
	}

	sessionID := r.PathValue("id")

	var req struct {
		Content     string `json:"content"`
		ContextPage string `json:"context_page"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Content == "" {
		h.endTask()
		h.writeErrorReq(w, r, http.StatusBadRequest, "missing_content")
		return
	}

	session, err := story.LoadChatSession(h.sessionsDir, sessionID)
	if err != nil {
		h.endTask()
		h.writeErrorReq(w, r, http.StatusNotFound, "chat_session_not_found")
		return
	}

	now := time.Now().Format(time.RFC3339)
	session.Messages = append(session.Messages, story.ChatMessage{
		Role:      "user",
		Content:   req.Content,
		Timestamp: now,
	})

	if len(session.Messages) == 1 {
		session.Title = story.GenerateChatTitle(req.Content)
	}

	if err := story.SaveChatSession(h.sessionsDir, session); err != nil {
		h.endTask()
		h.writeErrorReq(w, r, http.StatusInternalServerError, "save_session_failed", err)
		return
	}

	// deposituseinheavy
	h.lastChatMessage = req.Content

	go func() {
		// defer insurehowroadallassociationputpadlock, thenlaterhasBA 409
		defer h.endTask()
		h.logger.TaskStart("chat_message")
		// Snapshot the task context: endTask() cancels h.taskCancel when the last
		// unit of work finishes. If the agent's generate_section tool starts a
		// child generation that completes *before* this chat turn does, endTask
		// would cancel the context underneath the still-running agent loop,
		// producing " N: API use: context canceled". Capturing h.taskCtx
		// here (while the lock is definitely held) keeps this turn alive until it
		// really ends; later tryStartTask() creates a fresh context anyway.
		chatCtx := h.taskCtx

		// Deterministic fast path for [generate-section:X] messages (Generate
		// buttons): execute the forced tool call BEFORE touching the LLM, so a
		// click always triggers the generation even when the model endpoint is
		// down or slow. The generation runs on its own detached context (see
		// StartDetachedSectionGenerate) so it survives the end of this chat turn;
		// the normal agent loop below then runs as the summary turn.
		fastForced := agent.ParseGenerateSectionTag(req.Content)
		fastReply := ""
		// A chat turn ALWAYS holds the task lock here, so a "task_running_wait"
		// from the detached runners is not a real user-facing conflict: it means
		// startChildWork() lost a race against a sibling task that had just
		// finished. Retry once after a short grace period instead of dropping the
		// generation silently (that silent drop was why Pre-planning's Generate
		// button showed a spinner but produced no chat activity at all). Any
		// other error surfaces in the chat as before.
		if fastForced != nil {
			sectionName := agent.ForcedSectionName(fastForced)
			tcJSON, _ := json.Marshal(fastForced)
			h.logger.ToolCallStart(sessionID, fastForced.Name, string(tcJSON))
			result := fmt.Sprintf("generate_section(%s): started", sectionName)
			var genErr error
			startOnce := func() error {
				if h.cfg == nil {
					return errors.New("config_not_loaded")
				}
				if sectionName == "pre_planning" {
					// Book-level plan: dedicated detached runner (writes Progress, not config).
					return h.StartDetachedPrePlanGenerate()
				}
				return h.StartDetachedSectionGenerate(agent.SectionGenRequest{
					Section:   sectionName,
					StoryIdea: strings.TrimSpace(h.cfg.Story.StoryIdea),
					Story:     &h.cfg.Story,
				})
			}
			genErr = startOnce()
			for attempt := 0; genErr != nil && strings.Contains(genErr.Error(), "task_running_wait") && attempt < 3; attempt++ {
				time.Sleep(400 * time.Millisecond)
				genErr = startOnce()
			}
			if genErr != nil {
				result = fmt.Sprintf("generate_section(%s): failed: %v", sectionName, genErr)
			}
			h.logger.ToolCallEnd(sessionID, fastForced.Name, result, "", nil)
			fastReply = i18n.T(i18n.FromRequest(r), "agent.section_generate_started", sectionName)
			devlog.Log("chat fast-path generate_section=%s ok=%v", sectionName, genErr == nil)
			if genErr != nil {
				// Surface the failure immediately in the chat instead of a silent
				// dead button: save + broadcast an error reply and end the turn.
				msg := fastReply + " — " + genErr.Error()
				session.Messages = append(session.Messages, story.ChatMessage{
					Role:      "assistant",
					Content:   msg,
					Timestamp: time.Now().Format(time.RFC3339),
				})
				session.UpdatedAt = time.Now().Format(time.RFC3339)
				if saveErr := story.SaveChatSession(h.sessionsDir, session); saveErr != nil {
					h.logger.WarnKey("log.save_session_failed", saveErr)
				}
				h.logger.ChatChunk(sessionID, msg)
				h.logger.ErrorKey("log.chat_failed", genErr)
				h.logger.TaskEnd("chat_message", false)
				return
			}
		}

		ctx := h.activateSkills(chatCtx, story.SkillScopeAssistantChat, false)

		var history []agent.AgentStep
		for _, m := range session.Messages {
			if m.Role == "user" {
				history = append(history, agent.AgentStep{Role: "user", Content: m.Content})
			} else if m.Role == "assistant" {
				step := agent.AgentStep{Role: "assistant", Content: m.Content}
				if len(m.ToolCalls) > 0 {
					step.ToolCall = &m.ToolCalls[0]
				}
				history = append(history, step)
			} else if m.Role == "tool" {
				history = append(history, agent.AgentStep{
					Role:           "tool",
					ToolResult:     m.ToolResult,
					ToolResultKey:  m.ToolResultKey,
					ToolResultArgs: m.ToolResultArgs,
				})
			}
		}

		agentCtx := &agent.AgentContext{
			// Own context for the generate_section child task (see chatCtx note
			// above): WithoutCancel keeps the child alive when this turn's lock is
			// released early, while PostTaskStop can still cancel it through the
			// parent chain (h.taskCancel → chatCtx).
			SectionGenCtx: context.WithoutCancel(chatCtx),
			APICfg:        h.apiCfg,
			Settings:      h.settings,
			SettingsPath:  h.settingsPath,
			State:         h.state,
			Skills:        h.skills,
			Logger:        h.logger,
			ContextPage:   req.ContextPage,
			ProgressPath:  h.progressPath,
			CfgPath:       h.cfgPath,
			SessionsDir:   h.sessionsDir,
			ProjectDir:    filepath.Join(h.progDir, "storys", h.projectName),
			// Always act on the freshest on-disk config: the Generate button
			// persists the live form via PUT /api/config right before sending the
			// chat message, and the in-memory snapshot can lag (or be nil). The
			// getter makes every read (system prompt, generate_section seed/save)
			// pick up exactly what the author just typed.
			ConfigGetter: func() *config.Config {
				if h.cfgPath != "" {
					if cfg, err := config.LoadConfig(h.cfgPath); err == nil && cfg != nil {
						return cfg
					}
				}
				h.projectMu.RLock()
				defer h.projectMu.RUnlock()
				return h.cfg
			},
			// Wire the generate_section chat tool to the same background
			// runner used by the config-page buttons (ownsLock=false: the
			// agent loop already holds the task, so register as child work).
			StartSectionGenerate: func(req agent.SectionGenRequest) error {
				return h.StartSectionGenerateAsync(req, false)
			},
			StartPrePlanGenerate: func() error {
				return h.StartDetachedPrePlanGenerate()
			},
			StartAsync: func(taskName string, fn func(goCtx context.Context) error) {
				// comprueba si hay trabajo activo y luego libera el bloqueo del agente
				// stillintravelhournewcanandhairadvanceenter, buildnumberreceipt.
				if !h.startChildWork() {
					h.logger.WarnKey("log.child_task_start_failed", taskName)
					return
				}
				childCtx := h.activateSkills(h.taskCtx, skillScopeForTask(taskName), true)
				go func() {
					defer h.endTask()
					h.logger.TaskStart(taskName)
					err := fn(childCtx)
					h.broadcastProgress()
					h.logger.TaskEnd(taskName, err == nil)
				}()
			},
		}

		reply, newHistory, err := agent.RunAgentLoop(ctx, agentCtx, req.Content, history, 30)
		if err != nil {
			// i.e.make-tooinsuredepositalreadylife to, upperlower
			saveAgentSteps(session, newHistory[len(history):])
			session.UpdatedAt = time.Now().Format(time.RFC3339)
			if saveErr := story.SaveChatSession(h.sessionsDir, session); saveErr != nil {
				h.logger.WarnKey("log.save_session_failed", saveErr)
			}
			if ctx.Err() != nil {
				h.logger.WarnKey("log.chat_cancelled")
			} else {
				h.logger.ErrorKey("log.chat_failed", err)
			}
			h.logger.TaskEnd("chat_message", false)
			return
		}

		saveAgentSteps(session, newHistory[len(history):])

		if reply != "" {
			found := false
			for i := len(session.Messages) - 1; i >= 0; i-- {
				if session.Messages[i].Role == "assistant" && session.Messages[i].Content == reply {
					found = true
					break
				}
			}
			if !found {
				session.Messages = append(session.Messages, story.ChatMessage{
					Role:      "assistant",
					Content:   reply,
					Timestamp: time.Now().Format(time.RFC3339),
				})
			}
		}

		session.UpdatedAt = time.Now().Format(time.RFC3339)

		if err := story.SaveChatSession(h.sessionsDir, session); err != nil {
			h.logger.WarnKey("log.save_session_failed", err)
		}

		h.logger.ChatChunk(sessionID, reply)

		h.logger.SuccessKey("log.chat_done")
		h.logger.TaskEnd("chat_message", true)
	}()

	h.writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}

// saveAgentSteps BA Agent chaseforassociation.
func saveAgentSteps(session *story.ChatSession, steps []agent.AgentStep) {
	for _, step := range steps {
		if step.Role == "assistant" {
			msg := story.ChatMessage{
				Role:      "assistant",
				Content:   step.Content,
				Timestamp: time.Now().Format(time.RFC3339),
			}
			if step.ToolCall != nil {
				msg.ToolCalls = []agent.ToolCall{*step.ToolCall}
			}
			session.Messages = append(session.Messages, msg)
		} else if step.Role == "tool" {
			session.Messages = append(session.Messages, story.ChatMessage{
				Role:           "tool",
				ToolResult:     step.ToolResult,
				ToolResultKey:  step.ToolResultKey,
				ToolResultArgs: step.ToolResultArgs,
				Timestamp:      time.Now().Format(time.RFC3339),
			})
		}
	}
}
