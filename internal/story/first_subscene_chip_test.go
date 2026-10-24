package story

import (
	"strings"
	"testing"
)

// Regression: scenes whose paragraphs never alternate dialogue/narration used
// to produce at most two units, and the trailing-beat merge then swallowed the
// FIRST subscene boundary — the UI only showed subscene chips from #2 onward.
func TestFirstSubsceneChipSurvivesUniformScene(t *testing.T) {
	paras := []string{
		"The rain hammered the tin roof without mercy.",
		"She counted the bullets again, one by one.",
		"But the door opened before she was ready.",
		"He stepped in soaked through and smiling.",
		"She did not lower the gun right away.",
		"Then he said the name she had buried years ago.",
		"The room went very quiet after that.",
		"And the gun finally came down.",
	}
	beats, ranges := splitTropesFromParas(paras, strings.Join(paras, "\n\n"), "\n\n")
	if len(beats) < 3 {
		t.Fatalf("expected >=3 beats so the first subscene chip is visible, got %d", len(beats))
	}
	// Contiguous full tiling of prose paragraphs.
	prev := 0
	for i, r := range ranges {
		if r[0] != prev {
			t.Fatalf("range %d starts at %d, expected %d", i, r[0], prev)
		}
		prev = r[1]
	}
	if prev != len(paras) {
		t.Fatalf("ranges cover %d paragraphs, expected %d", prev, len(paras))
	}
}
