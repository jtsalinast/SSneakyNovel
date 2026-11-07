package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"showmethestory/internal/config"
	"showmethestory/internal/i18n"
	"showmethestory/internal/llm"
	"showmethestory/internal/story"
)

// —— Story Web: character quest threads ——
//
// The Story Web precedes pre-planning: every main-presence character owns a
// main quest (main plot), several minor quests (subplots) and one overarching
// quest (thematic throughline), each flagged resolved or open. The threads are
// persisted on the character (settings.json) and injected into pre-planning
// and every outline batch/revision prompt.

// characterEnums guards the three fixed dramatic-role fields. Empty means
// "not set yet" and stays allowed.
var characterEnums = map[string]map[string]bool{
	"relevance":   {"fundamental": true, "secondary": true, "contextual": true},
	"presence":    {"main": true, "recurring": true, "incidental": true, "shadow": true},
	"affiliation": {"protagonist": true, "antagonist": true, "ally": true, "neutral": true, "foil": true},
}

func validCharacterEnum(field, value string) bool {
	return value == "" || characterEnums[field][value]
}

// PutCharacterQuests replaces a character's Story Web threads. Unlike
// PutCharacter (which only applies non-empty values and therefore cannot clear
// a field), this endpoint is authoritative: empty quests are removed, flags are
// stored as sent, and minor-quest IDs are reassigned deterministically.
func (h *Handlers) PutCharacterQuests(w http.ResponseWriter, r *http.Request) {
	if h.rejectIfTaskRunning(w, r) {
		return
	}
	id := r.PathValue("id")
	var req struct {
		MainQuest           string                 `json:"main_quest"`
		MainResolved        bool                   `json:"main_resolved"`
		MinorQuests         []story.CharacterQuest `json:"minor_quests"`
		OverarchingQuest    string                 `json:"overarching_quest"`
		OverarchingResolved bool                   `json:"overarching_resolved"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeErrorReq(w, r, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	for i := range h.settings.Characters {
		c := &h.settings.Characters[i]
		if c.ID != id {
			continue
		}
		c.MainQuest, c.MainResolved, c.MinorQuests, c.OverarchingQuest, c.OverarchingResolved =
			story.NormalizeCharacterQuests(req.MainQuest, req.MainResolved, req.MinorQuests, req.OverarchingQuest, req.OverarchingResolved)
		if err := story.SaveProjectSettings(h.settingsPath, h.settings); err != nil {
			h.writeErrorReq(w, r, http.StatusInternalServerError, "save_failed", err)
			return
		}
		h.writeJSON(w, http.StatusOK, *c)
		return
	}
	h.writeErrorReq(w, r, http.StatusNotFound, "character_not_found")
}

// PostCharacterQuestsGenerate proposes the three thread types for one
// character. It persists nothing: the modal keeps the result as unsaved form
// state so the author can edit it and press Save, exactly like EnhanceField.
func (h *Handlers) PostCharacterQuestsGenerate(w http.ResponseWriter, r *http.Request) {
	if !h.ensureProject(w, r) {
		return
	}
	id := r.PathValue("id")
	var char *story.Character
	for i := range h.settings.Characters {
		if h.settings.Characters[i].ID == id {
			char = &h.settings.Characters[i]
			break
		}
	}
	if char == nil {
		h.writeErrorReq(w, r, http.StatusNotFound, "character_not_found")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()
	content := llm.CallAPIWithRetryLog(ctx, h.apiCfg, i18n.SystemPromptFor(h.cfg.Language, "author_default"), buildCharacterQuestsPrompt(h.cfg.Story, char, h.settings, h.cfg.Language), h.logger)
	var out struct {
		MainQuest        string   `json:"main_quest"`
		MinorQuests      []string `json:"minor_quests"`
		OverarchingQuest string   `json:"overarching_quest"`
	}
	if j := llm.ExtractJSON(content); j != "" {
		_ = json.Unmarshal([]byte(j), &out)
	}
	main := strings.TrimSpace(out.MainQuest)
	over := strings.TrimSpace(out.OverarchingQuest)
	var minors []story.CharacterQuest
	for _, q := range out.MinorQuests {
		if t := strings.TrimSpace(q); t != "" {
			minors = append(minors, story.CharacterQuest{Text: t})
		}
	}
	if main == "" && over == "" && len(minors) == 0 {
		h.writeErrorReq(w, r, http.StatusBadGateway, "empty_generation", "")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{
		"main_quest":        main,
		"minor_quests":      minors,
		"overarching_quest": over,
	})
}

func buildCharacterQuestsPrompt(sc config.StoryConfig, c *story.Character, ps *story.ProjectSettings, lang string) string {
	var b strings.Builder
	b.WriteString("You are a story architect building one character's Story Web threads before pre-planning. These threads become the base material for the acts and the chapter outlines.\n")
	cfg := &config.Config{Language: lang}
	cfg.Story = sc
	if np := story.NovelParametersBlock(cfg); np != "" {
		b.WriteString(np)
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "CHARACTER\nName: %s\n", story.StripNameMarks(c.Name))
	writeLine := func(label, value string) {
		if v := strings.TrimSpace(value); v != "" {
			fmt.Fprintf(&b, "%s: %s\n", label, v)
		}
	}
	writeLine("Dramatic role", strings.Join(nonEmptyStrings(c.Affiliation, c.Relevance, c.Presence), " / "))
	writeLine("Personality", c.Personality)
	writeLine("Background", c.Background)
	writeLine("Motivation", c.Motivation)
	writeLine("Conscious want", c.Want)
	writeLine("Unconscious need", c.Need)
	writeLine("Wound / lie", c.Wound)
	writeLine("Agency", c.Agency)
	writeLine("Goals", c.Goals)
	writeLine("Flaws", c.Flaws)
	writeLine("Arc", c.Arc)
	if ps != nil {
		var others []string
		for _, o := range ps.Characters {
			if o.ID == c.ID || strings.TrimSpace(o.Name) == "" {
				continue
			}
			role := strings.Join(nonEmptyStrings(o.Affiliation, o.Presence), "/")
			if role != "" {
				others = append(others, story.StripNameMarks(o.Name)+" ("+role+")")
			} else {
				others = append(others, story.StripNameMarks(o.Name))
			}
		}
		if len(others) > 0 {
			b.WriteString("Other characters (make the threads interlock with them):\n- " + strings.Join(others, "\n- ") + "\n")
		}
	}
	b.WriteString("Requirements:\n")
	b.WriteString("- main_quest: the concrete goal that drives this character's part of the plot (one active-voice sentence).\n")
	b.WriteString("- minor_quests: 2 to 4 subplots this character drags along (debts, secrets, allies, side goals); each one sentence and clearly distinct from the main quest.\n")
	b.WriteString("- overarching_quest: the thematic question that stays in the background for this character for the whole book (one sentence).\n")
	b.WriteString("All text in the output language pinned above.\n")
	b.WriteString(jsonRule(lang, `{"main_quest": "...", "minor_quests": ["...", "..."], "overarching_quest": "..."}`))
	return b.String()
}

func nonEmptyStrings(values ...string) []string {
	var out []string
	for _, v := range values {
		if t := strings.TrimSpace(v); t != "" {
			out = append(out, t)
		}
	}
	return out
}
