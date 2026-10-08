package config

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"showmethestory/internal/fsutil"
	"showmethestory/internal/i18n"
	"strings"
)

type APIConfig struct {
	APIKey              string `json:"api_key"`
	BaseURL             string `json:"base_url"`
	URLStrict           bool   `json:"url_strict,omitempty"` // true = 不自动插入 /v1，仅补 /chat/completions
	Model               string `json:"model"`
	MaxTokens           int    `json:"max_tokens,omitempty"` // 0 = 模型默认；新建默认 32768；Agent 调用建议 ≥ 8192
	HTTPTimeoutSeconds  int    `json:"http_timeout_seconds"`
	ContextBudgetTokens int    `json:"context_budget_tokens"` // 模型上下文预算，默认 300000
}

type Config struct {
	ProjectFormatVersion int           `json:"project_format_version"`
	CreatedWithVersion   string        `json:"created_with_version,omitempty"`
	Language             string        `json:"language"` // "zh" 或 "en"，影响 AI 提示词与生成内容
	Story                StoryConfig   `json:"story"`
	Prompts              PromptsConfig `json:"prompts"`
	SkillConfig          *SkillConfig  `json:"skill_config,omitempty"`
}

type StoryConfig struct {
	Type                  string `json:"type"`
	Title                 string `json:"title"`
	Subgenre              string `json:"subgenre,omitempty"`               // 子类型（受 Type 约束），借鉴 NovelWriter 的 Genre/Subgenre 参数
	Theme                 string `json:"theme,omitempty"`                  // 主题
	Tone                  string `json:"tone,omitempty"`                   // 基调
	Author                string `json:"author,omitempty"`                 // 作者名
	StoryLength           string `json:"story_length,omitempty"`           // 篇幅：short/novella/novel/epic，联动章节数与结构选项
	Structure             string `json:"structure,omitempty"`              // 故事结构框架（3幕/英雄之旅等，随篇幅变化）
	Motif                 string `json:"motif,omitempty"`                  // 文学母题：可随机生成，注入大纲/写作/生成 prompts
	InspirationalPieces   string `json:"inspirational_pieces,omitempty"`   // 灵感作品（Inspirational pieces）：作为风格/氛围参考注入大纲、写作与区块生成 prompts；可由 LLM 根据已填参数生成，不依赖故事构想
	OutputLanguage        string `json:"output_language,omitempty"`        // 生成内容语言（Story language）："en" / "es"；为空时跟随项目 Language。强制所有 AI 生成内容（大纲、正文、区块）使用该语言，避免生成中途换语言
	StoryIdea             string `json:"story_idea,omitempty"`             // 故事构想（Story idea）：作为 AI 生成风格/角色/组织/关系的依据；旧配置中的 "brief" 键由 normalizeLegacyStoryKeys 迁移
	ConflictScale         string `json:"conflict_scale,omitempty"`         // 冲突规模（借鉴 NovelWriter conflict_scales；"other" 时使用 ConflictOther）
	ConflictOther         string `json:"conflict_other,omitempty"`         // 自定义冲突规模
	SpecificSettings      string `json:"specific_settings,omitempty"`      // 特定设定，每行一条（借鉴 implied_settings）
	ProtagonistType       string `json:"protagonist_type,omitempty"`       // 主角类型（借鉴 protagonist_types；"other" 时使用 ProtagonistOther）
	ProtagonistOther      string `json:"protagonist_other,omitempty"`      // 自定义主角类型
	TargetAudience        string `json:"target_audience,omitempty"`        // 目标读者：kid/middle_grade/ya/new_adult/adult/all_ages；影响语言难度、尺度与题材处理（原 YA/儿童文学从类型层级移到这里）
	AudienceProfile       string `json:"audience_profile,omitempty"`       // 目标读者画像：自由文本，描述理想读者的阅读偏好；可由 LLM 根据已填参数生成
	RomanceLevel          string `json:"romance_level,omitempty"`          // 恋爱线比重：random/none/subplot/moderate/central
	SexualContent         string `json:"sexual_content,omitempty"`         // 性描写尺度：random/clean/fade_to_black/explicit
	GoreLevel             string `json:"gore_level,omitempty"`             // 暴力血腥尺度：random/none/mid/explicit
	WorldDarkness         string `json:"world_darkness,omitempty"`         // 世界黑暗与残酷度：idyllic/temperate/gritty/grim/abyssal
	GenderBias            string `json:"gender_bias,omitempty"`            // 人物性别倾向：empty/random/male/female/balanced；默认 random，不强制
	LocationsEnabled      bool   `json:"locations_enabled,omitempty"`      // 启用地点/场景实体（借鉴 NovelWriter locations）：生成设定与大纲时纳入地点
	CharacterArcsEnabled  bool   `json:"character_arcs_enabled,omitempty"` // 启用角色弧光字段（goals/flaws/strengths/arc，借鉴 NovelWriter lore）
	TargetWordsPerChapter int    `json:"target_words_per_chapter"`
	WritingStyle          string `json:"writing_style"`
	WritingPOV            string `json:"writing_pov"` // 叙述视角，如第一人称女主、第三人称限知等
}

// EffectiveConflict returns the conflict scale, resolving the "other" option.
func (s *StoryConfig) EffectiveConflict() string {
	if s.ConflictScale == "other" || s.ConflictScale == "" {
		return s.ConflictOther
	}
	return s.ConflictScale
}

// EffectiveProtagonist returns the protagonist type, resolving the "other" option.
func (s *StoryConfig) EffectiveProtagonist() string {
	if s.ProtagonistType == "other" || s.ProtagonistType == "" {
		return s.ProtagonistOther
	}
	return s.ProtagonistType
}

// EffectiveMotif returns the literary motif. Theme and Motif are merged in the
// UI into a single field, so the effective motif is either Motif or Theme.
func (s *StoryConfig) EffectiveMotif() string {
	if m := strings.TrimSpace(s.Motif); m != "" {
		return m
	}
	return strings.TrimSpace(s.Theme)
}

// —— Story output language (EN / ES) —
//
// OutputLanguage lets the author pin the language of every AI-generated text
// (story idea, sections, outline, prose). Valid values: "en", "es"; empty
// means "follow the project Language" (zh/en), i.e. legacy behavior.

// NormalizeOutputLanguage maps any accepted spelling to "en"/"es"; unknown or
// empty values return "" (= follow the project language).
func NormalizeOutputLanguage(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "en", "english", "inglés", "ingles":
		return "en"
	case "es", "spanish", "español", "espanol", "castellano":
		return "es"
	default:
		return ""
	}
}

// OutputLanguageName returns the display name for a story output language key.
func OutputLanguageName(key string) string {
	switch NormalizeOutputLanguage(key) {
	case "en":
		return "English"
	case "es":
		return "Español"
	}
	return ""
}

// EffectiveOutputLanguage resolves the story's pinned output language from a
// full config: "en"/"es" when set; otherwise "" meaning the prompts should
// keep following the project Language as before.
func EffectiveOutputLanguage(cfg *Config) string {
	if cfg == nil {
		return ""
	}
	return NormalizeOutputLanguage(cfg.Story.OutputLanguage)
}

// —— Novel parameters: length & structure options (borrowed from NovelWriter) —

const (
	LengthFlash      = "flash"       // 微型小说/闪小说
	LengthShort      = "short"       // 短篇
	LengthNovelette  = "novelette"   // 短中篇
	LengthNovella    = "novella"     // 中篇
	LengthLightNovel = "light_novel" // 轻小说（日系：章节长、节奏快）
	LengthAnthology  = "anthology"   // 短篇小说集/选集
	LengthNovel      = "novel"       // 长篇（标准）
	LengthEpic       = "epic"        // 长篇（史诗）
)

// LengthKeys is the ordered list of selectable story lengths.
var LengthKeys = []string{LengthFlash, LengthShort, LengthNovelette, LengthNovella, LengthLightNovel, LengthAnthology, LengthNovel, LengthEpic}

// AllStructureKeys lists every supported narrative-structure framework. The UI
// offers ALL of them regardless of the selected story length — length only
// suggests a default; no structure is ever hidden or force-swapped because of it.
var AllStructureKeys = []string{"three_act", "six_act", "fichtean", "freytag", "seven_point", "heros_journey", "heros_journey_simple", "save_the_cat", "story_circle", "kishotenketsu", "snowflake", "quest", "romance_arc", "episodic"}

// StructureKeysByLength maps a story length to its *recommended* structure
// frameworks (used for defaults and randomization only; the frontend shows
// AllStructureKeys so nothing is gated by length).
var StructureKeysByLength = map[string][]string{
	LengthFlash:      {"three_act", "freytag"},
	LengthShort:      {"three_act", "fichtean", "freytag"},
	LengthNovelette:  {"three_act", "seven_point"},
	LengthNovella:    {"three_act", "seven_point", "heros_journey_simple"},
	LengthLightNovel: {"kishotenketsu", "episodic", "three_act"},
	LengthAnthology:  {"episodic"},
	LengthNovel:      {"three_act", "six_act", "save_the_cat", "heros_journey"},
	LengthEpic:       {"six_act", "heros_journey", "save_the_cat", "snowflake", "episodic"},
}

// DefaultStructureForLength returns the recommended structure for a length.
func DefaultStructureForLength(length string) string {
	switch length {
	case LengthFlash, LengthShort, LengthNovelette, LengthNovella:
		return "three_act"
	case LengthLightNovel:
		return "kishotenketsu"
	case LengthAnthology:
		return "episodic"
	default:
		return "six_act"
	}
}

// SuggestedChaptersByLength gives an outline chapter-count hint per length.
func SuggestedChaptersByLength(length string) (min, max int) {
	switch length {
	case LengthFlash:
		return 1, 3
	case LengthShort:
		return 5, 12
	case LengthNovelette:
		return 8, 18
	case LengthNovella:
		return 12, 25
	case LengthLightNovel:
		return 20, 40
	case LengthAnthology:
		return 6, 15
	case LengthNovel:
		return 25, 45
	case LengthEpic:
		return 45, 90
	}
	return 0, 0
}

// TargetWordsPresetByLength maps a story length to the suggested
// target_words_per_chapter value applied when the author changes the length
// (the field stays freely editable afterwards). Light novels use short punchy
// chapters; flash fiction reads as one continuous piece; anthologies collect
// self-contained stories.
var TargetWordsPresetByLength = map[string]int{
	LengthFlash:      1000,
	LengthShort:      2000,
	LengthNovelette:  2500,
	LengthNovella:    2500,
	LengthLightNovel: 3500,
	LengthAnthology:  4000,
	LengthNovel:      3000,
	LengthEpic:       3000,
}

// DefaultTargetWordsForLength returns the suggested target_words_per_chapter
// preset for a story length, or 0 when the length is unknown / not specified
// (callers must then keep the current value). The frontend applies it live via
// /api/novel-params so changing "Story length" updates the word-target field.
func DefaultTargetWordsForLength(length string) int {
	return TargetWordsPresetByLength[strings.TrimSpace(length)]
}

type PromptsConfig struct {
	ChapterWriting                string `json:"chapter_writing"`
	ChapterRevision               string `json:"chapter_revision"`
	ChapterSegmentRevision        string `json:"chapter_segment_revision"`
	ChapterSummary                string `json:"chapter_summary"`
	FactCheck                     string `json:"fact_check"`
	OutlineRevision               string `json:"outline_revision"`
	ForeshadowPlanning            string `json:"foreshadow_planning"`
	ForeshadowUpdate              string `json:"foreshadow_update"`
	ContinuationOutlineGeneration string `json:"continuation_outline_generation"`
	SettingsReconciliation        string `json:"settings_reconciliation"`
	TransitionSmoothing           string `json:"transition_smoothing"`
	OutlineConsistencyCheck       string `json:"outline_consistency_check"`
	ForeshadowOutlineConsistency  string `json:"foreshadow_outline_consistency"`
	OutlineCharacterCheck         string `json:"outline_character_check"`
	WritingConflictAnalysis       string `json:"writing_conflict_analysis"`
	BookDiagnosis                 string `json:"book_diagnosis"`
	BookConsistencyCheck          string `json:"book_consistency_check"`
	BookRoadmap                   string `json:"book_roadmap"`
	MemoryUpdate                  string `json:"memory_update"`
	HistoryCompression            string `json:"history_compression,omitempty"`
	ImportMetaAnalysis            string `json:"import_meta_analysis"`
	ImportChapterAnalysis         string `json:"import_chapter_analysis"`
}

// DefaultContextBudgetTokens is the fallback context budget when the model's
// real context window cannot be fetched.
const DefaultContextBudgetTokens = 300000

// DefaultMaxTokens is the max_tokens written into a freshly created api.json.
const DefaultMaxTokens = 32768

// DefaultHTTPTimeoutSeconds is the HTTP client timeout for API calls.
const DefaultHTTPTimeoutSeconds = 600

// ProjectFormatVersion is the only on-disk project layout this binary writes.
const ProjectFormatVersion = 4

func DefaultAPIConfig() *APIConfig {
	return &APIConfig{
		MaxTokens:           DefaultMaxTokens,
		HTTPTimeoutSeconds:  DefaultHTTPTimeoutSeconds,
		ContextBudgetTokens: DefaultContextBudgetTokens,
	}
}

func DefaultConfig() *Config {
	return DefaultConfigForLang(i18n.LangZH)
}

func DefaultConfigForLang(lang string) *Config {
	lang = i18n.NormalizeLanguage(lang)
	cfg := &Config{
		ProjectFormatVersion: ProjectFormatVersion,
		Language:             lang,
		Story: StoryConfig{
			TargetWordsPerChapter: 5000,
		},
		SkillConfig: &SkillConfig{
			EnabledSkills: make(map[string]bool),
		},
	}
	cfg.Prompts.ApplyDefaults(lang)
	return cfg
}

func LoadAPIConfig(path string) (*APIConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := DefaultAPIConfig()
			if saveErr := saveAPIConfig(path, cfg); saveErr != nil {
				return nil, fmt.Errorf("创建默认API配置文件失败: %w", saveErr)
			}
			return cfg, nil
		}
		return nil, fmt.Errorf("读取API配置文件失败: %w", err)
	}

	var cfg APIConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析API配置文件失败: %w", err)
	}

	if cfg.HTTPTimeoutSeconds <= 0 {
		cfg.HTTPTimeoutSeconds = DefaultHTTPTimeoutSeconds
	}
	// ContextBudgetTokens <= 0 is filled in by llm.EnsureContextBudget at
	// startup (needs an API round-trip, so it lives outside this package).

	return &cfg, nil
}

func saveAPIConfig(path string, cfg *APIConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(path, data)
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := DefaultConfig()
			if saveErr := SaveConfig(path, cfg); saveErr != nil {
				return nil, fmt.Errorf("创建默认配置文件失败: %w", saveErr)
			}
			return cfg, nil
		}
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	// Legacy migration: the story field once called "brief" (easily confused
	// with chapter/outline synopses in AI prompts) is now "story_idea". Copy
	// the old key over when the new one is empty so existing projects keep
	// their data; the next SaveConfig persists it under the new key.
	if strings.TrimSpace(cfg.Story.StoryIdea) == "" {
		var legacy struct {
			Story struct {
				Brief string `json:"brief"`
			} `json:"story"`
		}
		if json.Unmarshal(data, &legacy) == nil && strings.TrimSpace(legacy.Story.Brief) != "" {
			cfg.Story.StoryIdea = legacy.Story.Brief
		}
	}

	if cfg.Story.TargetWordsPerChapter <= 0 {
		// Prefer the preset that matches the chosen story length (light novel,
		// flash fiction...); fall back to the generic 5000 when no length is set.
		if preset := DefaultTargetWordsForLength(cfg.Story.StoryLength); preset > 0 {
			cfg.Story.TargetWordsPerChapter = preset
		} else {
			cfg.Story.TargetWordsPerChapter = 5000
		}
	}

	// Round 10 — Removed the round-9 "MissingSuggestedSettings" auto-restore
	// guard: it re-injected every suggested preset token into
	// Story.SpecificSettings after any save that happened to leave the field
	// empty, which made the user's own edits look like they were "wiped" (and
	// made unchecking a suggestion impossible). The frontend now merges the
	// textarea remainder with the checkbox state before every PUT, so the
	// stored value is exactly what the author sees in the form.

	cfg.Language = i18n.NormalizeLanguage(cfg.Language)

	// 保存 applyDefaults 前的 prompts 状态，用于判断是否有字段被填充
	oldPrompts := cfg.Prompts
	cfg.Prompts.ApplyDefaults(cfg.Language)
	// 如果有字段被填充（从空变为默认值），写回磁盘
	if cfg.Prompts != oldPrompts {
		SaveConfig(path, &cfg)
	}

	if cfg.SkillConfig == nil {
		cfg.SkillConfig = &SkillConfig{
			EnabledSkills: make(map[string]bool),
		}
	} else {
		cfg.SkillConfig.ApplyDefaults()
	}

	return &cfg, nil
}

type SkillConfig struct {
	EnabledSkills map[string]bool `json:"enabled_skills"`
}

func (sc *SkillConfig) ApplyDefaults() {
	if sc.EnabledSkills == nil {
		sc.EnabledSkills = make(map[string]bool)
	}
}

func SaveConfig(path string, cfg *Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(path, data)
}

// ApplyDefaults fills empty fields with language-specific defaults without
// overwriting customized prompts.
func (p *PromptsConfig) ApplyDefaults(lang string) {
	defaults := DefaultPromptsForLang(lang)
	if p.ChapterWriting == "" {
		p.ChapterWriting = defaults.ChapterWriting
	}
	if p.ChapterRevision == "" {
		p.ChapterRevision = defaults.ChapterRevision
	}
	if p.ChapterSegmentRevision == "" {
		p.ChapterSegmentRevision = defaults.ChapterSegmentRevision
	}
	if p.ChapterSummary == "" {
		p.ChapterSummary = defaults.ChapterSummary
	}
	if p.FactCheck == "" {
		p.FactCheck = defaults.FactCheck
	}
	if p.OutlineRevision == "" {
		p.OutlineRevision = defaults.OutlineRevision
	}
	if p.ForeshadowPlanning == "" {
		p.ForeshadowPlanning = defaults.ForeshadowPlanning
	}
	if p.ForeshadowUpdate == "" {
		p.ForeshadowUpdate = defaults.ForeshadowUpdate
	}
	if p.ContinuationOutlineGeneration == "" {
		p.ContinuationOutlineGeneration = defaults.ContinuationOutlineGeneration
	}
	if p.SettingsReconciliation == "" {
		p.SettingsReconciliation = defaults.SettingsReconciliation
	}
	if p.TransitionSmoothing == "" {
		p.TransitionSmoothing = defaults.TransitionSmoothing
	}
	if p.OutlineConsistencyCheck == "" {
		p.OutlineConsistencyCheck = defaults.OutlineConsistencyCheck
	}
	if p.ForeshadowOutlineConsistency == "" {
		p.ForeshadowOutlineConsistency = defaults.ForeshadowOutlineConsistency
	}
	if p.OutlineCharacterCheck == "" {
		p.OutlineCharacterCheck = defaults.OutlineCharacterCheck
	}
	if p.WritingConflictAnalysis == "" {
		p.WritingConflictAnalysis = defaults.WritingConflictAnalysis
	}
	if p.BookDiagnosis == "" {
		p.BookDiagnosis = defaults.BookDiagnosis
	}
	if p.BookConsistencyCheck == "" {
		p.BookConsistencyCheck = defaults.BookConsistencyCheck
	}
	if p.BookRoadmap == "" {
		p.BookRoadmap = defaults.BookRoadmap
	}
	if p.MemoryUpdate == "" {
		p.MemoryUpdate = defaults.MemoryUpdate
	}
	if p.HistoryCompression == "" {
		p.HistoryCompression = defaults.HistoryCompression
	}
	if p.ImportMetaAnalysis == "" {
		p.ImportMetaAnalysis = defaults.ImportMetaAnalysis
	}
	if p.ImportChapterAnalysis == "" {
		p.ImportChapterAnalysis = defaults.ImportChapterAnalysis
	}
}

func DefaultPromptsForLang(lang string) PromptsConfig {
	if i18n.NormalizeLanguage(lang) == i18n.LangEN {
		return DefaultPromptsEN
	}
	return DefaultPromptsZH
}

// —— Novel parameters: genre presets (borrowed from NovelWriter genre_configs) —
// Each list always ends with "other", so suggestions help generation without forcing it.

var GenreConflictScales = map[string][]string{
	// NOTE (round 7): the trailing snake_case tokens that used to leak into
	// these conflict lists ("urban_setting", "wuxia", "cyberpunk", "noir"...)
	// were removed — they are specific-settings modifiers and belong in
	// GenreSpecificSettings below, where the UI renders them as checkable
	// suggestions instead of polluting the conflict-scale dropdown.
	"fantasy":     {"Personal Quest", "Kingdom-wide", "World-saving", "Good vs Evil", "Political Intrigue", "Sect/Realm Wars", "other"},
	"scifi":       {"Personal", "Planetary", "Interstellar", "Galactic", "Humanity vs Technology", "other"},
	"mystery":     {"Personal Mystery", "Community Secret", "Local Crime", "Family Mystery", "Historical Puzzle", "other"},
	"romance":     {"Personal Growth", "Relationship Obstacles", "Career vs Love", "Family Issues", "Past Trauma", "other"},
	"thriller":    {"International Conspiracy", "Government Secrets", "Spy Networks", "National Security", "Global Politics", "other"},
	"horror":      {"Personal Haunting", "Family Curse", "Supernatural Threat", "Psychological Terror", "Ancient Evil", "Cosmic Indifference", "other"},
	"historical":  {"Tribal Warfare", "Religious Conflicts", "Ancient Politics", "Survival Struggles", "Civilization Building", "other"},
	"western":     {"Personal Vendetta", "Town Protection", "Range War", "Law vs Lawlessness", "Civilization vs Wilderness", "other"},
	"litrpg":      {"System Rules vs Free Will", "Solo vs Guild", "Dungeon Economy", "Server-wide Meta War", "Tutorial to Raid Boss", "other"},
	"urban":       {"Neighborhood Turf", "Hidden Society Exposure", "Patron Debt", "City-wide Supernatural War", "Ordinary Life vs Calling", "other"},
	"military":    {"Unit Survival", "Campaign Objective", "Chain-of-Command Conflict", "Insurgency & Occupation", "War vs Conscience", "other"},
	"postapo":     {"Daily Survival", "Settlement vs Raiders", "Resource Scarcity", "Old-world Remnant Threat", "Rebuilding vs Warlordism", "other"},
	"adventure":   {"Man vs Nature", "Expedition Rivalry", "Ancient Trap/Trial", "Race Against Time", "Survival in the Unknown", "other", "swashbuckler", "nautical", "picaresque", "mythical_creatures", "expedition_epic_scale"},
	"wuxia":       {"Jianghu Feud", "Sect Honor", "Martial Supremacy", "Court vs Jianghu", "Righteous vs Unorthodox Paths", "other"},
	"xianxia":     {"Sect Competition", "Heavenly Tribulation", "Immortal Realm Power Struggle", "Fate vs Defiance", "Dao vs Demon Path", "other"},
	"cozy":        {"Small-town Gossip", "Community Secret", "Family Mystery", "Holiday Deadline", "Reputation at Stake", "other"},
	"heist":       {"The Score vs The Crew", "Double-cross", "One Last Job", "Institutional Corruption", "Clock-running Escape", "other"},
	"cyberpunk":   {"Corp vs Street", "Identity vs Augmentation", "Data Heist", "Class Uprising", "AI Emancipation", "other"},
	"solarpunk":   {"Community Resilience", "Ecological Balance", "Legacy of Collapse", "Cooperative Governance", "Rewilding vs Industry", "other"},
	"romantasy":   {"Political Alliance vs Heart", "Prophecy Bond", "Enemy-to-Lover Duty", "Magical Court Intrigue", "Doom vs Devotion", "other"},
	"dystopian":   {"Individual vs The Regime", "Resistance Cell", "Scarcity Ethics", "Memory vs Propaganda", "Escape or Reform", "other"},
	"space_opera": {"Galactic Empire Succession", "Federation vs Separatists", "Xeno Diplomacy", "Pirate Confederacy", "Ancient Precursor Threat", "other"},
	// new parent genres (round 3)
	"graphic_novel":             {"Personal vs Visual Legacy", "Community Story", "Inner Conflict Made Visible", "other"},
	"adventure:swashbuckler":    {"Crew Loyalty vs Captain's Orders", "Revenge on the High Seas", "Empire vs Free Companies", "Treasure That Tears a Crew Apart", "other"},
	"adventure:nautical":        {"Man vs Sea & Ship Mechanics", "Chain of Command at Sea", "Storm or Wreck Survival", "Mutiny Brewing Below Decks", "other"},
	"adventure:picaresque":      {"Con-Artist vs Mark of the Week", "Patron Demands vs Freedom", "Society Rules vs Rogue Ethics", "Running One Step Ahead of the Law", "other"},
	"horror:gothic":             {"House vs Family Memory", "Forbidden Kinship", "Reason vs Encroaching Madness", "Old Debt Collecting in Blood", "other"},
	"horror:dark_academia":      {"Institution vs Student", "Secret Society Initiation Cost", "Intellectual Rivalry Turned Dangerous", "Past Crime Surfacing in the Present", "other"},
	"scifi:steampunk":           {"Guild vs Inventor Monopoly", "Empire Machinery vs Individual", "Class Divide Above/Below the Deck", "Occult Industry Accident", "other"},
	"scifi:time_travel":         {"Butterfly Paradox Fallout", "Race to Repair a Fixed Point", "Love Across Eras", "History Police vs Personal Grief", "other"},
	"scifi:cli_fi":              {"Drought Resource Wars", "Water Rights Court Battle", "Community vs Utility Corporation", "Migration Under Heat", "other"},
	"scifi:afrofuturism":        {"Ancestral Tech vs Colonial Remnant", "Diaspora Homecoming Claim", "Spirit Network Uprising", "Utopia Defending Itself", "other"},
	"scifi:hard":                {"Physics Wins vs Human Hope", "Generation-Ship Politics", "Terraform Ethics", "First Contact Protocol Failure", "other"},
	"scifi:superhero":           {"Responsibility vs Personal Life", "Hero vs Arch-nemesis Cycle", "Public Trust After Collateral Damage", "Power Corruption Temptation", "Legacy Hero vs New Generation", "other"},
	"mystery:police_procedural": {"Precinct Chain-of-Casework", "Serial Pattern vs Bureau Politics", "Victim Community vs Procedure", "Cold Case Reopened", "other"},
	"literary":                  {"Self vs Meaning", "Memory vs Truth", "Duty vs Desire", "Identity Unraveling", "Silence Inside a Family", "other"},
	"romance:romcom":            {"Pride vs Attraction", "Fake Relationship Turns Real", "Rival-to-Lover Escalation", "Timing & Miscommunication Farce", "Career Crossroads vs Love", "other"},
	// round 6: Drama parent genre (family saga, coming-of-age, psychorealism...)
	"drama": {"Silence Inside a Family", "Self vs Meaning", "Duty vs Desire", "Identity Unraveling", "Institution vs Individual", "Generational Legacy", "other"},
}

var GenreProtagonistTypes = map[string][]string{
	"fantasy":     {"Chosen One", "Magic User", "Knight/Warrior", "Royal Heir", "Common Hero", "Prophesied One", "other"},
	"scifi":       {"Military Officer", "Merchant Captain", "Explorer", "Diplomat", "Rebel Leader", "Imperial Noble", "other"},
	"mystery":     {"Amateur Detective", "Librarian", "Shop Owner", "Retired Professional", "Local Resident", "Hobby Enthusiast", "other"},
	"romance":     {"Career Professional", "Single Parent", "Artist/Creative", "Business Owner", "Healthcare Worker", "Teacher/Academic", "other"},
	"thriller":    {"Secret Agent", "Intelligence Officer", "Double Agent", "Spy Handler", "Undercover Operative", "Government Analyst", "other"},
	"horror":      {"Haunted Individual", "Investigator", "Innocent Victim", "Cursed Person", "Gothic Hero", "Tormented Soul", "other"},
	"historical":  {"Ancient Warrior", "Priest/Priestess", "Tribal Leader", "Ancient Scholar", "Slave", "Ancient Ruler", "other"},
	"western":     {"Sheriff/Marshal", "Gunslinger", "Rancher", "Outlaw", "Bounty Hunter", "Frontier Doctor", "other"},
	"litrpg":      {"Regressor", "Hardworking Solo Player", "Game Designer Trapped Inside", "Support Class Strategist", "Speedrunner", "NPC Who Wakes Up", "other"},
	"urban":       {"Blue-collar with a secret", "Barista/Baker Owner", "Paramedic", "Building Manager", "Journalist", "Fixer for Hidden Folk", "other"},
	"military":    {"Junior Officer", "Sergeant NCO", "Sniper/Scout", "Medic", "Mechanic/Pilot", "Conscript Turned Veteran", "other"},
	"postapo":     {"Scavenger", "Settlement Medic", "Ex-soldier", "Wanderer", "Engineer Hoarding Tech", "Young-born (never saw the old world)", "other"},
	"adventure":   {"Archaeologist", "Cartographer", "Mountain Guide", "Shipwreck Survivor", "Treasure Hunter with Debts", "Documentary Filmmaker", "other"},
	"wuxia":       {"Disciple of a Fallen Sect", "Wandering Swordsman", "Sect Heir", "Beggar-Gang Prodigy", "Assassin Seeking Redemption", "Court Constable in Jianghu", "other"},
	"xianxia":     {"Outer-sect Underdog", "Reincarnated Ancestor", "Alchemist Apprentice", "Demon-cultivator Defying Fate", "Clan Young Master", "Body Cultivator with Broken Meridians", "other"},
	"cozy":        {"Retired Teacher", "Bookshop Owner", "Village Baker", "Gardener", "Cat-loving Librarian", "Newcomer in a Small Town", "other"},
	"heist":       {"Retired Mastermind", "Grifter Charming", "Tech Forger", "Getaway Driver", "Inside Man", "Crew Assembler", "other"},
	"cyberpunk":   {"Street Samurai", "Ripperdoc", "Netrunner", "Corp Defector", "Fixer", "Data Courier", "other"},
	"solarpunk":   {"Community Engineer", "Rewilder", "Cooperative Organizer", "Seed Librarian", "Rooftop Farmer", "Restoration Architect", "other"},
	"romantasy":   {"Mortal Envoy to Fae Courts", "Hex-Broken Heiress", "God-Touched General", "Witch Bound by Oath", "Fae Prince in Exile", "Scholar of Forbidden Magic", "other"},
	"dystopian":   {"Reluctant Messenger", "Compliance Officer Doubting Orders", "Archive Keeper", "District Laborer", "Elite Student Tasting Truth", "Smuggler of Banned Books", "other"},
	"space_opera": {"Starship Captain", "Disgraced Admiral", "Xenolinguist", "Freighter Pilot", "Heir to a Merchant Dynasty", "Marine on a Doomed Colony Ship", "other"},
	// new parent genres (round 3)
	"graphic_novel":             {"Silent Observer", "Visual Thinker Misread by Others", "Archivist of Family Pictures", "Street Kid Sketching the City", "other"},
	"adventure:swashbuckler":    {"Charming Privateer", "Boatswain Keeping Crew Honest", "Cartographer Turned Treasure Hunter", "Noble's Ward Ran Away to Sea", "Reformed Pirate Quartermaster", "other"},
	"adventure:nautical":        {"First Mate Against Their Convictions", "Stowaway With Useful Hands", "Lighthouse Keeper Drawn Into a Wreck", "Cabin Passenger Hiding a Past", "Deckhand Learning the Rope Code", "other"},
	"adventure:picaresque":      {"Likeable Con Artist One Job From Free", "Servant Outsmarting Every Master", "Wandering Actor Collecting Stories", "Orphan Running Scams to Survive", "Retired Rogue Training an Heir", "other"},
	"horror:gothic":             {"Inheritor of the Cursed Estate", "Live-in Governess or Tutor", "Skeptic Doctor Facing the Impossible", "Last Scion Returning for a Funeral", "Architect Restoring the Ruined House", "other"},
	"horror:dark_academia":      {"Ambitious Scholarship Student", "Tenured Professor With Something Buried", "Archivist or Rare-Books Curator", "Initiate Who Questions the Society", "Roommate Nobody Believes", "other"},
	"scifi:steampunk":           {"Clockwork Apprentice", "Airship Engineer", "Investigative Journalist with Gadgetry", "Union Organizer in the Mills", "Otherworldly Medium Hired by Industry", "other"},
	"scifi:time_travel":         {"Accidental Tourist Through Eras", "Temporal Repair Technician", "Griever Hunting One Fixed Moment", "Historian Living Inside Sources", "Smuggler of Small Knowledge", "other"},
	"scifi:cli_fi":              {"Water Rights Lawyer", "Drought Farm Heir", "Reservoir Engineer", "Heatwave Paramedic", "Migration Community Organizer", "other"},
	"scifi:afrofuturism":        {"Ancestral Tech Inheritor", "Diaspora Returnee Architect", "Spirit-Network Hacker", "Utopian Guard with Doubts", "Griot Preserving Encoded History", "other"},
	"scifi:hard":                {"Systems Engineer on Thin Margins", "Generation-Ship Mediator", "Terraform Ecologist", "Physicist Who Knows Too Much", "Salvage Crew Chief", "other"},
	"scifi:superhero":           {"Late-Debut Hero", "Sidekick Ready to Lead", "Villain Trying to Retire Clean", "Hero Without Public Trust", "Legacy Name Carrying Someone New", "other"},
	"mystery:police_procedural": {"By-the-Book Detective", "Burned-Out Veteran Mentor", "Forensics Specialist Who Sees Patterns", "Patrol Officer Promoted Too Fast", "Profiler With Boundary Issues", "other"},
	"literary":                  {"Unreliable Witness of Their Own Life", "Returnee Facing a Changed Home", "Quiet Professional Near a Breaking Point", "Questioner of Inherited Beliefs", "Caregiver Losing and Finding Themselves", "other"},
	"romance:romcom":            {"Cynical Romantic Under Cover of Jokes", "Overprepared Planner Meets Chaos", "Rival Who Secretly Reads Their Work", "Best Friend Waiting Too Long", "Fake Partner With Real Cracks", "other"},
	// round 6: Drama parent genre
	"drama": {"Matriarch or Patriarch Holding a Fractured Family", "Coming-of-age Everyteen on a Threshold", "Ordinary Person Facing an Unremarkable Crisis", "Idealist Ground Down by an Institution", "Griever Rebuilding a Life After Loss", "other"},
}

var GenreSpecificSettings = map[string][]string{
	// Round 7: every parent genre now exposes the cross-cutting modifiers the
	// author asked for (wuxia/xianxia/litrpg/cyberpunk/solarpunk/space_opera/
	// romantasy/dystopian/postapo/military/heist/cozy/urban/noir/gothic/
	// dark_academia/swashbuckler/nautical/picaresque/romcom/mystery_police/
	// magic_system/medieval_setting/mythical_creatures/epic_scale/scifi tech)
	// whenever they are thematically compatible with that genre.
	"fantasy":     {"magic_system", "medieval_setting", "mythical_creatures", "epic_scale", "political_dynasties", "wuxia", "xianxia", "cozy", "urban", "gothic", "dark_academia", "romantasy", "swashbuckler"},
	"scifi":       {"interstellar_travel", "advanced_technology", "multiple_species", "space_combat", "ai_singularity", "cyberpunk", "solarpunk", "steampunk", "space_opera", "dystopian", "postapo", "military", "superhero", "first_contact_paradigm", "generation_ship_society"},
	"mystery":     {"small_community", "amateur_detective", "low_violence", "puzzle_focus", "recurring_characters", "noir", "heist", "cozy", "urban", "locked_room_impossible_crime", "mystery_police", "gothic"},
	"romance":     {"modern_setting", "relationship_focus", "emotional_journey", "happy_ending", "realistic_world", "romcom", "romantasy", "small_town_or_urban_backdrop", "cozy", "urban", "historical_courtship", "dark_academia"},
	"thriller":    {"international_intrigue", "spy_networks", "government_secrets", "double_agents", "global_stakes", "heist", "noir", "military_intelligence", "urban", "mystery_police", "cyberpunk"},
	"horror":      {"atmospheric_dread", "isolated_setting", "supernatural_elements", "psychological_terror", "dark_atmosphere", "cosmic_horror", "gothic", "dark_academia", "folk_customs_and_rites", "postapo", "urban"},
	"historical":  {"ancient_civilizations", "mythological_elements", "tribal_societies", "ancient_religions", "primitive_technology", "medieval_setting", "mythical_creatures", "epic_scale", "court_intrigue", "nautical", "swashbuckler", "military"},
	"western":     {"frontier_setting", "lawlessness", "honor_code", "survival_focus", "horse_culture", "swashbuckler_frontier_action", "nautical_ports_and_trails", "picaresque", "noir"},
	"litrpg":      {"game_system_rules", "stats_and_levels", "dungeons_and_loot", "guild_politics", "progression_arc", "server_economy", "isekai", "system_apocalypse", "tower_climbing"},
	"urban":       {"hidden_supernatural_society", "modern_city_backdrop", "masquerade_rule", "part_time_hero_life", "night_market_magic", "noir", "cyberpunk", "romcom", "cozy"},
	"military":    {"unit_esprit_de_corps", "chain_of_command", "combined_arms_tactics", "rules_of_engagement", "rotations_and_leave", "war_crimes_inquiry", "postapo", "space_opera", "espionage_intelligence"},
	"postapo":     {"resource_scarcity", "mutated_fauna_flora", "pre_fall_artifacts", "settlement_politics", "radiation_zones", "water_and_power_grids", "military", "solarpunk", "dystopian"},
	"adventure":   {"expedition_logistics", "ancient_traps_puzzles", "extreme_environments", "rival_explorers", "local_mythology_clues", "treasure_curse", "swashbuckler", "nautical", "picaresque", "mythical_creatures", "expedition_epic_scale"},
	"wuxia":       {"jianghu_codes", "qi_cultivation", "sects_and_alliances", "neigong_manuals", "tea_house_rumors", "wuxia_martial_choreography", "epic_scale", "mythical_creatures"},
	"xianxia":     {"cultivation_realms", "spirit_roots", "sect_hierarchy", "alchemy_pill_refining", "heavenly_dao_laws", "immortal_realm_geography", "epic_scale", "mythical_creatures"},
	"cozy":        {"small_town_map", "baking_brewing_gardening_hobbies", "community_events", "pet_or_cat_presence", "low_stakes_danger", "found_family", "urban", "seasonal_rhythm"},
	"heist":       {"crew_specialties", "target_security_layers", "mark_and_distraction", "getaway_routes", "one_job_too_many", "double_cross_timer", "noir", "urban", "cyberpunk"},
	"cyberpunk":   {"megacorporations", "body_augmentation", "netrunning_matrix", "street_economy_implants", "rain_neon_aesthetic", "class_divide_vertical_cities", "dystopian", "urban", "heist"},
	"solarpunk":   {"renewable_infrastructure", "community_cooperatives", "rewilded_urbanism", "open_source_tools", "slow_healing_after_collapse", "festivals_and_commons", "postapo", "cozy"},
	"romantasy":   {"fae_or_god courts", "magic_cost_of_intimacy", "arranged_political_marriage", "enemy_to_lover_arc", "prophecy_bond", "court_intrigue_and_seasons", "magic_system", "epic_scale", "romcom"},
	"dystopian":   {"surveillance_state", "scarce_rations_and_permits", "state_propaganda_media", "resistance_cells", "caste_by_gene_or_record", "forbidden_knowledge_archive", "postapo", "military", "cyberpunk"},
	"space_opera": {"jump_gate_network", "galactic_senate_or_empire", "xeno_first_contact_protocols", "fleet_logistics", "dynastic_politics_in_stars", "precursor_relics", "interstellar_travel", "advanced_technology", "military"},
	// new parent genres (round 3)
	"graphic_novel":             {"panel_rhythm_matters", "visual_motif_recurs", "silence_and_gutter_time", "color_script_arc", "caption_vs_dialogue_balance", "memoir_or_biography_texture"},
	"adventure:swashbuckler":    {"sea_code_and_mutiny_risk", "privateer_letters_marques", "crew_share_system", "port_town_underworld", "chase_and_escape_rhythm", "treasure_curse_hook"},
	"adventure:nautical":        {"ship_as_microcosm", "watch_schedule_structure", "weather_is_antagonist", "rope_and_engine_detail", "long_voyage_time_scale", "rescue_or_wreck_inciting"},
	"adventure:picaresque":      {"episodic_con_chain", "satire_of_institutions", "rogue_with_a_code", "disguise_and_alias_layering", "patron_dependency", "road_or_river_progression"},
	"horror:gothic":             {"decaying_estate_setting", "family_sin_and_debt", "ambiguous_supernatural", "isolation_and_weather", "forbidden_kinship", "sanity_slipping_marker"},
	"horror:dark_academia":      {"gothic_campus_atmosphere", "secret_society_ladder", "mentor_dependence", "forbidden_text_or_archive", "tuition_class_tension", "crime_disguised_as_tradition"},
	"scifi:steampunk":           {"brass_and_steam_tech_level", "guild_or_factory_economy", "class_divide_vertical_cities", "occult_industry_accident", "airship_logistics", "empire_exhibition_era"},
	"scifi:time_travel":         {"travel_rule_or_device", "paradox_cost_visible", "fixed_point_or_fate", "butterfly_side_effects", "era_authenticity_detail", "history_vs_personal_grief"},
	"scifi:cli_fi":              {"water_rights_market", "heatwave_infrastructure", "drought_migration_routes", "dam_or_aquifer_stakes", "climate_refugee_policy", "adaptation_technology_gap"},
	"scifi:afrofuturism":        {"ancestral_technique_lineage", "diaspora_homecoming_pull", "spirit_network_interface", "utopia_or_liberation_agenda", "colonial_remnant_structures", "griot_encoded_history"},
	"scifi:hard":                {"physics_constraint_visible", "generation_ship_demographics", "terraform_ecology_budget", "life_support_margin_error", "first_contact_protocol", "computational_resource_limits"},
	"scifi:superhero":           {"power_cost_or_limitation", "secret_identity_pressure", "collateral_damage_debt", "rogues_gallery_recurring", "public_opinion_metronome", "legacy_symbol_transfer"},
	"mystery:police_procedural": {"chain_of_custody_detail", "partner_dynamic_carries_case", "department_politics_friction", "procedural_clock_pressure", "victim_not_plot_device"},
	"literary":                  {"interiority_over_plot", "motif_and_symbol_layering", "moral_ambiguity_no_cartoon_villains", "time_nonlinear_or_elided", "language_as_texture", "open_or_bittersweet_endings_ok"},
	"romance:romcom":            {"meet_cute_variants", "banter_engine_two_compatible_people", "escalating_set_pieces", "third_act_misunderstanding_kept_short", "family_and_friend_chorus", "guaranteed_happy_ending"},
	// round 6: Drama parent genre
	"drama": {"interiority_over_plot", "multi_generational_timeline", "quiet_realism", "moral_ambiguity_no_cartoon_villains", "time_nonlinear_or_elided", "open_or_bittersweet_endings_ok", "found_family_or_bloodline", "institution_as_stage", "letters_or_messages_format", "everyday_life_texture"},
}

// —— Cross-cutting modifier presets (round 7) ————————————————————————————————
// These are the subgenre-style modifiers authors most often want to stack on
// top of a base genre/subgenre (isekai, wuxia, cyberpunk, cozy, noir...). The
// /api/novel-params endpoint exposes them so the "Specific settings" section
// can offer them as checkable suggestions no matter which genre is selected —
// previously they only existed inside the anime preset map and were invisible
// unless Type+Subgenre text happened to match an anime keyword. Keys mirror
// config.MatchGenreKey / MatchAnimeModifierKey where those exist; values are
// fresh snake_case tokens safe to store in Story.SpecificSettings.
var ModifierPresets = map[string][]string{
	"litRPG":             {"game_system_rules", "stats_and_levels", "dungeons_and_loot", "guild_politics", "progression_arc", "server_economy"},
	"wuxia":              {"jianghu_codes", "qi_cultivation", "sects_and_alliances", "neigong_manuals", "tea_house_rumors", "wuxia_martial_choreography"},
	"xianxia":            {"cultivation_realms", "spirit_roots", "sect_hierarchy", "alchemy_pill_refining", "heavenly_dao_laws", "immortal_realm_geography"},
	"cyberpunk":          {"megacorporations", "body_augmentation", "netrunning_matrix", "street_economy_implants", "rain_neon_aesthetic", "class_divide_vertical_cities"},
	"solarpunk":          {"renewable_infrastructure", "community_cooperatives", "rewilded_urbanism", "open_source_tools", "slow_healing_after_collapse", "festivals_and_commons"},
	"space_opera":        {"jump_gate_network", "galactic_senate_or_empire", "xeno_first_contact_protocols", "fleet_logistics", "dynastic_politics_in_stars", "precursor_relics"},
	"romantasy":          {"fae_or_god courts", "magic_cost_of_intimacy", "arranged_political_marriage", "enemy_to_lover_arc", "prophecy_bond", "court_intrigue_and_seasons"},
	"dystopian":          {"surveillance_state", "scarce_rations_and_permits", "state_propaganda_media", "resistance_cells", "caste_by_gene_or_record", "forbidden_knowledge_archive"},
	"postapo":            {"resource_scarcity", "mutated_fauna_flora", "pre_fall_artifacts", "settlement_politics", "radiation_zones", "water_and_power_grids"},
	"military":           {"unit_esprit_de_corps", "chain_of_command", "combined_arms_tactics", "rules_of_engagement", "rotations_and_leave", "war_crimes_inquiry"},
	"heist":              {"crew_specialties", "target_security_layers", "mark_and_distraction", "getaway_routes", "one_job_too_many", "double_cross_timer"},
	"cozy":               {"small_town_map", "baking_brewing_gardening_hobbies", "community_events", "pet_or_cat_presence", "low_stakes_danger", "found_family"},
	"urban":              {"hidden_supernatural_society", "modern_city_backdrop", "masquerade_rule", "part_time_hero_life", "night_market_magic"},
	"noir":               {"noir_atmosphere", "femme_fatale_energy", "doomed_protagonist", "rain_slick_city", "corrupt_institutions", "moral_ambiguity"},
	"gothic":             {"decaying_estate_setting", "family_sin_and_debt", "ambiguous_supernatural", "isolation_and_weather", "forbidden_kinship", "sanity_slipping_marker"},
	"dark_academia":      {"gothic_campus_atmosphere", "secret_society_ladder", "mentor_dependence", "forbidden_text_or_archive", "tuition_class_tension", "crime_disguised_as_tradition"},
	"swashbuckler":       {"sea_code_and_mutiny_risk", "privateer_letters_marques", "crew_share_system", "port_town_underworld", "chase_and_escape_rhythm", "treasure_curse_hook"},
	"nautical":           {"ship_as_microcosm", "watch_schedule_structure", "weather_is_antagonist", "rope_and_engine_detail", "long_voyage_time_scale", "rescue_or_wreck_inciting"},
	"picaresque":         {"episodic_con_chain", "satire_of_institutions", "rogue_with_a_code", "disguise_and_alias_layering", "patron_dependency", "road_or_river_progression"},
	"romcom":             {"meet_cute_variants", "banter_engine_two_compatible_people", "escalating_set_pieces", "third_act_misunderstanding_kept_short", "family_and_friend_chorus", "guaranteed_happy_ending"},
	"mystery_police":     {"chain_of_custody_detail", "partner_dynamic_carries_case", "department_politics_friction", "procedural_clock_pressure", "victim_not_plot_device"},
	"magic_system":       {"hard_vs_soft_magic", "magic_cost_and_limits", "spell_schools_or_disciplines", "magic_authority_or_control", "rare_vs_common_magic", "ritual_components"},
	"medieval_setting":   {"feudal_system", "guilds_and_guildpolitics", "castle_and_manor_life", "religious_influence", "period_authenticity", "travel_and_communication_speed"},
	"mythical_creatures": {"dragon_ecology_and_politics", "fae_etiquette_and_iron", "beast_bonds_or_familiars", "monster_hunter_economy", "extinct_or_hidden_species", "creation_myths_as_fact"},
	"epic_scale":         {"world_spanning_journey", "multiple_poVs", "generative_time_span", "factions_and_warfare", "languages_and_maps", "personal_thread_in_history"},
	"scifi":              {"interstellar_travel", "advanced_technology", "multiple_species", "space_combat", "ai_singularity", "generation_ship_society"},
	"steampunk":          {"brass_and_steam_tech_level", "guild_or_factory_economy", "class_divide_vertical_cities", "occult_industry_accident", "airship_logistics", "empire_exhibition_era"},
	"time_travel":        {"travel_rule_or_device", "paradox_cost_visible", "fixed_point_or_fate", "butterfly_side_effects", "era_authenticity_detail", "history_vs_personal_grief"},
	"superhero":          {"power_cost_or_limitation", "secret_identity_pressure", "collateral_damage_debt", "rogues_gallery_recurring", "public_opinion_metronome", "legacy_symbol_transfer"},
	"magical_realism":    {"mundane_world_with_wonder", "matter_of_fact_marvels", "cultural_mythos", "poetic_prose", "generational_memory", "political_history_undercurrent"},
	"first_contact":      {"xeno_language_barrier", "protocol_and_politics", "cultural_misreading", "technology_gap_reveal", "signal_or_arrival_event", "coexistence_terms"},
	"isekai":             {"summoning_contract_rules", "cheat_ability_with_cost", "new_world_language_customs_gap", "return_home_option_live", "local_politics_knowledge_gap", "reincarnation_or_transport_logic"},
	"regression":         {"fixed_past_events_catalogue", "butterfly_effect_budget", "aging_body_young_face_tension", "insider_trading_knowledge_edge", "second_chance_relationships", "known_disaster_deadline"},
	"manhwa_system":      {"status_window_rules", "penalty_quest_enforcement", "hunter_rank_public_registry", "hidden_skill_market", "gate_and_dungeon_ecology", "ranked_society_media"},
	"villainess":         {"otome_game_script_known_events", "duelist_and_ballroom_etiquette", "curse_or_engagement_deadline", "servant_network_intelligence", "scripted_doom_counterplan", "court_gossip_economy"},
	"battle_royale":      {"shrinking_map_timer", "supply_drop_schedule", "audience_vote_power", "loadout_balance_rules", "forced_alliance_politics", "arena_ethics_debate"},
	"tower_climbing":     {"floor_theme_rotation", "gatekeeper_boss_contract", "checkpoint_resurrection_rules", "vertical_city_politics", "climber_guild_cartel", "summit_myth_economy"},
	"dungeon_core":       {"core_room_layout", "monster_roster", "floor_progression", "dungeon_ecology_tables", "party_meta_and_loot", "safe_room_checkpoint_rules"},
	"slice_of_life":      {"low_stakes_no_world_threat", "food_weather_and_routine_texture", "found_family_slow_burn", "episodic_season_structure", "seasonal_rhythm", "small_workplace_cast"},
	"iyashikei":          {"gentle_narrative_no_villain", "nature_hot_spring_food_motifs", "small_regulars_cast_roster", "seasonal_ritual_structure", "healing_arc_without_trauma_porn", "quiet_community_warmth"},
	"school_life":        {"term_calendar_drives_plot", "uniform_and_seating_codes", "cultural_festival_arc_beats", "teacher_administration_friction", "club_and_committee_politics", "exam_hierarchy_pressure"},
	"spokon":             {"training_montage_discipline", "match_by_match_bracket_structure", "sport_rule_authenticity", "physical_limits_plot_driver", "team_egos_and_roles", "career_window_pressure"},
	"mecha":              {"mech_sync_cost_to_pilot", "production_line_logistics", "mobile_suit_vs_frame_doctrine", "war_broadcast_propaganda", "pilot_chain_of_command", "colony_vs_earth_politics"},
	"harem":              {"cast_archetype_spread", "flag_reading_comedy", "jealousy_without_villainy", "relationship_negotiation_rules", "oblivious_protagonist_engine", "shared_goal_bonds"},
	"ecchi":              {"comic_timing_over_logic", "boundaries_consent_humor_rules", "serious_core_underneath_gags", "running_gag_inventory", "misreading_signal_comedy", "fan_service_with_limits"},
	"revenge":            {"evidence_chain_over_years", "identity_change_after_fall", "moral_line_defined_early", "allies_gathered_one_debt_at_a_time", "institutional_wall", "mirror_of_self_target"},
}

// MatchGenreKey maps a free-form story Type (any language) to a preset genre key, or "" if none matches.
// More specific keys are checked first so e.g. "space opera" wins over generic scifi and "urban fantasy"
// still resolves to fantasy (checked in list order below).
func MatchGenreKey(storyType string) string {
	t := strings.ToLower(strings.TrimSpace(storyType))
	if t == "" {
		return ""
	}
	type kw struct {
		key   string
		words []string
	}
	kws := []kw{
		// specific subgenre-flavored keys first
		{"xianxia", []string{"xianxia", "仙侠", "修仙"}},
		{"wuxia", []string{"wuxia", "武侠", "江湖"}},
		{"litrpg", []string{"litrpg", "lit rpg", "game novel", "system flow", "游戏文", "系统流", "无限流"}},
		{"cyberpunk", []string{"cyberpunk", "cyber-punk", "赛博朋克", "赛博"}},
		{"solarpunk", []string{"solarpunk", "solar punk", "太阳朋克"}},
		{"space_opera", []string{"space opera", "星际歌剧", "太空歌剧"}},
		{"romantasy", []string{"romantasy", "romantic fantasy", "epic romance", "恋与", "浪漫奇幻"}},
		{"dystopian", []string{"dystopia", "dystopian", "反乌托邦", "乌托邦崩塌"}},
		{"postapo", []string{"post-apocalyp", "post apocalyp", "postapoc", "zombie", "末日", "废土", "丧尸", "生存游戏"}},
		{"military", []string{"military", "war novel", "军旅", "战争", "军事"}},
		{"heist", []string{"heist", "caper", "robbery", "盗梦", "劫案", "千门"}},
		{"cozy", []string{"cozy", "comfort read", "治愈系", "温馨推理", "田园"}},
		{"urban", []string{"urban", "contemporary fantasy", "city", "都市", "现代异能"}},
		// round 4: demoted subgenres -> composite "parent:subgenre" keys (matched by Type+Subgenre)
		{"scifi:steampunk", []string{"steampunk", "steam punk", "蒸汽朋克"}},
		{"scifi:superhero", []string{"superhero", "super hero", "supervillain", "超级英雄", "超人"}},
		{"scifi:time_travel", []string{"time travel", "time-travel", "穿越时空", "时间旅行"}},
		{"scifi:cli_fi", []string{"cli-fi", "clifi", "climate fiction", "气候小说"}},
		{"scifi:afrofuturism", []string{"afrofuturism", "afro-futurism", "非洲未来主义"}},
		{"scifi:hard", []string{"hard scifi", "hard sci-fi", "hard science fiction", "硬科幻"}},
		{"horror:dark_academia", []string{"dark academia", "暗黑学院", "校园秘密社团"}},
		{"horror:gothic", []string{"gothic", "哥特"}},
		{"adventure:swashbuckler", []string{"swashbuckl", "pirate", "corsair", "海盗", "大航海"}},
		{"adventure:nautical", []string{"nautical", "seafaring", "voyage at sea", "航海", "海难"}},
		{"adventure:picaresque", []string{"picaresque", "rogue tale", "流浪汉", "骗子冒险"}},
		{"romance:romcom", []string{"romantic comedy", "rom-com", "romcom", "爱情喜剧", "甜宠喜剧"}},
		{"mystery:police_procedural", []string{"police procedural", "detective squad", "cold case", "警匪剧", "刑侦", "罪案小组"}},
		{"literary", []string{"literary fiction", "literary novel", "existential", "philosophical novel", "纯文学", "文艺小说", "存在主义", "哲学小说"}},
		// round 6: Drama as a first-class parent genre (checked before generic keys)
		{"drama", []string{"drama", "family saga", "coming of age", "coming-of-age", "bildungsroman", "psychorealism", "metafiction", "domestic saga", "slice of life drama", "剧情", "家庭史诗", "成长小说", "文艺"}},
		{"graphic_novel", []string{"graphic novel", "comic", "manga script", "图像小说", "漫画"}},
		// parent genres after the specific ones
		{"fantasy", []string{"fantasy", "奇幻", "玄幻", "魔幻"}},
		{"scifi", []string{"sci-fi", "scifi", "science fiction", "科幻"}},
		{"mystery", []string{"mystery", "detective", "悬疑", "推理", "侦探", "crime fiction"}},
		{"romance", []string{"romance", "言情", "爱情"}},
		{"thriller", []string{"thriller", "spy", "惊悚", "谍战"}},
		{"horror", []string{"horror", "恐怖", "灵异", "克苏鲁", "cosmic horror", "folk horror"}},
		{"historical", []string{"historical", "history", "历史"}},
		{"western", []string{"western", "西部"}},
		{"adventure", []string{"adventure", "exploration", "expedition", "tomb raiding", "盗墓", "探险", "冒险"}},
	}
	for _, k := range kws {
		for _, w := range k.words {
			if strings.Contains(t, w) {
				return k.key
			}
		}
	}
	return ""
}

// SubgenrePresetKeys lists subgenres that have dedicated character-extra-field presets
// (see SubgenreCharacterExtraFields); exposed to the frontend as datalist suggestions.
var SubgenrePresetKeys = []string{
	"epic fantasy", "urban fantasy", "dark fantasy", "wuxia", "xianxia",
	"space opera", "cyberpunk", "solarpunk", "post-apocalyptic", "cozy mystery",
	"noir", "heist", "legal thriller", "psychological horror", "supernatural horror",
	"historical romance", "contemporary romance", "romantasy", "litRPG", "dystopian",
	"military", "hard scifi", "time travel", "gold rush",
	// round 3
	"progression fantasy", "isekai", "dungeon core", "portal fantasy", "biopunk",
	"cli-fi", "afrofuturism", "sword and sorcery", "domestic thriller", "techno thriller",
	"police procedural", "cold case", "locked room", "suspense romance", "mafia romance",
	"sports romance", "historical fiction", "nautical", "picaresque", "gothic",
	"folk horror", "space western", "military fantasy", "cozy sci-fi",
	"zombie apocalypse", "superhero", "steampunk", "dark academia", "swashbuckler",
	// round 6: new subgenres + Drama parent-genre subgenres
	"magical realism", "noblebright", "grimdark", "haunted house",
	"mecha", "regression", "villainess", "otome", "battle royale", "tower climbing", "school life",
	"spokon", "iyashikei", "ecchi", "harem",
	// round 8: anime/webnovel modifiers promoted to first-class subgenres per
	// user request (wuxia/xianxia/romantasy/isekai/revenge/dungeon core/
	// battle royale/tower climbing/mecha/spokon/ecchi/harem/school life/
	// iyashikei/villainess/otome/regression already listed above or in earlier
	// rounds). Mahou-shoujo/shounen and erotica added now.
	"mahou shoujo", "mahou shonen", "erotica", "slice of life",
	"family saga", "coming-of-age", "psychorealism", "metafiction", "existentialist fiction",
	"philosophical", "historical saga", "legal drama", "medical drama", "sports drama",
	"war drama", "courtroom drama", "domestic drama", "epistolary", "tragedy", "melodrama",
	"satire", "allegory", "bildungsroman",
}

// SubgenreHints maps every subgenre preset (see SubgenrePresetKeys) to a
// one-line explanation [zh, en], shown as tooltips in the frontend and used
// nowhere else — purely informational. Keys are matched case-insensitively.
var SubgenreHints = map[string][2]string{
	"epic fantasy":         {"Alta fantasía con mapas, guerras y magia a gran escala.", "High fantasy with maps, wars and large-scale magic."},
	"urban fantasy":        {"Magia oculta en una ciudad moderna.", "Magic hidden inside a modern city."},
	"dark fantasy":         {"Mundo cruel donde la magia tiene un precio alto.", "Grim world where magic carries a heavy price."},
	"wuxia":                {"Héroes marciales del Jianghu: honor, sectas y qi.", "Martial heroes of the Jianghu: honor, sects and qi."},
	"xianxia":              {"Cultivadores que ascienden hacia la inmortalidad.", "Cultivators ascending toward immortality."},
	"space opera":          {"Aventuras interestelares con facciones y naves.", "Interstellar adventures with factions and starships."},
	"cyberpunk":            {"High tech, low life: corporaciones e implantes.", "High tech, low life: megacorps and implants."},
	"solarpunk":            {"Futuro ecológico optimista y comunitario.", "Optimistic eco-futurism, community and rewilding."},
	"post-apocalyptic":     {"Supervivencia tras el colapso de la civilización.", "Survival after civilization's collapse."},
	"cozy mystery":         {"Crimen sin gore en pueblos entrañables.", "Gentle crime in endearing small communities."},
	"noir":                 {"Detective cínico, moral gris y destino fatal.", "Cynical detective, grey morality and doom."},
	"heist":                {"Un equipo planea y ejecuta un gran robo.", "A crew plans and pulls off a big score."},
	"legal thriller":       {"Tensión en tribunales, casos y conflictos éticos.", "Courtroom tension, cases and ethical conflicts."},
	"psychological horror": {"El terror nace de la mente fracturada.", "Horror springs from the fractured mind."},
	"supernatural horror":  {"Amenazas espectrales, posesiones y rituales.", "Ghostly threats, possessions and rituals."},
	"historical romance":   {"Amor bajo las normas rígidas de otra época.", "Love under the strict rules of a past era."},
	"contemporary romance": {"Romance actual, carreras y vínculos reales.", "Modern love: careers and realistic relationships."},
	"romantasy":            {"Fantasía épica donde el romance es el motor.", "Epic fantasy where the romance drives the plot."},
	"litRPG":               {"El mundo funciona como RPG: niveles y sistema.", "The world runs like an RPG: levels and a System."},
	"dystopian":            {"Sociedad opresiva vista desde quien la desafía.", "Oppressive society seen through those who defy it."},
	"military":             {"Vida, jerarquía y combate entre soldados.", "Soldier life, hierarchy and combat."},
	"hard scifi":           {"Ciencia rigurosa: la física importa y limita.", "Rigorous science: physics matters and constrains."},
	"time travel":          {"Viajes temporales con paradojas y consecuencias.", "Time travel with paradoxes and consequences."},
	"gold rush":            {"Fiebre del oro: minas, claims y fiebre.", "Gold fever: claims, mines and greed."},
	"progression fantasy":  {"Poder que sube paso a paso, entrenable.", "Power that grows step by step, trainable."},
	"isekai":               {"Transportado a otro mundo con ventaja única.", "Summoned to another world with a unique edge."},
	"dungeon core":         {"Ser la mazmorra: gestionar salas y monstruos.", "Be the dungeon: manage rooms and monsters."},
	"portal fantasy":       {"Cruzar un umbral hacia un mundo paralelo.", "Crossing a threshold into a parallel world."},
	"biopunk":              {"Biotecnología viva, ingeniería genética ilegal.", "Living biotech and illegal gene-hacking."},
	"cli-fi":               {"Ficción climática: sequías, migración, adaptación.", "Climate fiction: droughts, migration, adaptation."},
	"afrofuturism":         {"Futurismo con raíces africanas y diáspora.", "Futures rooted in African culture and diaspora."},
	"sword and sorcery":    {"Héroes brutales, espadas y brujería.", "Brutal heroes, swords and dark sorcery."},
	"domestic thriller":    {"Secretos y peligro dentro del hogar.", "Secrets and danger behind closed home doors."},
	"techno thriller":      {"Espionaje digital, hackeos y corporaciones.", "Digital espionage, hacks and corporations."},
	"police procedural":    {"Investigación realista paso a paso.", "Realistic step-by-step police investigation."},
	"cold case":            {"Casos viejos reabiertos con estela personal.", "Old cases reopened with a personal stake."},
	"locked room":          {"El imposible misterio del cuarto cerrado.", "The impossible sealed-room puzzle."},
	"suspense romance":     {"Romance con peligro acechando de fondo.", "Romance shadowed by lurking danger."},
	"mafia romance":        {"Amor prohibido en la familia criminal.", "Forbidden love inside a crime family."},
	"sports romance":       {"Romanza entre entrenamientos y competencias.", "Love amid training seasons and rivalries."},
	"historical fiction":   {"Drama verosímil ambientado en el pasado.", "Plausible drama set convincingly in the past."},
	"nautical":             {"Vida a bordo, travesías y tormentas.", "Life aboard ships, voyages and storms."},
	"picaresque":           {"Un pícaro vaga estafando de patrón en patrón.", "A rogue drifts from scheme to scheme."},
	"gothic":               {"Mansiones en ruinas, linajes malditos.", "Ruined estates, cursed lineages, dread."},
	"folk horror":          {"Terror rural: cultos, cosechas y tradiciones.", "Rural horror: cults, harvests, old customs."},
	"space western":        {"Frontera espacial con pistols y saloons.", "Frontier grit transplanted to space."},
	"military fantasy":     {"Ejércitos, rangos y magia táctica.", "Armies, ranks and tactical war-magic."},
	"cozy sci-fi":          {"Sci-fi tranquilo: cuidar, reparar, convivir.", "Gentle sci-fi: caretaking, repair, community."},
	"zombie apocalypse":    {"Hordas, refugios y pérdida constante.", "Hordes, safehouses and constant loss."},
	"superhero":            {"Poderes, identidad secreta y villanos.", "Powers, secret identities and rogues galleries."},
	"steampunk":            {"Era victoriana con maquinaria de vapor.", "Victorian era driven by steam machinery."},
	"dark academia":        {"Universidades elegantes con secretos letales.", "Elegant universities hiding lethal secrets."},
	"swashbuckler":         {"Duelos, piratas y capas al viento.", "Duels, pirates and caped derring-do."},
	// round 6: new subgenre hints
	"magical realism":        {"Lo maravillozo ocurre sin asombro en un mundo realista.", "The marvelous happens matter-of-factly in a realistic world."},
	"noblebright":            {"Mundo esperanzador donde las acciones mejoran todo.", "Hopeful world where good actions genuinely improve things."},
	"grimdark":               {"Brutalidad moral donde cada victoria cuesta.", "Moral brutality where every victory exacts its price."},
	"haunted house":          {"El terror vive dentro de un hogar concreto.", "Terror lives inside one specific home."},
	"mecha":                  {"Pilotos, máquinas de guerra y sus costes humanos.", "Pilots, war machines and their human cost."},
	"regression":             {"Volver al pasado con el conocimiento del futuro.", "Returning to the past armed with future knowledge."},
	"villainess":             {"Reencarnada como villana doomed: reescribe su guion.", "Reborn as the doomed villainess: rewriting her script."},
	"otome":                  {"Mundo de juego otome: rutas, banderas y corazones.", "Otome-game world: routes, flags and hearts."},
	"battle royale":          {"Último en pie bajo un reloj que se cierra.", "Last standing under a closing-clock arena."},
	"tower climbing":         {"Plantas, puertas y pruebas hacia la cima.", "Floors, gates and trials toward the summit."},
	"school life":            {"Aulas, clubes y ritmos de calendario escolar.", "Classrooms, clubs and school-calendar rhythms."},
	"spokon":                 {"Deporte como disciplina: entrenamiento y superación.", "Sports as discipline: training and self-mastery."},
	"iyashikei":              {"Historia sanadora, sin villanos, solo calidez.", "Healing story: no villains, only warmth."},
	"ecchi":                  {"Comedia de enredos subidos de tono con corazón.", "Risqué comedy of errors with a sincere core."},
	"harem":                  {"Un protagonista, muchos afectos en equilibrio.", "One lead, many affections in comedic balance."},
	// round 8: remaining anime/webnovel modifiers as first-class subgenres
	"mahou shoujo":           {"Magia, transformación y vínculos con corazón adolescente.", "Transformation magic, familiar bonds and a teenage heart."},
	"mahou shonen":           {"Héroes mágicos jóvenes que aprenden el coste del poder.", "Young magic heroes learning the cost of power."},
	"slice of life":          {"Sin amenaza mundial: rutina, comida y vínculos.", "No world threat: routine, food and gentle bonds."},
	"erotica":                {"Intimidad explícita como trama central adulta.", "Explicit intimacy driving an adult-centered plot."},
	"family saga":            {"Generaciones de una familia sobre un siglo.", "Generations of one family across an era."},
	"coming-of-age":          {"El umbral doloroso y tierno de crecer.", "The painful, tender threshold of growing up."},
	"psychorealism":          {"Realismo psicológico: la vida interior como trama.", "Psychological realism: inner life drives the plot."},
	"metafiction":            {"Ficción consciente de ser ficción.", "Fiction knowingly about being fiction."},
	"existentialist fiction": {"Elección, absurdo y libertad sin red.", "Choice, absurdity and freedom without safety nets."},
	"philosophical":          {"Ideas puestas a prueba por personajes vivos.", "Living characters testing big ideas."},
	"historical saga":        {"Familias arrastradas por eventos históricos.", "Families swept along by historical events."},
	"legal drama":            {"Tribunales, ética y estrategias legales.", "Courtrooms, ethics and legal strategy."},
	"medical drama":          {"Hospitales, triaje y decisiones vitales.", "Hospitals, triage and life-or-death calls."},
	"sports drama":           {"Temporadas, rivales y cuerpos al límite.", "Seasons, rivals and bodies at their limit."},
	"war drama":              {"La guerra desde sus consecuencias humanas.", "War through its human consequences."},
	"courtroom drama":        {"Un juicio como escenario completo.", "One trial as the whole stage."},
	"domestic drama":         {"Conflicto íntimo dentro de un hogar.", "Intimate conflict inside one household."},
	"epistolary":             {"La historia contada en cartas y documentos.", "The story told through letters and documents."},
	"tragedy":                {"Un defecto conduce al derrumbe inevitable.", "One flaw drives the inevitable downfall."},
	"melodrama":              {"Emoción intensa, giros grandes, corazón sincero.", "Big feelings, big reversals, sincere heart."},
	"satire":                 {"Risa afilada contra instituciones y modas.", "Sharp laughter aimed at institutions and fads."},
	"allegory":               {"Cada elemento significa otra cosa además.", "Every element means something beyond itself."},
	"bildungsroman":          {"Formación de una persona a lo largo de los años.", "A person formed across the years of growth."},
}

// AudienceKeys lists the valid target_audience values. YA/middle-grade moved here
// from the genre tier: audience changes register & content handling, not story type.
var AudienceKeys = []string{"kid", "middle_grade", "ya", "new_adult", "adult", "all_ages"}

// AudienceLabel returns a short human label for a target-audience key.
func AudienceLabel(audience, lang string) string {
	switch strings.TrimSpace(audience) {
	case "kid":
		return pickLang(lang, "儿童（5-9岁）", "Children (5-9)")
	case "middle_grade":
		return pickLang(lang, "中童（8-12岁）", "Middle grade (8-12)")
	case "ya":
		return pickLang(lang, "青少年（13-18岁）", "Young adult (13-18)")
	case "new_adult":
		return pickLang(lang, "新成年（17-25岁）", "New adult (17-25)")
	case "adult":
		return pickLang(lang, "成人", "Adult")
	case "all_ages":
		return pickLang(lang, "全年龄", "All ages")
	}
	return strings.TrimSpace(audience)
}

// AudienceGuidance returns a suggestion block for the target audience, or "" when unset.
func AudienceGuidance(audience, lang string) string {
	switch strings.TrimSpace(audience) {
	case "kid":
		return pickLang(lang, "【目标读者】儿童（约5-9岁）：主角为儿童视角，语言简单、篇幅短小，基调温暖安全，冲突温和且必被妥善解决，无恐怖或露骨内容。",
			"[TARGET AUDIENCE] Children (approx. 5-9): child-age protagonists and viewpoint, simple short prose, warm and safe tone, gentle conflicts always resolved, no horror or explicit content.")
	case "middle_grade":
		return pickLang(lang, "【目标读者】中童（约8-12岁）：主角略长于读者，友谊与家庭为核心，幽默与惊奇并存，浪漫最多到暗恋，暴力不细节化，结局积极。",
			"[TARGET AUDIENCE] Middle grade (approx. 8-12): protagonists slightly older than readers, friendship/family at the core, humor and wonder, romance at most a crush, violence never graphic, hopeful endings.")
	case "ya":
		return pickLang(lang, "【目标读者】青少年（约13-18岁）：青少年主角与身份认同主线，情感强度高、节奏快，可涉及成人题材但聚焦于青少年体验，避免过度露骨。",
			"[TARGET AUDIENCE] Young adult (approx. 13-18): teen protagonists, identity-driven arcs, high emotional intensity and pace; mature themes allowed but filtered through the teen experience, nothing gratuitously explicit.")
	case "new_adult":
		return pickLang(lang, "【目标读者】新成年（约17-25岁）：主角处于大学/初入社会阶段，处理独立、职业与亲密关系的试错；情感与身体关系可更直接，仍重成长。",
			"[TARGET AUDIENCE] New adult (approx. 17-25): protagonists in college or first-job years; independence, career and intimate-relationship trial-and-error; emotions and physicality more direct, growth still central.")
	case "adult":
		return pickLang(lang, "【目标读者】成年读者：默认成人复杂度——道德灰度、长期后果、职业与家庭细节均可充分展开，语言不必回避成熟题材。",
			"[TARGET AUDIENCE] Adult readers: full adult complexity by default—moral gray areas, long-term consequences, career/family detail; prose need not shy away from mature material.")
	case "all_ages":
		return pickLang(lang, "【目标读者】全年龄向：各年龄段均可阅读——不含露骨或创伤性内容，主题层次让儿童与成人各有所得。",
			"[TARGET AUDIENCE] All ages: readable by every age group—no explicit or traumatic content, layered so children and adults each get something.")
	}
	return ""
}

// —— Content rating parameters: romance / sexual content / gore / world darkness ——

var (
	// RomanceLevelKeys lists the selectable romance-line weights ("random" rolls a fresh one).
	RomanceLevelKeys = []string{"random", "none", "subplot", "moderate", "central"}
	// SexualContentKeys lists the selectable sexual-content scales.
	SexualContentKeys = []string{"random", "clean", "fade_to_black", "explicit"}
	// GoreLevelKeys lists the selectable gore/violence scales.
	GoreLevelKeys = []string{"random", "none", "mid", "explicit"}
	// WorldDarknessKeys lists the selectable world cruelty levels.
	WorldDarknessKeys = []string{"idyllic", "temperate", "gritty", "grim", "abyssal"}
)

// RomanceLevelLabel returns a short human label for a romance-level key.
func RomanceLevelLabel(key, lang string) string {
	switch strings.TrimSpace(key) {
	case "random":
		return pickLang(lang, "随机", "Random")
	case "none":
		return pickLang(lang, "无恋爱线", "None")
	case "subplot":
		return pickLang(lang, "支线感情", "Subplot")
	case "moderate":
		return pickLang(lang, "中等比重", "Moderate")
	case "central":
		return pickLang(lang, "核心主线", "Central")
	}
	return strings.TrimSpace(key)
}

// SexualContentLabel returns a short human label for a sexual-content key.
func SexualContentLabel(key, lang string) string {
	switch strings.TrimSpace(key) {
	case "random":
		return pickLang(lang, "随机", "Random")
	case "clean":
		return pickLang(lang, "全年龄干净", "Clean")
	case "fade_to_black":
		return pickLang(lang, "渐黑留白", "Fade to Black")
	case "explicit":
		return pickLang(lang, "露骨直写", "Explicit")
	}
	return strings.TrimSpace(key)
}

// GoreLevelLabel returns a short human label for a gore-level key.
func GoreLevelLabel(key, lang string) string {
	switch strings.TrimSpace(key) {
	case "random":
		return pickLang(lang, "随机", "Random")
	case "none":
		return pickLang(lang, "无血腥", "None")
	case "mid":
		return pickLang(lang, "中度血腥", "Mid")
	case "explicit":
		return pickLang(lang, "非常露骨", "Very Explicit")
	}
	return strings.TrimSpace(key)
}

// WorldDarknessLabel returns a short human label for a world-darkness key.
func WorldDarknessLabel(key, lang string) string {
	switch strings.TrimSpace(key) {
	case "idyllic":
		return pickLang(lang, "田园牧歌", "Idyllic")
	case "temperate":
		return pickLang(lang, "温和写实", "Temperate")
	case "gritty":
		return pickLang(lang, "粗粝黑暗", "Gritty")
	case "grim":
		return pickLang(lang, "阴暗残酷", "Grim")
	case "abyssal":
		return pickLang(lang, "深渊绝望", "Abyssal")
	}
	return strings.TrimSpace(key)
}

// EffectiveRomanceLevel resolves "random" by rolling a concrete level.
func (s *StoryConfig) EffectiveRomanceLevel() string {
	k := strings.TrimSpace(s.RomanceLevel)
	if k == "" || k == "random" {
		return RomanceLevelKeys[1+rand.Intn(len(RomanceLevelKeys)-1)]
	}
	return k
}

// EffectiveSexualContent resolves "random" by rolling a concrete scale.
func (s *StoryConfig) EffectiveSexualContent() string {
	k := strings.TrimSpace(s.SexualContent)
	if k == "" || k == "random" {
		return SexualContentKeys[1+rand.Intn(len(SexualContentKeys)-1)]
	}
	return k
}

// EffectiveGoreLevel resolves "random" by rolling a concrete scale.
func (s *StoryConfig) EffectiveGoreLevel() string {
	k := strings.TrimSpace(s.GoreLevel)
	if k == "" || k == "random" {
		return GoreLevelKeys[1+rand.Intn(len(GoreLevelKeys)-1)]
	}
	return k
}

// ContentGuidance returns prompt lines constraining romance weight, sexual
// content, gore and world cruelty according to the story config. The audience
// age acts as a hard ceiling: explicit material is downgraded for young or
// all-ages readers no matter what the selects say. Returns "" when nothing is set.
func ContentGuidance(sc *StoryConfig, lang string) string {
	var lines []string
	add := func(zh, english string) {
		if strings.TrimSpace(english) != "" || strings.TrimSpace(zh) != "" {
			if i18n.NormalizeLanguage(lang) == i18n.LangEN {
				lines = append(lines, english)
			} else {
				lines = append(lines, zh)
			}
		}
	}
	aud := strings.TrimSpace(sc.TargetAudience)
	young := aud == "kid" || aud == "middle_grade" || aud == "all_ages"

	if v := strings.TrimSpace(sc.RomanceLevel); v != "" {
		switch sc.EffectiveRomanceLevel() {
		case "none":
			add("【恋爱线】本作不发展恋爱关系：人物之间可有深厚情谊，但不要写成恋情或表白。",
				"[ROMANCE] No romance line in this book: deep bonds between characters are fine, but do not turn them into love stories or confessions.")
		case "subplot":
			add("【恋爱线】恋爱只作为支线/暗线存在：点缀人物关系、服务主线，不占用主要篇幅，不喧宾夺主。",
				"[ROMANCE] Romance stays a subplot/backstory thread: it colors relationships and serves the main plot without taking over screen time.")
		case "moderate":
			add("【恋爱线】恋爱是重要副线：有完整的相识-发展-波折-进展弧线，与主线交织并互相推动，约占故事的三到四成。",
				"[ROMANCE] Romance is a significant secondary line with its own meet-develop-setback-progress arc, interwoven with and feeding the main plot (roughly 30-40% of the story).")
		case "central":
			add("【恋爱线】恋爱关系是故事核心主线：情感推进、关系变化驱动情节，即使有其他冲突也服务于感情主线。",
				"[ROMANCE] The romance itself is the central spine: emotional progression and relationship changes drive the plot; other conflicts serve the love story.")
		}
	}

	if v := strings.TrimSpace(sc.SexualContent); v != "" {
		switch sc.EffectiveSexualContent() {
		case "clean":
			add("【性描写尺度】干净向（类似全年龄）：不出现任何性暗示场景的具体展开，亲密止步于牵手、拥抱级别的表达。",
				"[SEXUAL CONTENT] Clean (all-ages style): no sexual situations depicted even implicitly; intimacy stops at hand-holding/hug-level expression.")
		case "fade_to_black":
			add("【性描写尺度】渐黑留白：亲密场景可被提及或以一句氛围带过，随后镜头移开，绝不描写过程与细节。",
				"[SEXUAL CONTENT] Fade to black: intimate scenes may be acknowledged or bridged with a single atmospheric line, then the camera turns away—never depict process or detail.")
			if young {
				add("【性描写尺度·读者上限】目标读者为儿童/低龄或全年龄：即便设定允许，也只保留最轻微的浪漫暗示。",
					"[SEXUAL CONTENT · AUDIENCE CEILING] The target audience is children or all-ages: keep at most the faintest romantic hint even where the setting would allow more.")
			}
		case "explicit":
			if young {
				add("【性描写尺度·读者上限】目标读者为儿童/低龄或全年龄：露骨内容一律禁止，自动降级为渐黑留白处理。",
					"[SEXUAL CONTENT · AUDIENCE CEILING] Explicit content is forbidden for this child/all-ages audience: downgrade automatically to fade-to-black treatment.")
			} else if aud == "ya" {
				add("【性描写尺度·读者上限】目标读者为青少年：可承认角色间的亲密关系，但一切露骨细节必须回避，止于暗示与留白。",
					"[SEXUAL CONTENT · AUDIENCE CEILING] Young-adult audience: acknowledge off-page intimacy between characters, but avoid all explicit detail—suggest, don't depict.")
			} else {
				add("【性描写尺度】露骨向：成人之间的亲密场景可直接、具体地描写，服务于人物与情节而非单纯猎奇；须保持知情、自愿的基调。",
					"[SEXUAL CONTENT] Explicit: adult intimate scenes may be written directly and concretely, serving character and plot rather than shock value; keep an informed, consensual register.")
			}
		}
	}

	if v := strings.TrimSpace(sc.GoreLevel); v != "" {
		switch sc.EffectiveGoreLevel() {
		case "none":
			add("【血腥尺度】无血腥：暴力不发生在地面上——战斗点到为止，伤亡用概述带过，不描写伤口与痛苦细节。",
				"[GORE] No gore: violence happens off-page—fights stay stylized and brief, casualties summarized, never dwell on wounds or suffering.")
		case "mid":
			add("【血腥尺度】中度血腥：战斗后果可见（血、伤口、疼痛），但不堆砌残肢内脏等极端意象，感官描写克制。",
				"[GORE] Moderate: consequences of violence are visible (blood, wounds, pain) but avoid extreme dismemberment imagery; keep sensory detail restrained.")
			if young {
				add("【血腥尺度·读者上限】目标读者为儿童/低龄或全年龄：血腥自动降级为几乎不可见，仅以结果一笔带过。",
					"[GORE · AUDIENCE CEILING] Child/all-ages audience: gore drops to nearly invisible—mention outcomes only.")
			}
		case "explicit":
			if young {
				add("【血腥尺度·读者上限】目标读者为儿童/低龄或全年龄：禁止细致血腥，自动降级为中度以下处理。",
					"[GORE · AUDIENCE CEILING] Child/all-ages audience: graphic gore is prohibited—downgrade automatically below moderate.")
			} else if aud == "ya" {
				add("【血腥尺度·读者上限】目标读者为青少年：暴力可以沉重，但避免细致入微的残虐描写，重在情绪冲击而非生理细节。",
					"[GORE · AUDIENCE CEILING] Young-adult audience: violence can be heavy, but skip meticulous torture-level detail—prioritize emotional impact over physiological description.")
			} else {
				add("【血腥尺度】非常露骨：可直面描写创伤、尸体与痛苦的生理细节，用于营造真实感与冲击力；不为猎奇而滥用。",
					"[GORE] Very explicit: trauma, corpses and the physiology of pain may be faced head-on for realism and impact—use deliberately, not gratuitously.")
			}
		}
	}

	if v := strings.TrimSpace(sc.WorldDarkness); v != "" {
		switch v {
		case "idyllic":
			add("【世界残酷度】田园世界：社会总体善良可信，威胁多来自外部或误会，恶意罕见且容易被化解，读完应感到温暖。",
				"[WORLD DARKNESS] Idyllic: society is fundamentally kind and trustworthy; threats come from outside or from misunderstandings; malice is rare and easily resolved—the book should leave readers warm.")
		case "temperate":
			add("【世界残酷度】温和写实：好人多但也有自私者，代价真实存在但世界总体讲理，黑暗存在于个体命运而非世界本质。",
				"[WORLD DARKNESS] Temperate: mostly decent people with some selfish ones; real costs but a fundamentally fair world—darkness lives in individual fates, not in the world's nature.")
		case "gritty":
			add("【世界残酷度】粗粝黑暗：资源紧张、制度腐败、背叛常见，主角每前进一步都要付出可见代价，善意常常没有好报。",
				"[WORLD DARKNESS] Gritty: scarce resources, corrupt institutions, frequent betrayal; every step forward costs something visible; kindness often goes unrewarded.")
		case "grim":
			add("【世界残酷度】阴暗残酷：系统性压迫与暴力常态化，无辜者会惨死，希望稀薄且需以巨大牺牲换取，胜利往往残缺。",
				"[WORLD DARKNESS] Grim: systemic oppression and normalized violence; innocents die horribly; hope is thin and bought with enormous sacrifice; victories remain partial.")
		case "abyssal":
			add("【世界残酷度】深渊绝望：世界本身充满敌意甚至恶意，道德在生存面前普遍崩塌，角色面对的往往是坏选择与更坏选择，主题直面苦难的意义本身。",
				"[WORLD DARKNESS] Abyssal: the world itself is hostile or outright malicious; morality collapses under survival; characters face bad choices and worse ones; the story confronts the meaning of suffering itself.")
		}
	}

	if p := strings.TrimSpace(sc.AudienceProfile); p != "" {
		add("【目标读者画像】理想读者素描（写作时想象 TA 在阅读，贴合其口味与雷点）：\n"+p,
			"[IDEAL READER PROFILE] Sketch of the ideal reader (write as if they are reading; match their tastes and avoid their deal-breakers):\n"+p)
	}

	if len(lines) == 0 {
		return ""
	}
	if i18n.NormalizeLanguage(lang) == i18n.LangEN {
		return "CONTENT RATING & MATURITY SETTINGS (hard constraints unless overridden above):\n" + strings.Join(lines, "\n")
	}
	return "内容与尺度设定（除上文另有说明外均为硬性约束）：\n" + strings.Join(lines, "\n")
}

func pickLang(lang, zh, en string) string {
	if i18n.NormalizeLanguage(lang) == i18n.LangEN {
		return en
	}
	return zh
}
