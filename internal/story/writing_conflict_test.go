package story

import (
	"strings"
	"testing"
)

func TestResolveForceReviewIndex(t *testing.T) {
	state := &Progress{
		CurrentChapterIndex: 1,
		Chapters: []ChapterState{
			{Num: 1, Status: StatusAccepted},
			{Num: 2, Status: StatusWriting, Content: "draft"},
		},
		PendingWritingConflict: &WritingConflict{ChapterIndex: 1, ChapterNum: 2},
	}
	idx, err := ResolveForceReviewIndex(state)
	if err != nil || idx != 1 {
		t.Fatalf("with conflict: idx=%d err=%v", idx, err)
	}

	state.PendingWritingConflict = nil
	idx, err = ResolveForceReviewIndex(state)
	if err != nil || idx != 1 {
		t.Fatalf("orphan writing: idx=%d err=%v", idx, err)
	}

	state.Chapters[1].Status = StatusReview
	if _, err := ResolveForceReviewIndex(state); err == nil {
		t.Fatal("expected error when no conflict and not writing")
	}
}

func TestPromoteWritingToReviewClearsConflict(t *testing.T) {
	state := &Progress{
		Chapters:               []ChapterState{{Num: 1, Status: StatusWriting}},
		PendingWritingConflict: &WritingConflict{ChapterIndex: 0, ChapterNum: 1},
	}
	if err := PromoteWritingToReview(state, 0); err != nil {
		t.Fatal(err)
	}
	if state.Chapters[0].Status != StatusReview {
		t.Fatalf("status=%s, want review", state.Chapters[0].Status)
	}
	if state.PendingWritingConflict != nil {
		t.Fatal("conflict should be cleared")
	}
}

func TestDedupeArcIssuesCollapsesSameRootEvent(t *testing.T) {
	// The seven wordings from the real Edmund feedback describe ONE root
	// event (his debut belongs to a later chapter); they must collapse.
	issues := []string{
		"Premature introduction of character Edmund: the stranger is clearly intended to be Edmund, whose reveal is assigned to Chapter 3.",
		"Identity reveal of the stranger as someone with a hidden background occurs prematurely; planned in Chapter 3.",
		"Premature introduction of the stranger character Edmund implies a first meeting assigned to Chapter 2.",
		"Contradiction with outline arc: interaction with Edmund was assigned to later chapters, specifically Chapter 2.",
		"Premature introduction of Lord Edmund Ashworth as a dancing stranger when his debut belongs to Chapter 3.",
		"The motif of Edmund's glove implies he already left the ballroom, an event planned for a later chapter, Chapter 3.",
		"Timeline contradiction: previous chapter ended at night, this one reverts to morning.",
	}
	got := dedupeArcIssues(issues)
	if len(got) != 2 {
		t.Fatalf("expected Edmund cluster collapsed to 1 + timeline issue kept, got %d: %#v", len(got), got)
	}
	if !strings.HasPrefix(got[0], "Premature introduction of character Edmund") {
		t.Fatalf("first wording should win, got %q", got[0])
	}
}

func TestSplitFactCheckIssuesDedupes(t *testing.T) {
	raw := "Edmund's debut is premature, it belongs to Chapter 3; Edmund introduced early again in Chapter 3; unrelated name inconsistency with Lady Beatrice"
	got := dedupeArcIssuesWithCast(strings.Split(raw, "; "), nil, -1)
	if len(got) != 2 {
		t.Fatalf("expected 2 issues after dedup, got %d: %#v", len(got), got)
	}
}

func TestDedupeArcIssuesWithCastResolvesObliqueMentions(t *testing.T) {
	// Mirrors the real Edmund feedback: several wordings describe ONE root
	// event; some never spell the character out ("the stranger") but quote a
	// sentence naming exactly one future-debut cast member. The cast-aware
	// pass resolves them and everything collapses to a single issue, while an
	// unrelated timeline problem survives.
	state := &Progress{
		Chapters: []ChapterState{
			{Num: 1, Status: StatusWriting},
			{Num: 2, Status: StatusPending, Characters: []OutlineChapterCharacter{{Name: "Cordelia"}}},
			{Num: 3, Status: StatusPending, Characters: []OutlineChapterCharacter{
				{Name: "Lord Edmund Ashworth", FirstAppearance: true},
			}},
			{Num: 4, Status: StatusPending, Characters: []OutlineChapterCharacter{
				{Name: "Lady Beatrice Holloway", FirstAppearance: true},
			}},
		},
	}
	issues := []string{
		"Premature introduction of character Edmund: the stranger is clearly intended to be Edmund, whose reveal is assigned to Chapter 3.",
		"Identity reveal of the stranger as someone with a hidden background occurs prematurely; planned in Chapter 3.",
		"Premature introduction of the stranger character implies a first meeting assigned to Chapter 2 where Lord Edmund Ashworth was seen at a gathering.",
		"Contradiction with outline arc: interaction with the stranger who danced with Cordelia was assigned to later chapters, specifically Chapter 2.",
		"The motif of the glove abandoned at the foot of the stairs implies Lord Edmund Ashworth already left the ballroom, an event planned for a later chapter.",
		"Timeline contradiction: previous chapter ended at night, this one reverts to morning.",
	}
	got := dedupeArcIssuesWithCast(issues, state, 0)
	if len(got) != 2 {
		t.Fatalf("expected oblique wordings to collapse into the Edmund cluster + timeline kept, got %d: %#v", len(got), got)
	}
	if !strings.HasPrefix(got[0], "Premature introduction of character Edmund") {
		t.Fatalf("first wording should win, got %q", got[0])
	}
	if !strings.Contains(got[1], "Timeline") {
		t.Fatalf("timeline issue must survive, got %q", got[1])
	}
}
