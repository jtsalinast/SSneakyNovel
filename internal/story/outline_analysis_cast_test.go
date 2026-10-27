package story

import (
	"path/filepath"
	"testing"
)

// newStateWithSuggestion builds a minimal progress state with two editable
// chapters and one "Propose outline changes"-style suggestion whose second
// recommendation also reshapes the chapter cast.
func newStateWithSuggestion() *Progress {
	return &Progress{
		Phase: "outlining",
		Chapters: []ChapterState{
			{Num: 1, Title: "Old One", Outline: "Old outline one", Status: StatusPending},
			{Num: 2, Title: "Old Two", Outline: "Old outline two", Status: StatusPending,
				Characters: []OutlineChapterCharacter{{Name: "Alice"}}},
		},
		OutlineAnalysis: &OutlineAnalysisReport{
			Suggestions: []OutlineSuggestion{{
				ID:     "s1",
				Status: "pending",
				Recommendations: []OutlineRecommendation{
					{ChapterNum: 1, Present: true, NewTitle: "Sharper Hook", NewOutline: "A bestseller-grade opening beat.", Proposed: true},
					{ChapterNum: 2, Present: true, NewTitle: "Raised Stakes", NewOutline: "The antagonist strikes first.", Proposed: true,
						Cast: []OutlineChapterCharacter{
							{Name: "Alice"},
							{Name: "Marcus", FirstAppearance: true, Note: "rival investigator"},
						}},
				},
			}},
		},
	}
}

func TestApplyOutlineSuggestionReplacesCast(t *testing.T) {
	state := newStateWithSuggestion()
	dir := t.TempDir()
	progressPath := filepath.Join(dir, "progress.json")

	changed, err := ApplyOutlineSuggestion(state, "s1", progressPath)
	if err != nil {
		t.Fatalf("ApplyOutlineSuggestion: %v", err)
	}
	if changed != 2 {
		t.Fatalf("changed = %d, want 2", changed)
	}
	ch1, ch2 := state.Chapters[0], state.Chapters[1]
	if ch1.Title != "Sharper Hook" || ch1.Outline != "A bestseller-grade opening beat." {
		t.Fatalf("chapter 1 not replaced: %+v", ch1)
	}
	// Chapter 1 had no cast proposal: its (empty) cast must stay untouched.
	if len(ch1.Characters) != 0 {
		t.Fatalf("chapter 1 cast should stay empty, got %+v", ch1.Characters)
	}
	if ch2.Title != "Raised Stakes" {
		t.Fatalf("chapter 2 title not replaced: %+v", ch2)
	}
	// The proposed REPLACED cast must land on the chapter (Alice kept, Marcus added).
	if len(ch2.Characters) != 2 || ch2.Characters[0].Name != "Alice" || ch2.Characters[1].Name != "Marcus" || !ch2.Characters[1].FirstAppearance {
		t.Fatalf("chapter 2 cast not applied: %+v", ch2.Characters)
	}
	if state.OutlineAnalysis.Suggestions[0].Status != "applied" {
		t.Fatalf("suggestion status = %q, want applied", state.OutlineAnalysis.Suggestions[0].Status)
	}
	// Re-applying an already-applied suggestion must fail cleanly.
	if _, err := ApplyOutlineSuggestion(state, "s1", progressPath); err == nil {
		t.Fatal("expected error re-applying an applied suggestion")
	}
}

func TestRegisterCastCharactersAddsNewOnly(t *testing.T) {
	settings := &ProjectSettings{
		Characters: []Character{{ID: "c1", Name: "Alice"}},
	}
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")

	recs := newStateWithSuggestion().OutlineAnalysis.Suggestions[0].Recommendations
	added, err := RegisterCastCharacters(settings, recs, settingsPath)
	if err != nil {
		t.Fatalf("RegisterCastCharacters: %v", err)
	}
	if added != 1 {
		t.Fatalf("added = %d, want 1 (only brand-new characters)", added)
	}
	if len(settings.Characters) != 2 || settings.Characters[1].Name != "Marcus" {
		t.Fatalf("character sheet = %+v", settings.Characters)
	}
	if settings.Characters[1].Background != "rival investigator" {
		t.Fatalf("debut note not carried to background: %+v", settings.Characters[1])
	}
	// Registering the same proposals again must be a no-op (no duplicates).
	added, err = RegisterCastCharacters(settings, recs, settingsPath)
	if err != nil || added != 0 {
		t.Fatalf("second pass: added=%d err=%v, want 0/<nil>", added, err)
	}
	if len(settings.Characters) != 2 {
		t.Fatalf("duplicate registration: %+v", settings.Characters)
	}
}

func TestCastSummaryAndSanitization(t *testing.T) {
	raw := []OutlineChapterCharacter{
		{Name: "  Alice  "}, {Name: "Alice"}, {Name: "「Bob」", Note: "witness"},
	}
	c := normalizeOutlineCharacters(raw)
	if len(c) != 2 || c[0].Name != "Alice" || c[1].Name != "Bob" {
		t.Fatalf("normalizeOutlineCharacters = %+v", c)
	}
	// CastSummary renders the (already sanitized) proposal cast for the UI.
	r := OutlineRecommendation{Cast: c}
	if got := r.CastSummary(); got != "Alice; Bob" {
		t.Fatalf("CastSummary = %q, want %q", got, "Alice; Bob")
	}
	// Empty cast renders as "" meaning "keep the current cast".
	if (OutlineRecommendation{}).CastSummary() != "" {
		t.Fatal("empty cast summary must be empty string")
	}
}
