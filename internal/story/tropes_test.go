package story

import (
	"strings"
	"testing"
)

// splitTropes must segment a scene into trope units — coarser than a single
// beat, finer than the whole scene — using structural cues only.
func TestSplitTropesSegmentsByModeShifts(t *testing.T) {
	scene := strings.Join([]string{
		// Opening narration block (>= tropeMinParagraphs paragraphs).
		"The hall was empty when Zara arrived.",
		"Dust drifted through the broken windows.",
		// Dialogue-led block: mode shift after enough narration accumulates.
		"\u201cYou came,\u201d he said from the shadows.",
		"\u201cI said I would,\u201d she answered.",
		// Narration resumes: second mode shift.
		"She crossed to the table and sat down slowly.",
		"The chair creaked under her weight.",
	}, "\n\n")

	units, ranges := splitTropes(scene)
	if len(units) < 2 {
		t.Fatalf("expected multiple trope units from narration/dialogue alternation, got %d", len(units))
	}
	// Ranges must tile the six paragraphs without gaps or overlaps.
	prevEnd := 0
	for i, r := range ranges {
		if r[0] != prevEnd {
			t.Fatalf("unit %d starts at %d, expected %d (ranges must tile)", i, r[0], prevEnd)
		}
		if r[1] <= r[0] {
			t.Fatalf("unit %d has empty span %v", i, r)
		}
		prevEnd = r[1]
	}
	if prevEnd != 6 {
		t.Fatalf("ranges cover %d paragraphs, want 6", prevEnd)
	}
	// No empty units.
	for i, u := range units {
		if strings.TrimSpace(u) == "" {
			t.Fatalf("unit %d is empty", i)
		}
	}
}

// A hard break marker forces a new trope unit even without a mode shift.
func TestSplitTropesHardBreakSeparatesUnits(t *testing.T) {
	scene := strings.Join([]string{
		"Zara ran through the burning streets.",
		"Smoke filled her lungs with every breath.",
		"***",
		"Hours later she woke in a cellar.",
		"Someone had bound her wounds.",
	}, "\n\n")
	units, ranges := splitTropes(scene)
	if len(units) != 2 {
		t.Fatalf("hard break must split into exactly 2 units, got %d: %q", len(units), units)
	}
	if ranges[0][1] != 2 || ranges[1][0] != 3 {
		t.Fatalf("break marker paragraph must be dropped from spans, got %v", ranges)
	}
	if strings.Contains(units[0]+"units[1]", "***") {
		t.Fatal("separator must not leak into unit text")
	}
}

// Tiny trailing runs fold into their predecessor so units stay substantial.
func TestSplitTropesMergesTinyTrailingUnit(t *testing.T) {
	scene := strings.Join([]string{
		"\u201cWait for me,\u201d she called out.",
		"\u201cAlways,\u201d he replied without turning.",
		"He disappeared into the fog.", // lone narration paragraph at the end
	}, "\n\n")
	units, _ := splitTropes(scene)
	for i, u := range units {
		if i > 0 && paragraphCount(u) < tropeMinParagraphs {
			t.Fatalf("trailing unit %d has %d paragraphs, below floor %d", i, paragraphCount(u), tropeMinParagraphs)
		}
	}
}

// classifyTrope labels units by dramatic move from structural cues.
func TestClassifyTropeLabels(t *testing.T) {
	narration := "The road unspooled ahead of her. She counted the milestones one by one."
	dialogue := "\u201cWhy did you lie to me?\u201d \u201cBecause the truth would have stopped you.\u201d"
	action := "Suddenly the riders charged; she ran for the tree line."
	reveal := "She opened the letter and realized the estate had never been hers."

	if got := classifyTrope(narration, true, false); got != TropeOpening {
		t.Errorf("first narration unit: got %q, want %q", got, TropeOpening)
	}
	if got := classifyTrope(dialogue, false, false); got != TropeDialogue {
		t.Errorf("dialogue-heavy unit: got %q, want %q", got, TropeDialogue)
	}
	if got := classifyTrope(action, false, false); got != TropeAction {
		t.Errorf("action keywords: got %q, want %q", got, TropeAction)
	}
	if got := classifyTrope(reveal, false, true); got != TropeReveal {
		t.Errorf("reveal keywords: got %q, want %q", got, TropeReveal)
	}
}

// tropesOfScene caches labeled units on the Scene so reloads stay deterministic.
func TestTropesOfSceneCachesOnScene(t *testing.T) {
	sc := &Scene{ID: 1, Title: "Arrival", WordsBudget: 400}
	scene := strings.Join([]string{
		"Zara stepped off the train into the rain.",
		"The platform smelled of coal and wet wool.",
		"\u201cOver here!\u201d the courier waved from the gate.",
		"\u201cLead the way,\u201d she said.",
	}, "\n\n")
	units := tropesOfScene(scene, sc)
	if len(units) < 2 {
		t.Fatalf("want at least 2 trope units, got %d", len(units))
	}
	if len(sc.TropeUnits) != len(units) || len(sc.TropeRanges) != len(units) {
		t.Fatalf("units/ranges not cached on scene: %d vs %d", len(sc.TropeUnits), len(units))
	}
	for i, u := range units {
		if u.Index != i {
			t.Fatalf("unit %d has Index %d", i, u.Index)
		}
		if u.ParaEnd <= u.ParaStart || u.Words <= 0 {
			t.Fatalf("unit %d malformed: %+v", i, u)
		}
		if u.Label == "" {
			t.Fatalf("unit %d unlabeled", i)
		}
	}
	// First unit of an opening narration run should read as an opening.
	if units[0].Label != TropeOpening {
		t.Errorf("first unit label %q, want %q", units[0].Label, TropeOpening)
	}
}

// syncSceneBlocks must produce one block per trope unit, carrying the owning
// scene's metadata (position/title) and trope metadata (label/index/count).
func TestSyncSceneBlocksPerTropeWithMetadata(t *testing.T) {
	ch := &ChapterState{}
	ch.ScenePlan = &ScenePlan{TargetWords: 500, Scenes: []Scene{
		{ID: 1, Title: "The Platform", Description: "Zara arrives", WordsBudget: 250},
		{ID: 2, Title: "The Courier", Description: "Joss delivers the ledger", WordsBudget: 250},
	}}
	// Two scenes; each has narration+dialogue so splitTropes yields >=2 units.
	s1 := strings.Join([]string{
		"Zara stepped onto the rain-soaked platform.",
		"The train hissed and pulled away behind her.",
		"\u201cYou're late,\u201d said a voice at the gate.",
		"\u201cAnd you're early,\u201d she replied.",
	}, "\n\n")
	s2 := strings.Join([]string{
		"The courier handed over the wrapped ledger.",
		"Wax seals glinted under the lantern light.",
		"\u201cShe sent me,\u201d he said.",
		"\u201cThen she knows,\u201d Zara whispered.",
	}, "\n\n")
	ch.Content = s1 + "\n\n" + s2

	if !syncSceneBlocks(ch) {
		t.Fatal("syncSceneBlocks returned false")
	}
	if len(ch.Blocks) < 4 {
		t.Fatalf("want at least 4 trope blocks (2 per scene), got %d", len(ch.Blocks))
	}
	seenScene := map[int]string{}
	for _, b := range ch.Blocks {
		if b.ScenePos < 0 || b.ScenePos > 1 {
			t.Fatalf("block %d has invalid scene_pos %d", b.ID, b.ScenePos)
		}
		if b.SceneTitle == "" {
			t.Fatalf("block %d missing scene_title", b.ID)
		}
		// TropeIndex is 0-based (matches the JSON contract consumed by the UI).
		if b.TropeIndex < 0 || b.TropeCount < 1 || b.TropeIndex >= b.TropeCount {
			t.Fatalf("block %d bad trope index %d/%d", b.ID, b.TropeIndex, b.TropeCount)
		}
		if b.TropeLabel == "" {
			t.Fatalf("block %d missing trope_label", b.ID)
		}
		seenScene[b.ScenePos] = b.SceneTitle
	}
	if len(seenScene) != 2 {
		t.Fatalf("blocks must cover both scenes, saw %v", seenScene)
	}
	if seenScene[0] != "The Platform" || seenScene[1] != "The Courier" {
		t.Fatalf("scene titles not propagated: %v", seenScene)
	}
	// Block texts joined must reproduce the chapter content (modulo separators).
	var rebuilt []string
	for _, b := range ch.Blocks {
		rebuilt = append(rebuilt, b.Text)
	}
	if strings.Join(rebuilt, "\n\n") != strings.TrimSpace(ch.Content) {
		t.Fatal("trope blocks must tile the full chapter prose")
	}
}
