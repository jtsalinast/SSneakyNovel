package story

import (
	"path/filepath"
	"testing"

	"showmethestory/internal/sse"
)

// newTestLogger returns a discard-only log broadcaster for tests.
func newTestLogger() *sse.LogBroadcaster { return sse.NewLogBroadcaster() }

// newRecoveryState builds a minimal in-progress book with two chapters; the
// current chapter (#1) holds checkpointed prose from an interrupted run.
func newRecoveryState(t *testing.T, dir string) *Progress {
	t.Helper()
	st := &Progress{
		Title:               "Test",
		Phase:               "writing",
		CurrentChapterIndex: 0,
		Chapters: []ChapterState{
			{Num: 1, Title: "Uno", Status: StatusWriting, Content: "Párrafo uno.\n\nPárrafo dos.", DraftSavedAt: "2026-10-21T00:00:00Z"},
			{Num: 2, Title: "Dos", Status: StatusPending},
		},
	}
	SyncChapterBlocks(&st.Chapters[0])
	if err := SaveProgress(filepath.Join(dir, "progress.json"), st); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestResetChapterActionClearsDraftAndPersists(t *testing.T) {
	dir := t.TempDir()
	pp := filepath.Join(dir, "progress.json")
	st := newRecoveryState(t, dir)

	if err := ResetChapterAction(st, pp, 1, newTestLogger()); err != nil {
		t.Fatal(err)
	}
	ch := st.Chapters[0]
	if ch.Status != StatusPending || ch.Content != "" || ch.Summary != "" || len(ch.Blocks) != 0 || ch.DraftSavedAt != "" {
		t.Fatalf("capítulo no limpiado por completo: %+v", ch)
	}
	// Must be persisted for the UI to pick up the cleared state on reload.
	reloaded, err := LoadProgress(pp)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Chapters[0].Status != StatusPending || reloaded.Chapters[0].Content != "" {
		t.Fatalf("reset no persistido: %+v", reloaded.Chapters[0])
	}
}

func TestResetChapterActionGuards(t *testing.T) {
	dir := t.TempDir()
	pp := filepath.Join(dir, "progress.json")
	st := newRecoveryState(t, dir)

	if err := ResetChapterAction(st, pp, 99, newTestLogger()); err == nil {
		t.Fatal("expected error for unknown chapter")
	}
	// Clearing a future (pending, contentless) chapter is now allowed — the
	// user may want to wipe/reset any chapter once generation finished. It
	// must not corrupt state though: chapter 2 was already pending/empty, so
	// resetting it is a no-op that still persists cleanly.
	if err := ResetChapterAction(st, pp, 2, newTestLogger()); err != nil {
		t.Fatalf("resetting a pending chapter should succeed: %v", err)
	}
	// Accepted chapters can also be cleared ("empezar de cero"), rewinding
	// the cursor to them.
	st.Chapters[0].Status = StatusAccepted
	st.Chapters[0].Content = "Prosa aceptada."
	if err := ResetChapterAction(st, pp, 1, newTestLogger()); err != nil {
		t.Fatalf("resetting an accepted chapter should succeed: %v", err)
	}
	if st.Chapters[0].Status != StatusPending || st.Chapters[0].Content != "" {
		t.Fatalf("accepted chapter not fully cleared: %+v", st.Chapters[0])
	}
	if st.CurrentChapterIndex != 0 {
		t.Fatalf("cursor not rewound to reset chapter: %d", st.CurrentChapterIndex)
	}
}

func TestAbortChapterWithDraftCheckpointKeepsProse(t *testing.T) {
	dir := t.TempDir()
	pp := filepath.Join(dir, "progress.json")
	st := newRecoveryState(t, dir)
	st.Chapters[0].DraftSavedAt = ""

	err := abortChapterWithDraftCheckpoint(st, pp, 0, newTestLogger(), "log.chapter_draft_saved", ErrSummaryStepFailed)
	if !IsSummaryStepFailed(err) {
		t.Fatalf("cause must be propagated wrapped as summary-step failure, got %v", err)
	}
	reloaded, err := LoadProgress(pp)
	if err != nil {
		t.Fatal(err)
	}
	ch := reloaded.Chapters[0]
	if ch.Content == "" {
		t.Fatal("prose was discarded instead of checkpointed")
	}
	if ch.Status != StatusWriting || ch.DraftSavedAt == "" {
		t.Fatalf("draft marker missing: status=%q draft=%q", ch.Status, ch.DraftSavedAt)
	}
}
