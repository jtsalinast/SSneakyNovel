package story

import (
	"strings"
	"testing"
)

// The "rewrite from feedback" loop must leave a machine-readable WHY behind:
// after a pass that ran to completion but did not clear the checks, the
// successor conflict carries a last_failure note classifying what happened
// (surgery applied / errored / looping on the same trope unit). Otherwise the
// UI shows a fresh-looking conflict every time and the author cannot tell why
// it keeps failing.

func TestRecordReviseLoopFailure_LoopOnSameTarget(t *testing.T) {
	prev := &WritingConflict{
		ChapterIndex: 0, ChapterNum: 1,
		SurgicalAttempts: 2, LastSurgicalTarget: [2]int{3, 1},
		Issues: []string{"Aldric lost his sword in chapter 2."},
	}
	cur := &WritingConflict{
		ChapterIndex: 0, ChapterNum: 1, SurgicalAttempts: 2, ReviseAttempts: 1,
		SceneTargets: [][2]int{{3, 1}},
		Issues:       []string{"Aldric lost his sword in chapter 2."}, // same issue (normalized), same unit
	}
	recordReviseLoopFailure(cur, prev, 2, 0)
	if cur.LastFailure == nil {
		t.Fatal("expected a last_failure note on the looping successor")
	}
	f := cur.LastFailure
	if f.Stage != "revise" {
		t.Errorf("stage = %q, want revise", f.Stage)
	}
	if !strings.Contains(f.Reason, "SAME trope unit") {
		t.Errorf("looping reason should name the repeated-target diagnosis, got %q", f.Reason)
	}
	if f.SurgicalRepaired != 2 || f.FailedCount != 3 {
		t.Errorf("counters: repaired=%d failed_count=%d, want 2/3", f.SurgicalRepaired, f.FailedCount)
	}
	if !f.KeptDraft {
		t.Error("KeptDraft must be true: the rewrite never discards the draft")
	}
}

func TestRecordReviseLoopFailure_PartialSurgeryErrors(t *testing.T) {
	prev := &WritingConflict{ChapterIndex: 0, ChapterNum: 5}
	cur := &WritingConflict{ChapterIndex: 0, ChapterNum: 5}
	recordReviseLoopFailure(cur, prev, 3, 2)
	if cur.LastFailure == nil {
		t.Fatal("expected a note when some surgeries errored")
	}
	if !strings.Contains(cur.LastFailure.Reason, "3 trope-unit repair(s) were applied but 2 failed") {
		t.Errorf("partial-failure wording missing, got %q", cur.LastFailure.Reason)
	}
	if cur.LastFailure.ChapterNum != 5 {
		t.Errorf("chapter_num = %d, want 5", cur.LastFailure.ChapterNum)
	}
}

func TestRecordReviseLoopFailure_NoopOnFirstPlainRewrite(t *testing.T) {
	prev := &WritingConflict{ChapterIndex: 0}
	cur := &WritingConflict{ChapterIndex: 0}
	recordReviseLoopFailure(cur, prev, 0, 0)
	if cur.LastFailure != nil {
		t.Errorf("first plain rewrite with no surgery must not fabricate a failure note, got %+v", cur.LastFailure)
	}
}

func TestRecordConflictFailure_ClassifiesTimeoutAndKeepsRetry(t *testing.T) {
	state := &Progress{
		Chapters: []ChapterState{{Num: 7, Status: StatusWriting}},
	}
	path := t.TempDir() + "/progress.json"
	RecordConflictFailure(state, path, "revise", errForTest("net/http: request canceled (Client.Timeout exceeded while awaiting headers)"), nil)
	c := state.PendingWritingConflict
	if c == nil {
		t.Fatal("hard error on an orphaned draft must recreate a conflict so Retry stays available")
	}
	if c.LastFailure == nil {
		t.Fatal("failure note missing")
	}
	if !strings.Contains(c.LastFailure.Reason, "timeout") {
		t.Errorf("timeout cause should be classified as such, got %q", c.LastFailure.Reason)
	}
	if c.LastFailure.Stage != "revise" || c.LastFailure.ChapterNum != 7 {
		t.Errorf("stage/chapter: %q/%d, want revise/7", c.LastFailure.Stage, c.LastFailure.ChapterNum)
	}
	// The recreated conflict must keep offering retry actions, never a dead end.
	hasRetry := false
	for _, a := range c.SuggestedActions {
		if a.ID == "retry" || a.ID == "revise_from_feedback" {
			hasRetry = true
		}
	}
	if !hasRetry {
		t.Errorf("recreated conflict must carry retry-capable actions, got %+v", c.SuggestedActions)
	}
}

type errForTest string

func (e errForTest) Error() string { return string(e) }
