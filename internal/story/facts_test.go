package story

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestApplyChapterMemoryResultAcceptsParaphrase(t *testing.T) {
	ch := ChapterState{Num: 1, Blocks: []Block{{ID: 1, Text: "Cordelia burned the letter."}}}
	next := Progress{MemoryEntries: []MemoryEntry{{ID: 8, Content: "Cordelia burned the letter", Category: "event"}}}
	raw, _ := json.Marshal(map[string]any{"new_memories": []any{
		map[string]any{"id": 8, "content": "cordelia has burned the letter.", "category": "event", "block_ids": []int{1}},
	}})
	if err := applyChapterMemoryResult(&next, ch, map[int]bool{8: true}, string(raw)); err != nil {
		t.Fatalf("paraphrase of existing fact should be accepted: %v", err)
	}
	if len(next.MemoryEntries) != 1 || next.MemoryEntries[0].ID != 8 {
		t.Fatalf("entry count/identity wrong: %+v", next.MemoryEntries)
	}
	if len(next.MemoryEntries[0].References) != 1 {
		t.Fatalf("reference not attached: %+v", next.MemoryEntries[0].References)
	}
}

func TestApplyChapterMemoryResultSurvivesDifferentContent(t *testing.T) {
	// A model rewriting an existing id with unrelated content used to fail the
	// whole sync forever ("must preserve the supplied content exactly"). Now the
	// stored fact keeps its wording, the bogus rewrite is dropped, and only the
	// evidence anchors refresh — sync completes instead of erroring.
	ch := ChapterState{Num: 1, Blocks: []Block{{ID: 1, Text: "Cordelia burned the letter."}}}
	next := Progress{MemoryEntries: []MemoryEntry{{ID: 8, Content: "Cordelia burned the letter", Category: "event"}}}
	raw, _ := json.Marshal(map[string]any{"new_memories": []any{
		map[string]any{"id": 8, "content": "The countess poisoned the wine at the ball in front of everyone.", "category": "event", "block_ids": []int{1}},
	}})
	if err := applyChapterMemoryResult(&next, ch, map[int]bool{8: true}, string(raw)); err != nil {
		t.Fatalf("reworded existing fact must not fail sync: %v", err)
	}
	if len(next.MemoryEntries) != 1 || next.MemoryEntries[0].Content != "Cordelia burned the letter" {
		t.Fatalf("stored fact wording must be preserved: %+v", next.MemoryEntries)
	}
	if len(next.MemoryEntries[0].References) != 1 {
		t.Fatalf("evidence anchor should still refresh: %+v", next.MemoryEntries[0].References)
	}
}

func TestApplyChapterMemoryResultNegationFlipKeepsStoredFact(t *testing.T) {
	// "alive" vs "not alive" flips meaning; instead of aborting the batch, the
	// stored content survives and the sync completes.
	ch := ChapterState{Num: 1, Blocks: []Block{{ID: 1, Text: "The guard says he is not alive anymore."}}}
	next := Progress{MemoryEntries: []MemoryEntry{{ID: 34, Content: "The butler is alive", Category: "character"}}}
	raw, _ := json.Marshal(map[string]any{"new_memories": []any{
		map[string]any{"id": 34, "content": "The butler is not alive", "category": "character", "block_ids": []int{1}},
	}})
	if err := applyChapterMemoryResult(&next, ch, map[int]bool{34: true}, string(raw)); err != nil {
		t.Fatalf("negation flip must not fail sync: %v", err)
	}
	if next.MemoryEntries[0].Content != "The butler is alive" {
		t.Fatalf("stored fact must be kept on negation flip: %+v", next.MemoryEntries[0])
	}
}

func TestApplyChapterMemoryResultCondensedRewriteUpdatesInPlace(t *testing.T) {
	// Moderate rewording (Jaccard between minFactOverlapRatio and 0.5) is a
	// legitimate condensation: accepted as an update, no duplicate entry.
	ch := ChapterState{Num: 1, Blocks: []Block{{ID: 1, Text: "He bears a scar on his left cheek."}}}
	next := Progress{MemoryEntries: []MemoryEntry{{ID: 5, Content: "Kael has a scar on his left cheek from a knife fight years ago", Category: "character"}}}
	raw, _ := json.Marshal(map[string]any{"new_memories": []any{
		map[string]any{"id": 5, "content": "Kael bears a scar, left cheek", "category": "character", "block_ids": []int{1}},
	}})
	if err := applyChapterMemoryResult(&next, ch, map[int]bool{5: true}, string(raw)); err != nil {
		t.Fatalf("condensed rewrite must be accepted: %v", err)
	}
	if len(next.MemoryEntries) != 1 {
		t.Fatalf("no duplicate entries expected: %+v", next.MemoryEntries)
	}
	if !strings.Contains(next.MemoryEntries[0].Content, "scar") {
		t.Fatalf("unexpected content: %+v", next.MemoryEntries[0])
	}
}

func TestApplyChapterMemoryResultNoPartialMutationOnBadRow(t *testing.T) {
	ch := ChapterState{Num: 1, Blocks: []Block{{ID: 1, Text: "A"}, {ID: 2, Text: "B"}}}
	next := Progress{MemoryEntries: []MemoryEntry{{ID: 8, Content: "fact eight", Category: "event"}}}
	before := next.MemoryEntries[0].References
	raw := `{"new_memories":[
{"id":0,"content":"a brand new separate fact about the moon","category":"other","block_ids":[1]},
{"id":9,"content":"ghost fact","category":"other","block_ids":[2]}
]}`
	err := applyChapterMemoryResult(&next, ch, map[int]bool{8: true}, raw)
	if err == nil || !strings.Contains(err.Error(), "was not supplied") {
		t.Fatalf("expected invisible-id error, got %v", err)
	}
	if len(next.MemoryEntries) != 1 {
		t.Fatalf("bad row must not leave partial entries behind: %+v", next.MemoryEntries)
	}
	if fmt.Sprint(next.MemoryEntries[0].References) != fmt.Sprint(before) {
		t.Fatalf("references mutated on failed batch")
	}
}

func TestFactContentEquivalent(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"Cordelia burned the letter.", "cordelia has burned the letter", true},
		{"Lord Edmund arrived at dawn", "Lady Beatrice left at dusk", false},
		{"same text", "same text", true},
	}
	for _, c := range cases {
		if got := factContentEquivalent(c.a, c.b); got != c.want {
			t.Errorf("factContentEquivalent(%q,%q)=%v want %v", c.a, c.b, got, c.want)
		}
	}
}
