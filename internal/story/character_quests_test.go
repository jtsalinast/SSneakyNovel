package story

import (
	"strings"
	"testing"
)

func TestCharacterQuestsBlockRendersThreadsOnlyForQuestOwners(t *testing.T) {
	ps := &ProjectSettings{Characters: []Character{
		{
			ID: "c_1", Name: "Ana", Affiliation: "protagonist", Relevance: "fundamental", Presence: "main",
			MainQuest: "Recover the seal", MainResolved: true,
			MinorQuests: []CharacterQuest{{Text: "Pay the smuggler", Resolved: false}},
			OverarchingQuest: "Can she be forgiven?", OverarchingResolved: false,
		},
		{ID: "c_2", Name: "Background guard", Presence: "incidental"},
	}}
	block := CharacterQuestsBlock(ps, "en")
	if !strings.Contains(block, "STORY WEB") {
		t.Fatalf("missing header:\n%s", block)
	}
	if !strings.Contains(block, "Ana (protagonist/fundamental/main)") {
		t.Fatalf("missing role tag:\n%s", block)
	}
	if !strings.Contains(block, "Recover the seal (RESOLVED)") || !strings.Contains(block, "Pay the smuggler (OPEN)") {
		t.Fatalf("resolution flags missing:\n%s", block)
	}
	if !strings.Contains(block, "Can she be forgiven? (OPEN)") {
		t.Fatalf("overarching quest missing:\n%s", block)
	}
	if strings.Contains(block, "Background guard") {
		t.Fatalf("character without quests must be skipped:\n%s", block)
	}
}

func TestCharacterQuestsBlockEmpty(t *testing.T) {
	if got := CharacterQuestsBlock(nil, "en"); got != "" {
		t.Fatalf("nil settings must render nothing, got %q", got)
	}
	if got := CharacterQuestsBlock(&ProjectSettings{Characters: []Character{{Name: "Ana"}}}, "en"); got != "" {
		t.Fatalf("questless cast must render nothing, got %q", got)
	}
}

func TestNormalizeCharacterQuestsClearsAndReassigns(t *testing.T) {
	main, mainResolved, minors, over, overResolved := NormalizeCharacterQuests(
		"  chase the traitor  ", true,
		[]CharacterQuest{{ID: "old", Text: "  ", Resolved: true}, {ID: "old", Text: " protect the sister ", Resolved: true}},
		"   ", true,
	)
	if main != "chase the traitor" || !mainResolved {
		t.Fatalf("main quest not normalized: %q %v", main, mainResolved)
	}
	if len(minors) != 1 || minors[0].Text != "protect the sister" || minors[0].ID != "q_1" || !minors[0].Resolved {
		t.Fatalf("minor quests not normalized: %+v", minors)
	}
	if over != "" || overResolved {
		t.Fatalf("empty overarching quest must clear its flag: %q %v", over, overResolved)
	}
	// Unchecking a flag on a filled quest must persist.
	_, mainResolved, _, _, _ = NormalizeCharacterQuests("goal", false, nil, "", false)
	if mainResolved {
		t.Fatal("unchecked main_resolved must stay false")
	}
}
