package story

import (
	"encoding/json"
	"fmt"
	"os"
	"showmethestory/internal/fsutil"
	"strconv"
	"strings"
)

type Character struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Age         string `json:"age,omitempty"`
	Appearance  string `json:"appearance,omitempty"`
	Personality string `json:"personality,omitempty"`
	Background  string `json:"background,omitempty"`
	Motivation  string `json:"motivation,omitempty"`
	Abilities   string `json:"abilities,omitempty"`
	Notes       string `json:"notes,omitempty"`
	// Character-arc fields (borrowed from NovelWriter's lore character sheet:
	// goals/motivations/flaws/strengths/arc). Optional; filled by the AI
	// generator when the story config enables character arcs.
	Goals     string `json:"goals,omitempty"`
	Flaws     string `json:"flaws,omitempty"`
	Strengths string `json:"strengths,omitempty"`
	Arc       string `json:"arc,omitempty"`
	// Genre-specific fields (e.g. magic_school for fantasy, rank for scifi),
	// suggested by the AI generator according to story type/subgenre. Optional;
	// rendered in the UI via a dynamic key/value list.
	Extra map[string]string `json:"extra,omitempty"`
}

type WorldviewEntry struct {
	ID          string `json:"id"`
	Category    string `json:"category"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Tags        string `json:"tags,omitempty"`
}

type Organization struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Members     []string `json:"members,omitempty"`
	Locations   []string `json:"locations,omitempty"` // location (worldview, category "location") IDs this org operates in
}

// Location is a first-class place/scene entity (borrowed from NovelWriter's
// locations worldbuilding entries). Locations are stored as worldview entries
// with Category == "location" so they reuse the existing worldview CRUD,
// filters and injection; helper accessors live below.
type Location struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Tags        string `json:"tags,omitempty"`
}

const WorldviewCategoryLocation = "location"

// Locations returns the worldview entries that are places/scenes.
func (ps *ProjectSettings) Locations() []WorldviewEntry {
	var out []WorldviewEntry
	for _, w := range ps.Worldview {
		if w.Category == WorldviewCategoryLocation {
			out = append(out, w)
		}
	}
	return out
}

type Relation struct {
	ID         string `json:"id"`
	SourceID   string `json:"source_id"`
	SourceType string `json:"source_type"`
	TargetID   string `json:"target_id"`
	TargetType string `json:"target_type"`
	Label      string `json:"label"`
}

type ProjectSettings struct {
	StoryChanges  []SettingChange  `json:"story_changes,omitempty"`
	StorySynced   map[int]string   `json:"story_synced,omitempty"`
	Characters    []Character      `json:"characters"`
	Worldview     []WorldviewEntry `json:"worldview"`
	Organizations []Organization   `json:"organizations"`
	Relations     []Relation       `json:"relations"`
}

func LoadProjectSettings(path string) (*ProjectSettings, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &ProjectSettings{}, nil
		}
		return nil, fmt.Errorf("读取设定文件失败: %w", err)
	}

	var ps ProjectSettings
	if err := json.Unmarshal(data, &ps); err != nil {
		return nil, fmt.Errorf("解析设定文件失败: %w", err)
	}

	return &ps, nil
}

func SaveProjectSettings(path string, ps *ProjectSettings) error {
	data, err := json.MarshalIndent(ps, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化设定失败: %w", err)
	}
	return fsutil.WriteFileAtomic(path, data)
}

func nextID(prefix string, existingIDs []string) string {
	maxNum := 0
	for _, id := range existingIDs {
		if strings.HasPrefix(id, prefix+"_") {
			numStr := strings.TrimPrefix(id, prefix+"_")
			if n, err := strconv.Atoi(numStr); err == nil && n > maxNum {
				maxNum = n
			}
		}
	}
	return fmt.Sprintf("%s_%d", prefix, maxNum+1)
}

func (ps *ProjectSettings) allIDs() []string {
	var ids []string
	for _, c := range ps.StoryChanges {
		ids = append(ids, c.EntityID)
	}
	for _, c := range ps.Characters {
		ids = append(ids, c.ID)
	}
	for _, w := range ps.Worldview {
		ids = append(ids, w.ID)
	}
	for _, o := range ps.Organizations {
		ids = append(ids, o.ID)
	}
	for _, r := range ps.Relations {
		ids = append(ids, r.ID)
	}
	return ids
}

func (ps *ProjectSettings) NextCharacterID() string {
	return nextID("c", ps.allIDs())
}

func (ps *ProjectSettings) NextWorldviewID() string {
	return nextID("w", ps.allIDs())
}

func (ps *ProjectSettings) NextOrganizationID() string {
	return nextID("o", ps.allIDs())
}

func (ps *ProjectSettings) NextRelationID() string {
	return nextID("r", ps.allIDs())
}

// StripNameMarks removes 「」 wrapping from a name string.
// "「林小明」" → "林小明", "林小明" → "林小明" (unchanged)
func StripNameMarks(name string) string {
	runes := []rune(name)
	if len(runes) >= 2 && runes[0] == '「' && runes[len(runes)-1] == '」' {
		return string(runes[1 : len(runes)-1])
	}
	return name
}
