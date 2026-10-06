package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

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
	case "style", "characters", "organizations", "relations", "locations", "worldview", "motif", "story_idea", "audience_profile":
	default:
		h.writeErrorReq(w, r, http.StatusBadRequest, "unknown_section", section)
		return
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
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.endTask()
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	req := SectionGenRequest{
		Section:     section,
		StoryIdea:   body.StoryIdea,
		Story:       body.Story,
		Name:        body.Name,
		Category:    body.Category,
		EntryID:     body.EntryID,
		SourceID:    body.SourceID,
		TargetID:    body.TargetID,
		CharacterID: body.CharacterID,
		OrgID:       body.OrgID,
		Ctx:         h.taskCtx, // snapshot: the caller holds the task lock
	}
	if err := h.validateSectionGen(req); err != nil {
		h.endTask()
		h.writeErrorReq(w, r, http.StatusBadRequest, "story_idea_required")
		return
	}

	go func() {
		defer h.endTask()
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
	if storyIdea == "" && req.Section != "motif" && req.Section != "story_idea" && req.Section != "audience_profile" {
		return storyCfg, errors.New("story_idea_required")
	}
	return storyCfg, nil
}

// validateSectionGen reports whether the request can run right now (used for
// synchronous HTTP error responses before starting the task).
func (h *Handlers) validateSectionGen(req SectionGenRequest) error {
	_, err := h.normalizeSectionGen(req)
	return err
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
			err = h.generateWorldview(ctx, &storyCfg, req.Name, req.Category, req.EntryID)
		case "motif":
			err = h.generateMotif(ctx, &storyCfg)
		case "story_idea":
			err = h.generateStoryIdeaFromParams(ctx, &storyCfg)
		case "audience_profile":
			err = h.generateAudienceProfile(ctx, &storyCfg)
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
			err = h.generateWorldview(ctx, &storyCfg, req.Name, req.Category, req.EntryID)
		case "motif":
			err = h.generateMotif(ctx, &storyCfg)
		case "story_idea":
			err = h.generateStoryIdeaFromParams(ctx, &storyCfg)
		case "audience_profile":
			err = h.generateAudienceProfile(ctx, &storyCfg)
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
	zh := i18n.NormalizeLanguage(lang) == i18n.LangZH
	switch section {
	case "style":
		if zh {
			return "写作风格与视角"
		}
		return "writing style & POV"
	case "characters":
		if zh {
			return "角色"
		}
		return "characters"
	case "organizations":
		if zh {
			return "组织"
		}
		return "organizations"
	case "locations":
		if zh {
			return "地点"
		}
		return "locations"
	case "worldview":
		if zh {
			return "世界观条目"
		}
		return "worldview entry"
	case "motif":
		if zh {
			return "文学母题"
		}
		return "literary motif"
	case "story_idea":
		if zh {
			return "故事构想"
		}
		return "story idea"
	case "audience_profile":
		if zh {
			return "目标读者画像"
		}
		return "ideal reader profile"
	default:
		if zh {
			return "关系"
		}
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

// —— motif ——

// motifSeedCount is how many candidate motifs the LLM proposes; one is picked
// at random so repeated clicks give varied results.
const motifSeedCount = 12

func (h *Handlers) generateMotif(ctx context.Context, sc *config.StoryConfig) error {
	zh := i18n.NormalizeLanguage(h.cfg.Language) == i18n.LangZH
	var b strings.Builder
	if zh {
		b.WriteString("你是文学顾问。基于以下作品参数，提出" + fmt.Sprint(motifSeedCount) + "个彼此不同、适合这部作品的文学母题（motif）。\n")
	} else {
		b.WriteString("You are a literary consultant. Based on the work parameters below, propose " + fmt.Sprint(motifSeedCount) + " distinct literary motifs suitable for this work.\n")
	}
	wroteParams := false
	if t := strings.TrimSpace(sc.Type); t != "" {
		if zh {
			b.WriteString("类型：" + t + "\n")
		} else {
			b.WriteString("Genre: " + t + "\n")
		}
		wroteParams = true
	}
	if t := strings.TrimSpace(sc.Subgenre); t != "" {
		if zh {
			b.WriteString("子类型：" + t + "\n")
		} else {
			b.WriteString("Subgenre: " + t + "\n")
		}
		wroteParams = true
	}
	if t := strings.TrimSpace(sc.Theme); t != "" {
		if zh {
			b.WriteString("主题： " + t + "\n")
		} else {
			b.WriteString("Theme: " + t + "\n")
		}
		wroteParams = true
	}
	if t := strings.TrimSpace(sc.Tone); t != "" {
		if zh {
			b.WriteString("基调：" + t + "\n")
		} else {
			b.WriteString("Tone: " + t + "\n")
		}
		wroteParams = true
	}
	if v := strings.TrimSpace(sc.EffectiveConflict()); v != "" {
		if zh {
			b.WriteString("冲突规模：" + v + "\n")
		} else {
			b.WriteString("Conflict scale: " + v + "\n")
		}
		wroteParams = true
	}
	if v := strings.TrimSpace(sc.TargetAudience); v != "" {
		if zh {
			b.WriteString("目标读者：" + config.AudienceLabel(v, "zh") + "\n")
		} else {
			b.WriteString("Target audience: " + config.AudienceLabel(v, "en") + "\n")
		}
		wroteParams = true
	}
	if p := strings.TrimSpace(sc.AudienceProfile); p != "" {
		if zh {
			b.WriteString("目标读者画像：\n" + p + "\n")
		} else {
			b.WriteString("Ideal reader profile:\n" + p + "\n")
		}
		wroteParams = true
	}
	if v := strings.TrimSpace(sc.WorldDarkness); v != "" {
		if zh {
			b.WriteString("世界残酷度：" + config.WorldDarknessLabel(v, "zh") + "\n")
		} else {
			b.WriteString("World darkness: " + config.WorldDarknessLabel(v, "en") + "\n")
		}
		wroteParams = true
	}
	if bt := strings.TrimSpace(sc.StoryIdea); bt != "" {
		if zh {
			b.WriteString("已有故事构想（仅作背景参考）：\n" + bt + "\n")
		} else {
			b.WriteString("Existing story idea (background only):\n" + bt + "\n")
		}
		wroteParams = true
	}
	if !wroteParams && zh {
		b.WriteString("没有任何已填参数，请从经典与现当代文学中广泛取材（如轮回、替身、门槛、水与净化、镜子、复仇、归乡、禁忌之爱等），确保多样。\n")
	} else if !wroteParams {
		b.WriteString("No parameters were provided; draw broadly from classic and modern literature (rebirth, doubles, thresholds, water & purification, mirrors, revenge, homecoming, forbidden love...), keeping the list diverse.\n")
	}
	schema := `{"motifs": ["...", "..."]}`
	if zh {
		b.WriteString("\n每个母题用一句中文短语表达（可附一两个关键意象），不要解释。只输出一个 JSON 对象，不要其他文字，不要用代码块包裹。格式：" + schema)
	} else {
		b.WriteString("\nEach motif must be a short English phrase (optionally naming one or two key images); no explanations. Output only one JSON object, no other text, no code fences. Format: " + schema)
	}

	var out struct {
		Motifs []string `json:"motifs"`
	}
	if err := h.llmJSON(ctx, b.String(), &out); err != nil {
		return err
	}
	var valid []string
	for _, m := range out.Motifs {
		if m = strings.TrimSpace(m); m != "" {
			valid = append(valid, m)
		}
	}
	if len(valid) == 0 {
		return errEmptyGeneration
	}
	pick := valid[seededRandInt(len(valid))]

	newCfg := *h.cfg
	newCfg.Story = *sc
	newCfg.Story.Theme = pick // theme & motif are merged in the UI
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

// seededRandInt returns a pseudo-random int in [0,n) seeded from the clock —
// good enough to vary motif picks between clicks without importing math/rand
// global state.
func seededRandInt(n int) int {
	if n <= 1 {
		return 0
	}
	ns := time.Now().UnixNano()
	return int(ns % int64(n))
}

// —— story_idea ——

func (h *Handlers) generateStoryIdeaFromParams(ctx context.Context, sc *config.StoryConfig) error {
	zh := i18n.NormalizeLanguage(h.cfg.Language) == i18n.LangZH
	var b strings.Builder
	if zh {
		b.WriteString("你是小说策划助手。请根据以下已填写的作品参数，撰写一段连贯的故事构想（story idea），150-300字中文：概括世界观、主角处境、核心冲突与故事走向；只使用这些参数作为依据，不要发明与参数矛盾的类型或基调。若某参数为空则忽略它。\n\n")
	} else {
		b.WriteString("You are a novel planning assistant. From the work parameters below, write a coherent story idea of 120-220 words in English: cover the world, the protagonist's situation, the central conflict and the story's direction; rely only on these parameters and never contradict them. Skip any parameter that is empty.\n\n")
	}
	addPair := func(zhLbl, enLbl, v string) {
		if v = strings.TrimSpace(v); v == "" {
			return
		}
		if zh {
			b.WriteString(zhLbl + "：" + v + "\n")
		} else {
			b.WriteString(enLbl + ": " + v + "\n")
		}
	}
	addPair("类型", "Genre", sc.Type)
	addPair("子类型", "Subgenre", sc.Subgenre)
	addPair("书名/工作名", "Working title", sc.Title)
	addPair("主题与母题", "Theme & motif", strings.TrimSpace(sc.EffectiveMotif()+" | "+sc.Theme))
	addPair("基调", "Tone", sc.Tone)
	if lbl := lengthLabelZH(sc.StoryLength); lbl != "" {
		chMin, chMax := config.SuggestedChaptersByLength(sc.StoryLength)
		if chMin > 0 {
			if zh {
				b.WriteString("篇幅：" + lbl + fmt.Sprintf("（规划约 %d-%d 章）\n", chMin, chMax))
			} else {
				b.WriteString("Length: " + lengthLabelEN(sc.StoryLength) + fmt.Sprintf(" (plan for roughly %d-%d chapters)\n", chMin, chMax))
			}
		} else if zh {
			b.WriteString("篇幅：" + lbl + "\n")
		} else {
			b.WriteString("Length: " + lengthLabelEN(sc.StoryLength) + "\n")
		}
	}
	if t := structureLabel(sc.Structure, h.cfg.Language); t != "" {
		addPair("故事结构", "Structure", t)
	}
	addPair("冲突规模", "Conflict scale", sc.EffectiveConflict())
	addPair("主角类型", "Protagonist type", sc.EffectiveProtagonist())
	addPair("特定设定", "Specific settings", sc.SpecificSettings)
	if v := strings.TrimSpace(sc.TargetAudience); v != "" {
		if zh {
			b.WriteString("目标读者：" + config.AudienceLabel(v, "zh") + "\n")
		} else {
			b.WriteString("Target audience: " + config.AudienceLabel(v, "en") + "\n")
		}
	}
	if v := strings.TrimSpace(sc.RomanceLevel); v != "" {
		lbl := sc.EffectiveRomanceLevel()
		if lbl == "random" {
			lbl = config.RomanceLevelKeys[1] // the story idea should read concretely, not "random"
		}
		addPair("恋爱线比重", "Romance level", config.RomanceLevelLabel(lbl, h.cfg.Language))
	}
	if v := strings.TrimSpace(sc.SexualContent); v != "" {
		addPair("性描写尺度", "Sexual content", config.SexualContentLabel(sc.EffectiveSexualContent(), h.cfg.Language))
	}
	if v := strings.TrimSpace(sc.GoreLevel); v != "" {
		addPair("血腥尺度", "Gore level", config.GoreLevelLabel(sc.EffectiveGoreLevel(), h.cfg.Language))
	}
	if v := strings.TrimSpace(sc.WorldDarkness); v != "" {
		addPair("世界残酷度", "World darkness", config.WorldDarknessLabel(v, h.cfg.Language))
	}
	if p := strings.TrimSpace(sc.AudienceProfile); p != "" {
		addPair("目标读者画像", "Ideal reader profile", p)
	}
	switch bias := strings.TrimSpace(sc.GenderBias); bias {
	case "male":
		addPair("人物性别倾向", "Gender lean", "male")
	case "female":
		addPair("人物性别倾向", "Gender lean", "female")
	case "balanced":
		addPair("人物性别倾向", "Gender lean", "balanced")
	}
	if bt := strings.TrimSpace(sc.StoryIdea); bt != "" {
		if zh {
			b.WriteString("已有简介（可在其基础上改写扩充，但须与上述参数一致）：\n" + bt + "\n")
		} else {
			b.WriteString("Existing story idea (rewrite/expand it, but keep it consistent with the parameters above):\n" + bt + "\n")
		}
	}
	if zh {
		b.WriteString("缺少参数时也要尽量用现有参数提供方向。\n")
	} else {
		b.WriteString("When some parameters are missing, still ground the story idea in whatever was provided.\n")
	}
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
		return "短篇"
	case config.LengthNovella:
		return "中篇"
	case config.LengthNovel:
		return "长篇（标准）"
	case config.LengthEpic:
		return "史诗长篇"
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
	"three_act":            {"三幕式结构", "3-Act Structure"},
	"six_act":              {"六幕式结构", "6-Act Structure"},
	"fichtean":             {"菲希坦曲线", "Fichtean Curve"},
	"freytag":              {"弗莱塔格金字塔", "Freytag's Pyramid"},
	"seven_point":          {"七点式结构", "Seven-Point Structure"},
	"heros_journey":        {"英雄之旅", "Hero's Journey"},
	"heros_journey_simple": {"英雄之旅（简化）", "Hero's Journey (Simplified)"},
	"save_the_cat":         {"救猫咪（Save the Cat!）", "Save the Cat!"},
	"episodic":             {"单元剧结构", "Episodic Structure"},
}

func structureLabel(structure, lang string) string {
	pair, ok := structureLabels[strings.TrimSpace(structure)]
	if !ok {
		return strings.TrimSpace(structure) // free-form value passes through
	}
	if i18n.NormalizeLanguage(lang) == i18n.LangZH {
		return pair[0]
	}
	return pair[1]
}

func jsonRule(lang, schema string) string {
	if i18n.NormalizeLanguage(lang) == i18n.LangZH {
		return "\n\n只输出一个 JSON 对象，不要输出其他文字，不要用代码块包裹。格式：" + schema
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
	zh := i18n.NormalizeLanguage(h.cfg.Language) == i18n.LangZH
	prompt := storyParamsContext(sc, h.cfg.Language)
	if zh {
		prompt += "\n基于以上作品参数与目标读者年龄段，为这部作品撰写一段“理想读者画像”：这位读者的年龄与身份、阅读习惯、偏好的题材与雷区、为什么会被本书吸引（4-6 句可执行的中文描述）。"
	} else {
		prompt += "\nBased on the work parameters and target-age band above, write an \"ideal reader profile\" for this work: the reader's age and identity, reading habits, preferred genres and deal-breakers, and why this book would attract them (4-6 actionable sentences)."
	}
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

// —— style：写作风格与叙事视角（写入 config.json 的故事配置）——

func (h *Handlers) generateStyle(ctx context.Context, sc *config.StoryConfig) error {
	type styleOut struct {
		WritingStyle string `json:"writing_style"`
		WritingPOV   string `json:"writing_pov"`
	}
	prompt := storyParamsContext(sc, h.cfg.Language)
	if i18n.NormalizeLanguage(h.cfg.Language) == i18n.LangZH {
		prompt += "\n请为这部作品推荐最合适的“写作风格”（语气、句式、节奏、修辞倾向，2-4 句可执行的描述）和“叙事视角”（如第三人称限知、第一人称女主等，并说明理由要点）。"
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
			lines = append(lines, fmt.Sprintf("- %s（%s）", c.Name, c.Personality))
		}
		if i18n.NormalizeLanguage(h.cfg.Language) == i18n.LangZH {
			prompt += "\n【已有角色】\n" + strings.Join(lines, "\n") + "\n请在保留并完善已有角色的基础上，补充故事构想与小说参数中需要但缺失的角色。"
		} else {
			prompt += "\n[EXISTING CHARACTERS]\n" + strings.Join(lines, "\n") + "\nKeep and enrich the existing characters, and add any characters the story idea requires that are missing."
		}
	}
	zh := i18n.NormalizeLanguage(h.cfg.Language) == i18n.LangZH
	if zh {
		prompt += "\n请设计 4-7 位核心角色（主角、对手、关键配角），每位包含姓名、年龄、外貌、性格、背景、动机、能力、备注（可为空）。"
	} else {
		prompt += "\nDesign 4-7 core characters (protagonist, antagonist, key supporting roles). For each provide name, age, appearance, personality, background, motivation, abilities, notes (may be empty)."
	}
	arcsOn := sc.CharacterArcsEnabled
	genreKey := config.MatchGenreKey(sc.Type + " " + sc.Subgenre) // composite keys (e.g. scifi:steampunk) supported by presets
	if genreKey == "" {
		genreKey = config.MatchAnimeModifierKey(sc.Type + " " + sc.Subgenre) // anime/manhwa/game modifiers carry their own field presets
	}
	extraKeys, extraDesc := config.GenreCharacterExtraFields(genreKey, zh)
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
		if zh {
			extraDesc += "根据子类型还可补充：" + strings.Join(subKeys, "、") + "（同样可选）。"
		} else {
			extraDesc += "Given the subgenre, you may also add: " + strings.Join(subKeys, ", ") + " (also optional)."
		}
	}

	// Genre-specific character fields (borrowed from NovelWriter's per-genre lore
	// sheets): suggested by the story type/subgenre but never forced.
	if extraDesc != "" {
		if zh {
			prompt += "\n【类型附加字段（建议，可选）】" + extraDesc
		} else {
			prompt += "\n[GENRE-SPECIFIC FIELDS (optional suggestions)] " + extraDesc
		}
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
			lines = append(lines, fmt.Sprintf("- %s（%s）", o.Name, o.Type))
		}
		if i18n.NormalizeLanguage(h.cfg.Language) == i18n.LangZH {
			prompt += "\n【已有组织】\n" + strings.Join(lines, "\n") + "\n请在保留已有组织的基础上补充简介需要但缺失的组织。"
		} else {
			prompt += "\n[EXISTING ORGANIZATIONS]\n" + strings.Join(lines, "\n") + "\nKeep the existing ones and add any organizations the story idea implies but that are missing."
		}
	}
	if len(charLines) > 0 {
		if i18n.NormalizeLanguage(h.cfg.Language) == i18n.LangZH {
			prompt += "\n【可用角色】\n" + strings.Join(charLines, "、") + "\nmembers 字段只能使用上述角色名。"
		} else {
			prompt += "\n[AVAILABLE CHARACTER NAMES]\n" + strings.Join(charLines, ", ") + "\nThe members field may only reference names from this list."
		}
	}
	if i18n.NormalizeLanguage(h.cfg.Language) == i18n.LangZH {
		prompt += "\n请设计 2-5 个对剧情有推动作用的组织/势力（门派、公司、家族、秘密结社等），包含名称、类型、描述、成员角色名列表。"
	} else {
		prompt += "\nDesign 2-5 organizations/factions that drive the plot (sects, companies, families, secret societies...). Provide name, type, description and member character names."
	}
	prompt += jsonRule(h.cfg.Language, `{"organizations":[{"name":"...","type":"...","description":"...","members":["角色名"]}]}`)

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
	zh := i18n.NormalizeLanguage(h.cfg.Language) == i18n.LangZH
	var target *story.Character
	for i := range h.settings.Characters {
		if h.settings.Characters[i].ID == charID {
			target = &h.settings.Characters[i]
			break
		}
	}
	if target == nil {
		if zh {
			return errors.New("找不到该角色，请先保存后再点击生成")
		}
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
			if zh {
				line += "（" + p + "）"
			} else {
				line += " (" + p + ")"
			}
		}
		others = append(others, "- "+line)
	}
	if len(others) > 0 {
		if zh {
			prompt += "\n【其他已有角色】\n" + strings.Join(others, "\n") + "\n请让新设定与其他角色互补、不冲突。"
		} else {
			prompt += "\n[OTHER EXISTING CHARACTERS]\n" + strings.Join(others, "\n") + "\nMake this character complement the others without contradicting them."
		}
	}

	fieldLines := []string{}
	addField := func(key, label, val string) {
		val = strings.TrimSpace(val)
		if val == "" {
			return
		}
		if zh {
			fieldLines = append(fieldLines, fmt.Sprintf("- %s：%s", label, val))
		} else {
			fieldLines = append(fieldLines, fmt.Sprintf("- %s: %s", key, val))
		}
	}
	addField("age", "年龄", target.Age)
	addField("appearance", "外貌", target.Appearance)
	addField("personality", "性格", target.Personality)
	addField("background", "背景", target.Background)
	addField("motivation", "动机", target.Motivation)
	addField("abilities", "能力", target.Abilities)
	addField("notes", "备注", target.Notes)
	if zh {
		prompt += fmt.Sprintf("\n请完善角色「%s」。以下字段作者已填写，必须原样保留、不得改写：\n%s\n", target.Name, strings.Join(fieldLines, "\n"))
		prompt += "只为空缺的字段（age/appearance/personality/background/motivation/abilities/notes，以及 goals/flaws/strengths/arc 若启用）生成与故事构想一致的内容；name 字段返回原值。"
	} else {
		prompt += fmt.Sprintf("\nComplete the character \"%s\". The author already filled these fields — keep them verbatim, never rewrite them:\n%s\n", target.Name, strings.Join(fieldLines, "\n"))
		prompt += "Generate content ONLY for the empty fields (age/appearance/personality/background/motivation/abilities/notes, plus goals/flaws/strengths/arc when enabled), consistent with the story idea; return name unchanged."
	}
	if sc.CharacterArcsEnabled {
		if zh {
			prompt += "并补充 goals、flaws、strengths、arc。"
		} else {
			prompt += " Also fill goals, flaws, strengths and arc."
		}
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
	zh := i18n.NormalizeLanguage(h.cfg.Language) == i18n.LangZH
	var target *story.Organization
	for i := range h.settings.Organizations {
		if h.settings.Organizations[i].ID == orgID {
			target = &h.settings.Organizations[i]
			break
		}
	}
	if target == nil {
		if zh {
			return errors.New("找不到该组织，请先保存后再点击生成")
		}
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
		if zh {
			prompt += "\n【可用角色】\n" + strings.Join(charLines, "、") + "\nmembers 字段只能使用上述角色名。"
		} else {
			prompt += "\n[AVAILABLE CHARACTER NAMES]\n" + strings.Join(charLines, ", ") + "\nThe members field may only reference names from this list."
		}
	}
	desc := strings.TrimSpace(target.Description)
	if zh {
		prompt += fmt.Sprintf("\n请完善组织「%s」（类型：%s）。", target.Name, target.Type)
		if desc != "" {
			prompt += "作者已填写的描述必须保留并在其基础上扩写：" + desc
		} else {
			prompt += "请生成一段完整、具体、与故事构想一致的描述。"
		}
		prompt += "同时给出 1-4 个最相关的成员（仅限上面列表中的角色名）。不要修改名称与类型。"
	} else {
		prompt += fmt.Sprintf("\nComplete the organization \"%s\" (type: %s).", target.Name, target.Type)
		if desc != "" {
			prompt += " Keep the author's existing description and enrich it: " + desc
		} else {
			prompt += " Generate one complete, concrete description consistent with the story idea."
		}
		prompt += " Also suggest 1-4 most relevant members (only names from the list above). Do not change the name or type."
	}
	prompt += jsonRule(h.cfg.Language, `{"organization":{"description":"...","members":["角色名"]}}`)

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
	zh := i18n.NormalizeLanguage(h.cfg.Language) == i18n.LangZH
	name = strings.TrimSpace(name)
	category = strings.TrimSpace(category)
	entryID = strings.TrimSpace(entryID)
	if category == "" {
		category = "general"
	}
	if name == "" && entryID == "" {
		if zh {
			return errors.New("请先填写该世界观条目的名称，再点击生成")
		}
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
	if zh {
		prompt += fmt.Sprintf("\n请为世界观条目「%s」（类别：%s）生成本故事所需的设定内容。", name, category)
		prompt += "\ndescription：一段完整、具体、与故事构想和类型一致的设定描述（地点写氛围与用途，组织写性质与目标，概念写规则与代价等）。"
		prompt += "\ntags：逗号分隔的关键词（可关联已有角色/组织名）。不要修改名称与类别。"
	} else {
		prompt += fmt.Sprintf("\nGenerate the worldbuilding content for the entry \"%s\" (category: %s) required by this story.", name, category)
		prompt += "\ndescription: one complete, concrete paragraph consistent with the story idea, genre and tone (for locations: atmosphere & purpose; organizations: nature & goals; concepts: rules & costs)."
		prompt += "\ntags: comma-separated keywords (you may reference existing characters/organizations). Do not change the name or category."
	}
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

// —— locations —— (worldview entries with Category == "location")

func (h *Handlers) generateLocations(ctx context.Context, sc *config.StoryConfig) error {
	prompt := storyParamsContext(sc, h.cfg.Language)
	zh := i18n.NormalizeLanguage(h.cfg.Language) == i18n.LangZH

	existing := h.settings.Locations()
	if len(existing) > 0 {
		lines := make([]string, 0, len(existing))
		for _, w := range existing {
			lines = append(lines, "- "+w.Name)
		}
		if zh {
			prompt += "\n【已有地点】\n" + strings.Join(lines, "\n") + "\n请在保留已有地点的基础上补充简介需要但缺失的地点。"
		} else {
			prompt += "\n[EXISTING LOCATIONS]\n" + strings.Join(lines, "\n") + "\nKeep the existing ones and add any locations the story idea implies but that are missing."
		}
	}
	orgLines := make([]string, 0, len(h.settings.Organizations))
	for _, o := range h.settings.Organizations {
		orgLines = append(orgLines, o.Name)
	}
	charLines := make([]string, 0, len(h.settings.Characters))
	for _, c := range h.settings.Characters {
		charLines = append(charLines, c.Name)
	}
	if zh {
		prompt += "\n请设计 3-6 个对剧情重要的地点/场景（城市、据点、地标、秘境等），每个包含名称、简短描述、标签（逗号分隔，可关联组织或角色名）。"
	} else {
		prompt += "\nDesign 3-6 important locations/scenes for this story (cities, strongholds, landmarks, hidden places...). For each provide name, a short description and comma-separated tags (may mention existing organizations or characters)."
	}
	if len(orgLines) > 0 || len(charLines) > 0 {
		names := append(append([]string{}, orgLines...), charLines...)
		if zh {
			prompt += "\n【相关实体】" + strings.Join(names, "、") + "（可在 tags 中引用）"
		} else {
			prompt += "\n[RELATED ENTITIES] " + strings.Join(names, ", ") + " (you may reference them in tags)"
		}
	}
	prompt += jsonRule(h.cfg.Language, `{"locations":[{"name":"...","description":"...","tags":"..."}]}`)

	var out struct {
		Locations []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Tags        string `json:"tags"`
		} `json:"locations"`
	}
	if err := h.llmJSON(ctx, prompt, &out); err != nil {
		return err
	}
	if len(out.Locations) == 0 {
		return errEmptyGeneration
	}

	next := *h.settings
	next.Worldview = append([]story.WorldviewEntry{}, h.settings.Worldview...)
	existingNames := map[string]bool{}
	for _, w := range next.Worldview {
		if w.Category == story.WorldviewCategoryLocation {
			existingNames[strings.TrimSpace(w.Name)] = true
		}
	}
	ps := &next
	added := 0
	for _, gen := range out.Locations {
		name := strings.TrimSpace(gen.Name)
		if name == "" || existingNames[name] {
			continue
		}
		next.Worldview = append(next.Worldview, story.WorldviewEntry{
			ID:          ps.NextWorldviewID(),
			Category:    story.WorldviewCategoryLocation,
			Name:        name,
			Description: strings.TrimSpace(gen.Description),
			Tags:        strings.TrimSpace(gen.Tags),
		})
		existingNames[name] = true
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
	zh := i18n.NormalizeLanguage(h.cfg.Language) == i18n.LangZH

	kinds := h.entityKindByID()
	if srcID != "" && tgtID != "" {
		if _, ok := kinds[srcID]; !ok {
			if zh {
				return errors.New("源实体不存在，请先保存该实体后再生成关系")
			}
			return errors.New("source entity not found; save it before generating the relation")
		}
		if _, ok := kinds[tgtID]; !ok {
			if zh {
				return errors.New("目标实体不存在，请先保存该实体后再生成关系")
			}
			return errors.New("target entity not found; save it before generating the relation")
		}
		if srcID == tgtID {
			if zh {
				return errors.New("请选择两个不同的实体来生成关系")
			}
			return errors.New("pick two different entities to generate a relation")
		}
	} else if len(h.settings.Characters)+len(h.settings.Organizations) < 2 {
		if zh {
			return errors.New("请先生成或添加至少两个角色/组织，然后再生成关系")
		}
		return errors.New("generate or add at least two characters/organizations before generating relations")
	}

	prompt := storyParamsContext(sc, h.cfg.Language)
	lines := h.entityListLines()
	if srcID != "" && tgtID != "" {
		// Single-pair mode: fill the relation between the two chosen entities.
		if zh {
			prompt += "\n【现有实体（id | 名称）】\n" + strings.Join(lines, "\n")
			prompt += fmt.Sprintf("\n只为这一对实体生成一条关系：source_id=%s，target_id=%s。", srcID, tgtID)
			prompt += "\n结合双方的设定与故事构想，设计一条最有戏剧张力的一条关系（敌对、师承、暗恋、效忠、血缘、阴谋等），label 用一句话描述关系及其暗流。"
			prompt += "\n只返回 JSON 数组中包含这一条关系的对象。"
		} else {
			prompt += "\n[EXISTING ENTITIES (id | name)]\n" + strings.Join(lines, "\n")
			prompt += fmt.Sprintf("\nGenerate exactly ONE relationship for this pair: source_id=%s, target_id=%s.", srcID, tgtID)
			prompt += "\nBased on both entities' profiles and the story idea, design the single most dramatically charged relation (rivalry, mentorship, secret loyalty, blood tie, conspiracy...); label is one sentence describing the relation and its undercurrents."
			prompt += "\nReturn only that one relation in the JSON array."
		}
	} else {
		if zh {
			prompt += "\n【现有实体（id | 名称）】\n" + strings.Join(lines, "\n") + "\nsource_id/target_id 必须使用上面的 id；source_type/target_type 取对应方括号中的类型。"
			prompt += "\n请设计 5-10 条对剧情有张力的关键关系（敌对、师承、暗恋、效忠、血缘等），label 用一句话描述关系及其暗流。避免重复已有关系。"
		} else {
			prompt += "\n[EXISTING ENTITIES (id | name)]\n" + strings.Join(lines, "\n") + "\nUse the ids above for source_id/target_id and the bracketed kind for source_type/target_type."
			prompt += "\nDesign 5-10 key relationships with dramatic tension (rivals, mentorship, secret loyalty, blood ties...); label is one sentence describing the relation and its undercurrents. Avoid duplicating existing relations."
		}
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
			ex = append(ex, fmt.Sprintf("%s → %s：%s", nameByID[rl.SourceID], nameByID[rl.TargetID], rl.Label))
		}
		if i18n.NormalizeLanguage(h.cfg.Language) == i18n.LangZH {
			prompt += "\n【已有关系】\n" + strings.Join(ex, "\n")
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
	subgenreHints := make(map[string]string, len(config.SubgenreHints))
	for k, v := range config.SubgenreHints {
		subgenreHints[k] = pick(v)
	}
	structureHints := make(map[string]string)
	for k, v := range story.StructureDescriptions() {
		structureHints[k] = pick(v)
	}
	h.writeJSON(w, 200, map[string]any{
		"conflict_scales":     config.GenreConflictScales,
		"protagonist_types":   config.GenreProtagonistTypes,
		"specific_settings":   config.GenreSpecificSettings,
		"gender_bias_options": []string{"random", "balanced", "male", "female"},
		"subgenre_presets":    config.SubgenrePresetKeys,
		"subgenre_hints":      subgenreHints,
		"structure_hints":     structureHints,
	})
}
