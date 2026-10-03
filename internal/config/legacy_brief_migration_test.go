package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLegacyBriefMigratesToStoryIdea verifies that configs saved with the old
// "brief" key are automatically migrated to StoryIdea ("story_idea").
func TestLegacyBriefMigratesToStoryIdea(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	if err := os.WriteFile(p, []byte(`{"story":{"type":"fantasy","brief":"A dragon learns to bake."}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Story.StoryIdea != "A dragon learns to bake." {
		t.Fatalf("legacy brief migration failed, StoryIdea=%q", cfg.Story.StoryIdea)
	}
}
