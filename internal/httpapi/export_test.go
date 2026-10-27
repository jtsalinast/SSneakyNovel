package httpapi

import (
	"net/http/httptest"
	"showmethestory/internal/config"
	"showmethestory/internal/story"
	"strings"
	"testing"
)

func TestBookExportUsesNovelTitleAndMarkdownHeadings(t *testing.T) {
	h := &Handlers{
		cfg: &config.Config{Language: "en", Story: config.StoryConfig{Title: "smallsaymetric"}},
		state: &story.Progress{
			Title:    "advancedegreemetric",
			Chapters: []story.ChapterState{{Num: 3, Title: "new open", Content: "body text"}},
		},
	}
	res := httptest.NewRecorder()
	h.GetBookExport(res, httptest.NewRequest("GET", "/api/export/txt", nil))
	want := "# smallsaymetric\n\n## Chapter 3: new open\n\nbody text"
	if got := res.Body.String(); got != want {
		t.Fatalf("export mismatch:\n got %q\nwant %q", got, want)
	}
}

func TestOutlineExportIncludesEveryChapter(t *testing.T) {
	h := &Handlers{projectName: "test", cfg: config.DefaultConfig(), state: &story.Progress{Title: "Book", Chapters: []story.ChapterState{{Num: 1, Title: "One", Outline: "First"}, {Num: 2, Title: "Two", Outline: "Second"}}}}
	res := httptest.NewRecorder()
	h.GetOutlineExport(res, httptest.NewRequest("GET", "/api/export/outline", nil))
	got := res.Body.String()
	if !strings.Contains(got, "## Chapter 1: One\n\nFirst") || !strings.Contains(got, "## Chapter 2: Two\n\nSecond") {
		t.Fatal(got)
	}
}
