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

func TestApplyChapterMemoryResultRejectsDifferentContent(t *testing.T) {
	ch := ChapterState{Num: 1, Blocks: []Block{{ID: 1, Text: "Cordelia burned the letter."}}}
	next := Progress{MemoryEntries: []MemoryEntry{{ID: 8, Content: "Cordelia burned the letter", Category: "event"}}}
	raw, _ := json.Marshal(map[string]any{"new_memories": []any{
		map[string]any{"id": 8, "content": "The countess poisoned the wine at the ball in front of everyone.", "category": "event", "block_ids": []int{1}},
	}})
	// A substantially different rewrite of an existing fact must NOT abort the
	// batch anymore (that hard failure made memory sync permanent after any
	// chapter rewrite). The row falls back to the stored content instead.
	if err := applyChapterMemoryResult(&next, ch, map[int]bool{8: true}, string(raw)); err != nil {
		t.Fatalf("different content should fall back to stored text, not abort: %v", err)
	}
	if len(next.MemoryEntries) != 1 || next.MemoryEntries[0].Content != "Cordelia burned the letter" {
		t.Fatalf("stored content must be preserved on fallback: %+v", next.MemoryEntries)
	}
	if len(next.MemoryEntries[0].References) != 1 {
		t.Fatalf("evidence references must still attach: %+v", next.MemoryEntries[0].References)
	}
}

func TestApplyChapterMemoryResultRejectsNegationFlip(t *testing.T) {
	ch := ChapterState{Num: 1, Blocks: []Block{{ID: 1, Text: "Cordelia burned the letter."}}}
	next := Progress{MemoryEntries: []MemoryEntry{{ID: 8, Content: "Cordelia burned the letter", Category: "event"}}}
	raw, _ := json.Marshal(map[string]any{"new_memories": []any{
		map[string]any{"id": 8, "content": "Cordelia did not burn the letter", "category": "event", "block_ids": []int{1}},
	}})
	if err := applyChapterMemoryResult(&next, ch, map[int]bool{8: true}, string(raw)); err == nil || !strings.Contains(err.Error(), "negation flipped") {
		t.Fatalf("negation flip must remain fatal, got: %v", err)
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
