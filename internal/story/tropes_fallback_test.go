package story

import "testing"

// A 12-paragraph scene with UNIFORM narration mode (no dialogue/narration
// alternation) must still segment into multiple beats, not one giant beat.
func TestSplitTropesUniformLongSceneSegments(t *testing.T) {
	paras := make([]string, 0, 12)
	for i := 0; i < 12; i++ {
		if i == 6 {
			paras = append(paras, "De repente el suelo tembló bajo sus pies y todo cambió.")
		} else {
			paras = append(paras, "Narración plana sin diálogos en el párrafo número "+string(rune('A'+i))+".")
		}
	}
	scene := joinWithSepForTest(paras, "\n\n")
	beats, ranges := splitTropes(scene)
	if len(beats) < 2 {
		t.Fatalf("expected >=2 beats for a 12-para uniform scene, got %d", len(beats))
	}
	total := 0
	for _, r := range ranges {
		total += r[1] - r[0]
	}
	if total != 12 {
		t.Fatalf("ranges must tile all 12 paragraphs, got %d", total)
	}
	t.Logf("beats=%d", len(beats))
}

func joinWithSepForTest(paras []string, sep string) string {
	out := ""
	for i, p := range paras {
		if i > 0 {
			out += sep
		}
		out += p
	}
	return out
}
