package story

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"showmethestory/internal/config"
	"showmethestory/internal/sse"
)

func TestDetectUnplannedCastChars(t *testing.T) {
	state := &Progress{Chapters: []ChapterState{
		{Num: 1, Characters: []OutlineChapterCharacter{{Name: "Alice Whitlock"}}},
		{Num: 2, Characters: []OutlineChapterCharacter{{Name: "Marcus Vane"}}}, // future debut — NOT unplanned
	}}
	content := "Alice walked the corridor. A gaunt stranger named Bartholomew Quill watched her from the doorway, while servants hurried past."
	got := detectUnplannedCastChars(state, 0, content)
	joined := strings.Join(got, "|")
	if !strings.Contains(joined, "Bartholomew Quill") {
		t.Fatalf("expected unplanned debut detected, got %v", got)
	}
	if strings.Contains(joined, "Alice") || strings.Contains(joined, "Marcus") {
		t.Fatalf("planned names must not be flagged: %v", got)
	}
	// Honorific variants of planned names are not new people.
	content2 := "Lord Marcus Vane greeted Alice at the gate."
	if got := detectUnplannedCastChars(state, 0, content2); len(got) != 0 {
		t.Fatalf("honorific variants should match planned cast, got %v", got)
	}
	// Scene-plan participants count as planned.
	state.Chapters[0].ScenePlan = &ScenePlan{Scenes: []Scene{
		{Title: "Ledger", Description: "Reginald Finch audits the accounts", PoV: "Alice"},
	}}
	content3 := "Reginald Finch signed the ledger beside Alice."
	if got := detectUnplannedCastChars(state, 0, content3); len(got) != 0 {
		t.Fatalf("scene-plan names should count as planned, got %v", got)
	}
}

func TestParseUnplannedCastVerdicts(t *testing.T) {
	raw := "Here you go:\n[{\"name\":\"Bartholomew Quill\",\"relevant\":true,\"role\":\"spy\"},{\"name\":\"Tom\",\"relevant\":false,\"role\":\"passerby\"}]\nDone."
	vs := parseUnplannedCastVerdicts(raw)
	if len(vs) != 2 {
		t.Fatalf("want 2 verdicts, got %d (%+v)", len(vs), vs)
	}
	if !vs[0].Relevant || vs[0].Role != "spy" || vs[1].Relevant {
		t.Fatalf("wrong verdicts: %+v", vs)
	}
	if parseUnplannedCastVerdicts("no json here") != nil {
		t.Fatal("garbage must parse to nil")
	}
}

func TestRegisterUnplannedCastAction(t *testing.T) {
	api := batchAPI(t, func(prompt string) string {
		if strings.Contains(prompt, "NOT listed in any chapter's planned cast") || strings.Contains(prompt, "NO están listados") {
			// The detector correctly filters role epithets ("The Old Cook") as
			// background noise, so only the genuine debut reaches the judge.
			return `[{"name":"Bartholomew Quill","relevant":true,"role":"a spy watching the hall"}]`
		}
		t.Errorf("unexpected request: %.80s", prompt)
		return "{}"
	})
	cfg := config.DefaultConfigForLang("en")
	path := filepath.Join(t.TempDir(), "progress.json")
	state := &Progress{Phase: "writing", Chapters: []ChapterState{
		{Num: 1, Status: StatusReview, Content: "Bartholomew Quill watched from the doorway. The Old Cook merely grunted.", Outline: "The hall is watched."},
		{Num: 2, Status: StatusPending},
	}}
	res, err := RegisterUnplannedCastAction(context.Background(), api, cfg, state, 0, path, sse.NewLogBroadcaster())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Registered) != 1 || res.Registered[0] != "Bartholomew Quill" {
		t.Fatalf("registered = %+v", res.Registered)
	}
	// Role epithets never reach the judge at all (filtered deterministically),
	// so nothing is reported as skipped.
	if len(res.Skipped) != 0 {
		t.Fatalf("skipped = %+v, want empty", res.Skipped)
	}
	if len(state.Chapters[0].Characters) != 1 || state.Chapters[0].Characters[0].Name != "Bartholomew Quill" {
		t.Fatalf("cast not updated: %+v", state.Chapters[0].Characters)
	}
	// Idempotent: second run finds nothing new (already registered).
	res2, err := RegisterUnplannedCastAction(context.Background(), api, cfg, state, 0, path, sse.NewLogBroadcaster())
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.Registered) != 0 || len(res2.Skipped) != 0 {
		t.Fatalf("second run should be a no-op, got %+v", res2)
	}
	// Persisted.
	loaded, err := LoadProgress(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Chapters[0].Characters) != 1 {
		t.Fatal("registration not persisted")
	}
}
