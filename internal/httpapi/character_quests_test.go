package httpapi

import (
	"strings"
	"testing"

	"showmethestory/internal/config"
	"showmethestory/internal/story"
)

func TestCharacterQuestsPromptCarriesSheetAndSchema(t *testing.T) {
	sc := config.StoryConfig{Type: "fantasy", OutputLanguage: "en"}
	c := &story.Character{ID: "c_1", Name: "Ana", Affiliation: "protagonist", Presence: "main", Motivation: "clear her name"}
	ps := &story.ProjectSettings{Characters: []story.Character{*c, {ID: "c_2", Name: "Borin", Affiliation: "antagonist", Presence: "recurring"}}}
	prompt := buildCharacterQuestsPrompt(sc, c, ps, "en")
	for _, want := range []string{"Ana", "clear her name", "Borin (antagonist/recurring)", "main_quest", "minor_quests", "overarching_quest", "output language"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("quest prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestValidCharacterEnum(t *testing.T) {
	cases := []struct {
		field, value string
		ok           bool
	}{
		{"presence", "", true},
		{"presence", "main", true},
		{"presence", "shadow", true},
		{"presence", "Main", false},
		{"affiliation", "foil", true},
		{"affiliation", "villain", false},
		{"relevance", "contextual", true},
		{"relevance", "critical", false},
	}
	for _, tc := range cases {
		if got := validCharacterEnum(tc.field, tc.value); got != tc.ok {
			t.Fatalf("validCharacterEnum(%q, %q) = %v, want %v", tc.field, tc.value, got, tc.ok)
		}
	}
}
