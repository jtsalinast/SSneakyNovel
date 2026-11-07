package story

import (
	"encoding/json"
	"fmt"
	"os"
	"showmethestory/internal/fsutil"
	"showmethestory/internal/i18n"
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
	// Aliases are the other names the prose may use for this character
	// (nicknames, titles, short forms). They keep fact progression attached to
	// one entity and are extended by the setting sync when prose invents one.
	Aliases []string `json:"aliases,omitempty"`
	// Protagonist arc quartet (classic want / need / wound / agency model).
	// Optional, author-editable; the generator fills them when character arcs
	// are enabled. Kept separate from goals/flaws/strengths/arc because those
	// are free-form, while these four have fixed dramatic roles.
	Want   string `json:"want,omitempty"`   // conscious want: what they chase in the outer world
	Need   string `json:"need,omitempty"`   // unconscious need: the inner truth they must accept to grow
	Wound  string `json:"wound,omitempty"`  // primary wound / lie: trauma or false belief conditioning their choices
	Agency string `json:"agency,omitempty"` // edge: physical trait, intellect or skill that makes them capable
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
	// Enriched dramatic sheet: structural impact, page presence and narrative
	// function. Enum values are validated by the API, never free text.
	Relevance   string `json:"relevance,omitempty"`   // fundamental | secondary | contextual
	Presence    string `json:"presence,omitempty"`    // main | recurring | incidental | shadow
	Affiliation string `json:"affiliation,omitempty"` // protagonist | antagonist | ally | neutral | foil
	// Story Web quest threads: what this character is chasing (main), the
	// subplots they drag along (minor) and the theme that always stays in the
	// background (overarching). Each records whether the book resolves it.
	MainQuest           string           `json:"main_quest,omitempty"`
	MainResolved        bool             `json:"main_resolved,omitempty"`
	MinorQuests         []CharacterQuest `json:"minor_quests,omitempty"`
	OverarchingQuest    string           `json:"overarching_quest,omitempty"`
	OverarchingResolved bool             `json:"overarching_resolved,omitempty"`
}

// CharacterQuest is one subplot thread owned by a character. Resolved records
// whether the book closes it.
type CharacterQuest struct {
	ID       string `json:"id,omitempty"`
	Text     string `json:"text"`
	Resolved bool   `json:"resolved,omitempty"`
}

// PresenceMain marks the characters the Story Web opens with.
const PresenceMain = "main"

// HasQuests reports whether any Story Web thread is filled in.
func (c Character) HasQuests() bool {
	return strings.TrimSpace(c.MainQuest) != "" ||
		strings.TrimSpace(c.OverarchingQuest) != "" ||
		len(c.MinorQuests) > 0
}

// CharacterQuestsBlock renders the Story Web threads as prompt material for
// pre-planning and outline generation. It returns "" when no character has
// quests, so callers can append it blindly.
func CharacterQuestsBlock(ps *ProjectSettings, lang string) string {
	if ps == nil {
		return ""
	}
	en := i18n.NormalizeLanguage(lang) == i18n.LangEN
	status := func(resolved bool) string {
		if resolved {
			return pick2(en, "RESUELTA", "RESOLVED")
		}
		return pick2(en, "ABIERTA", "OPEN")
	}
	var lines []string
	for _, c := range ps.Characters {
		if !c.HasQuests() {
			continue
		}
		name := StripNameMarks(c.Name)
		role := strings.TrimSpace(strings.Join(nonEmpty(c.Affiliation, c.Relevance, c.Presence), "/"))
		line := "- " + name
		if role != "" {
			line += " (" + role + ")"
		}
		if q := strings.TrimSpace(c.MainQuest); q != "" {
			line += pick2(en, "; trama principal: ", "; main quest: ") + q + " (" + status(c.MainResolved) + ")"
		}
		if len(c.MinorQuests) > 0 {
			var minors []string
			for _, q := range c.MinorQuests {
				if t := strings.TrimSpace(q.Text); t != "" {
					minors = append(minors, t+" ("+status(q.Resolved)+")")
				}
			}
			if len(minors) > 0 {
				line += pick2(en, "; subtramas: ", "; minor quests: ") + strings.Join(minors, " | ")
			}
		}
		if q := strings.TrimSpace(c.OverarchingQuest); q != "" {
			line += pick2(en, "; trama de fondo: ", "; overarching quest: ") + q + " (" + status(c.OverarchingResolved) + ")"
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return ""
	}
	var b strings.Builder
	if en {
		b.WriteString("[STORY WEB — CHARACTER QUEST THREADS]\n")
		b.WriteString("These threads are the base for the act breakdown and the chapter outlines: build the main plot from the main quests, the subplots from the minor quests, and keep the overarching quests as the thematic throughline. Respect each thread's RESOLVED/OPEN status.\n")
	} else {
		b.WriteString("[TRAMA DE PERSONAJES — HILOS DE QUESTS]\n")
		b.WriteString("Estos hilos son la base para los actos y los outlines de capítulos: construye la trama principal con las quests principales, las subtramas con las quests menores y mantén las quests de fondo como hilo temático. Respeta el estado RESUELTA/ABIERTA de cada hilo.\n")
	}
	b.WriteString(strings.Join(lines, "\n"))
	return b.String()
}

func nonEmpty(values ...string) []string {
	var out []string
	for _, v := range values {
		if t := strings.TrimSpace(v); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func pick2(en bool, es, in string) string {
	if en {
		return in
	}
	return es
}

// NormalizeCharacterQuests trims every thread, drops empty minor quests, clears
// resolution flags on empty quests and reassigns minor IDs deterministically.
// The Story Web modal saves through this so removal and unchecking persist.
func NormalizeCharacterQuests(mainQuest string, mainResolved bool, minors []CharacterQuest, overQuest string, overResolved bool) (string, bool, []CharacterQuest, string, bool) {
	mainQuest = strings.TrimSpace(mainQuest)
	overQuest = strings.TrimSpace(overQuest)
	out := make([]CharacterQuest, 0, len(minors))
	for _, q := range minors {
		text := strings.TrimSpace(q.Text)
		if text == "" {
			continue
		}
		out = append(out, CharacterQuest{ID: fmt.Sprintf("q_%d", len(out)+1), Text: text, Resolved: q.Resolved})
	}
	return mainQuest, mainResolved && mainQuest != "", out, overQuest, overResolved && overQuest != ""
}

type WorldviewEntry struct {
	ID          string   `json:"id"`
	Category    string   `json:"category"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        string   `json:"tags,omitempty"`
	Aliases     []string `json:"aliases,omitempty"`
}

type Organization struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Members     []string `json:"members,omitempty"`
	Locations   []string `json:"locations,omitempty"` // location (worldview, category "location") IDs this org operates in
	Aliases     []string `json:"aliases,omitempty"`
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
	// Aesthetic is the world's visual/tonal reference (Wes Anderson, Burton,
	// Sabrina, A Series of Unfortunate Events…). It is a project-level prompt
	// lens, not a lore entry, so it applies to every generation step.
	Aesthetic string `json:"aesthetic,omitempty"`
}

func LoadProjectSettings(path string) (*ProjectSettings, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &ProjectSettings{}, nil
		}
		return nil, fmt.Errorf("leer entrada: %w", err)
	}

	var ps ProjectSettings
	if err := json.Unmarshal(data, &ps); err != nil {
		return nil, fmt.Errorf("analizar entrada: %w", err)
	}

	return &ps, nil
}

func SaveProjectSettings(path string, ps *ProjectSettings) error {
	data, err := json.MarshalIndent(ps, "", "  ")
	if err != nil {
		return fmt.Errorf("orderrow: %w", err)
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

// StripNameMarks removes "" wrapping from a name string.
// ""forestsmallbright"" → "forestsmallbright", "forestsmallbright" → "forestsmallbright" (unchanged)
func StripNameMarks(name string) string {
	runes := []rune(name)
	if len(runes) >= 2 && runes[0] == '「' && runes[len(runes)-1] == '」' {
		return string(runes[1 : len(runes)-1])
	}
	return name
}

// NormalizeAliases trims, dedupes (case-insensitive) and drops empty aliases.
func NormalizeAliases(in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, a := range in {
		a = strings.TrimSpace(a)
		key := strings.ToLower(a)
		if a == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, a)
	}
	return out
}

// WorldbuildingAestheticBlock renders the project's visual/tonal reference as
// prompt material. It returns "" when unset, so callers can append it blindly.
func WorldbuildingAestheticBlock(ps *ProjectSettings, lang string) string {
	if ps == nil {
		return ""
	}
	aesthetic := strings.TrimSpace(ps.Aesthetic)
	if aesthetic == "" {
		return ""
	}
	if i18n.NormalizeLanguage(lang) == i18n.LangEN {
		return "[WORLD AESTHETIC]\n" + aesthetic + "\nApply this as the visual, tonal and stylistic lens of every location, costume, object, character description and scene rhythm. Translate the references into original details; never quote or copy the source works."
	}
	return "[ESTÉTICA DEL MUNDO]\n" + aesthetic + "\nAplícala como lente visual, tonal y estilística de cada lugar, vestuario, objeto, descripción de personaje y ritmo de escena. Traduce las referencias a detalles originales; nunca cites ni copies las obras de referencia."
}
