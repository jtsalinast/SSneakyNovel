package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"showmethestory/internal/config"
	"showmethestory/internal/fsutil"
	"showmethestory/internal/i18n"
	"showmethestory/internal/llm"
	"showmethestory/internal/story"
)

// PostSectionGenerate uses the story brief (plus any settings the author has
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
	case "style", "characters", "organizations", "relations", "locations", "motif", "brief":
	default:
		h.writeErrorReq(w, r, http.StatusBadRequest, "unknown_section", section)
		return
	}
	if !h.tryStartTask() {
		h.writeErrorReq(w, r, http.StatusConflict, "task_running_wait")
		return
	}

	var body struct {
		Brief string `json:"brief"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.endTask()
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	brief := strings.TrimSpace(body.Brief)
	if brief == "" {
		brief = strings.TrimSpace(h.cfg.Story.Brief)
	}
	if brief == "" && section != "motif" && section != "brief" {
		h.endTask()
		h.writeErrorReq(w, r, http.StatusBadRequest, "brief_required")
		return
	}

	go func() {
		defer h.endTask()
		taskName := "section_generate_" + section
		h.logger.TaskStart(taskName)
		ctx := h.taskCtx
		h.logger.InfoKey("log.section_generating", sectionLabel(section, h.cfg.Language))

		storyCfg := h.cfg.Story
		storyCfg.Brief = brief

		var err error
		switch section {
		case "style":
			err = h.generateStyle(ctx, &storyCfg)
		case "characters":
			err = h.generateCharacters(ctx, &storyCfg)
		case "organizations":
			err = h.generateOrganizations(ctx, &storyCfg)
		case "relations":
			err = h.generateRelations(ctx, &storyCfg)
		case "locations":
			err = h.generateLocations(ctx, &storyCfg)
		case "motif":
			err = h.generateMotif(ctx, &storyCfg)
		case "brief":
			err = h.generateBriefFromParams(ctx, &storyCfg)
		}

		if err != nil {
			if ctx.Err() != nil {
				h.logger.WarnKey("log.section_generate_cancelled")
			} else {
				h.logger.ErrorKey("log.section_generate_failed", err)
			}
			h.logger.TaskEnd(taskName, false)
			return
		}
		h.logger.SuccessKey("log.section_generate_done", sectionLabel(section, h.cfg.Language))
		h.logger.TaskEnd(taskName, true)
		h.broadcastProgress()
	}()

	h.writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
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
	case "motif":
		if zh {
			return "文学母题"
		}
		return "literary motif"
	case "brief":
		if zh {
			return "故事简介"
		}
		return "story brief"
	default:
		if zh {
			return "关系"
		}
		return "relations"
	}
}

// briefContext assembles the brief plus basic story setup into the prompt header.
func briefContext(sc *config.StoryConfig, lang string) string {
	var b strings.Builder
	if i18n.NormalizeLanguage(lang) == i18n.LangZH {
		b.WriteString("你是小说策划助手。请严格依据以下故事简介生成设定，保持与简介的世界观、基调、人物一致；不要引入与简介矛盾的元素。\n\n")
		if m := strings.TrimSpace(sc.Motif); m != "" {
			b.WriteString("【文学母题】" + m + "（请将其融入人物、情节与意象）\n")
		}
		b.WriteString("【故事简介】\n" + sc.Brief + "\n")
		if t := strings.TrimSpace(sc.Type); t != "" {
			b.WriteString("【类型】" + t + "\n")
		}
		if t := strings.TrimSpace(sc.Title); t != "" {
			b.WriteString("【书名】" + t + "\n")
		}
		if t := strings.TrimSpace(sc.Subgenre); t != "" {
			b.WriteString("【子类型】" + t + "\n")
		}
		if t := strings.TrimSpace(sc.Theme); t != "" {
			b.WriteString("【主题】" + t + "\n")
		}
		if t := strings.TrimSpace(sc.Tone); t != "" {
			b.WriteString("【基调】" + t + "\n")
		}
		if lbl := lengthLabelZH(sc.StoryLength); lbl != "" {
			chMin, chMax := config.SuggestedChaptersByLength(sc.StoryLength)
			if chMin > 0 {
				b.WriteString("【篇幅】" + lbl + fmt.Sprintf("（建议总章数约 %d-%d 章）\n", chMin, chMax))
			} else {
				b.WriteString("【篇幅】" + lbl + "\n")
			}
		}
		if t := structureLabel(sc.Structure, lang); t != "" {
			b.WriteString("【故事结构】" + t + "\n")
		}
		if v := strings.TrimSpace(sc.EffectiveConflict()); v != "" {
			b.WriteString("【冲突规模】" + v + "（建议方向，不必强制）\n")
		}
		if v := strings.TrimSpace(sc.EffectiveProtagonist()); v != "" {
			b.WriteString("【主角类型】" + v + "（建议方向，不必强制）\n")
		}
		if v := strings.TrimSpace(sc.SpecificSettings); v != "" {
			b.WriteString("【特定设定】\n" + v + "\n")
		}
		if v := config.AudienceGuidance(sc.TargetAudience, "zh"); v != "" {
			b.WriteString(v + "\n")
		}
		switch bias := strings.TrimSpace(sc.GenderBias); bias {
		case "", "random":
			b.WriteString("【人物性别】随机自然即可，不要刻意偏向任何性别。\n")
		case "male":
			b.WriteString("【人物性别】主要角色倾向男性为主。\n")
		case "female":
			b.WriteString("【人物性别】主要角色倾向女性为主。\n")
		case "balanced":
			b.WriteString("【人物性别】主要角色男女均衡分布。\n")
		}
	} else {
		b.WriteString("You are a novel planning assistant. Create settings strictly based on the story brief below, staying consistent with its worldview, tone and characters; never contradict the brief.\n\n")
		if m := strings.TrimSpace(sc.Motif); m != "" {
			b.WriteString("[LITERARY MOTIF] " + m + " (weave it into characters, plot and imagery)\n")
		}
		b.WriteString("[STORY BRIEF]\n" + sc.Brief + "\n")
		if t := strings.TrimSpace(sc.Type); t != "" {
			b.WriteString("[GENRE] " + t + "\n")
		}
		if t := strings.TrimSpace(sc.Title); t != "" {
			b.WriteString("[TITLE] " + t + "\n")
		}
		if t := strings.TrimSpace(sc.Subgenre); t != "" {
			b.WriteString("[SUBGENRE] " + t + "\n")
		}
		if t := strings.TrimSpace(sc.Theme); t != "" {
			b.WriteString("[THEME] " + t + "\n")
		}
		if t := strings.TrimSpace(sc.Tone); t != "" {
			b.WriteString("[TONE] " + t + "\n")
		}
		if lbl := lengthLabelEN(sc.StoryLength); lbl != "" {
			chMin, chMax := config.SuggestedChaptersByLength(sc.StoryLength)
			if chMin > 0 {
				b.WriteString(fmt.Sprintf("[LENGTH] %s (suggest roughly %d-%d chapters in total)\n", lbl, chMin, chMax))
			} else {
				b.WriteString("[LENGTH] " + lbl + "\n")
			}
		}
		if t := structureLabel(sc.Structure, lang); t != "" {
			b.WriteString("[STRUCTURE] " + t + "\n")
		}
		if v := strings.TrimSpace(sc.EffectiveConflict()); v != "" {
			b.WriteString("[CONFLICT SCALE] " + v + " (a suggestion, not a constraint)\n")
		}
		if v := strings.TrimSpace(sc.EffectiveProtagonist()); v != "" {
			b.WriteString("[PROTAGONIST TYPE] " + v + " (a suggestion, not a constraint)\n")
		}
		if v := strings.TrimSpace(sc.SpecificSettings); v != "" {
			b.WriteString("[SPECIFIC SETTINGS]\n" + v + "\n")
		}
		if v := config.AudienceGuidance(sc.TargetAudience, "en"); v != "" {
			b.WriteString(v + "\n")
		}
		switch bias := strings.TrimSpace(sc.GenderBias); bias {
		case "", "random":
			b.WriteString("[CHARACTER GENDER] Leave it to chance; do not deliberately skew toward any gender.\n")
		case "male":
			b.WriteString("[CHARACTER GENDER] Skew main characters toward male.\n")
		case "female":
			b.WriteString("[CHARACTER GENDER] Skew main characters toward female.\n")
		case "balanced":
			b.WriteString("[CHARACTER GENDER] Keep main characters gender-balanced.\n")
		}
	}
	return b.String()
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
	if bt := strings.TrimSpace(sc.Brief); bt != "" {
		if zh {
			b.WriteString("已有故事简介（仅作背景参考）：\n" + bt + "\n")
		} else {
			b.WriteString("Existing story brief (background only):\n" + bt + "\n")
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
	newCfg.Story.Motif = pick
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

// —— brief ——

func (h *Handlers) generateBriefFromParams(ctx context.Context, sc *config.StoryConfig) error {
	zh := i18n.NormalizeLanguage(h.cfg.Language) == i18n.LangZH
	var b strings.Builder
	if zh {
		b.WriteString("你是小说策划助手。请根据以下已填写的作品参数，撰写一段连贯的故事简介（brief），150-300字中文：概括世界观、主角处境、核心冲突与故事走向；只使用这些参数作为依据，不要发明与参数矛盾的类型或基调。若某参数为空则忽略它。\n\n")
	} else {
		b.WriteString("You are a novel planning assistant. From the work parameters below, write a coherent story brief of 120-220 words in English: cover the world, the protagonist's situation, the central conflict and the story's direction; rely only on these parameters and never contradict them. Skip any parameter that is empty.\n\n")
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
	addPair("文学母题", "Literary motif", sc.Motif)
	addPair("主题", "Theme", sc.Theme)
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
	switch bias := strings.TrimSpace(sc.GenderBias); bias {
	case "male":
		addPair("人物性别倾向", "Gender lean", "male")
	case "female":
		addPair("人物性别倾向", "Gender lean", "female")
	case "balanced":
		addPair("人物性别倾向", "Gender lean", "balanced")
	}
	if bt := strings.TrimSpace(sc.Brief); bt != "" {
		if zh {
			b.WriteString("已有简介（可在其基础上改写扩充，但须与上述参数一致）：\n" + bt + "\n")
		} else {
			b.WriteString("Existing brief (rewrite/expand it, but keep it consistent with the parameters above):\n" + bt + "\n")
		}
	}
	if zh {
		b.WriteString("缺少参数时也要尽量用现有参数提供方向。\n")
	} else {
		b.WriteString("When some parameters are missing, still ground the brief in whatever was provided.\n")
	}
	b.WriteString(jsonRule(h.cfg.Language, `{"brief": "..."}`))

	var out struct {
		Brief string `json:"brief"`
	}
	if err := h.llmJSON(ctx, b.String(), &out); err != nil {
		return err
	}
	generated := strings.TrimSpace(out.Brief)
	if generated == "" {
		return errEmptyGeneration
	}

	newCfg := *h.cfg
	newCfg.Story = *sc
	newCfg.Story.Brief = generated
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

// —— style：写作风格与叙事视角（写入 config.json 的故事配置）——

func (h *Handlers) generateStyle(ctx context.Context, sc *config.StoryConfig) error {
	type styleOut struct {
		WritingStyle string `json:"writing_style"`
		WritingPOV   string `json:"writing_pov"`
	}
	prompt := briefContext(sc, h.cfg.Language)
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
	prompt := briefContext(sc, h.cfg.Language)
	if len(h.settings.Characters) > 0 {
		lines := make([]string, 0, len(h.settings.Characters))
		for _, c := range h.settings.Characters {
			lines = append(lines, fmt.Sprintf("- %s（%s）", c.Name, c.Personality))
		}
		if i18n.NormalizeLanguage(h.cfg.Language) == i18n.LangZH {
			prompt += "\n【已有角色】\n" + strings.Join(lines, "\n") + "\n请在保留并完善已有角色的基础上补充简介中需要但缺失的角色。"
		} else {
			prompt += "\n[EXISTING CHARACTERS]\n" + strings.Join(lines, "\n") + "\nKeep and enrich the existing characters, and add any characters the brief requires that are missing."
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
	prompt := briefContext(sc, h.cfg.Language)
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
			prompt += "\n[EXISTING ORGANIZATIONS]\n" + strings.Join(lines, "\n") + "\nKeep the existing ones and add any organizations the brief implies but that are missing."
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

// —— locations —— (worldview entries with Category == "location")

func (h *Handlers) generateLocations(ctx context.Context, sc *config.StoryConfig) error {
	prompt := briefContext(sc, h.cfg.Language)
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
			prompt += "\n[EXISTING LOCATIONS]\n" + strings.Join(lines, "\n") + "\nKeep the existing ones and add any locations the brief implies but that are missing."
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

func (h *Handlers) generateRelations(ctx context.Context, sc *config.StoryConfig) error {
	if len(h.settings.Characters)+len(h.settings.Organizations) < 2 {
		if i18n.NormalizeLanguage(h.cfg.Language) == i18n.LangZH {
			return errors.New("请先生成或添加至少两个角色/组织，然后再生成关系")
		}
		return errors.New("generate or add at least two characters/organizations before generating relations")
	}

	prompt := briefContext(sc, h.cfg.Language)
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
	if i18n.NormalizeLanguage(h.cfg.Language) == i18n.LangZH {
		prompt += "\n【现有实体（id | 名称）】\n" + strings.Join(lines, "\n") + "\nsource_id/target_id 必须使用上面的 id；source_type/target_type 取对应方括号中的类型。"
		prompt += "\n请设计 5-10 条对剧情有张力的关键关系（敌对、师承、暗恋、效忠、血缘等），label 用一句话描述关系及其暗流。避免重复已有关系。"
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

	validID := map[string]string{} // id -> type
	for _, c := range h.settings.Characters {
		validID[c.ID] = "character"
	}
	for _, o := range h.settings.Organizations {
		validID[o.ID] = "organization"
	}
	for _, w := range h.settings.Worldview {
		validID[w.ID] = "worldview"
	}

	next := *h.settings
	next.Relations = append([]story.Relation{}, h.settings.Relations...)
	added := 0
	for _, rel := range out.Relations {
		srcType, srcOK := validID[strings.TrimSpace(rel.SourceID)]
		tgtType, tgtOK := validID[strings.TrimSpace(rel.TargetID)]
		if !srcOK || !tgtOK || rel.SourceID == rel.TargetID || strings.TrimSpace(rel.Label) == "" {
			continue
		}
		dup := false
		for _, ex := range next.Relations {
			if ex.SourceID == rel.SourceID && ex.TargetID == rel.TargetID {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		rel.ID = nextRelationIDFor(&next.Relations)
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
