package story

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"showmethestory/internal/config"
	"showmethestory/internal/fsutil"
	"showmethestory/internal/i18n"
	"showmethestory/internal/sse"
	"strconv"
	"strings"
)

// storyFieldSpec describes one StoryConfig field the agent chat can read and
// write: its JSON key, an English label and a Chinese label (used in prompts
// and conflict messages), plus typed accessors so bool/int fields keep their
// JSON types when persisted through the guard.
type storyFieldSpec struct {
	Key     string
	EN      string
	ZH      string
	Get     func(config.StoryConfig) string
	SetStr  func(*config.StoryConfig, string)
	SetBool func(*config.StoryConfig, bool)
	SetInt  func(*config.StoryConfig, int)
}

func trimmed(s string) string { return strings.TrimSpace(s) }

var storyFieldSpecs = []storyFieldSpec{
	{"type", "Genre", "故事类型", func(s config.StoryConfig) string { return s.Type }, func(c *config.StoryConfig, v string) { c.Type = v }, nil, nil},
	{"title", "Title", "小说标题", func(s config.StoryConfig) string { return s.Title }, func(c *config.StoryConfig, v string) { c.Title = v }, nil, nil},
	{"subgenre", "Subgenre", "子类型", func(s config.StoryConfig) string { return s.Subgenre }, func(c *config.StoryConfig, v string) { c.Subgenre = v }, nil, nil},
	{"theme", "Theme", "主题", func(s config.StoryConfig) string { return s.Theme }, func(c *config.StoryConfig, v string) { c.Theme = v }, nil, nil},
	{"tone", "Tone", "基调", func(s config.StoryConfig) string { return s.Tone }, func(c *config.StoryConfig, v string) { c.Tone = v }, nil, nil},
	{"author", "Author", "作者名", func(s config.StoryConfig) string { return s.Author }, func(c *config.StoryConfig, v string) { c.Author = v }, nil, nil},
	{"story_length", "Story length", "篇幅", func(s config.StoryConfig) string { return s.StoryLength }, func(c *config.StoryConfig, v string) { c.StoryLength = v }, nil, nil},
	{"structure", "Story structure", "故事结构", func(s config.StoryConfig) string { return s.Structure }, func(c *config.StoryConfig, v string) { c.Structure = v }, nil, nil},
	{"motif", "Literary motif", "文学母题", func(s config.StoryConfig) string { return s.Motif }, func(c *config.StoryConfig, v string) { c.Motif = v }, nil, nil},
	{"story_idea", "Story idea (core premise; NOT a chapter/outline synopsis)", "故事构想", func(s config.StoryConfig) string { return s.StoryIdea }, func(c *config.StoryConfig, v string) { c.StoryIdea = v }, nil, nil},
	{"conflict_scale", "Conflict scale", "冲突规模", func(s config.StoryConfig) string { return s.ConflictScale }, func(c *config.StoryConfig, v string) { c.ConflictScale = v }, nil, nil},
	{"conflict_other", "Custom conflict scale", "自定义冲突规模", func(s config.StoryConfig) string { return s.ConflictOther }, func(c *config.StoryConfig, v string) { c.ConflictOther = v }, nil, nil},
	{"specific_settings", "Specific settings (one per line)", "特定设定（每行一条）", func(s config.StoryConfig) string { return s.SpecificSettings }, func(c *config.StoryConfig, v string) { c.SpecificSettings = v }, nil, nil},
	{"protagonist_type", "Protagonist type", "主角类型", func(s config.StoryConfig) string { return s.ProtagonistType }, func(c *config.StoryConfig, v string) { c.ProtagonistType = v }, nil, nil},
	{"protagonist_other", "Custom protagonist type", "自定义主角类型", func(s config.StoryConfig) string { return s.ProtagonistOther }, func(c *config.StoryConfig, v string) { c.ProtagonistOther = v }, nil, nil},
	{"target_audience", "Target audience", "目标读者", func(s config.StoryConfig) string { return s.TargetAudience }, func(c *config.StoryConfig, v string) { c.TargetAudience = v }, nil, nil},
	{"audience_profile", "Audience profile", "目标读者画像", func(s config.StoryConfig) string { return s.AudienceProfile }, func(c *config.StoryConfig, v string) { c.AudienceProfile = v }, nil, nil},
	{"romance_level", "Romance level", "恋爱线比重", func(s config.StoryConfig) string { return s.RomanceLevel }, func(c *config.StoryConfig, v string) { c.RomanceLevel = v }, nil, nil},
	{"sexual_content", "Sexual content level", "性描写尺度", func(s config.StoryConfig) string { return s.SexualContent }, func(c *config.StoryConfig, v string) { c.SexualContent = v }, nil, nil},
	{"gore_level", "Gore level", "暴力血腥尺度", func(s config.StoryConfig) string { return s.GoreLevel }, func(c *config.StoryConfig, v string) { c.GoreLevel = v }, nil, nil},
	{"world_darkness", "World darkness", "世界黑暗度", func(s config.StoryConfig) string { return s.WorldDarkness }, func(c *config.StoryConfig, v string) { c.WorldDarkness = v }, nil, nil},
	{"gender_bias", "Gender bias", "人物性别倾向", func(s config.StoryConfig) string { return s.GenderBias }, func(c *config.StoryConfig, v string) { c.GenderBias = v }, nil, nil},
	{"locations_enabled", "Locations enabled", "启用地点实体", func(s config.StoryConfig) string { return strconv.FormatBool(s.LocationsEnabled) }, nil, func(c *config.StoryConfig, v bool) { c.LocationsEnabled = v }, nil},
	{"character_arcs_enabled", "Character arcs enabled", "启用角色弧光", func(s config.StoryConfig) string { return strconv.FormatBool(s.CharacterArcsEnabled) }, nil, func(c *config.StoryConfig, v bool) { c.CharacterArcsEnabled = v }, nil},
	{"target_words_per_chapter", "Target words per chapter", "每章目标字数", func(s config.StoryConfig) string { return strconv.Itoa(s.TargetWordsPerChapter) }, nil, nil, func(c *config.StoryConfig, v int) { c.TargetWordsPerChapter = v }},
	{"writing_style", "Writing style", "写作风格", func(s config.StoryConfig) string { return s.WritingStyle }, func(c *config.StoryConfig, v string) { c.WritingStyle = v }, nil, nil},
	{"writing_pov", "Narration POV", "叙述视角", func(s config.StoryConfig) string { return s.WritingPOV }, func(c *config.StoryConfig, v string) { c.WritingPOV = v }, nil, nil},
}

var storyFieldSpecByKey = func() map[string]storyFieldSpec {
	m := make(map[string]storyFieldSpec, len(storyFieldSpecs))
	for _, sp := range storyFieldSpecs {
		m[sp.Key] = sp
	}
	return m
}()

// StoryFieldSpec is the exported view of a StoryConfig field's typed accessors.
type StoryFieldSpec struct {
	Key     string
	EN      string
	ZH      string
	Get     func(config.StoryConfig) string
	SetStr  func(*config.StoryConfig, string)
	SetBool func(*config.StoryConfig, bool)
	SetInt  func(*config.StoryConfig, int)
}

// LookupStoryFieldSpec returns the typed accessor spec for a StoryConfig JSON
// key (e.g. "story_idea", "theme"), so tools can read/write any field generically.
func LookupStoryFieldSpec(key string) (StoryFieldSpec, bool) {
	sp, ok := storyFieldSpecByKey[key]
	if !ok {
		return StoryFieldSpec{}, false
	}
	return StoryFieldSpec(sp), true
}

// StoryFieldLabel returns the localized display label for a story-config field
// key; unknown keys fall back to the raw key.
func StoryFieldLabel(field, lang string) string {
	sp, ok := storyFieldSpecByKey[field]
	if !ok {
		return field
	}
	if i18n.NormalizeLanguage(lang) == i18n.LangEN {
		return sp.EN
	}
	return sp.ZH
}

// FormatStoryConfigForPrompt renders all non-empty story-config fields as
// "- key: value" lines so the chat model always sees the current form values
// (including the story idea) without needing a tool call.
func FormatStoryConfigForPrompt(story config.StoryConfig, lang string) string {
	en := i18n.NormalizeLanguage(lang) == i18n.LangEN
	var sb strings.Builder
	for _, sp := range storyFieldSpecs {
		v := trimmed(sp.Get(story))
		if v == "" || v == "false" || v == "0" {
			continue
		}
		label := sp.EN
		if !en {
			label = sp.ZH
		}
		sb.WriteString(fmt.Sprintf("- %s: %s\n", label, v))
	}
	return sb.String()
}

// protectedStoryFields lists the user-editable fields that must never be
// silently overwritten by generated content. Every StoryConfig field is now
// covered so conflicts surface for story_idea/theme/tone/... too.
var protectedStoryFields = func() []string {
	out := make([]string, 0, len(storyFieldSpecs))
	for _, sp := range storyFieldSpecs {
		out = append(out, sp.Key)
	}
	return out
}()

type ConfigFieldChange struct {
	Field    string `json:"field"`
	Current  string `json:"current"`
	Proposed string `json:"proposed"`
	Source   string `json:"source"`
	Reason   string `json:"reason,omitempty"`
}

type PendingConfigChanges struct {
	Changes []ConfigFieldChange `json:"changes"`
}

func PendingConfigChangesPath(progressPath string) string {
	return filepath.Join(filepath.Dir(progressPath), "pending_config_changes.json")
}

func storyFieldValue(story config.StoryConfig, field string) string {
	if sp, ok := storyFieldSpecByKey[field]; ok {
		return sp.Get(story)
	}
	return ""
}

func setStoryFieldValue(story *config.StoryConfig, field, value string) {
	sp, ok := storyFieldSpecByKey[field]
	if !ok {
		return
	}
	if sp.SetStr != nil {
		sp.SetStr(story, value)
		return
	}
	if sp.SetBool != nil {
		sp.SetBool(story, strings.EqualFold(strings.TrimSpace(value), "true"))
		return
	}
	if sp.SetInt != nil {
		if n, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
			sp.SetInt(story, n)
		}
	}
}

func isUserFilledStoryField(story config.StoryConfig, field string) bool {
	return strings.TrimSpace(storyFieldValue(story, field)) != ""
}

func storyFieldsEqual(a, b string) bool {
	return strings.TrimSpace(a) == strings.TrimSpace(b)
}

func storyConfigFromOutline(resp OutlineResponse, current config.StoryConfig) config.StoryConfig {
	proposed := current
	if resp.Title != "" {
		proposed.Title = resp.Title
	}
	return proposed
}

func storyConfigFromReconciliation(result ReconciliationResult, base config.StoryConfig) config.StoryConfig {
	adjusted := base
	if result.Type != "" {
		adjusted.Type = result.Type
	}
	if result.WritingStyle != "" {
		adjusted.WritingStyle = result.WritingStyle
	}
	if result.WritingPOV != "" {
		adjusted.WritingPOV = result.WritingPOV
	}
	return adjusted
}

func CollectStoryConfigConflicts(current, proposed config.StoryConfig, source, reason string) []ConfigFieldChange {
	var conflicts []ConfigFieldChange
	for _, field := range protectedStoryFields {
		prop := storyFieldValue(proposed, field)
		if prop == "" {
			continue
		}
		cur := storyFieldValue(current, field)
		if isUserFilledStoryField(current, field) && !storyFieldsEqual(cur, prop) {
			conflicts = append(conflicts, ConfigFieldChange{
				Field:    field,
				Current:  cur,
				Proposed: prop,
				Source:   source,
				Reason:   reason,
			})
		}
	}
	return conflicts
}

func applyStoryConfigMerge(current, proposed config.StoryConfig, allowFields map[string]bool) config.StoryConfig {
	merged := current
	for _, field := range protectedStoryFields {
		prop := storyFieldValue(proposed, field)
		if prop == "" {
			continue
		}
		if allowFields != nil && allowFields[field] {
			setStoryFieldValue(&merged, field, prop)
			continue
		}
		if !isUserFilledStoryField(current, field) {
			setStoryFieldValue(&merged, field, prop)
		}
	}
	return merged
}

func mergePending(existing, incoming []ConfigFieldChange) []ConfigFieldChange {
	byField := make(map[string]ConfigFieldChange, len(existing)+len(incoming))
	for _, c := range existing {
		byField[c.Field] = c
	}
	for _, c := range incoming {
		byField[c.Field] = c
	}
	out := make([]ConfigFieldChange, 0, len(byField))
	for _, field := range protectedStoryFields {
		if c, ok := byField[field]; ok {
			out = append(out, c)
			delete(byField, field)
		}
	}
	for _, c := range byField {
		out = append(out, c)
	}
	return out
}

func LoadPendingConfigChanges(path string) (*PendingConfigChanges, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &PendingConfigChanges{}, nil
		}
		return nil, err
	}
	var pending PendingConfigChanges
	if err := json.Unmarshal(data, &pending); err != nil {
		return nil, err
	}
	if pending.Changes == nil {
		pending.Changes = []ConfigFieldChange{}
	}
	return &pending, nil
}

func SavePendingConfigChanges(path string, pending *PendingConfigChanges) error {
	if pending == nil || len(pending.Changes) == 0 {
		if err := fsutil.Delete(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	data, err := json.MarshalIndent(pending, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(path, data)
}

func appendPendingChanges(pendingPath string, incoming []ConfigFieldChange, logger *sse.LogBroadcaster) error {
	if len(incoming) == 0 {
		return nil
	}
	existing, err := LoadPendingConfigChanges(pendingPath)
	if err != nil {
		return err
	}
	existing.Changes = mergePending(existing.Changes, incoming)
	if err := SavePendingConfigChanges(pendingPath, existing); err != nil {
		return err
	}
	if logger != nil {
		logger.ConfigChangeProposal(existing.Changes)
	}
	return nil
}

func RemovePendingFields(pendingPath string, fields ...string) error {
	pending, err := LoadPendingConfigChanges(pendingPath)
	if err != nil {
		return err
	}
	if len(pending.Changes) == 0 {
		return nil
	}
	remove := make(map[string]bool, len(fields))
	for _, f := range fields {
		remove[f] = true
	}
	kept := make([]ConfigFieldChange, 0, len(pending.Changes))
	for _, c := range pending.Changes {
		if !remove[c.Field] {
			kept = append(kept, c)
		}
	}
	pending.Changes = kept
	return SavePendingConfigChanges(pendingPath, pending)
}

func SyncProgressMetaFromStory(state *Progress, story config.StoryConfig) {
	if story.Title != "" {
		state.Title = story.Title
	}
}

func applyOutlineMetaWithGuard(cfg *config.Config, state *Progress, resp OutlineResponse, source, pendingPath, cfgPath string, logger *sse.LogBroadcaster) error {
	proposed := storyConfigFromOutline(resp, cfg.Story)
	conflicts := CollectStoryConfigConflicts(cfg.Story, proposed, source, "")

	cfg.Story = applyStoryConfigMerge(cfg.Story, proposed, nil)
	SyncProgressMetaFromStory(state, cfg.Story)
	if resp.CorePrompt != "" {
		state.CorePrompt = resp.CorePrompt
	}

	if err := config.SaveConfig(cfgPath, cfg); err != nil {
		return fmt.Errorf("保存配置失败: %w", err)
	}
	return appendPendingChanges(pendingPath, conflicts, logger)
}

func FormatConfigConflictMessage(conflicts []ConfigFieldChange, lang string) string {
	en := i18n.NormalizeLanguage(lang) == i18n.LangEN
	var sb strings.Builder
	if en {
		sb.WriteString("Cannot overwrite user-filled config fields without explicit consent. Conflicts:\n")
	} else {
		sb.WriteString("无法覆盖用户已填写的配置字段，需先征得用户同意。冲突字段：\n")
	}
	for _, c := range conflicts {
		sb.WriteString(fmt.Sprintf("- %s (%s): current=%q proposed=%q\n", c.Field, StoryFieldLabel(c.Field, lang), c.Current, c.Proposed))
	}
	if en {
		sb.WriteString("Explain the changes to the user and wait for explicit approval, then retry update_project_config with confirm_overwrite=true.")
	} else {
		sb.WriteString("请向用户说明变更理由并等待明确同意，然后带 confirm_overwrite=true 重试 update_project_config。")
	}
	return sb.String()
}

func ApplySelectedPendingChanges(cfg *config.Config, state *Progress, pending *PendingConfigChanges, fields []string) {
	allow := make(map[string]bool, len(fields))
	for _, f := range fields {
		allow[f] = true
	}
	proposed := cfg.Story
	for _, change := range pending.Changes {
		if allow[change.Field] {
			setStoryFieldValue(&proposed, change.Field, change.Proposed)
		}
	}
	cfg.Story = proposed
	SyncProgressMetaFromStory(state, cfg.Story)
	snapshot := cfg.Story
	state.StoryConfigSnapshot = &snapshot
}
