package config

import (
	"encoding/json"
	"fmt"
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
	Type                 string `json:"type"`
	ParentGenre          string `json:"parent_genre,omitempty"` // 父类型（UI select，取值为 ParentGenreKeys 之一或空；"other" 表示自定义）
	Title                string `json:"title"`
	Subgenre             string `json:"subgenre,omitempty"`               // 子类型（受 Type 约束），借鉴 NovelWriter 的 Genre/Subgenre 参数
	Theme                string `json:"theme,omitempty"`                  // 主题
	Tone                 string `json:"tone,omitempty"`                   // 基调
	Author               string `json:"author,omitempty"`                 // 作者名
	StoryLength          string `json:"story_length,omitempty"`           // 篇幅：short/novella/novel/epic，联动章节数与结构选项
	Structure            string `json:"structure,omitempty"`              // 故事结构框架（3幕/英雄之旅等，随篇幅变化）
	Motif                string `json:"motif,omitempty"`                  // 文学母题：可随机生成，注入大纲/写作/生成 prompts
	Brief                string `json:"brief,omitempty"`                  // 故事简介：作为 AI 生成风格/角色/组织/关系的依据
	ConflictScale        string `json:"conflict_scale,omitempty"`         // 冲突规模（借鉴 NovelWriter conflict_scales；"other" 时使用 ConflictOther）
	ConflictOther        string `json:"conflict_other,omitempty"`         // 自定义冲突规模
	SpecificSettings     string `json:"specific_settings,omitempty"`      // 特定设定，每行一条（借鉴 implied_settings）
	ProtagonistType      string `json:"protagonist_type,omitempty"`       // 主角类型（借鉴 protagonist_types；"other" 时使用 ProtagonistOther）
	ProtagonistOther     string `json:"protagonist_other,omitempty"`      // 自定义主角类型
	TargetAudience       string `json:"target_audience,omitempty"`        // 目标读者：kid/middle_grade/ya/new_adult/adult/all_ages；影响语言难度、尺度与题材处理（原 YA/儿童文学从类型层级移到这里）
	GenderBias           string `json:"gender_bias,omitempty"`            // 人物性别倾向：empty/random/male/female/balanced；默认 random，不强制
	LocationsEnabled     bool   `json:"locations_enabled,omitempty"`      // 启用地点/场景实体（借鉴 NovelWriter locations）：生成设定与大纲时纳入地点
	CharacterArcsEnabled bool   `json:"character_arcs_enabled,omitempty"` // 启用角色弧光字段（goals/flaws/strengths/arc，借鉴 NovelWriter lore）
	ThemeMotifInput      string `json:"combined_theme,omitempty"`         // 仅前端提交用：合并后的"主题/母题"字段值；PutConfig 会拆回 Theme+Motif
	// ThemeMotifInputPtr mirrors ThemeMotifInput but distinguishes "sent empty"
	// from "not sent at all", so clearing the merged UI field also clears Motif.
	ThemeMotifInputPtr    *string `json:"-"`
	TargetWordsPerChapter int     `json:"target_words_per_chapter"`
	WritingStyle          string  `json:"writing_style"`
	WritingPOV            string  `json:"writing_pov"` // 叙述视角，如第一人称女主、第三人称限知等
}

// UnmarshalJSON decodes the story config and additionally records whether the
// transient merged Theme/Motif UI field ("combined_theme") was present in the
// payload at all (even if empty), via ThemeMotifInputPtr. This lets PutConfig
// distinguish "user cleared the merged field" from "legacy client that never
// sends it".
func (s *StoryConfig) UnmarshalJSON(data []byte) error {
	type alias StoryConfig
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	var probe struct {
		Combined *string `json:"combined_theme"`
	}
	_ = json.Unmarshal(data, &probe)
	*s = StoryConfig(a)
	s.ThemeMotifInputPtr = probe.Combined
	return nil
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

// EffectiveTheme returns the theme field with the literary motif appended when
// both are set — Theme and Motif were merged into a single UI field; the two
// storage fields remain so old projects keep their data and prompts stay rich.
func (s *StoryConfig) EffectiveTheme() string {
	t := strings.TrimSpace(s.Theme)
	m := strings.TrimSpace(s.Motif)
	switch {
	case t != "" && m != "":
		return t + " / " + m
	default:
		return t + m
	}
}

// SetCombinedTheme writes the merged Theme/Motif UI field back to storage.
// A " / " separator splits the value into Theme and Motif again (the same
// format EffectiveTheme produces); values without the separator are stored in
// Theme, leaving any previous Motif untouched when empty.
func (s *StoryConfig) SetCombinedTheme(v string) {
	v = strings.TrimSpace(v)
	if i := strings.Index(v, " / "); i >= 0 {
		s.Theme = strings.TrimSpace(v[:i])
		s.Motif = strings.TrimSpace(v[i+3:])
		return
	}
	s.Theme = v
	if v == "" {
		s.Motif = ""
	}
}

// EffectiveStoryType is the genre string prompts and option lookups should use:
// the canonical parent-genre key when the UI select picked one, otherwise the
// legacy free-form Type field.
func (s *StoryConfig) EffectiveStoryType() string {
	return EffectiveGenreType(s.ParentGenre, s.Type)
}

// SettingOptionsForGenre returns the checkbox options for the selected parent
// genre (NovelWriter-style implied_settings). When no preset matches the free-
// form Type, it falls back to the legacy per-genre suggestion map so older
// genre keys (litrpg, cyberpunk, ...) keep working.
func SettingOptionsForGenre(storyType string) []string {
	for _, key := range ParentGenreKeys {
		if MatchParentGenreKey(storyType) == key {
			if opts, ok := GenreSpecificSettingOptions[key]; ok && len(opts) > 0 {
				return opts
			}
		}
	}
	if k := MatchGenreKey(storyType); k != "" {
		if opts, ok := GenreSpecificSettings[k]; ok && len(opts) > 0 {
			return append([]string{}, opts...)
		}
	}
	return nil
}

// ToneOptionsForGenre returns tone suggestions for the selected parent genre
// (adapted from NovelWriter tones); empty when there is no preset match.
func ToneOptionsForGenre(storyType string) []string {
	if k := MatchParentGenreKey(storyType); k != "" {
		if opts, ok := GenreToneOptions[k]; ok {
			return append([]string{}, opts...)
		}
	}
	return nil
}

// —— Novel parameters: length & structure options (borrowed from NovelWriter) —

const (
	LengthShort   = "short"   // 短篇
	LengthNovella = "novella" // 中篇
	LengthNovel   = "novel"   // 长篇（标准）
	LengthEpic    = "epic"    // 长篇（史诗）
)

// LengthKeys is the ordered list of selectable story lengths.
var LengthKeys = []string{LengthShort, LengthNovella, LengthNovel, LengthEpic}

// StructureKeysByLength maps a story length to its applicable structure frameworks.
var StructureKeysByLength = map[string][]string{
	LengthShort:   {"three_act", "fichtean", "freytag"},
	LengthNovella: {"three_act", "seven_point", "heros_journey_simple"},
	LengthNovel:   {"three_act", "six_act", "save_the_cat", "heros_journey"},
	LengthEpic:    {"six_act", "heros_journey", "save_the_cat", "episodic"},
}

// DefaultStructureForLength returns the recommended structure for a length.
func DefaultStructureForLength(length string) string {
	switch length {
	case LengthShort, LengthNovella:
		return "three_act"
	default:
		return "six_act"
	}
}

// SuggestedChaptersByLength gives an outline chapter-count hint per length.
func SuggestedChaptersByLength(length string) (min, max int) {
	switch length {
	case LengthShort:
		return 5, 12
	case LengthNovella:
		return 12, 25
	case LengthNovel:
		return 25, 45
	case LengthEpic:
		return 45, 90
	}
	return 0, 0
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

	if cfg.Story.TargetWordsPerChapter <= 0 {
		cfg.Story.TargetWordsPerChapter = 5000
	}

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
	"adventure":   {"Man vs Nature", "Expedition Rivalry", "Ancient Trap/Trial", "Race Against Time", "Survival in the Unknown", "other"},
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
}

var GenreSpecificSettings = map[string][]string{
	"fantasy":     {"magic_system", "medieval_setting", "mythical_creatures", "epic_scale", "political_dynasties"},
	"scifi":       {"interstellar_travel", "advanced_technology", "multiple_species", "space_combat", "ai_singularity"},
	"mystery":     {"small_community", "amateur_detective", "low_violence", "puzzle_focus", "recurring_characters"},
	"romance":     {"modern_setting", "relationship_focus", "emotional_journey", "happy_ending", "realistic_world"},
	"thriller":    {"international_intrigue", "spy_networks", "government_secrets", "double_agents", "global_stakes"},
	"horror":      {"atmospheric_dread", "isolated_setting", "supernatural_elements", "psychological_terror", "dark_atmosphere", "cosmic_horror"},
	"historical":  {"ancient_civilizations", "mythological_elements", "tribal_societies", "ancient_religions", "primitive_technology"},
	"western":     {"frontier_setting", "lawlessness", "honor_code", "survival_focus", "horse_culture"},
	"litrpg":      {"game_system_rules", "stats_and_levels", "dungeons_and_loot", "guild_politics", "progression_arc", "server_economy"},
	"urban":       {"hidden_supernatural_society", "modern_city_backdrop", "masquerade_rule", "part_time_hero_life", "night_market_magic"},
	"military":    {"unit_esprit_de_corps", "chain_of_command", "combined_arms_tactics", "rules_of_engagement", "rotations_and_leave", "war_crimes_inquiry"},
	"postapo":     {"resource_scarcity", "mutated_fauna_flora", "pre_fall_artifacts", "settlement_politics", "radiation_zones", "water_and_power_grids"},
	"adventure":   {"expedition_logistics", "ancient_traps_puzzles", "extreme_environments", "rival_explorers", "local_mythology_clues", "treasure_curse"},
	"wuxia":       {"jianghu_codes", "qi_cultivation", "sects_and_alliances", "neigong_manuals", "tea_house_rumors", "wuxia_martial_choreography"},
	"xianxia":     {"cultivation_realms", "spirit_roots", "sect_hierarchy", "alchemy_pill_refining", "heavenly_dao_laws", "immortal_realm_geography"},
	"cozy":        {"small_town_map", "baking_brewing_gardening_hobbies", "community_events", "pet_or_cat_presence", "low_stakes_danger", "found_family"},
	"heist":       {"crew_specialties", "target_security_layers", "mark_and_distraction", "getaway_routes", "one_job_too_many", "double_cross_timer"},
	"cyberpunk":   {"megacorporations", "body_augmentation", "netrunning_matrix", "street_economy_implants", "rain_neon_aesthetic", "class_divide_vertical_cities"},
	"solarpunk":   {"renewable_infrastructure", "community_cooperatives", "rewilded_urbanism", "open_source_tools", "slow_healing_after_collapse", "festivals_and_commons"},
	"romantasy":   {"fae_or_god courts", "magic_cost_of_intimacy", "arranged_political_marriage", "enemy_to_lover_arc", "prophecy_bond", "court_intrigue_and_seasons"},
	"dystopian":   {"surveillance_state", "scarce_rations_and_permits", "state_propaganda_media", "resistance_cells", "caste_by_gene_or_record", "forbidden_knowledge_archive"},
	"space_opera": {"jump_gate_network", "galactic_senate_or_empire", "xeno_first_contact_protocols", "fleet_logistics", "dynastic_politics_in_stars", "precursor_relics"},
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
}

// AudienceKeys lists the canonical target_audience shortcut values. The UI now
// renders target_audience as a free-text field with these keys as quick-fill
// chips; custom text is allowed and passes through to the prompts verbatim.
var AudienceKeys = []string{"kid", "middle_grade", "ya", "new_adult", "adult", "all_ages"}

// AudienceGuidanceOrText returns preset guidance for a known audience key, or a
// generic guidance line wrapping the user's free text (for custom audiences).
func AudienceGuidanceOrText(audience, lang string) string {
	if v := AudienceGuidance(audience, lang); v != "" {
		return v
	}
	if a := strings.TrimSpace(audience); a != "" {
		return pickLang(lang, "【目标读者】"+a+"：请让语言难度、内容尺度与题材处理贴合这一读者定位。",
			"[TARGET AUDIENCE] "+a+": match vocabulary level, content boundaries and thematic handling to this readership.")
	}
	return ""
}

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

func pickLang(lang, zh, en string) string {
	if i18n.NormalizeLanguage(lang) == i18n.LangEN {
		return en
	}
	return zh
}
