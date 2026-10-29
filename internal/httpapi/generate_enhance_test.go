package httpapi

import (
	"fmt"
	"strings"
	"testing"

	"showmethestory/internal/agent"
	"showmethestory/internal/config"
	"showmethestory/internal/story"
)

func TestStoryEnhanceFieldRoundTrip(t *testing.T) {
	sc := &config.StoryConfig{}
	fields := []string{
		"theme", "motif", "philosophical_dilemma", "tone", "specific_settings",
		"inspirational_pieces", "audience_profile", "reader_expectation",
		"differentiating_idea", "story_idea", "forces_in_tension", "elevator_pitch",
		"lighthouse_sentence", "writing_style", "writing_pov",
	}
	for _, field := range fields {
		want := "draft-" + field
		if !setStoryField(sc, field, want) {
			t.Fatalf("setStoryField rejected %q", field)
		}
		if got := getStoryField(sc, field); got != want {
			t.Fatalf("getStoryField(%q) = %q, want %q", field, got, want)
		}
	}
	if setStoryField(sc, "title", "x") {
		t.Fatal("setStoryField must reject fields outside the enhance whitelist")
	}
	if _, ok := storyEnhanceFields["title"]; ok {
		t.Fatal("title must not be enhanceable")
	}
}

func TestEnhanceNotStartedByHTTPFastPath(t *testing.T) {
	enhance := agent.ParseGenerateSectionTag("[generate-section:enhance;field=tone]")
	if startForcedFastPath(enhance) {
		t.Fatal("enhance must be left to the agent loop's forced call")
	}
	style := agent.ParseGenerateSectionTag("[generate-section:style]")
	if !startForcedFastPath(style) {
		t.Fatal("other sections must still start from the HTTP fast path")
	}
}

func TestForcedSectionCreativityFromTag(t *testing.T) {
	call := agent.ParseGenerateSectionTag("suggest [generate-section:differentiating_idea] [creativity=4]")
	if got := forcedSectionCreativity(call); got != 4 {
		t.Fatalf("forcedSectionCreativity = %d, want 4", got)
	}
}

// The wand only polishes the author's draft: it must never carry a creativity
// level (the slider belongs to Suggest).
func TestEnhancePromptHasNoCreativityLevel(t *testing.T) {
	sc := &config.StoryConfig{Title: "T", Type: "fantasy", StoryIdea: "idea"}
	for _, field := range []string{"tone", "differentiating_idea", "theme"} {
		prompt := buildFieldEnhancePrompt(sc, "en", field, "draft")
		if strings.Contains(prompt, "Creativity level") || strings.Contains(prompt, "playbook") {
			t.Fatalf("%s enhance prompt must stay level-neutral:\n%s", field, prompt)
		}
	}
}

// Every creativity level must carry its own instruction block, not just a
// different adjective or temperature.
func TestCreativityPlaybooksPerLevel(t *testing.T) {
	seen := map[string]int{}
	for level := 1; level <= 5; level++ {
		idea := creativityIdeaPlaybook(level)
		for _, want := range []string{fmt.Sprintf("Level %d", level), "Forbidden:", "Shelf test:"} {
			if !strings.Contains(idea, want) {
				t.Fatalf("idea playbook %d missing %q:\n%s", level, want, idea)
			}
		}
		if prev, dup := seen[idea]; dup {
			t.Fatalf("idea playbooks for levels %d and %d are identical", prev, level)
		}
		seen[idea] = level
	}
	if !strings.Contains(creativityIdeaPlaybook(0), "Level 3") || !strings.Contains(creativityIdeaPlaybook(9), "Level 3") {
		t.Fatal("unknown levels must fall back to the Balanced playbook")
	}
}

func TestDifferentiatingPromptUsesLevelPlaybook(t *testing.T) {
	sc := &config.StoryConfig{Title: "T", Type: "fantasy", StoryIdea: "idea", Tone: "bleak"}
	safe := buildDifferentiatingPrompt(sc, "en", 1)
	bold := buildDifferentiatingPrompt(sc, "en", 5)
	for _, want := range []string{"Level 1 — Pragmatic playbook", "Forbidden:", "Shelf test:"} {
		if !strings.Contains(safe, want) {
			t.Fatalf("level 1 prompt missing %q", want)
		}
	}
	if !strings.Contains(bold, "Level 5 — Disruptive playbook") || !strings.Contains(bold, "Break ONE named rule") {
		t.Fatalf("level 5 prompt missing its playbook:\n%s", bold)
	}
	// Output shape and the cross-genre escalation are contract, not style.
	if !strings.Contains(bold, "ONE single sentence") {
		t.Fatal("prompt must ask for a single sentence")
	}
	if !strings.Contains(bold, "Transplant the mechanism of a field nobody in this genre has used") {
		t.Fatal("level 5 must force a foreign-field transplant")
	}
	if !strings.Contains(buildDifferentiatingPrompt(sc, "en", 4), "structural engine of ANOTHER genre") {
		t.Fatal("level 4 must force a cross-genre engine")
	}
	if !strings.Contains(bold, "three most predictable premises") {
		t.Fatal("every level must ban the shelf's default premises")
	}
	if safe == bold {
		t.Fatal("levels 1 and 5 must produce different prompts")
	}
}

func TestTextEnhancePromptAndParse(t *testing.T) {
	prompt := buildTextEnhancePrompt("en", "character personality", "grim and quiet")
	for _, want := range []string{"character personality", "grim and quiet", `"text"`} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("enhance prompt missing %q:\n%s", want, prompt)
		}
	}
	cases := map[string]string{
		"```json\n{\"text\":\"  sharper  \"}\n```": "sharper",
		"plain rewritten text":                     "plain rewritten text",
		"":                                         "",
	}
	for in, want := range cases {
		if got := parseEnhancedText(in); got != want {
			t.Fatalf("parseEnhancedText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseStoryFieldText(t *testing.T) {
	if got := parseStoryFieldText("```json\n{\"tone\":\"  bleak and lyrical  \"}\n```", "tone"); got != "bleak and lyrical" {
		t.Fatalf("json extraction = %q", got)
	}
	if got := parseStoryFieldText("plain rewritten text", "tone"); got != "plain rewritten text" {
		t.Fatalf("plain fallback = %q", got)
	}
	if got := parseStoryFieldText("", "tone"); got != "" {
		t.Fatalf("empty = %q", got)
	}
}

func TestMergeCharactersFillsArcQuartet(t *testing.T) {
	got := mergeCharacters(
		[]story.Character{{ID: "c_1", Name: "Mara"}},
		[]story.Character{{Name: "Mara", Want: "w", Need: "n", Wound: "x", Agency: "a"}},
	)
	if len(got) != 1 || got[0].Want != "w" || got[0].Need != "n" || got[0].Wound != "x" || got[0].Agency != "a" {
		t.Fatalf("arc quartet not merged: %+v", got)
	}
	keep := mergeCharacters(
		[]story.Character{{Name: "Mara", Want: "mine"}},
		[]story.Character{{Name: "Mara", Want: "theirs"}},
	)
	if keep[0].Want != "mine" {
		t.Fatalf("existing want overwritten: %q", keep[0].Want)
	}
}
