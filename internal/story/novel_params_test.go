package story

import (
	"strings"
	"testing"

	"showmethestory/internal/config"
)

func TestNovelParametersHiddenAgendaBlock(t *testing.T) {
	cfg := &config.Config{Language: "en", Story: config.StoryConfig{AuthorHiddenAgenda: "Win the Clarín prize without ever saying so"}}
	block := NovelParametersBlock(cfg)
	if !strings.Contains(block, "[AUTHOR'S HIDDEN AGENDA") || !strings.Contains(block, "Win the Clarín prize") {
		t.Fatalf("hidden agenda missing from prompt block: %q", block)
	}
	if !strings.Contains(block, "NEVER STATE IT") {
		t.Fatal("hidden agenda must be injected as a never-stated steering note")
	}
}

func TestNovelParametersFanficBlock(t *testing.T) {
	cfg := &config.Config{Language: "en", Story: config.StoryConfig{WorkType: "fanfic", FanficSource: "Dune — Frank Herbert"}}
	block := NovelParametersBlock(cfg)
	if !strings.Contains(block, "[FAN FICTION]") || !strings.Contains(block, "Dune — Frank Herbert") {
		t.Fatalf("fanfic parameters missing from prompt block: %q", block)
	}

	cfg.Story.WorkType = "original"
	if strings.Contains(NovelParametersBlock(cfg), "[FAN FICTION]") {
		t.Fatal("original work must not inject the fan fiction rule")
	}
}
