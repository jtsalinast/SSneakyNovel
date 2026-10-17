package story

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSplitImportContentHeadings(t *testing.T) {
	content := "Synopsis: the story of an ordinary boy.\n\nChapter 1 Awakening\n\nLin Fan opened his eyes.\n\n## Chapter 2 Setting Off\n\nHe packed his bag.\n\nPrologue of the old edition\n\nOnce upon a time.\n\nChapter 3 The Road\n\nHe walked on."
	chs, preamble := SplitImportContent(content)
	if len(chs) != 4 {
		t.Fatalf("expected 4 chapters, got %d", len(chs))
	}
	if !strings.Contains(preamble, "Synopsis") {
		t.Fatalf("preamble missing: %q", preamble)
	}
	if chs[0].Title != "Chapter 1 Awakening" {
		t.Fatalf("title wrong: %q", chs[0].Title)
	}
	if !strings.Contains(chs[1].Content, "packed his bag") {
		t.Fatalf("content assigned to wrong chapter: %q", chs[1].Content)
	}
	if !strings.Contains(chs[2].Title, "Prologue") {
		t.Fatalf("prologue heading should split a section, got %q", chs[2].Title)
	}
	if chs[3].Title != "Chapter 3 The Road" {
		t.Fatalf("markdown-stripped title wrong: %q", chs[3].Title)
	}
}
func TestSplitImportContentNoHeadings(t *testing.T) {
	// Heading-less text falls back to size-based chunks.
	para := strings.Repeat("This paragraph carries no chapter heading at all. ", 12) // ~540 runes
	content := strings.Repeat(para+"\n\n", 40)                                       // well over one chunk
	chs, _ := SplitImportContent(content)
	if len(chs) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chs))
	}
	if chs[0].Title != "Chapter 1" {
		t.Fatalf("placeholder title wrong: %q", chs[0].Title)
	}
	if splitEmpty, _ := SplitImportContent("   "); splitEmpty != nil {
		t.Fatal("empty content should split to nil")
	}
}
func TestBuildImportPreview(t *testing.T) {
	chs := []importedChapter{{Title: "Chapter 1 Awakening", Content: "Chapter 1 Awakening\nLin Fan opened his eyes, " + strings.Repeat("very long body content. ", 30)}}
	pv := BuildImportPreview(chs)
	if pv[0].Num != 1 || pv[0].WordCount == 0 {
		t.Fatalf("preview meta wrong: %+v", pv[0])
	}
	if !strings.HasSuffix(pv[0].Preview, "…") {
		t.Fatalf("long preview should be truncated: %q", pv[0].Preview)
	}
	if strings.Contains(pv[0].Preview, "Chapter 1 Awakening") {
		t.Fatalf("preview should skip the heading line: %q", pv[0].Preview)
	}
}

func TestImportStateRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "import.json")
	if LoadImportState(path) != nil {
		t.Fatal("missing file should load as nil")
	}
	st := &ImportState{Active: true, Total: 10, Cursor: 3, MetaDone: true}
	if err := SaveImportState(path, st); err != nil {
		t.Fatal(err)
	}
	got := LoadImportState(path)
	if got == nil || got.Cursor != 3 || got.Total != 10 || !got.MetaDone {
		t.Fatalf("roundtrip wrong: %+v", got)
	}
	// Inactive state loads as nil (finished import).
	st.Active = false
	SaveImportState(path, st)
	if LoadImportState(path) != nil {
		t.Fatal("inactive state should load as nil")
	}
}
