package story

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"showmethestory/internal/config"
	"showmethestory/internal/prose"
	"showmethestory/internal/sse"
	"strings"
	"sync"
	"testing"
)

func TestCalcChapterLengthRange(t *testing.T) {
	tests := []struct {
		target  int
		wantMin int
		wantMax int
	}{
		{5000, 4500, 9000},
		{2500, 2250, 4500},
		{10000, 9000, 18000},
		{0, 2250, 4500}, // defaults to 2500
	}
	for _, tt := range tests {
		minLen, maxLen := calcChapterLengthRange(tt.target)
		if minLen != tt.wantMin || maxLen != tt.wantMax {
			t.Errorf("calcChapterLengthRange(%d) = (%d,%d), want (%d,%d)",
				tt.target, minLen, maxLen, tt.wantMin, tt.wantMax)
		}
	}
}

func TestCalcSceneLengthRange(t *testing.T) {
	tests := []struct {
		budget  int
		wantMin int
		wantMax int
	}{
		{1000, 900, 1100}, // ±10% band
		{833, 750, 916},   // ceil at the floor, floor at the ceiling of ±10%
		{5, 5, 5},         // tiny budgets: ±10% collapses onto the budget itself
		{0, 0, 0},         // degenerate budget: nothing to enforce
	}
	for _, tt := range tests {
		minLen, maxLen := calcSceneLengthRange(tt.budget)
		if minLen != tt.wantMin || maxLen != tt.wantMax {
			t.Errorf("calcSceneLengthRange(%d) = (%d,%d), want (%d,%d)",
				tt.budget, minLen, maxLen, tt.wantMin, tt.wantMax)
		}
	}
}

func TestFormatSceneLengthRetryFeedbackMentionsRetries(t *testing.T) {
	got := formatSceneLengthRetryFeedback("Ambush", 500, 900, 1100, 1, sceneBudgetMaxRetries, "en")
	if !strings.Contains(got, "retry 2 of 3") || !strings.Contains(got, "2 attempt(s) left") {
		t.Fatalf("feedback should count remaining retries, got %q", got)
	}
	if !strings.Contains(got, "too short") || !strings.Contains(got, "aiming for about 1000 words") || !strings.Contains(got, "roughly 500 more words") {
		t.Fatalf("feedback should give direction + concrete delta, got %q", got)
	}
	last := formatSceneLengthRetryFeedback("Ambush", 500, 900, 1100, 3, sceneBudgetMaxRetries, "en")
	if strings.Contains(last, "more attempt") {
		t.Fatalf("final retry must not promise more attempts, got %q", last)
	}
	over := formatSceneLengthRetryFeedback("Ambush", 1400, 900, 1100, 1, sceneBudgetMaxRetries, "en")
	if !strings.Contains(over, "too long") || !strings.Contains(over, "trim roughly 400 words") {
		t.Fatalf("over-length feedback should ask to cut a concrete amount, got %q", over)
	}
	es := formatSceneLengthRetryFeedback("Emboscada", 500, 900, 1100, 2, sceneBudgetMaxRetries, "es")
	if !strings.Contains(es, "reintento 3 de 3") {
		t.Fatalf("spanish feedback missing retry counter, got %q", es)
	}
	if !strings.Contains(es, "salió corta") || !strings.Contains(es, "apuntando a unas 1000") || !strings.Contains(es, "500 palabras más") {
		t.Fatalf("spanish feedback should give direction + concrete delta, got %q", es)
	}
	esOver := formatSceneLengthRetryFeedback("Emboscada", 1400, 900, 1100, 1, sceneBudgetMaxRetries, "es")
	if !strings.Contains(esOver, "salió larga") || !strings.Contains(esOver, "recorta unas 400") {
		t.Fatalf("spanish over-length feedback should ask to cut a concrete amount, got %q", esOver)
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
	minLen, maxLen := calcChapterLengthRange(5000) // 4500–9000 (90%–180%)
	if !chapterLengthInRange(strings.Repeat("word ", 5000), minLen, maxLen) {
		t.Fatal("5000 prose units should be in range for 5000 target")
	}
	if !chapterLengthInRange(strings.Repeat("word ", 8900), minLen, maxLen) {
		t.Fatal("8900 prose units should be inside the 180% ceiling")
	}
	if chapterLengthInRange(strings.Repeat("word ", 4400), minLen, maxLen) {
		t.Fatal("4400 prose units is below the 90% floor and must be out of range")
	}
	if chapterLengthInRange(strings.Repeat("word ", 9100), minLen, maxLen) {
		t.Fatal("9100 prose units exceeds the 180% ceiling and must be out of range")
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
	_, maxLen := calcChapterLengthRange(5000) // 9000 (180% hard bound)
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
	minLen, maxLen := calcChapterLengthRange(5000) // 4500–9000
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
// --- continuation / truncation helpers -------------------------------------

func TestIsLikelyTruncatedFragment(t *testing.T) {
	minLen, maxLen := calcChapterLengthRange(4000) // 3600–6000
	// A fragment well under 40% of the floor is treated as truncated output.
	short := strings.Repeat("word ", 1200)
	if !isLikelyTruncatedFragment(short, minLen, maxLen, 4000) {
		t.Fatal("1200-word draft for a 4000 target must be flagged as truncated")
	}
	// 1500 words ≥ 40% of 3600 (1440): not a fragment, just a short draft.
	longish := strings.Repeat("word ", 1500)
	if isLikelyTruncatedFragment(longish, minLen, maxLen, 4000) {
		t.Fatal("1500-word draft should not be flagged as truncated fragment")
	}
	// In-band drafts are never fragments.
	ok := strings.Repeat("word ", 4000)
	if isLikelyTruncatedFragment(ok, minLen, maxLen, 4000) {
		t.Fatal("in-band draft must not be flagged as truncated")
	}
}

func TestSceneAnchorFragment(t *testing.T) {
	sc := Scene{Title: "The Market Confrontation", Description: "Kira accuses Dorian in front of the crowd"}
	got := sceneAnchorFragment(sc)
	if got != "the market confrontation" {
		t.Fatalf("anchor from title = %q, want %q", got, "the market confrontation")
	}
	empty := Scene{Description: "A long description with many words in it about a duel at dawn"}
	got = sceneAnchorFragment(empty)
	if got != "a long description with many" {
		t.Fatalf("anchor from description = %q, want first 5 words", got)
	}
	if sceneAnchorFragment(Scene{}) != "" {
		t.Fatal("empty scene must produce empty anchor")
	}
}

func TestRemainingScenesFromPlan(t *testing.T) {
	plan := &ScenePlan{Scenes: []Scene{
		{ID: 1, Title: "Dawn patrol", WordsBudget: 800},
		{ID: 2, Title: "Broken signal", WordsBudget: 1200},
		{ID: 3, Title: "The ambush", WordsBudget: 1200},
		{ID: 4, Title: "Aftermath silence", WordsBudget: 800},
	}}
	// Draft covers scenes 1 and 2 only → 3 and 4 remain.
	partial := "…dawn patrol begins here… then the broken signal reaches camp and everyone freezes…"
	remaining := remainingScenesFromPlan(plan, partial)
	if len(remaining) != 2 || remaining[0].ID != 3 || remaining[1].ID != 4 {
		t.Fatalf("remaining = %+v, want scenes 3 and 4", remaining)
	}
	// Draft covers everything → nothing remains.
	full := partial + "…the ambush unfolds violently… aftermath silence falls over the ridge…"
	if rem := remainingScenesFromPlan(plan, full); rem != nil {
		t.Fatalf("expected no remaining scenes, got %+v", rem)
	}
	// Anchors mostly missing (>2/3) → assume everything after scene 1 remains.
	nolinks := "some unrelated prose that matches none of the anchors at all"
	if rem := remainingScenesFromPlan(plan, nolinks); len(rem) != 3 || rem[0].ID != 2 {
		t.Fatalf("fallback remaining = %+v, want scenes 2..4", rem)
	}
	// Single-scene or nil plans cannot drive continuation.
	if rem := remainingScenesFromPlan(&ScenePlan{Scenes: []Scene{{ID: 1}}}, partial); rem != nil {
		t.Fatal("one-scene plan must return nil")
	}
	if rem := remainingScenesFromPlan(nil, partial); rem != nil {
		t.Fatal("nil plan must return nil")
	}
}

func TestFormatSceneContinuationBlock(t *testing.T) {
	remaining := []Scene{
		{ID: 3, Title: "The ambush", Description: "Raiders cut off the convoy", WordsBudget: 1200},
		{ID: 4, Title: "", Description: "Silence after the fight", WordsBudget: 800},
	}
	en := formatSceneContinuationBlock(1500, 2000, remaining, "en")
	for _, want := range []string{"CONTINUATION", "~1500 words", "Scene 3 (1200 words): The ambush", "Scene 4 (800 words)", "approximately 2000 words"} {
		if !strings.Contains(en, want) {
			t.Fatalf("EN block missing %q\n---\n%s", want, en)
		}
	}
	es := formatSceneContinuationBlock(1500, 2000, remaining, "es")
	for _, want := range []string{"CONTINUACIÓN", "Escena 3 (1200 palabras)", "Escena 4 (800 palabras)"} {
		if !strings.Contains(es, want) {
			t.Fatalf("ES block missing %q\n---\n%s", want, es)
		}
	}
}

func TestContentOverlapRatio(t *testing.T) {
	lineA := "The rain hammered the tin roof while Mara counted the seconds aloud."
	lineB := "She had promised him she would wait until the very last bell rang out."
	existing := lineA + "\n\nSome other prose entirely about the harbor lights flickering on."
	// Fresh segment: zero overlap.
	if r := contentOverlapRatio(existing, lineB); r != 0 {
		t.Fatalf("overlap for fresh text = %v, want 0", r)
	}
	// Exact restatement: full overlap.
	if r := contentOverlapRatio(existing, lineA); r < 0.99 {
		t.Fatalf("overlap for duplicated line = %v, want ~1", r)
	}
	// Short lines (<30 runes) are ignored as evidence.
	if r := contentOverlapRatio("yes yes yes", "yes"); r != 0 {
		t.Fatalf("short-line overlap = %v, want 0", r)
	}
	// Mixed: half duplicated.
	mixed := lineA + "\n" + lineB
	if r := contentOverlapRatio(existing, mixed); r < 0.4 || r > 0.6 {
		t.Fatalf("mixed overlap = %v, want ~0.5", r)
	}
}

func TestPerSceneWordCountsAndBudgetLines(t *testing.T) {
	plan := &ScenePlan{Scenes: []Scene{
		{ID: 1, Title: "Dawn patrol", WordsBudget: 800},
		{ID: 2, Title: "Broken signal", WordsBudget: 1200},
	}}
	content := "dawn patrol starts with cold coffee and wet boots " +
		strings.Repeat("filler ", 100) +
		"broken signal arrives crackling through the static " +
		strings.Repeat("more ", 50)
	counts := perSceneWordCounts(plan, content)
	if counts == nil {
		t.Fatal("both anchors present: split must succeed")
	}
	if counts[0] <= counts[1] {
		t.Fatalf("scene 1 (%d) should have more words than scene 2 (%d)", counts[0], counts[1])
	}
	block := scenePlanBudgetLines(plan, content, "en")
	if !strings.Contains(block, "[Scene plan budgets]") ||
		!strings.Contains(block, "Dawn patrol (~800 words budget") ||
		!strings.Contains(block, "Broken signal (~1200 words budget") {
		t.Fatalf("budget block malformed:\n%s", block)
	}
	// Unreliable split (no anchors found) → plain budget list without counts.
	orphan := &ScenePlan{Scenes: []Scene{
		{ID: 1, Title: "Zeta protocol", WordsBudget: 700},
		{ID: 2, Title: "Yawl rescue", WordsBudget: 900},
	}}
	block = scenePlanBudgetLines(orphan, "totally unrelated prose here", "es")
	if !strings.Contains(block, "[Presupuesto del plan de escenas]") || !strings.Contains(block, "(~700 palabras)") {
		t.Fatalf("ES fallback block malformed:\n%s", block)
	}
	if scenePlanBudgetLines(nil, content, "en") != "" {
		t.Fatal("nil plan must render empty block")
	}
}

func TestHardBandRejectsVeryShortDraft(t *testing.T) {
	minLen, maxLen := calcChapterLengthRange(4000) // 3600–7200 (90%–180%)
	if minLen != 3600 || maxLen != 7200 {
		t.Fatalf("range for 4000 = (%d,%d), want (3600,7200)", minLen, maxLen)
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

// --- continueTruncatedChapter orchestration ---------------------------------

// streamAPI serves SSE completions whose content/finish_reason come from a
// per-request-index function, recording each prompt for assertions.
func streamAPI(t *testing.T, answer func(reqIndex int, prompt string) (content, finishReason string)) (*config.APIConfig, *[]string) {
	t.Helper()
	var mu sync.Mutex
	prompts := &[]string{}
	count := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "bad request", 400)
			return
		}
		prompt := request.Messages[len(request.Messages)-1].Content
		mu.Lock()
		idx := count
		count++
		*prompts = append(*prompts, prompt)
		mu.Unlock()
		content, fr := answer(idx, prompt)
		payload, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]string{"content": content}, "finish_reason": fr}}})
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", payload)
	}))
	t.Cleanup(server.Close)
	return &config.APIConfig{BaseURL: server.URL, APIKey: "test", Model: "test"}, prompts
}

func newContinuationFixture() (*config.Config, *Progress) {
	cfg := config.DefaultConfig()
	cfg.Language = "en"
	state := &Progress{
		Phase:               "writing",
		CurrentChapterIndex: 0,
		Chapters: []ChapterState{{
			Num:     1,
			Title:   "The Gate",
			Outline: "Ka reaches the gate with Rin; an ambush splits the party; Ka holds the line alone.",
			Status:  StatusPending,
			ScenePlan: &ScenePlan{Approved: true, TargetWords: 3000, Scenes: []Scene{
				{ID: 1, Title: "Road to the gate", Description: "Ka and Rin walk the salt road toward the gate", WordsBudget: 1000},
				{ID: 2, Title: "Ambush at dusk", Description: "Masked riders spring the ambush at dusk", WordsBudget: 1000},
				{ID: 3, Title: "Holding the line", Description: "Ka holds the collapsing line alone", WordsBudget: 1000},
			}},
		}},
	}
	return cfg, state
}

func TestContinueTruncatedChapterAppendsRemainingScenes(t *testing.T) {
	api, prompts := streamAPI(t, func(idx int, prompt string) (string, string) {
		switch idx {
		case 0: // writes scene 2, still cut off by the cap
			return strings.Repeat("rider ", 500) + "ambush at dusk erupts along the ridge line ", "length"
		default: // finishes scene 3 cleanly
			return strings.Repeat("shield ", 500) + "holding the line costs Ka everything ", "stop"
		}
	})
	cfg, state := newContinuationFixture()
	partial := strings.Repeat("salt ", 300) + "road to the gate stretches ahead of them " + strings.Repeat("dust ", 300)

	content, complete := continueTruncatedChapter(context.Background(), api, cfg, state, 0, &ProjectSettings{}, "", partial, sse.NewLogBroadcaster())

	if !complete {
		t.Fatal("clean final round must mark the chapter as completed")
	}
	if !strings.Contains(content, "ambush at dusk") || !strings.Contains(content, "holding the line") {
		t.Fatal("both continuation segments must be appended to the partial draft")
	}
	if prose.CountProseUnits(content) <= prose.CountProseUnits(partial) {
		t.Fatal("continuation must grow the chapter")
	}
	if len(*prompts) != 2 {
		t.Fatalf("expected 2 continuation rounds, got %d", len(*prompts))
	}
	// The continuation prompt must carry the [CONTINUATION] block AND list only
	// the scenes missing from the partial draft (2 and 3, not 1).
	first := (*prompts)[0]
	if !strings.Contains(first, "[CONTINUATION") || !strings.Contains(first, "Ambush at dusk") || !strings.Contains(first, "Holding the line") {
		t.Fatalf("first continuation prompt missing block/scenes:\n%.400s", first)
	}
	if strings.Contains(first, "Road to the gate (") {
		t.Fatal("already-written scene 1 must not be listed as remaining")
	}
}

func TestContinueTruncatedChapterStopsOnDuplicateSegment(t *testing.T) {
	restated := strings.Repeat("salt ", 300) + "road to the gate stretches ahead of them " + strings.Repeat("dust ", 300)
	api, _ := streamAPI(t, func(idx int, prompt string) (string, string) {
		return restated, "stop" // model ignores instructions and copies the draft
	})
	cfg, state := newContinuationFixture()
	partial := restated

	content, complete := continueTruncatedChapter(context.Background(), api, cfg, state, 0, &ProjectSettings{}, "", partial, sse.NewLogBroadcaster())

	if complete {
		t.Fatal("duplicate-guarded stop must report an incomplete chapter")
	}
	if content != partial {
		t.Fatal("duplicated segment must NOT be appended")
	}
}

func TestContinueTruncatedChapterStopsOnDegenerateSegment(t *testing.T) {
	api, _ := streamAPI(t, func(idx int, prompt string) (string, string) {
		return "too short to matter", "stop"
	})
	cfg, state := newContinuationFixture()
	partial := strings.Repeat("salt ", 300) + "road to the gate stretches ahead of them "

	content, _ := continueTruncatedChapter(context.Background(), api, cfg, state, 0, &ProjectSettings{}, "", partial, sse.NewLogBroadcaster())

	if content != partial {
		t.Fatal("degenerate (<80 words) continuation must be discarded")
	}
}

func TestContinueTruncatedChapterStopsWhenAllScenesCovered(t *testing.T) {
	called := false
	api, _ := streamAPI(t, func(idx int, prompt string) (string, string) {
		called = true
		return "should never be requested", "stop"
	})
	cfg, state := newContinuationFixture()
	partial := "road to the gate stretches on... ambush at dusk breaks out... holding the line until dawn..."

	content, _ := continueTruncatedChapter(context.Background(), api, cfg, state, 0, &ProjectSettings{}, "", partial, sse.NewLogBroadcaster())

	if called {
		t.Fatal("no API call expected when every planned scene is already drafted")
	}
	if content != partial {
		t.Fatal("content must pass through untouched")
	}
}

// --- scene-by-scene sequential writing (writing_scenes.go) ------------------

func wordsN(n int) string { return strings.Repeat("word ", n) }

func TestSceneSequentialHappyPath(t *testing.T) {
	api, prompts := streamAPI(t, func(idx int, prompt string) (string, string) {
		return wordsN(1000), "stop" // three scenes × 1000 = 3000 = plan target
	})
	cfg, state := newContinuationFixture()
	content, ok := generateChapterScenesSequentially(context.Background(), api, cfg, state, 0, nil, "", 3000, 2700, 5400, nil)
	if !ok || content == "" {
		t.Fatalf("sequential pass should succeed, got ok=%v len=%d", ok, len(content))
	}
	if got := prose.CountProseUnits(content); got != 3000 {
		t.Fatalf("joined chapter = %d units, want 3000", got)
	}
	if len(*prompts) != 3 {
		t.Fatalf("expected exactly 3 scene calls, got %d", len(*prompts))
	}
	// Each call must target only its own scene and carry the length block.
	if !strings.Contains((*prompts)[1], "Write scene 2 of 3") ||
		!strings.Contains((*prompts)[1], "approximately 1000-word fully dramatized scene") ||
		!strings.Contains((*prompts)[1], "Keep the length within roughly 900–1100 words") {
		t.Fatalf("scene 2 prompt missing task/length block:\n%s", (*prompts)[1])
	}
	// Later calls receive the already-written scenes for continuity.
	if !strings.Contains((*prompts)[2], "scenes already written in this chapter") {
		t.Fatal("last scene prompt should include the written-scenes tail")
	}
}

func TestSceneSequentialRetriesOutOfBandScene(t *testing.T) {
	var mu sync.Mutex
	count := 0
	api, _ := streamAPI(t, func(idx int, prompt string) (string, string) {
		mu.Lock()
		count++
		c := count
		mu.Unlock()
		switch c {
		case 1:
			return wordsN(600), "stop" // scene 1 too short → retry
		case 2:
			return wordsN(1000), "stop" // retry lands in band
		default:
			return wordsN(1000), "stop" // scenes 2 and 3 first try
		}
	})
	cfg, state := newContinuationFixture()
	content, ok := generateChapterScenesSequentially(context.Background(), api, cfg, state, 0, nil, "", 3000, 2700, 5400, nil)
	if !ok {
		t.Fatal("sequential pass should succeed after a successful retry")
	}
	if got := prose.CountProseUnits(content); got != 3000 {
		t.Fatalf("joined chapter = %d units, want 3000 (bad draft must be replaced by retry)", got)
	}
}

func TestSceneSequentialKeepsClosestAfterThreeFailedRetries(t *testing.T) {
	var mu sync.Mutex
	count := 0
	api, _ := streamAPI(t, func(idx int, prompt string) (string, string) {
		mu.Lock()
		count++
		c := count
		mu.Unlock()
		if c <= 1+sceneBudgetMaxRetries { // scene 1 misses the band on every attempt
			return wordsN(600), "stop"
		}
		return wordsN(1000), "stop"
	})
	cfg, state := newContinuationFixture()
	content, ok := generateChapterScenesSequentially(context.Background(), api, cfg, state, 0, nil, "", 3000, 2700, 5400, nil)
	if !ok {
		t.Fatal("stubborn scene must not stall the chapter")
	}
	want := 600 + 2*1000
	if got := prose.CountProseUnits(content); got != want {
		t.Fatalf("joined chapter = %d units, want %d (closest scene-1 draft kept)", got, want)
	}
	if !strings.Contains(content, "LENGTH") && strings.Count(content, "word") != want {
		t.Fatal("unexpected content")
	}
}

func TestSceneSequentialFallsBackWithoutPlan(t *testing.T) {
	api, prompts := streamAPI(t, func(idx int, prompt string) (string, string) {
		return wordsN(1000), "stop"
	})
	cfg, state := newContinuationFixture()
	state.Chapters[0].ScenePlan = nil
	if _, ok := generateChapterScenesSequentially(context.Background(), api, cfg, state, 0, nil, "", 3000, 2700, 5400, nil); ok {
		t.Fatal("unplanned chapters must fall back to the one-shot pipeline")
	}
	if len(*prompts) != 0 {
		t.Fatalf("fallback should make no LLM calls, got %d", len(*prompts))
	}
}

// An empty scene call (API produced no text) is retried in a controlled way:
// one initial call plus up to sceneBudgetMaxRetries (3) retries per scene.
func TestSceneSequentialRetriesEmptyCallUpToThreeTimes(t *testing.T) {
	var mu sync.Mutex
	count := 0
	api, _ := streamAPI(t, func(idx int, prompt string) (string, string) {
		mu.Lock()
		c := count
		count++
		mu.Unlock()
		switch c {
		case 0:
			return "", "stop" // scene 1 first call fails → controlled retry
		case 1:
			return "", "stop" // second call also fails → keep retrying
		default:
			return wordsN(1000), "stop" // third call succeeds; scenes 2-3 succeed too
		}
	})
	cfg, state := newContinuationFixture()
	content, ok := generateChapterScenesSequentially(context.Background(), api, cfg, state, 0, nil, "", 3000, 2700, 5400, nil)
	if !ok {
		t.Fatal("empty-call retries must recover the scene, not stall the chapter")
	}
	if got := prose.CountProseUnits(content); got != 3000 {
		t.Fatalf("joined chapter = %d units, want 3000", got)
	}
}

// If every attempt of a scene returns no text at all, the scene is skipped
// after exactly 1 initial call + sceneBudgetMaxRetries (3) controlled retries
// (4 calls for that scene), and the remaining scenes still get written.
func TestSceneSequentialSkipsSceneAfterExhaustingEmptyRetries(t *testing.T) {
	var mu sync.Mutex
	count := 0
	api, _ := streamAPI(t, func(idx int, prompt string) (string, string) {
		mu.Lock()
		count++
		mu.Unlock()
		if strings.Contains(prompt, "Write scene 1 of 3") || strings.Contains(prompt, "Escribe la escena 1 de 3") {
			return "", "stop" // scene 1 never produces text
		}
		return wordsN(1000), "stop"
	})
	cfg, state := newContinuationFixture()
	content, ok := generateChapterScenesSequentially(context.Background(), api, cfg, state, 0, nil, "", 3000, 2700, 5400, nil)
	if !ok {
		t.Fatal("one dead scene must not fail the whole sequential pass")
	}
	if got := prose.CountProseUnits(content); got != 2000 {
		t.Fatalf("joined chapter = %d units, want 2000 (scene 1 skipped)", got)
	}
}

// When every attempt of a scene misses the band, the draft kept must be the
// one CLOSEST to the budget — not simply the first or the last one generated.
func TestSceneSequentialKeepsAttemptClosestToBudget(t *testing.T) {
	var mu sync.Mutex
	count := 0
	api, _ := streamAPI(t, func(idx int, prompt string) (string, string) {
		mu.Lock()
		count++
		c := count
		mu.Unlock()
		if c <= 1+sceneBudgetMaxRetries { // all attempts at scene 1 miss the band
			switch c {
			case 1:
				return wordsN(400), "stop" // dist 600
			case 2:
				return wordsN(800), "stop" // dist 200 → closest so far
			case 3:
				return wordsN(500), "stop" // dist 500 → worse, must NOT replace
			default:
				return wordsN(1300), "stop" // dist 300 → worse than 800, must NOT replace
			}
		}
		return wordsN(1000), "stop" // scenes 2 and 3 land in band first try
	})
	cfg, state := newContinuationFixture()
	content, ok := generateChapterScenesSequentially(context.Background(), api, cfg, state, 0, nil, "", 3000, 2700, 5400, nil)
	if !ok {
		t.Fatal("stubborn scene must not stall the chapter")
	}
	want := 800 + 2*1000
	if got := prose.CountProseUnits(content); got != want {
		t.Fatalf("joined chapter = %d units, want %d (the 800-word draft is closest to the 1000-word budget)", got, want)
	}
}
