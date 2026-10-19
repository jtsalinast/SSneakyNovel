package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"showmethestory/internal/agent"
	"showmethestory/internal/config"
	"showmethestory/internal/fsutil"
	"showmethestory/internal/i18n"
	"showmethestory/internal/llm"
	"showmethestory/internal/story"
)

// PostSectionGenerate uses the story idea (plus every novel parameter the author has
// already filled in) to AI-generate one config section: style, characters,
// organizations or relations. It runs as a background task so the existing
// SSE progress/log indicators show activity while generation is in flight;
// the frontend polls /api/status until the task finishes, then refreshes.
func (h *Handlers) PostSectionGenerate(w http.ResponseWriter, r *http.Request) {
	if !h.ensureProject(w, r) {
		return
	}
	section := r.PathValue("section")
	switch section {
	case "style", "characters", "organizations", "relations", "locations", "worldview", "motif", "story_idea", "audience_profile", "inspirational_pieces", "pre_planning":
	default:
		// Per-category worldview sections (wv_geography, wv_faction, ...) are
		// also valid — see agent.WorldviewCategoryBySection.
		if !agent.IsWorldviewCategorySection(section) {
			h.writeErrorReq(w, r, http.StatusBadRequest, "unknown_section", section)
			return
		}
	}
	if !h.tryStartTask() {
		h.writeErrorReq(w, r, http.StatusConflict, "task_running_wait")
		return
	}

	var body struct {
		StoryIdea string              `json:"story_idea"`
		Story     *config.StoryConfig `json:"story"`
		Name      string              `json:"name"`
		Category  string              `json:"category"`
		EntryID   string              `json:"entry_id"` // worldview: update an existing entry by id
		SourceID  string              `json:"source_id"`
		TargetID  string              `json:"target_id"`
		// Single-entry generation (characters/organizations forms): when set, the
		// LLM fills in only this existing entry's empty fields instead of doing a
		// whole-section batch generation.
		CharacterID string `json:"character_id"`
		OrgID       string `json:"org_id"`
		// Per-category worldview section (wv_geography, ...): the frontend can
		// pass the category explicitly; otherwise it is derived from the key.
		WorldviewCategory string `json:"worldview_category"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.endTask()
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	req := SectionGenRequest{
		Section:           section,
		StoryIdea:         body.StoryIdea,
		Story:             body.Story,
		Name:              body.Name,
		Category:          body.Category,
		EntryID:           body.EntryID,
		SourceID:          body.SourceID,
		TargetID:          body.TargetID,
		CharacterID:       body.CharacterID,
		OrgID:             body.OrgID,
		WorldviewCategory: body.WorldviewCategory,
		Ctx:               h.taskCtx, // snapshot: the caller holds the task lock
	}
	if err := h.validateSectionGen(req); err != nil {
		h.endTask()
		h.writeErrorReq(w, r, http.StatusBadRequest, "story_idea_required")
		return
	}

	go func() {
		defer h.endTask()
		if section == "pre_planning" {
			// Book-level plan lives in Progress, not Story: dedicated runner.
			h.runPrePlanGenerate(h.taskCtx)
			return
		}
		h.runSectionGenerate(req)
	}()

	h.writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}

// StartSectionGenerateAsync launches a section generation in the background.
// It is shared by PostSectionGenerate (config-page buttons) and the agent's
// generate_section chat tool so both paths have identical semantics.
// ownsLock=true means the caller already holds the task lock (HTTP handler);
// ownsLock=false (chat tool, running inside an agent turn) registers the run
// as child work so the task stays alive even if the agent loop finishes first.
func (h *Handlers) StartSectionGenerateAsync(req SectionGenRequest, ownsLock bool) error {
	if err := h.validateSectionGen(req); err != nil {
		return err
	}
	if !ownsLock && !h.startChildWork() {
		return errors.New("task_running_wait")
	}
	// Snapshot the generation id: endTask() bumps it and cancels the task ctx
	// when the last unit of work finishes. If that happens while this child is
	// still running (e.g. a chat turn whose tool started the generation), any
	// resulting "context canceled" is spurious — we suppress its error log/toast
	// below by comparing ids.
	h.taskMu.Lock()
	genID := h.generationID
	h.taskMu.Unlock()
	go func() {
		defer h.endTask()
		err := h.runSectionGenerate(req)
		if err != nil && req.Ctx != nil && req.Ctx.Err() != nil {
			h.taskMu.Lock()
			stale := genID != h.generationID
			h.taskMu.Unlock()
			if stale {
				// Task lock was released underneath us; treat as cancelled,
				// not as a generation failure.
				return
			}
		}
		_ = err
	}()
	return nil
}

// SectionGenRequest is an alias of the agent-side request struct (the agent
// package owns the type to avoid an import cycle). It carries everything a
// section generation needs, including the section name itself, and is used
// both by PostSectionGenerate (the config-page buttons) and by the agent's
// generate_section chat tool, so both paths share identical semantics.
type SectionGenRequest = agent.SectionGenRequest

// normalizeSectionGen merges the request with the saved config: the frontend
// (or chat tool) may send live form values so generation reflects exactly what
// the author just entered; the story idea falls back to the saved config.
func (h *Handlers) normalizeSectionGen(req SectionGenRequest) (config.StoryConfig, error) {
	storyCfg := h.cfg.Story
	if req.Story != nil {
		storyCfg = *req.Story
	}
	storyIdea := strings.TrimSpace(req.StoryIdea)
	if storyIdea == "" {
		storyIdea = strings.TrimSpace(storyCfg.StoryIdea)
	}
	if storyIdea == "" {
		storyIdea = strings.TrimSpace(h.cfg.Story.StoryIdea)
	}
	storyCfg.StoryIdea = storyIdea
	// Theme & Motif sections (motif, inspirational pieces) and the generators
	// that *produce* a story idea must never require one: they are seeded from
	// the other novel parameters already entered. Everything else needs the
	// story idea as its generation seed.
	if storyIdea == "" && req.Section != "motif" && req.Section != "story_idea" && req.Section != "audience_profile" && req.Section != "style" && req.Section != "inspirational_pieces" && req.Section != "pre_planning" {
		return storyCfg, errors.New("story_idea_required")
	}
	return storyCfg, nil
}

// validateSectionGen reports whether the request can run right now (used for
// synchronous HTTP error responses before starting the task).
func (h *Handlers) validateSectionGen(req SectionGenRequest) error {
	if req.Section == "pre_planning" {
		// Book-level plan: seeded from every novel parameter (notably Story
		// structure); never requires a story idea and never writes Story
		// fields, so it is always runnable.
		return nil
	}
	_, err := h.normalizeSectionGen(req)
	return err
}

// StartDetachedPrePlanGenerate runs the book-level pre-planning generation as
// an independent background task, detached from any chat turn — same survival
// guarantees as StartDetachedSectionGenerate (own cancellable context wired
// under taskMu so StopTask can still cancel it). Used by the
// [generate-section:pre_planning] fast path in PostChatMessage. The caller
// must already hold the task lock; this registers the run as child work.
func (h *Handlers) StartDetachedPrePlanGenerate() error {
	if !h.startChildWork() {
		return errors.New("task_running_wait")
	}
	ctx, cancel := context.WithCancel(context.Background())
	// Same token wiring as tryStartTask: without a llm.TaskTokenUsage tracker in
	// the context, the LLM calls inside this detached run report no token
	// activity to the frontend badge. Re-use the live task's tracker when one
	// exists (chat fast path); only create a fresh one when nothing else owns a
	// tracker, so /api/status keeps reading the right numbers.
	h.taskMu.Lock()
	genID := h.generationID
	// Attach the tracker that /api/status actually reads: re-use the live task's
	// tracker when one exists (chat fast path); otherwise OWN h.taskTokens with a
	// fresh one. The old fallback created a tracker that was never published to
	// h.taskTokens, so this detached run's tokens went to an orphan and the badge
	// showed no counts at all.
	if h.taskTokens == nil {
		h.taskTokens = llm.NewTaskTokenUsage(h.logger)
	}
	ctx = llm.AttachTaskTokens(ctx, h.taskTokens)
	h.taskCtx = ctx
	h.taskCancel = cancel
	h.taskMu.Unlock()
	go func() {
		defer cancel()
		defer h.endTask()
		h.logger.InfoKey("log.preplan_generating")
		err := h.runPrePlanGenerate(ctx)
		if err != nil && ctx.Err() != nil {
			h.taskMu.Lock()
			stale := genID != h.generationID
			h.taskMu.Unlock()
			if stale {
				return
			}
		}
		if err != nil {
			h.logger.ErrorKey("log.preplan_generate_failed", err)
			return
		}
		h.logger.SuccessKey("log.preplan_generate_done")
		h.broadcastProgress()
	}()
	return nil
}

// StartDetachedSectionGenerate runs a section generation as an independent
// background task, fully detached from any chat turn. It is used by the
// [generate-section:X] fast path in PostChatMessage: the click must always
// trigger the generation even if the model endpoint is down or slow. The
// caller must already hold the task lock (chat turn); this registers the
// generation as child work so the lock stays alive until it finishes.
//
// Unlike StartSectionGenerateAsync it does NOT reuse the chat turn's context:
// when the turn ends, endTask() cancels that context, which would kill the
// still-running generation. Here we build our own cancellable context and
// wire it into the handler state (under taskMu, guarded by generationID) so
// StopTask can still cancel it while it runs.
func (h *Handlers) StartDetachedSectionGenerate(req SectionGenRequest) error {
	if err := h.validateSectionGen(req); err != nil {
		return err
	}
	if !h.startChildWork() {
		return errors.New("task_running_wait")
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.taskMu.Lock()
	genID := h.generationID
	// Re-use the live task's token tracker (the chat turn owns it): without this
	// the detached generation accumulated its tokens in an orphaned tracker and
	// the frontend badge showed no prompt/completion counts at all.
	ctx = llm.AttachTaskTokens(ctx, h.taskTokens)
	h.taskCtx = ctx
	h.taskCancel = cancel
	h.taskMu.Unlock()
	go func() {
		defer cancel()
		defer h.endTask()
		err := h.runDetachedSectionGenerate(req, ctx)
		if err != nil && ctx.Err() != nil {
			h.taskMu.Lock()
			stale := genID != h.generationID
			h.taskMu.Unlock()
			if stale {
				// Task lock was released underneath us; treat as cancelled,
				// not as a generation failure.
				return
			}
		}
		_ = err
	}()
	return nil
}

// runDetachedSectionGenerate mirrors runSectionGenerate but executes the
// generation with its own explicit context instead of the shared task ctx,
// so it survives the end of the chat turn that started it. The switch below
// must stay in sync with runSectionGenerate (same sections, same helpers).
func (h *Handlers) runDetachedSectionGenerate(req SectionGenRequest, ctx context.Context) error {
	section := req.Section
	if section == "pre_planning" {
		return h.runPrePlanGenerate(ctx)
	}
	storyCfg, err := h.normalizeSectionGen(req)
	taskName := "section_generate_" + section
	h.logger.TaskStart(taskName)
	if err == nil {
		h.logger.InfoKey("log.section_generating", sectionLabel(section, h.cfg.Language))
		switch section {
		case "style":
			err = h.generateStyle(ctx, &storyCfg)
		case "characters":
			err = h.generateCharactersEntryOrBatch(ctx, &storyCfg, req.CharacterID)
		case "organizations":
			err = h.generateOrganizationsEntryOrBatch(ctx, &storyCfg, req.OrgID)
		case "relations":
			err = h.generateRelations(ctx, &storyCfg, req.SourceID, req.TargetID)
		case "locations":
			err = h.generateLocations(ctx, &storyCfg)
		case "worldview":
			// With an entry target the button enriches that single form entry;
			// without one it batch-generates entries across all categories.
			if strings.TrimSpace(req.Name) != "" || strings.TrimSpace(req.EntryID) != "" {
				err = h.generateWorldview(ctx, &storyCfg, req.Name, req.Category, req.EntryID)
			} else {
				err = h.generateWorldviewBatch(ctx, &storyCfg)
			}
		case "wv_geography", "wv_location", "wv_faction", "wv_rule", "wv_knowledge", "wv_history", "wv_other":
			// Per-category worldview sections: each Worldview-card section has its
			// own Generate button and only produces entries for that category.
			err = h.generateWorldviewCategory(ctx, &storyCfg, worldviewCategoryForSection(section, req.WorldviewCategory))
		case "motif":
			err = h.generateMotif(ctx, &storyCfg)
		case "story_idea":
			err = h.generateStoryIdeaFromParams(ctx, &storyCfg)
		case "audience_profile":
			err = h.generateAudienceProfile(ctx, &storyCfg)
		case "inspirational_pieces":
			err = h.generateInspirationalPieces(ctx, &storyCfg)
		default:
			err = fmt.Errorf("unknown_section: %s", section)
		}
	}
	if err != nil {
		if ctx.Err() != nil {
			h.logger.WarnKey("log.section_generate_cancelled")
		} else {
			h.logger.ErrorKey("log.section_generate_failed", err)
		}
		h.logger.TaskEnd(taskName, false)
		return err
	}
	h.logger.SuccessKey("log.section_generate_done", sectionLabel(section, h.cfg.Language))
	h.logger.TaskEnd(taskName, true)
	h.broadcastProgress()
	return nil
}

// runSectionGenerate executes one section generation as a background task and
// releases the task lock when done (it is always started via tryStartTask or
// startChildWork, which take the lock). Returns the generation error.
func (h *Handlers) runSectionGenerate(req SectionGenRequest) error {
	section := req.Section
	if section == "pre_planning" {
		// Book-level plan lives in Progress, not Story: dedicated runner.
		return h.runPrePlanGenerate(h.taskCtx)
	}
	storyCfg, err := h.normalizeSectionGen(req)
	taskName := "section_generate_" + section
	h.logger.TaskStart(taskName)
	ctx := h.taskCtx
	if req.Ctx != nil {
		// Chat-tool path: use the detached context captured when the tool ran.
		ctx = req.Ctx
	}
	if err == nil {
		h.logger.InfoKey("log.section_generating", sectionLabel(section, h.cfg.Language))
		switch section {
		case "style":
			err = h.generateStyle(ctx, &storyCfg)
		case "characters":
			err = h.generateCharactersEntryOrBatch(ctx, &storyCfg, req.CharacterID)
		case "organizations":
			err = h.generateOrganizationsEntryOrBatch(ctx, &storyCfg, req.OrgID)
		case "relations":
			err = h.generateRelations(ctx, &storyCfg, req.SourceID, req.TargetID)
		case "locations":
			err = h.generateLocations(ctx, &storyCfg)
		case "worldview":
			// With an entry target the button enriches that single form entry;
			// without one it batch-generates entries across all categories.
			if strings.TrimSpace(req.Name) != "" || strings.TrimSpace(req.EntryID) != "" {
				err = h.generateWorldview(ctx, &storyCfg, req.Name, req.Category, req.EntryID)
			} else {
				err = h.generateWorldviewBatch(ctx, &storyCfg)
			}
		case "wv_geography", "wv_location", "wv_faction", "wv_rule", "wv_knowledge", "wv_history", "wv_other":
			// Per-category worldview sections: each Worldview-card section has its
			// own Generate button and only produces entries for that category.
			err = h.generateWorldviewCategory(ctx, &storyCfg, worldviewCategoryForSection(section, req.WorldviewCategory))
		case "motif":
			err = h.generateMotif(ctx, &storyCfg)
		case "story_idea":
			err = h.generateStoryIdeaFromParams(ctx, &storyCfg)
		case "audience_profile":
			err = h.generateAudienceProfile(ctx, &storyCfg)
		case "inspirational_pieces":
			err = h.generateInspirationalPieces(ctx, &storyCfg)
		default:
			err = fmt.Errorf("unknown_section: %s", section)
		}
	}
	if err != nil {
		if ctx.Err() != nil {
			h.logger.WarnKey("log.section_generate_cancelled")
		} else {
			h.logger.ErrorKey("log.section_generate_failed", err)
		}
		h.logger.TaskEnd(taskName, false)
		return err
	}
	h.logger.SuccessKey("log.section_generate_done", sectionLabel(section, h.cfg.Language))
	h.logger.TaskEnd(taskName, true)
	h.broadcastProgress()
	return nil
}

func sectionLabel(section, lang string) string {
	switch section {
	case "style":

		return "writing style & POV"
	case "characters":

		return "characters"
	case "organizations":

		return "organizations"
	case "locations":

		return "locations"
	case "worldview":

		return "worldview entry"
	case "wv_geography":

		return "geography entries"
	case "wv_location":

		return "location entries"
	case "wv_faction":

		return "faction entries"
	case "wv_rule":

		return "world rules entries"
	case "wv_knowledge":

		return "story knowledge entries"
	case "wv_history":

		return "history entries"
	case "wv_other":

		return "other worldview entries"
	case "motif":

		return "literary motif"
	case "story_idea":

		return "story idea"
	case "audience_profile":

		return "ideal reader profile"
	case "inspirational_pieces":

		return "inspirational pieces"
	case "pre_planning":

		return "pre-planning (book plan)"
	default:

		return "relations"
	}
}

// storyParamsContext assembles the full novel parameters (including the story
// idea) into the prompt header. It delegates to story.NovelParametersBlock so
// section generation uses exactly the same authoritative parameter block as
// outline/writing — characters, organizations and worldview now respect every
// "Novel parameters" field, not just the story idea.
func storyParamsContext(sc *config.StoryConfig, lang string) string {
	cfg := &config.Config{Language: lang}
	cfg.Story = *sc
	return story.NovelParametersBlock(cfg) + "\n"
}

// —— motif (Theme & Motif card: theme + literary motif + tone) ——

func (h *Handlers) generateMotif(ctx context.Context, sc *config.StoryConfig) error {
	var b strings.Builder
	b.WriteString("You are a literary consultant. Based on the work parameters below, propose the central theme, one recurring literary motif and a fitting tone for this work:\n")
	b.WriteString("- \"theme\": the novel's central idea stated as a short thematic statement (one clause, no explanations);\n")
	b.WriteString("- \"motif\": one concrete recurring image or device that carries that theme (a short phrase naming one or two key images, no explanations);\n")
	b.WriteString("- \"tone\": 2-4 mood adjectives describing the work's emotional register, comma-separated.\n")

	wroteParams := false
	if t := strings.TrimSpace(sc.Type); t != "" {
		b.WriteString("Genre: " + t + "\n")

		wroteParams = true
	}
	if t := strings.TrimSpace(sc.Subgenre); t != "" {
		// Multi-select: "Cozy Mystery, Noir" -> every entry on its own line.
		for _, part := range config.SplitSubgenreList(t) {
			b.WriteString("Subgenre: " + part + "\n")

			wroteParams = true
		}
	}
	if t := strings.TrimSpace(sc.Theme); t != "" {
		b.WriteString("Theme: " + t + "\n")

		wroteParams = true
	}
	if t := strings.TrimSpace(sc.Tone); t != "" {
		b.WriteString("Tone: " + t + "\n")

		wroteParams = true
	}
	if v := strings.TrimSpace(sc.EffectiveConflict()); v != "" {
		b.WriteString("Conflict scale: " + v + "\n")

		wroteParams = true
	}
	if v := strings.TrimSpace(sc.TargetAudience); v != "" {
		b.WriteString("Target audience: " + config.AudienceLabel(v, "en") + "\n")

		wroteParams = true
	}
	if p := strings.TrimSpace(sc.AudienceProfile); p != "" {
		b.WriteString("Ideal reader profile:\n" + p + "\n")

		wroteParams = true
	}
	if v := strings.TrimSpace(sc.WorldDarkness); v != "" {
		b.WriteString("World darkness: " + config.WorldDarknessLabel(v, "en") + "\n")

		wroteParams = true
	}
	if bt := strings.TrimSpace(sc.StoryIdea); bt != "" {
		b.WriteString("Existing story idea (background only):\n" + bt + "\n")

		wroteParams = true
	}
	if !wroteParams {
		b.WriteString("No parameters were provided; draw broadly from classic and modern literature (rebirth, doubles, thresholds, water & purification, mirrors, revenge, homecoming, forbidden love...), keeping theme, motif and tone coherent with each other.\n")
	}
	schema := `{"theme": "...", "motif": "...", "tone": "..."}`
	b.WriteString("\nOutput only one JSON object, no other text, no code fences. Format: " + schema)

	var out struct {
		Theme string `json:"theme"`
		Motif string `json:"motif"`
		Tone  string `json:"tone"`
	}
	if err := h.llmJSON(ctx, b.String(), &out); err != nil {
		return err
	}
	theme := strings.TrimSpace(out.Theme)
	motif := strings.TrimSpace(out.Motif)
	tone := strings.TrimSpace(out.Tone)
	if theme == "" && motif == "" && tone == "" {
		return errEmptyGeneration
	}

	newCfg := *h.cfg
	newCfg.Story = *sc
	// The Theme & Motif Generate button fills the three fields of its card:
	// Theme, Literary motif and Tone. Only non-empty LLM answers overwrite.
	if theme != "" {
		newCfg.Story.Theme = theme
	}
	if motif != "" {
		newCfg.Story.Motif = motif
	}
	if tone != "" {
		newCfg.Story.Tone = tone
	}
	data, err := json.MarshalIndent(newCfg, "", "  ")
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := fsutil.WriteFileAtomic(h.cfgPath, data); err != nil {
		return err
	}
	h.cfg = &newCfg
	return nil
}

// —— story_idea ——

func (h *Handlers) generateStoryIdeaFromParams(ctx context.Context, sc *config.StoryConfig) error {
	var b strings.Builder
	b.WriteString("You are a novel planning assistant. From the work parameters below, write a coherent story idea of 120-220 words in English: cover the world, the protagonist's situation, the central conflict and the story's direction; rely only on these parameters and never contradict them. Skip any parameter that is empty.\n\n")

	addPair := func(zhLbl, enLbl, v string) {
		if v = strings.TrimSpace(v); v == "" {
			return
		}
		b.WriteString(enLbl + ": " + v + "\n")

	}
	addPair("class", "Genre", sc.Type)
	// Multi-select subgenres: list every comma-separated entry.
	if subs := config.SplitSubgenreList(sc.Subgenre); len(subs) > 0 {
		parts := make([]string, 0, len(subs))
		for _, s := range subs {
			parts = append(parts, "Subgenre: "+s)

		}
		b.WriteString(strings.Join(parts, "\n") + "\n")
	} else {
		addPair("class", "Subgenre", sc.Subgenre)
	}
	addPair("/make", "Working title", sc.Title)
	addPair("and", "Theme & motif", strings.TrimSpace(sc.EffectiveMotif()+" | "+sc.Theme))
	addPair("", "Tone", sc.Tone)
	if lbl := lengthLabelEN(sc.StoryLength); lbl != "" {
		chMin, chMax := config.SuggestedChaptersByLength(sc.StoryLength)
		if chMin > 0 {
			b.WriteString("Length: " + lengthLabelEN(sc.StoryLength) + fmt.Sprintf(" (plan for roughly %d-%d chapters)\n", chMin, chMax))

		} else {
			b.WriteString("Length: " + lengthLabelEN(sc.StoryLength) + "\n")
		}

	}
	if t := structureLabel(sc.Structure, h.cfg.Language); t != "" {
		addPair("", "Structure", t)
	}
	addPair("", "Conflict scale", sc.EffectiveConflict())
	addPair("", "Protagonist type", sc.EffectiveProtagonist())
	addPair("", "Specific settings", sc.SpecificSettings)
	if v := strings.TrimSpace(sc.TargetAudience); v != "" {
		b.WriteString("Target audience: " + config.AudienceLabel(v, "en") + "\n")

	}
	if v := strings.TrimSpace(sc.RomanceLevel); v != "" {
		lbl := sc.EffectiveRomanceLevel()
		if lbl == "random" {
			lbl = config.RomanceLevelKeys[1] // the story idea should read concretely, not "random"
		}
		addPair("lovethreadratioheavy", "Romance level", config.RomanceLevelLabel(lbl, h.cfg.Language))
	}
	if v := strings.TrimSpace(sc.SexualContent); v != "" {
		addPair("writedegree", "Sexual content", config.SexualContentLabel(sc.EffectiveSexualContent(), h.cfg.Language))
	}
	if v := strings.TrimSpace(sc.GoreLevel); v != "" {
		addPair("bloodfishydegree", "Gore level", config.GoreLevelLabel(sc.EffectiveGoreLevel(), h.cfg.Language))
	}
	if v := strings.TrimSpace(sc.WorldDarkness); v != "" {
		addPair("degree", "World darkness", config.WorldDarknessLabel(v, h.cfg.Language))
	}
	if p := strings.TrimSpace(sc.AudienceProfile); p != "" {
		addPair("metricreaddraw", "Ideal reader profile", p)
	}
	switch bias := strings.TrimSpace(sc.GenderBias); bias {
	case "male":
		addPair("otherdirection", "Gender lean", "male")
	case "female":
		addPair("otherdirection", "Gender lean", "female")
	case "balanced":
		addPair("otherdirection", "Gender lean", "balanced")
	}
	if bt := strings.TrimSpace(sc.StoryIdea); bt != "" {
		b.WriteString("Existing story idea (rewrite/expand it, but keep it consistent with the parameters above):\n" + bt + "\n")

	}
	b.WriteString("When some parameters are missing, still ground the story idea in whatever was provided.\n")

	b.WriteString(jsonRule(h.cfg.Language, `{"story_idea": "..."}`))

	var out struct {
		StoryIdea string `json:"story_idea"`
	}
	if err := h.llmJSON(ctx, b.String(), &out); err != nil {
		return err
	}
	generated := strings.TrimSpace(out.StoryIdea)
	if generated == "" {
		return errEmptyGeneration
	}

	newCfg := *h.cfg
	newCfg.Story = *sc
	newCfg.Story.StoryIdea = generated
	data, err := json.MarshalIndent(newCfg, "", "  ")
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := fsutil.WriteFileAtomic(h.cfgPath, data); err != nil {
		return err
	}
	h.cfg = &newCfg
	return nil
}

// —— labels for the novel-parameter selects (borrowed from NovelWriter) ——

func lengthLabelZH(length string) string {
	switch length {
	case config.LengthShort:
		return "shortpiece"
	case config.LengthNovella:
		return "middlepiece"
	case config.LengthNovel:
		return "longpiece(metricstandard)"
	case config.LengthEpic:
		return "longpiece"
	}
	return ""
}

func lengthLabelEN(length string) string {
	switch length {
	case config.LengthShort:
		return "Short Story"
	case config.LengthNovella:
		return "Novella"
	case config.LengthNovel:
		return "Novel (Standard)"
	case config.LengthEpic:
		return "Novel (Epic)"
	}
	return ""
}

var structureLabels = map[string][2]string{ // key -> {zh, en}
	"three_act":            {"Tres actos", "3-Act Structure"},
	"six_act":              {"Seis actos", "6-Act Structure"},
	"fichtean":             {"Curva de Fichtean", "Fichtean Curve"},
	"freytag":              {"Pirámide de Freytag", "Freytag's Pyramid"},
	"seven_point":          {"Estructura de siete puntos", "Seven-Point Structure"},
	"heros_journey":        {"El viaje del héroe", "Hero's Journey"},
	"heros_journey_simple": {"El viaje del héroe (simplificado)", "Hero's Journey (Simplified)"},
	"save_the_cat":         {"Save the Cat!", "Save the Cat!"},
	"episodic":             {"Estructura episódica", "Episodic Structure"},
}

func structureLabel(structure, lang string) string {
	pair, ok := structureLabels[strings.TrimSpace(structure)]
	if !ok {
		return strings.TrimSpace(structure) // free-form value passes through
	}
	if i18n.NormalizeLanguage(lang) == i18n.LangEN {
		return pair[0]
	}
	return pair[1]
}

func jsonRule(lang, schema string) string {
	if i18n.NormalizeLanguage(lang) == i18n.LangEN {
		return "\n\nOutput only one JSON object, no other text, no code fences. Format: " + schema
	}
	return "\n\nOutput only one JSON object, no other text, no code fences. Format: " + schema
}

var errEmptyGeneration = errors.New("empty_generation")

func (h *Handlers) llmJSON(ctx context.Context, prompt string, out any) error {
	content := llm.CallAPIWithRetryLog(ctx, h.apiCfg, i18n.SystemPromptFor(h.cfg.Language, "author_default"), prompt, h.logger)
	content = strings.TrimSpace(content)
	if content == "" {
		return errEmptyGeneration
	}
	extracted := llm.ExtractJSON(content)
	if extracted == "" {
		return errEmptyGeneration
	}
	return json.Unmarshal([]byte(extracted), out)
}

// —— audience_profile: free-text ideal-reader description (Theme & Motif block) ——

func (h *Handlers) generateAudienceProfile(ctx context.Context, sc *config.StoryConfig) error {
	prompt := storyParamsContext(sc, h.cfg.Language)
	prompt += "\nBased on the work parameters and target-age band above, write an \"ideal reader profile\" for this work: the reader's age and identity, reading habits, preferred genres and deal-breakers, and why this book would attract them (4-6 actionable sentences)."

	prompt += jsonRule(h.cfg.Language, `{"audience_profile": "..."}`)

	var out struct {
		AudienceProfile string `json:"audience_profile"`
	}
	if err := h.llmJSON(ctx, prompt, &out); err != nil {
		return err
	}
	profile := strings.TrimSpace(out.AudienceProfile)
	if profile == "" {
		return errEmptyGeneration
	}

	newCfg := *h.cfg
	newCfg.Story = *sc
	newCfg.Story.AudienceProfile = profile
	data, err := json.MarshalIndent(newCfg, "", "  ")
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := fsutil.WriteFileAtomic(h.cfgPath, data); err != nil {
		return err
	}
	h.cfg = &newCfg
	return nil
}

// —— inspirational_pieces: reference works that steer style/atmosphere (Theme & Motif block) ——

// generateInspirationalPieces proposes a set of real books/films/games/anime
// whose tone and craft fit the novel parameters already entered. Like motif
// and audience_profile it does NOT require a story idea: it is seeded from the
// other parameters. The result is stored in Story.InspirationalPieces and
// injected into outline/writing/section prompts via NovelParametersBlock, so
// it genuinely influences generation (as a craft reference, never copied).
func (h *Handlers) generateInspirationalPieces(ctx context.Context, sc *config.StoryConfig) error {
	prompt := storyParamsContext(sc, h.cfg.Language)
	prompt += "\nBased on the novel parameters above (genre, subgenre, theme & motif, tone, conflict scale, world darkness, target audience, etc.; a story idea may be absent), recommend 4-6 real \"inspirational pieces\" for this work — novels, films, games or anime that match its genre temperament and narrative craft. One entry per line, formatted \"Title — author/studio: what to borrow (style/atmosphere/structure/technique, one phrase)\". Only recommend works that actually exist; they are craft references, never plot sources."

	// Respect the pinned story output language (EN/ES) for this generated text.
	switch lang := config.NormalizeOutputLanguage(sc.OutputLanguage); lang {
	case "en":
		prompt += " Write every entry in English."
	case "es":
		prompt += " Escribe cada entrada en español."
	}
	prompt += jsonRule(h.cfg.Language, `{"inspirational_pieces": ["...", "..."]}`)

	var out struct {
		Pieces []string `json:"inspirational_pieces"`
	}
	if err := h.llmJSON(ctx, prompt, &out); err != nil {
		return err
	}
	var valid []string
	for _, p := range out.Pieces {
		if p = strings.TrimSpace(p); p != "" {
			valid = append(valid, p)
		}
	}
	if len(valid) == 0 {
		return errEmptyGeneration
	}

	newCfg := *h.cfg
	newCfg.Story = *sc
	newCfg.Story.InspirationalPieces = strings.Join(valid, "\n")
	data, err := json.MarshalIndent(newCfg, "", "  ")
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := fsutil.WriteFileAtomic(h.cfgPath, data); err != nil {
		return err
	}
	h.cfg = &newCfg
	return nil
}

// -- style: writing style & POV (written into config.json) --

func (h *Handlers) generateStyle(ctx context.Context, sc *config.StoryConfig) error {
	type styleOut struct {
		WritingStyle string `json:"writing_style"`
		WritingPOV   string `json:"writing_pov"`
	}
	prompt := storyParamsContext(sc, h.cfg.Language)
	if i18n.NormalizeLanguage(h.cfg.Language) == i18n.LangEN {
		prompt += "\nRecommend the most fitting \"writing_style\" (tone, sentence rhythm, rhetoric \u2014 2-4 actionable sentences) and \"writing_pov\" (e.g. third-person limited, first-person heroine) for this work."
	} else {
		prompt += "\nRecommend the most fitting \"writing_style\" (tone, sentence rhythm, rhetoric — 2-4 actionable sentences) and \"writing_pov\" (e.g. third-person limited, first-person heroine) for this work."
	}
	prompt += jsonRule(h.cfg.Language, `{"writing_style": "...", "writing_pov": "..."}`)

	var out styleOut
	if err := h.llmJSON(ctx, prompt, &out); err != nil {
		return err
	}
	if strings.TrimSpace(out.WritingStyle) == "" && strings.TrimSpace(out.WritingPOV) == "" {
		return errEmptyGeneration
	}

	newCfg := *h.cfg
	newCfg.Story = *sc
	if s := strings.TrimSpace(out.WritingStyle); s != "" {
		newCfg.Story.WritingStyle = s
	}
	if p := strings.TrimSpace(out.WritingPOV); p != "" {
		newCfg.Story.WritingPOV = p
	}
	data, err := json.MarshalIndent(newCfg, "", "  ")
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := fsutil.WriteFileAtomic(h.cfgPath, data); err != nil {
		return err
	}
	h.cfg = &newCfg
	return nil
}

// —— characters ——

func (h *Handlers) generateCharacters(ctx context.Context, sc *config.StoryConfig) error {
	prompt := storyParamsContext(sc, h.cfg.Language)
	if len(h.settings.Characters) > 0 {
		lines := make([]string, 0, len(h.settings.Characters))
		for _, c := range h.settings.Characters {
			lines = append(lines, fmt.Sprintf("- %s(%s)", c.Name, c.Personality))
		}
		if i18n.NormalizeLanguage(h.cfg.Language) == i18n.LangEN {
			prompt += "\n[alreadyhashorn]\n" + strings.Join(lines, "\n") + "\npleaseininsurestayandalreadyhashorn upper, thinkandsmallsaynumbermiddlewant tobut horn. "
		} else {
			prompt += "\n[EXISTING CHARACTERS]\n" + strings.Join(lines, "\n") + "\nKeep and enrich the existing characters, and add any characters the story idea requires that are missing."
		}
	}
	prompt += "\nDesign 4-7 core characters (protagonist, antagonist, key supporting roles). For each provide name, age, appearance, personality, background, motivation, abilities, notes (may be empty)."

	arcsOn := sc.CharacterArcsEnabled
	genreKey := config.MatchGenreKey(sc.Type + " " + sc.Subgenre) // composite keys (e.g. scifi:steampunk) supported by presets
	if genreKey == "" {
		genreKey = config.MatchAnimeModifierKey(sc.Type + " " + sc.Subgenre) // anime/manhwa/game modifiers carry their own field presets
	}
	extraKeys, extraDesc := config.GenreCharacterExtraFields(genreKey)
	// Subgenre presets refine (and take precedence over) the parent-genre fields.
	if subKeys := config.SubgenreCharacterExtraFields(sc.Subgenre); len(subKeys) > 0 {
		merged := append([]string{}, subKeys...)
		for _, k := range extraKeys {
			found := false
			for _, m := range merged {
				if m == k {
					found = true
					break
				}
			}
			if !found {
				merged = append(merged, k)
			}
		}
		extraKeys = merged
		if extraDesc != "" {
			extraDesc += " "
		}
		extraDesc += "Given the subgenre, you may also add: " + strings.Join(subKeys, ", ") + " (also optional)."

	}

	// Genre-specific character fields (borrowed from NovelWriter's per-genre lore
	// sheets): suggested by the story type/subgenre but never forced.
	if extraDesc != "" {
		prompt += "\n[GENRE-SPECIFIC FIELDS (optional suggestions)] " + extraDesc

	}
	jsonShape := `{"characters":[{"name":"...","age":"...","appearance":"...","personality":"...","background":"...","motivation":"...","abilities":"...","notes":"..."}]}`
	tail := `"notes":"..."`
	if arcsOn {
		tail += `,"goals":"...","flaws":"...","strengths":"...","arc":"..."`
	}
	for _, k := range extraKeys {
		tail += `,"` + k + `":"..."`
	}
	jsonShape = strings.Replace(jsonShape, `"notes":"..."`, tail, 1)
	prompt += jsonRule(h.cfg.Language, jsonShape)

	var out struct {
		Characters []story.Character `json:"characters"`
	}
	if err := h.llmJSON(ctx, prompt, &out); err != nil {
		return err
	}
	if len(out.Characters) == 0 {
		return errEmptyGeneration
	}

	next := *h.settings
	next.Characters = mergeCharacters(h.settings.Characters, out.Characters)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := story.SaveProjectSettings(h.settingsPath, &next); err != nil {
		return err
	}
	h.settings = &next
	return nil
}

func mergeCharacters(existing, generated []story.Character) []story.Character {
	byName := map[string]int{}
	result := make([]story.Character, len(existing))
	copy(result, existing)
	for i, c := range result {
		byName[strings.TrimSpace(c.Name)] = i
	}
	for _, g := range generated {
		name := strings.TrimSpace(g.Name)
		if name == "" {
			continue
		}
		if idx, ok := byName[name]; ok {
			c := result[idx]
			if c.Age == "" {
				c.Age = g.Age
			}
			if c.Appearance == "" {
				c.Appearance = g.Appearance
			}
			if c.Personality == "" {
				c.Personality = g.Personality
			}
			if c.Background == "" {
				c.Background = g.Background
			}
			if c.Motivation == "" {
				c.Motivation = g.Motivation
			}
			if c.Abilities == "" {
				c.Abilities = g.Abilities
			}
			if c.Notes == "" {
				c.Notes = g.Notes
			}
			if c.Goals == "" {
				c.Goals = g.Goals
			}
			if c.Flaws == "" {
				c.Flaws = g.Flaws
			}
			if c.Strengths == "" {
				c.Strengths = g.Strengths
			}
			if c.Arc == "" {
				c.Arc = g.Arc
			}
			if len(g.Extra) > 0 {
				if c.Extra == nil {
					c.Extra = map[string]string{}
				}
				for k, v := range g.Extra {
					if _, ok := c.Extra[k]; !ok && strings.TrimSpace(v) != "" {
						c.Extra[k] = v
					}
				}
			}
			result[idx] = c
			continue
		}
		g.ID = nextCharacterIDFor(&result)
		byName[name] = len(result)
		result = append(result, g)
	}
	return result
}

func nextCharacterIDFor(list *[]story.Character) string {
	ps := story.ProjectSettings{Characters: *list}
	id := ps.NextCharacterID()
	*list = append(*list, story.Character{ID: id})
	*list = (*list)[:len(*list)-1]
	return id
}

// —— organizations ——

func (h *Handlers) generateOrganizations(ctx context.Context, sc *config.StoryConfig) error {
	prompt := storyParamsContext(sc, h.cfg.Language)
	charLines := make([]string, 0, len(h.settings.Characters))
	for _, c := range h.settings.Characters {
		charLines = append(charLines, c.Name)
	}
	if len(h.settings.Organizations) > 0 {
		lines := make([]string, 0, len(h.settings.Organizations))
		for _, o := range h.settings.Organizations {
			lines = append(lines, fmt.Sprintf("- %s(%s)", o.Name, o.Type))
		}
		if i18n.NormalizeLanguage(h.cfg.Language) == i18n.LangEN {
			prompt += "\n[existing organizations]\n" + strings.Join(lines, "\n") + "\nPlease keep the existing organizations and supplement only those that are needed but missing. "
		} else {
			prompt += "\n[EXISTING ORGANIZATIONS]\n" + strings.Join(lines, "\n") + "\nKeep the existing ones and add any organizations the story idea implies but that are missing."
		}
	}
	if len(charLines) > 0 {
		if i18n.NormalizeLanguage(h.cfg.Language) == i18n.LangEN {
			prompt += "\n[NOMBRES DE PERSONAJES DISPONIBLES]\n" + strings.Join(charLines, ", ") + "\nEl campo members solo puede referenciar nombres de esta lista."
		} else {
			prompt += "\n[AVAILABLE CHARACTER NAMES]\n" + strings.Join(charLines, ", ") + "\nThe members field may only reference names from this list."
		}
	}
	if i18n.NormalizeLanguage(h.cfg.Language) == i18n.LangEN {
		prompt += "\nPlease design 2-5 organizations/factions that drive the plot (sects, companies, families, secret societies, etc.), each with name, type, description, and a list of member character names. "
	} else {
		prompt += "\nDesign 2-5 organizations/factions that drive the plot (sects, companies, families, secret societies...). Provide name, type, description and member character names."
	}
	prompt += jsonRule(h.cfg.Language, `{"organizations":[{"name":"...","type":"...","description":"...","members":["character"]}]}`)

	var out struct {
		Organizations []struct {
			Name        string   `json:"name"`
			Type        string   `json:"type"`
			Description string   `json:"description"`
			Members     []string `json:"members"`
		} `json:"organizations"`
	}
	if err := h.llmJSON(ctx, prompt, &out); err != nil {
		return err
	}
	if len(out.Organizations) == 0 {
		return errEmptyGeneration
	}

	nameToID := map[string]string{}
	for _, c := range h.settings.Characters {
		nameToID[strings.TrimSpace(c.Name)] = c.ID
	}

	next := *h.settings
	next.Organizations = append([]story.Organization{}, h.settings.Organizations...)
	existingNames := map[string]bool{}
	for _, o := range next.Organizations {
		existingNames[strings.TrimSpace(o.Name)] = true
	}
	for _, gen := range out.Organizations {
		name := strings.TrimSpace(gen.Name)
		if name == "" || existingNames[name] {
			continue
		}
		var memberIDs []string
		for _, m := range gen.Members {
			if id, ok := nameToID[strings.TrimSpace(m)]; ok {
				memberIDs = append(memberIDs, id)
			}
		}
		o := story.Organization{
			ID:          nextOrganizationIDFor(&next.Organizations),
			Name:        name,
			Type:        strings.TrimSpace(gen.Type),
			Description: strings.TrimSpace(gen.Description),
			Members:     memberIDs,
		}
		existingNames[name] = true
		next.Organizations = append(next.Organizations, o)
	}
	if len(next.Organizations) == len(h.settings.Organizations) {
		return errEmptyGeneration
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := story.SaveProjectSettings(h.settingsPath, &next); err != nil {
		return err
	}
	h.settings = &next
	return nil
}

func nextOrganizationIDFor(list *[]story.Organization) string {
	ps := story.ProjectSettings{Organizations: *list}
	id := ps.NextOrganizationID()
	*list = append(*list, story.Organization{ID: id})
	*list = (*list)[:len(*list)-1]
	return id
}

// —— single-entry generation (characters / organizations forms) ——

// generateCharactersEntryOrBatch dispatches between filling one existing
// character form (when charID is set — the per-entry "generate" button) and
// the whole-section batch generation.
func (h *Handlers) generateCharactersEntryOrBatch(ctx context.Context, sc *config.StoryConfig, charID string) error {
	if strings.TrimSpace(charID) != "" {
		return h.generateCharacterEntry(ctx, sc, strings.TrimSpace(charID))
	}
	return h.generateCharacters(ctx, sc)
}

// generateCharacterEntry fills the empty fields of one saved character with
// LLM output derived from the story parameters and the character's own name.
func (h *Handlers) generateCharacterEntry(ctx context.Context, sc *config.StoryConfig, charID string) error {
	var target *story.Character
	for i := range h.settings.Characters {
		if h.settings.Characters[i].ID == charID {
			target = &h.settings.Characters[i]
			break
		}
	}
	if target == nil {

		return errors.New("character not found; save it before generating")
	}

	prompt := storyParamsContext(sc, h.cfg.Language)
	others := make([]string, 0, len(h.settings.Characters))
	for _, c := range h.settings.Characters {
		if c.ID == charID {
			continue
		}
		line := c.Name
		if p := strings.TrimSpace(c.Personality); p != "" {
			line += " (" + p + ")"

		}
		others = append(others, "- "+line)
	}
	if len(others) > 0 {
		prompt += "\n[OTHER EXISTING CHARACTERS]\n" + strings.Join(others, "\n") + "\nMake this character complement the others without contradicting them."

	}

	fieldLines := []string{}
	addField := func(key, label, val string) {
		val = strings.TrimSpace(val)
		if val == "" {
			return
		}
		fieldLines = append(fieldLines, fmt.Sprintf("- %s: %s", key, val))

	}
	addField("age", "year", target.Age)
	addField("appearance", "outside", target.Appearance)
	addField("personality", "grid", target.Personality)
	addField("background", "back", target.Background)
	addField("motivation", "movemachine", target.Motivation)
	addField("abilities", "canforce", target.Abilities)
	addField("notes", "equipment", target.Notes)
	prompt += fmt.Sprintf("\nComplete the character \"%s\". The author already filled these fields — keep them verbatim, never rewrite them:\n%s\n", target.Name, strings.Join(fieldLines, "\n"))
	prompt += "Generate content ONLY for the empty fields (age/appearance/personality/background/motivation/abilities/notes, plus goals/flaws/strengths/arc when enabled), consistent with the story idea; return name unchanged."

	if sc.CharacterArcsEnabled {
		prompt += " Also fill goals, flaws, strengths and arc."

	}
	prompt += jsonRule(h.cfg.Language, `{"character":{"name":"...","age":"...","appearance":"...","personality":"...","background":"...","motivation":"...","abilities":"...","notes":"..."}}`)

	var out struct {
		Character story.Character `json:"character"`
	}
	if err := h.llmJSON(ctx, prompt, &out); err != nil {
		return err
	}
	g := out.Character
	changed := false
	next := *h.settings
	next.Characters = append([]story.Character{}, h.settings.Characters...)
	for i := range next.Characters {
		if next.Characters[i].ID != charID {
			continue
		}
		c := next.Characters[i]
		fillIfEmpty := func(dst *string, src string) {
			src = strings.TrimSpace(src)
			if *dst == "" && src != "" {
				*dst = src
				changed = true
			}
		}
		fillIfEmpty(&c.Age, g.Age)
		fillIfEmpty(&c.Appearance, g.Appearance)
		fillIfEmpty(&c.Personality, g.Personality)
		fillIfEmpty(&c.Background, g.Background)
		fillIfEmpty(&c.Motivation, g.Motivation)
		fillIfEmpty(&c.Abilities, g.Abilities)
		fillIfEmpty(&c.Notes, g.Notes)
		if sc.CharacterArcsEnabled {
			fillIfEmpty(&c.Goals, g.Goals)
			fillIfEmpty(&c.Flaws, g.Flaws)
			fillIfEmpty(&c.Strengths, g.Strengths)
			fillIfEmpty(&c.Arc, g.Arc)
		}
		next.Characters[i] = c
		break
	}
	if !changed {
		return errEmptyGeneration
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := story.SaveProjectSettings(h.settingsPath, &next); err != nil {
		return err
	}
	h.settings = &next
	return nil
}

// generateOrganizationsEntryOrBatch dispatches between filling one existing
// organization form (when orgID is set) and the whole-section batch generation.
func (h *Handlers) generateOrganizationsEntryOrBatch(ctx context.Context, sc *config.StoryConfig, orgID string) error {
	if strings.TrimSpace(orgID) != "" {
		return h.generateOrganizationEntry(ctx, sc, strings.TrimSpace(orgID))
	}
	return h.generateOrganizations(ctx, sc)
}

// generateOrganizationEntry fills the empty description of one saved
// organization (and suggests members from existing characters).
func (h *Handlers) generateOrganizationEntry(ctx context.Context, sc *config.StoryConfig, orgID string) error {
	var target *story.Organization
	for i := range h.settings.Organizations {
		if h.settings.Organizations[i].ID == orgID {
			target = &h.settings.Organizations[i]
			break
		}
	}
	if target == nil {

		return errors.New("organization not found; save it before generating")
	}

	prompt := storyParamsContext(sc, h.cfg.Language)
	charLines := make([]string, 0, len(h.settings.Characters))
	nameToID := map[string]string{}
	for _, c := range h.settings.Characters {
		charLines = append(charLines, c.Name)
		nameToID[strings.TrimSpace(c.Name)] = c.ID
	}
	if len(charLines) > 0 {
		prompt += "\n[AVAILABLE CHARACTER NAMES]\n" + strings.Join(charLines, ", ") + "\nThe members field may only reference names from this list."

	}
	desc := strings.TrimSpace(target.Description)
	prompt += fmt.Sprintf("\nComplete the organization \"%s\" (type: %s).", target.Name, target.Type)
	if desc != "" {
		prompt += " Keep the author's existing description and enrich it: " + desc
	} else {
		prompt += " Generate one complete, concrete description consistent with the story idea."
	}
	prompt += " Also suggest 1-4 most relevant members (only names from the list above). Do not change the name or type."

	prompt += jsonRule(h.cfg.Language, `{"organization":{"description":"...","members":["character"]}}`)

	var out struct {
		Organization struct {
			Description string   `json:"description"`
			Members     []string `json:"members"`
		} `json:"organization"`
	}
	if err := h.llmJSON(ctx, prompt, &out); err != nil {
		return err
	}
	newDesc := strings.TrimSpace(out.Organization.Description)
	if newDesc == "" && len(out.Organization.Members) == 0 {
		return errEmptyGeneration
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	next := *h.settings
	next.Organizations = append([]story.Organization{}, h.settings.Organizations...)
	for i := range next.Organizations {
		if next.Organizations[i].ID != orgID {
			continue
		}
		o := next.Organizations[i]
		if newDesc != "" {
			o.Description = newDesc
		}
		if len(o.Members) == 0 {
			seen := map[string]bool{}
			for _, m := range out.Organization.Members {
				if id, ok := nameToID[strings.TrimSpace(m)]; ok && !seen[id] {
					seen[id] = true
					o.Members = append(o.Members, id)
				}
			}
		}
		next.Organizations[i] = o
		break
	}
	if err := story.SaveProjectSettings(h.settingsPath, &next); err != nil {
		return err
	}
	h.settings = &next
	return nil
}

// —— worldview —— (single-entry generation for the worldview form's "generate" button)

// generateWorldview fills one worldview entry (identified by name, or a new
// unnamed placeholder when name is empty) with AI-generated description and
// tags derived from the story parameters. When an entry already exists with
// that name it is enriched in place; otherwise a new entry is created.
func (h *Handlers) generateWorldview(ctx context.Context, sc *config.StoryConfig, name, category, entryID string) error {
	name = strings.TrimSpace(name)
	category = strings.TrimSpace(category)
	entryID = strings.TrimSpace(entryID)
	if category == "" {
		category = "general"
	}
	if name == "" && entryID == "" {

		return errors.New("enter a name for this worldview entry before generating")
	}
	// When editing an existing entry, keep its canonical name/category so the
	// result is written back to the right row even if the form was renamed.
	if entryID != "" {
		for _, w := range h.settings.Worldview {
			if w.ID == entryID {
				if name == "" {
					name = w.Name
				}
				category = w.Category
				break
			}
		}
	}

	prompt := storyParamsContext(sc, h.cfg.Language)
	prompt += fmt.Sprintf("\nGenerate the worldbuilding content for the entry \"%s\" (category: %s) required by this story.", name, category)
	prompt += "\ndescription: one complete, concrete paragraph consistent with the story idea, genre and tone (for locations: atmosphere & purpose; organizations: nature & goals; concepts: rules & costs)."
	prompt += "\ntags: comma-separated keywords (you may reference existing characters/organizations). Do not change the name or category."

	prompt += jsonRule(h.cfg.Language, `{"entry":{"description":"...","tags":"..."}}`)

	var out struct {
		Entry struct {
			Description string `json:"description"`
			Tags        string `json:"tags"`
		} `json:"entry"`
	}
	if err := h.llmJSON(ctx, prompt, &out); err != nil {
		return err
	}
	desc := strings.TrimSpace(out.Entry.Description)
	tags := strings.TrimSpace(out.Entry.Tags)
	if desc == "" && tags == "" {
		return errEmptyGeneration
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	next := *h.settings
	next.Worldview = append([]story.WorldviewEntry{}, h.settings.Worldview...)
	idx := -1
	for i, w := range next.Worldview {
		if entryID != "" {
			if w.ID == entryID {
				idx = i
				break
			}
			continue
		}
		if strings.TrimSpace(w.Name) == name && w.Category == category {
			idx = i
			break
		}
	}
	if idx >= 0 {
		w := next.Worldview[idx]
		if name != "" {
			w.Name = name
		}
		if desc != "" {
			w.Description = desc
		}
		if tags != "" {
			w.Tags = tags
		}
		next.Worldview[idx] = w
	} else {
		ps := &next
		next.Worldview = append(next.Worldview, story.WorldviewEntry{
			ID:          ps.NextWorldviewID(),
			Category:    category,
			Name:        name,
			Description: desc,
			Tags:        tags,
		})
	}
	if err := story.SaveProjectSettings(h.settingsPath, &next); err != nil {
		return err
	}
	h.settings = &next
	return nil
}

// —— worldview batch —— (generate entries across ALL categories at once:
// geography, location, faction, rule, knowledge, history, other). Kept for
// the chat "worldview" section when no entry target is given. The config-page
// Worldview card now uses generateWorldviewCategory per category instead, so
// each section only produces what the author actually asked for. When
// LocationsEnabled is false the "location" category is excluded.

func (h *Handlers) generateWorldviewBatch(ctx context.Context, sc *config.StoryConfig) error {
	prompt := storyParamsContext(sc, h.cfg.Language)

	categories := []string{"geography", "faction", "rule", "knowledge", "history", "other"}
	if sc.LocationsEnabled {
		categories = append(categories, "location")
	}

	existingLines := make([]string, 0, len(h.settings.Worldview))
	for _, w := range h.settings.Worldview {
		existingLines = append(existingLines, fmt.Sprintf("- [%s] %s", w.Category, w.Name))
	}
	if len(existingLines) > 0 {
		prompt += "\n[EXISTING WORLDBUILDING ENTRIES]\n" + strings.Join(existingLines, "\n") + "\nKeep the existing ones; only add new entries for what the story implies but is missing."
	} else {
		prompt += "\nThere are no worldbuilding entries yet — create them from scratch."
	}

	orgLines := make([]string, 0, len(h.settings.Organizations))
	for _, o := range h.settings.Organizations {
		orgLines = append(orgLines, o.Name)
	}
	charLines := make([]string, 0, len(h.settings.Characters))
	for _, c := range h.settings.Characters {
		charLines = append(charLines, c.Name)
	}

	prompt += fmt.Sprintf("\nDesign the worldview of this story as concise entries spread across these categories: %s. Cover every category that matters for this story (geography & key places, factions/power groups, governing rules or magic-system constraints, knowledge/lore, pivotal history) and generate as many entries per category as the story genuinely needs — skip a category entirely if it does not apply. For each entry provide name, category, a short concrete description consistent with the story idea, genre and tone, and comma-separated tags (you may reference existing organizations or characters).", strings.Join(categories, ", "))

	if len(orgLines) > 0 || len(charLines) > 0 {
		names := append(append([]string{}, orgLines...), charLines...)
		prompt += "\n[RELATED ENTITIES] " + strings.Join(names, ", ") + " (you may reference them in tags)"
	}

	prompt += jsonRule(h.cfg.Language, `{"entries":[{"name":"...","category":"geography|location|faction|rule|knowledge|history|other","description":"...","tags":"..."}]}`)

	var out struct {
		Entries []struct {
			Name        string `json:"name"`
			Category    string `json:"category"`
			Description string `json:"description"`
			Tags        string `json:"tags"`
		} `json:"entries"`
	}
	if err := h.llmJSON(ctx, prompt, &out); err != nil {
		return err
	}
	if len(out.Entries) == 0 {
		return errEmptyGeneration
	}

	validCategory := map[string]bool{}
	for _, c := range categories {
		validCategory[c] = true
	}

	next := *h.settings
	next.Worldview = append([]story.WorldviewEntry{}, h.settings.Worldview...)
	existingNames := map[string]bool{}
	for _, w := range next.Worldview {
		existingNames[strings.ToLower(strings.TrimSpace(w.Name))] = true
	}
	ps := &next
	added := 0
	for _, gen := range out.Entries {
		name := strings.TrimSpace(gen.Name)
		cat := strings.TrimSpace(strings.ToLower(gen.Category))
		if name == "" || existingNames[strings.ToLower(name)] {
			continue
		}
		if !validCategory[cat] {
			// Unknown/disallowed category (e.g. "location" when disabled): keep
			// the entry but file it under "other" so it still shows in the UI.
			cat = "other"
		}
		next.Worldview = append(next.Worldview, story.WorldviewEntry{
			ID:          ps.NextWorldviewID(),
			Category:    cat,
			Name:        name,
			Description: strings.TrimSpace(gen.Description),
			Tags:        strings.TrimSpace(gen.Tags),
		})
		existingNames[strings.ToLower(name)] = true
		added++
	}
	if added == 0 {
		return errEmptyGeneration
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := story.SaveProjectSettings(h.settingsPath, &next); err != nil {
		return err
	}
	h.settings = &next
	return nil
}

// —— worldview per-category —— (one section per category in the Worldview
// card: geography, location, faction, rule, knowledge, history, other). Each
// section's Generate button only produces entries for its own category, and
// the number of entries is decided by the LLM according to what the story
// actually needs ("other" gets a soft cap so it never swells). The "location"
// category requires LocationsEnabled. Existing entries are kept; only new,
// non-duplicated ones are added.

// worldviewCategoryForSection resolves the target category for a per-category
// worldview section (wv_geography, ...). An explicit request override wins;
// otherwise the category is derived from the section key.
func worldviewCategoryForSection(section, override string) string {
	if c := strings.TrimSpace(override); c != "" {
		return c
	}
	return agent.WorldviewCategoryBySection()[section]
}

func (h *Handlers) generateWorldviewCategory(ctx context.Context, sc *config.StoryConfig, category string) error {
	category = strings.TrimSpace(strings.ToLower(category))
	if category == "" {
		return errors.New("worldview category is required")
	}
	if category == story.WorldviewCategoryLocation && !sc.LocationsEnabled {
		return errors.New("locations are disabled in the novel parameters")
	}

	prompt := storyParamsContext(sc, h.cfg.Language)

	existing := make([]string, 0)
	for _, w := range h.settings.Worldview {
		if strings.EqualFold(strings.TrimSpace(w.Category), category) {
			existing = append(existing, "- "+w.Name+": "+w.Description)
		}
	}
	if len(existing) > 0 {
		prompt += "\n[EXISTING \"" + category + "\" ENTRIES]\n" + strings.Join(existing, "\n") + "\nKeep the existing ones; only add new entries for what the story implies but is missing."
	} else {
		prompt += "\nThere are no \"" + category + "\" entries yet — create them from scratch."
	}

	orgLines := make([]string, 0, len(h.settings.Organizations))
	for _, o := range h.settings.Organizations {
		orgLines = append(orgLines, o.Name)
	}
	charLines := make([]string, 0, len(h.settings.Characters))
	for _, c := range h.settings.Characters {
		charLines = append(charLines, c.Name)
	}

	guidance := map[string]string{
		"geography": "regions, terrain, climate, borders, routes and other spatial facts of the world",
		"location":  "specific cities, buildings, landmarks or scenes where the story takes place",
		"faction":   "power groups that drive the plot (sects, companies, families, guilds, secret societies...)",
		"rule":      "governing rules of the world: physical/magic-system constraints, laws, taboos and their costs",
		"knowledge": "lore the characters can know or discover: legends, sciences, secrets, in-world books",
		"history":   "pivotal past events, wars, eras and their consequences for the present story",
		"other":     "any other worldbuilding element that does not fit the standard categories",
	}[category]
	if guidance == "" {
		guidance = "worldbuilding elements of this kind"
	}

	countRule := "Generate as many entries as this story genuinely needs for the category (typically 2-6)."
	if category == "other" {
		countRule = "Only generate entries that clearly do not fit geography/location/faction/rule/knowledge/history, and keep it minimal (at most 3)."
	}

	prompt += fmt.Sprintf("\nDesign the \"%s\" dimension of this story's worldview: %s. %s For each entry provide name, a short concrete description consistent with the story idea, genre and tone, and comma-separated tags (you may reference existing organizations or characters). Every entry MUST belong to the category \"%s\".", category, guidance, countRule, category)

	if len(orgLines) > 0 || len(charLines) > 0 {
		names := append(append([]string{}, orgLines...), charLines...)
		prompt += "\n[RELATED ENTITIES] " + strings.Join(names, ", ") + " (you may reference them in tags)"
	}

	prompt += jsonRule(h.cfg.Language, `{"entries":[{"name":"...","description":"...","tags":"..."}]}`)

	var out struct {
		Entries []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Tags        string `json:"tags"`
		} `json:"entries"`
	}
	if err := h.llmJSON(ctx, prompt, &out); err != nil {
		return err
	}
	if len(out.Entries) == 0 {
		return errEmptyGeneration
	}

	next := *h.settings
	next.Worldview = append([]story.WorldviewEntry{}, h.settings.Worldview...)
	existingNames := map[string]bool{}
	for _, w := range next.Worldview {
		existingNames[strings.ToLower(strings.TrimSpace(w.Name))] = true
	}
	ps := &next
	added := 0
	for _, gen := range out.Entries {
		name := strings.TrimSpace(gen.Name)
		if name == "" || existingNames[strings.ToLower(name)] {
			continue
		}
		next.Worldview = append(next.Worldview, story.WorldviewEntry{
			ID:          ps.NextWorldviewID(),
			Category:    category,
			Name:        name,
			Description: strings.TrimSpace(gen.Description),
			Tags:        strings.TrimSpace(gen.Tags),
		})
		existingNames[strings.ToLower(name)] = true
		added++
	}
	if added == 0 {
		return errEmptyGeneration
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := story.SaveProjectSettings(h.settingsPath, &next); err != nil {
		return err
	}
	h.settings = &next
	return nil
}

// —— locations —— (worldview entries with Category == "location"). Thin alias
// of the per-category worldview generator so both section keys ("locations"
// from the chat tool and "wv_location" from the Worldview card button) share
// exactly one implementation.

func (h *Handlers) generateLocations(ctx context.Context, sc *config.StoryConfig) error {
	return h.generateWorldviewCategory(ctx, sc, "location")
}

// —— relations ——

// entityKindByID maps every known entity id to its kind ("character",
// "organization" or "worldview"). Used to validate LLM-produced relation ends.
func (h *Handlers) entityKindByID() map[string]string {
	out := map[string]string{}
	for _, c := range h.settings.Characters {
		out[c.ID] = "character"
	}
	for _, o := range h.settings.Organizations {
		out[o.ID] = "organization"
	}
	for _, w := range h.settings.Worldview {
		out[w.ID] = "worldview"
	}
	return out
}

// entityListLines renders the existing entities as "- [kind] id | name" lines
// for inclusion in generation prompts.
func (h *Handlers) entityListLines() []string {
	lines := make([]string, 0)
	for _, c := range h.settings.Characters {
		lines = append(lines, fmt.Sprintf("- [character] %s | %s", c.ID, c.Name))
	}
	for _, o := range h.settings.Organizations {
		lines = append(lines, fmt.Sprintf("- [organization] %s | %s", o.ID, o.Name))
	}
	for _, w := range h.settings.Worldview {
		lines = append(lines, fmt.Sprintf("- [worldview] %s | %s", w.ID, w.Name))
	}
	return lines
}

// generateRelations creates relations between story entities. When srcID/tgtID
// are both provided (the frontend "generate" button next to a relation form),
// only that single pair is generated; otherwise a batch of key relations across
// all entities is produced.
func (h *Handlers) generateRelations(ctx context.Context, sc *config.StoryConfig, srcID, tgtID string) error {
	srcID = strings.TrimSpace(srcID)
	tgtID = strings.TrimSpace(tgtID)

	kinds := h.entityKindByID()
	if srcID != "" && tgtID != "" {
		if _, ok := kinds[srcID]; !ok {

			return errors.New("source entity not found; save it before generating the relation")
		}
		if _, ok := kinds[tgtID]; !ok {

			return errors.New("target entity not found; save it before generating the relation")
		}
		if srcID == tgtID {

			return errors.New("pick two different entities to generate a relation")
		}
	} else if len(h.settings.Characters)+len(h.settings.Organizations) < 2 {

		return errors.New("generate or add at least two characters/organizations before generating relations")
	}

	prompt := storyParamsContext(sc, h.cfg.Language)
	lines := h.entityListLines()
	if srcID != "" && tgtID != "" {
		// Single-pair mode: fill the relation between the two chosen entities.
		prompt += "\n[EXISTING ENTITIES (id | name)]\n" + strings.Join(lines, "\n")
		prompt += fmt.Sprintf("\nGenerate exactly ONE relationship for this pair: source_id=%s, target_id=%s.", srcID, tgtID)
		prompt += "\nBased on both entities' profiles and the story idea, design the single most dramatically charged relation (rivalry, mentorship, secret loyalty, blood tie, conspiracy...); label is one sentence describing the relation and its undercurrents."
		prompt += "\nReturn only that one relation in the JSON array."

	} else {
		prompt += "\n[EXISTING ENTITIES (id | name)]\n" + strings.Join(lines, "\n") + "\nUse the ids above for source_id/target_id and the bracketed kind for source_type/target_type."
		prompt += "\nDesign 5-10 key relationships with dramatic tension (rivals, mentorship, secret loyalty, blood ties...); label is one sentence describing the relation and its undercurrents. Avoid duplicating existing relations."

	}
	if len(h.settings.Relations) > 0 {
		ex := make([]string, 0, len(h.settings.Relations))
		nameByID := map[string]string{}
		for _, c := range h.settings.Characters {
			nameByID[c.ID] = c.Name
		}
		for _, o := range h.settings.Organizations {
			nameByID[o.ID] = o.Name
		}
		for _, rl := range h.settings.Relations {
			ex = append(ex, fmt.Sprintf("%s → %s: %s", nameByID[rl.SourceID], nameByID[rl.TargetID], rl.Label))
		}
		if i18n.NormalizeLanguage(h.cfg.Language) == i18n.LangEN {
			prompt += "\n[alreadyhascloseis]\n" + strings.Join(ex, "\n")
		} else {
			prompt += "\n[EXISTING RELATIONS]\n" + strings.Join(ex, "\n")
		}
	}
	prompt += jsonRule(h.cfg.Language, `{"relations":[{"source_id":"...","source_type":"character|organization|worldview","target_id":"...","target_type":"...","label":"..."}]}`)

	var out struct {
		Relations []story.Relation `json:"relations"`
	}
	if err := h.llmJSON(ctx, prompt, &out); err != nil {
		return err
	}

	validID := h.entityKindByID()

	singlePair := srcID != "" && tgtID != ""

	next := *h.settings
	next.Relations = append([]story.Relation{}, h.settings.Relations...)
	added := 0
	for _, rel := range out.Relations {
		src := strings.TrimSpace(rel.SourceID)
		tgt := strings.TrimSpace(rel.TargetID)
		srcType, srcOK := validID[src]
		tgtType, tgtOK := validID[tgt]
		if !srcOK || !tgtOK || src == tgt || strings.TrimSpace(rel.Label) == "" {
			continue
		}
		if singlePair && (src != srcID || tgt != tgtID) {
			// Single-pair mode: keep only the relation for the requested pair.
			continue
		}
		dup := false
		for _, ex := range next.Relations {
			if ex.SourceID == src && ex.TargetID == tgt {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		rel.ID = nextRelationIDFor(&next.Relations)
		rel.SourceID = src
		rel.TargetID = tgt
		rel.SourceType = srcType
		rel.TargetType = tgtType
		next.Relations = append(next.Relations, rel)
		added++
	}
	if added == 0 {
		return errEmptyGeneration
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := story.SaveProjectSettings(h.settingsPath, &next); err != nil {
		return err
	}
	h.settings = &next
	return nil
}

func nextRelationIDFor(list *[]story.Relation) string {
	ps := story.ProjectSettings{Relations: *list}
	id := ps.NextRelationID()
	*list = append(*list, story.Relation{ID: id})
	*list = (*list)[:len(*list)-1]
	return id
}

// GetNovelParams exposes the genre presets (conflict scales, protagonist types,
// specific settings) so the frontend can render them; every conflict/protagonist
// list includes "other" and an empty ("auto") choice is always allowed.
func (h *Handlers) GetNovelParams(w http.ResponseWriter, r *http.Request) {
	en := i18n.NormalizeLanguage(h.cfg.Language) == i18n.LangEN
	pick := func(p [2]string) string {
		if en {
			return p[1]
		}
		return p[0]
	}
	subgenreHints := make(map[string]string, 2*len(config.SubgenreHints))
	for k, v := range config.SubgenreHints {
		subgenreHints[k] = pick(v)
		// Round 27 (key-normalization fix): the frontend subgenre combobox
		// looks hints up by snake_case token ("cozy_mystery", "lit_rpg"),
		// while SubgenreHints keys are display labels with spaces. Without a
		// normalized alias every label-keyed hint silently missed and chips
		// rendered without a description. Ship both spellings.
		if tok := strings.ToLower(strings.NewReplacer(" ", "_", "-", "_").Replace(k)); tok != k {
			if _, exists := subgenreHints[tok]; !exists {
				subgenreHints[tok] = pick(v)
			}
		}
	}
	// Per-subgenre catalogs (normalized token -> lists + canonical parent).
	// The frontend used to keep its own duplicated copy of these tables; now
	// the backend ships everything and the UI only filters client-side from
	// this payload (single source of truth). Keys are normalized so
	// "Cozy Mystery" and "cozy_mystery" resolve alike.
	normTok := func(s string) string {
		s = strings.ToLower(strings.TrimSpace(s))
		s = strings.ReplaceAll(s, " ", "_")
		s = strings.ReplaceAll(s, "-", "_")
		return s
	}
	withoutOther := func(list []string) []string {
		out := make([]string, 0, len(list))
		for _, x := range list {
			if x != "other" {
				out = append(out, x)
			}
		}
		return out
	}
	type subcatalog struct {
		Conflicts    []string `json:"conflicts"`
		Protagonists []string `json:"protagonists"`
		Settings     []string `json:"settings"`
		Genre        string   `json:"genre"`
	}
	subgenreCatalogs := map[string]subcatalog{}
	putSubCatalog := func(key string, c subcatalog) {
		if key == "" {
			return
		}
		if prev, ok := subgenreCatalogs[key]; ok &&
			len(prev.Conflicts)+len(prev.Protagonists)+len(prev.Settings) >=
				len(c.Conflicts)+len(c.Protagonists)+len(c.Settings) {
			return // existing entry is at least as rich — keep it
		}
		if c.Genre == "" {
			c.Genre = subgenreCatalogs[key].Genre
		}
		subgenreCatalogs[key] = c
	}
	// 1) Subgenres that carry their own full preset bucket (anime modifiers,
	// drama subgenres, promoted first-class subgenres...).
	for _, tok := range config.SubgenrePresetKeys {
		k := normTok(tok)
		putSubCatalog(k, subcatalog{
			Conflicts:    withoutOther(config.GenreConflictScales[k]),
			Protagonists: withoutOther(config.GenreProtagonistTypes[k]),
			Settings:     withoutOther(config.GenreSpecificSettings[k]),
			Genre:        config.CanonicalParentForSubgenre(k),
		})
	}
	// 2) Classic NovelWriter subgenres without a dedicated bucket: ship the
	// extra character-field keys as settings so the cascade stays populated.
	for k, fields := range config.SubgenreExtraFieldsSnapshot() {
		key := normTok(k)
		c := subcatalog{Settings: fields, Genre: config.CanonicalParentForSubgenre(key)}
		if base, ok := subgenreCatalogs[key]; ok {
			c.Conflicts, c.Protagonists = base.Conflicts, base.Protagonists
			if len(c.Settings) == 0 {
				c.Settings = base.Settings
			}
			if c.Genre == "" {
				c.Genre = base.Genre
			}
		}
		putSubCatalog(key, c)
	}
	structureHints := make(map[string]string)
	for k, v := range story.StructureDescriptions() {
		structureHints[k] = pick(v)
	}
	// Cross-cutting modifier presets (isekai, wuxia, cyberpunk, cozy, noir...):
	// exposed so the "Specific settings" section can offer their signature
	// tokens as checkable suggestions whenever the author's Type/Subgenre text
	// matches one of these modifiers — previously they were invisible unless a
	// classic genre key happened to resolve.
	modifierPresets := config.ModifierPresets
	if len(config.GenreSpecificSettings["isekai"]) > 0 {
		// animeModifierPresets already contributed genre-keyed entries; merge
		// them into the same map copy we ship so nothing mutates global state.
		modifierPresets = make(map[string][]string, len(config.ModifierPresets)+len(config.GenreSpecificSettings))
		for k, v := range config.ModifierPresets {
			modifierPresets[k] = v
		}
		for _, k := range []string{"isekai", "isekai:villainess", "isekai:otome", "manhwa_system", "regression_rebirth", "mahou_shoujo", "mahou_shonen", "battle_shonen", "dungeon", "battle_royale", "tower_climbing", "slice_of_life", "iyashikei", "school_life", "mecha", "harem", "ecchi", "spokon", "revenge"} {
			if v, ok := config.GenreSpecificSettings[k]; ok {
				modifierPresets[k] = v
			}
		}
	}
	h.writeJSON(w, 200, map[string]any{
		"conflict_scales":     config.GenreConflictScales,
		"protagonist_types":   config.GenreProtagonistTypes,
		"specific_settings":   config.GenreSpecificSettings,
		"gender_bias_options": []string{"random", "balanced", "male", "female"},
		"subgenre_presets":    config.SubgenrePresetKeys,
		"subgenre_hints":      subgenreHints,
		// Full per-subgenre cascade catalogs (token -> {conflicts,
		// protagonists, settings, genre}): the backend ships ALL parameters
		// and the frontend is the only place that filters them by the
		// selected parent genre(s).
		"subgenre_catalogs": subgenreCatalogs,
		// Round 18: authoritative single-parent taxonomy (snake_case token ->
		// canonical parent genre key), consumed by the frontend as the definitive
		// source for which Subgenres appear under which Genre.
		"subgenre_parents":       config.CanonicalSubgenreParents(),
		"structure_hints":        structureHints,
		"length_keys":            config.LengthKeys,
		"all_structure_keys":     config.AllStructureKeys,
		"structures_by_length":   config.StructureKeysByLength,
		"target_words_by_length": config.TargetWordsPresetByLength,
		"modifier_presets":       modifierPresets,
	})
}
