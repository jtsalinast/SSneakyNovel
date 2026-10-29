package story

import (
	"strings"
	"testing"

	"showmethestory/internal/config"
)

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
