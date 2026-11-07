package story

import (
	"encoding/json"
	"fmt"
	"strings"
)

// BrainstormEntity is one Lore record offered by the Commune & Consult picker.
type BrainstormEntity struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Name     string `json:"name"`
	Subtitle string `json:"subtitle,omitempty"`
}

func brainstormKindKey(kind string) string {
	switch kind {
	case "character":
		return "characters"
	case "location", "worldview":
		return "worldview"
	case "organization":
		return "organizations"
	}
	return ""
}

// BrainstormEntities lists the Lore records the author can talk to. Relations
// are excluded: they are links, not entities.
func BrainstormEntities(s *ProjectSettings) []BrainstormEntity {
	out := []BrainstormEntity{}
	if s == nil {
		return out
	}
	for _, c := range s.Characters {
		out = append(out, BrainstormEntity{Kind: "character", ID: c.ID, Name: c.Name, Subtitle: strings.TrimSpace(c.Personality)})
	}
	for _, w := range s.Worldview {
		kind := "worldview"
		if w.Category == WorldviewCategoryLocation {
			kind = "location"
		}
		out = append(out, BrainstormEntity{Kind: kind, ID: w.ID, Name: w.Name, Subtitle: w.Category})
	}
	for _, o := range s.Organizations {
		out = append(out, BrainstormEntity{Kind: "organization", ID: o.ID, Name: o.Name, Subtitle: o.Type})
	}
	return out
}

// BrainstormLoreContext retrieves the Lore records relevant to the author's
// query plus facts recorded up to the latest chapter. It reuses the same BM25
// retrieval and budget as chapter writing so a long book never dumps the whole
// registry into one brainstorm prompt.
func BrainstormLoreContext(s *ProjectSettings, state *Progress, query, lang string) string {
	if s == nil {
		return ""
	}
	selected, _ := retrieveSettings(s, query, settingsContextRunes)
	data, _ := json.Marshal(settingsEntities(selected))
	var b strings.Builder
	b.WriteString(knowledgeSelectionNotice(lang))
	b.Write(data)
	if state != nil {
		num := len(state.Chapters)
		if num == 0 {
			num = 1
		}
		entries := retrieveMemories(state, ChapterState{Num: num}, memoryContextRunes/2, false)
		if len(entries) > 0 {
			mem, _ := json.Marshal(entries)
			b.WriteString("\n[Facts recorded so far]\n")
			b.Write(mem)
		}
	}
	return b.String()
}

// BrainstormEntityContext returns one entity plus its relations and every fact
// recorded so far that names it, so Commune can roleplay from canon instead of
// guessing.
func BrainstormEntityContext(s *ProjectSettings, state *Progress, kind, id, lang string) (BrainstormEntity, string) {
	key := brainstormKindKey(kind)
	if key == "" || s == nil {
		return BrainstormEntity{}, ""
	}
	ent := entityAt(s, key, id)
	if ent == nil {
		return BrainstormEntity{}, ""
	}
	name, _ := ent["name"].(string)
	entity := BrainstormEntity{Kind: kind, ID: id, Name: name}
	data, _ := json.Marshal(ent)
	var b strings.Builder
	b.WriteString(knowledgeSelectionNotice(lang))
	b.Write(data)
	for _, r := range s.Relations {
		if r.SourceID != id && r.TargetID != id {
			continue
		}
		other := r.SourceID
		if other == id {
			other = r.TargetID
		}
		b.WriteString(fmt.Sprintf("\n[Relation] %s — %s — %s", r.Label, name, brainstormEntityName(s, other)))
	}
	if state != nil && name != "" {
		lower := strings.ToLower(name)
		facts := []string{}
		for _, m := range state.MemoryEntries {
			if strings.Contains(strings.ToLower(m.Content), lower) {
				facts = append(facts, fmt.Sprintf("[%s] %s", m.Category, m.Content))
			}
		}
		if len(facts) > 40 {
			facts = facts[len(facts)-40:]
		}
		if len(facts) > 0 {
			b.WriteString("\n[Facts recorded so far]\n")
			b.WriteString(strings.Join(facts, "\n"))
		}
	}
	return entity, b.String()
}

func brainstormEntityName(s *ProjectSettings, id string) string {
	for _, k := range []string{"characters", "worldview", "organizations"} {
		if e := entityAt(s, k, id); e != nil {
			if n, ok := e["name"].(string); ok && n != "" {
				return n
			}
		}
	}
	return id
}
