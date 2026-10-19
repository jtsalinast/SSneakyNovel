package story

import (
	"showmethestory/internal/config"
	"showmethestory/internal/prose"
	"strings"
	"testing"
)

func TestCalcChapterLengthRange(t *testing.T) {
	tests := []struct {
		target  int
		wantMin int
		wantMax int
	}{
		{5000, 4500, 7500},
		{2500, 2250, 3750},
		{10000, 9000, 15000},
		{0, 2250, 3750}, // defaults to 2500
	}
	for _, tt := range tests {
		minLen, maxLen := calcChapterLengthRange(tt.target)
		if minLen != tt.wantMin || maxLen != tt.wantMax {
			t.Errorf("calcChapterLengthRange(%d) = (%d,%d), want (%d,%d)",
				tt.target, minLen, maxLen, tt.wantMin, tt.wantMax)
		}
	}
}

func TestResolveChapterTargetWords(t *testing.T) {
	cfg := &config.Config{}
	cfg.Story.TargetWordsPerChapter = 4000
	// nil snapshot -> live config value (never a silent default).
	st := &Progress{}
	if got, src := resolveChapterTargetWords(st, cfg); got != 4000 || src != "config" {
		t.Fatalf("nil snapshot: got (%d,%s), want (4000,config)", got, src)
	}
	// snapshot wins over live config.
	snap := config.StoryConfig{TargetWordsPerChapter: 3000}
	st.StoryConfigSnapshot = &snap
	if got, src := resolveChapterTargetWords(st, cfg); got != 3000 || src != "snapshot" {
		t.Fatalf("snapshot: got (%d,%s), want (3000,snapshot)", got, src)
	}
	if got, _ := resolveChapterTargetWords(nil, nil); got != 2500 {
		t.Fatalf("nil everything: got %d, want 2500", got)
	}
}

func TestChapterLengthInRange(t *testing.T) {
	minLen, maxLen := calcChapterLengthRange(5000)
	if !chapterLengthInRange(strings.Repeat("word ", 5000), minLen, maxLen) {
		t.Fatal("5000 prose units should be in range for 5000 target")
	}
	if chapterLengthInRange(strings.Repeat("word ", 15000), minLen, maxLen) {
		t.Fatal("15000 prose units should be out of range for 5000 target")
	}
}

func TestProseLengthScore(t *testing.T) {
	minLen, maxLen := calcChapterLengthRange(5000)
	target := 5000
	inRange := proseLengthScore(5000, minLen, maxLen, target)
	slightlyOver := proseLengthScore(6200, minLen, maxLen, target)
	farOver := proseLengthScore(15000, minLen, maxLen, target)
	if inRange >= slightlyOver || slightlyOver >= farOver {
		t.Fatalf("scores want inRange < slightlyOver < farOver, got %d %d %d", inRange, slightlyOver, farOver)
	}
}

func TestIsSoftLengthDeviation(t *testing.T) {
	_, maxLen := calcChapterLengthRange(5000) // 7500 (150% hard bound)
	if !isSoftLengthDeviation(maxLen+200, 4500, maxLen) {
		t.Fatal("200 over max should be soft")
	}
	if isSoftLengthDeviation(maxLen+500, 4500, maxLen) {
		t.Fatal("500 over max should not be soft for a hard percentage band")
	}
}

func TestShouldAttemptLengthAdjust(t *testing.T) {
	minLen, maxLen := calcChapterLengthRange(5000)
	if shouldAttemptLengthAdjust(maxLen+200, minLen, maxLen) {
		t.Fatal("soft overflow should not attempt adjust")
	}
	if !shouldAttemptLengthAdjust(maxLen+400, minLen, maxLen) {
		t.Fatal("moderate overflow should attempt adjust")
	}
	if shouldAttemptLengthAdjust(maxLen*120/100+1, minLen, maxLen) {
		t.Fatal(">120% max should not attempt adjust")
	}
}

func TestMaybeUpdateBestDraft(t *testing.T) {
	minLen, maxLen := calcChapterLengthRange(5000) // 4500–7500
	target := 5000
	var best string
	score := int(^uint(0) >> 1)
	maybeUpdateBestDraft(&best, &score, strings.Repeat("word ", 15000), minLen, maxLen, target)
	maybeUpdateBestDraft(&best, &score, strings.Repeat("word ", 6200), minLen, maxLen, target)
	if prose.CountProseUnits(best) != 6200 {
		t.Fatalf("best draft = %d units, want 6200", prose.CountProseUnits(best))
	}
}

// The hard 90% floor must reject a 1050-word draft for a 4000-word target and
// prefer a closer-to-band draft.
func TestHardBandRejectsVeryShortDraft(t *testing.T) {
	minLen, maxLen := calcChapterLengthRange(4000) // 3600–6000
	if minLen != 3600 || maxLen != 6000 {
		t.Fatalf("range for 4000 = (%d,%d), want (3600,6000)", minLen, maxLen)
	}
	short := strings.Repeat("palabra ", 1050)
	if chapterLengthInRange(short, minLen, maxLen) {
		t.Fatal("1050-word draft must NOT be in range for 4000 target")
	}
	if isSoftLengthDeviation(1050, minLen, maxLen) {
		t.Fatal("1050 vs 3600 floor is not a soft deviation")
	}
	var best string
	score := int(^uint(0) >> 1)
	maybeUpdateBestDraft(&best, &score, short, minLen, maxLen, 4000)
	maybeUpdateBestDraft(&best, &score, strings.Repeat("palabra ", 3800), minLen, maxLen, 4000)
	if prose.CountProseUnits(best) != 3800 {
		t.Fatalf("best draft = %d units, want 3800", prose.CountProseUnits(best))
	}
}
