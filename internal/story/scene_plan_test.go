package story

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"showmethestory/internal/config"
	"showmethestory/internal/sse"
)

// ---------------------------------------------------------------------------
// parseScenePlanJSON
// ---------------------------------------------------------------------------

func TestParseScenePlanJSONPlain(t *testing.T) {
	raw := `[{"title":"Arrival","description":"Ka arrives at the gate","pov":"Ka","words_budget":900},{"title":"Betrayal","description":"The guard turns on Ka","pov":"Rin","words_budget":1100}]`
	scenes, err := parseScenePlanJSON(raw)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(scenes) != 2 || scenes[0].WordsBudget != 900 || scenes[1].PoV != "Rin" {
		t.Fatalf("unexpected scenes: %+v", scenes)
	}
}

func TestParseScenePlanJSONToleratesFencesAndProse(t *testing.T) {
	raw := "Here is the plan:\n```json\n[{\"title\":\"A\",\"description\":\"d\",\"pov\":\"p\",\"words_budget\":500}]\n```\nHope that helps!"
	scenes, err := parseScenePlanJSON(raw)
	if err != nil {
		t.Fatalf("fenced/prose response should parse: %v", err)
	}
	if len(scenes) != 1 || scenes[0].Title != "A" {
		t.Fatalf("unexpected scenes: %+v", scenes)
	}
}

func TestParseScenePlanJSONNoArray(t *testing.T) {
	if _, err := parseScenePlanJSON("I could not plan this chapter."); err == nil {
		t.Fatal("expected error for response without JSON array")
	}
}

// ---------------------------------------------------------------------------
// normalizeScenePlan
// ---------------------------------------------------------------------------

func sumBudgets(scenes []Scene) int {
	total := 0
	for _, s := range scenes {
		total += s.WordsBudget
	}
	return total
}

func TestNormalizeScenePlanRescalesToTarget(t *testing.T) {
	in := []Scene{
		{Title: "One", Description: "d1", WordsBudget: 1000},
		{Title: "Two", Description: "d2", WordsBudget: 1000},
		{Title: "Three", Description: "d3", WordsBudget: 1000},
	}
	out := normalizeScenePlan(in, 4000)
	if len(out) != 3 {
		t.Fatalf("expected 3 scenes, got %d", len(out))
	}
	if got := sumBudgets(out); got != 4000 {
		t.Fatalf("budgets must sum exactly to target, got %d", got)
	}
	for i, s := range out {
		if s.ID != i+1 {
			t.Fatalf("IDs must be renumbered from 1, scene %d has ID %d", i, s.ID)
		}
		if s.WordsBudget < scenePlanMinSceneWords {
			t.Fatalf("scene %d below per-scene floor: %d", i, s.WordsBudget)
		}
	}
}

func TestNormalizeScenePlanEvenSplitFallback(t *testing.T) {
	// Missing/absurd budgets (<10) fall back to an even split of the target.
	in := []Scene{
		{Title: "A", Description: "d", WordsBudget: 0},
		{Title: "B", Description: "d", WordsBudget: 3}, // absurd -> fallback
		{Title: "C", Description: "d", WordsBudget: 700},
	}
	out := normalizeScenePlan(in, 2100)
	if len(out) != 3 {
		t.Fatalf("expected 3 scenes, got %d", len(out))
	}
	if got := sumBudgets(out); got != 2100 {
		t.Fatalf("sum must equal target after rescale, got %d", got)
	}
}

func TestNormalizeScenePlanDropsEmptyScenes(t *testing.T) {
	in := []Scene{
		{Title: "", Description: "   ", WordsBudget: 800},
		{Title: "Real", Description: "happens", WordsBudget: 800},
	}
	out := normalizeScenePlan(in, 1600)
	if len(out) != 1 || out[0].Title != "Real" {
		t.Fatalf("empty scene should be dropped, got %+v", out)
	}
	if sumBudgets(out) != 1600 {
		t.Fatalf("remaining scene should absorb full target, got %d", sumBudgets(out))
	}
}

func TestNormalizeScenePlanTrimsOvershootWithoutBreakingFloor(t *testing.T) {
	// Many small budgets over a tiny target: trimming must stop at the floor
	// rather than producing negative/zero budgets.
	in := make([]Scene, 0, scenePlanMaxScenes)
	for i := 0; i < scenePlanMaxScenes; i++ {
		in = append(in, Scene{Title: fmt.Sprintf("S%d", i), Description: "d", WordsBudget: 1000})
	}
	out := normalizeScenePlan(in, 3000)
	for i, s := range out {
		if s.WordsBudget < scenePlanMinSceneWords {
			t.Fatalf("scene %d trimmed below floor: %d", i, s.WordsBudget)
		}
	}
}

func TestNormalizeScenePlanAllEmptyReturnsNil(t *testing.T) {
	if out := normalizeScenePlan([]Scene{{Title: "", Description: ""}}, 4000); out != nil {
		t.Fatalf("expected nil for all-empty plan, got %+v", out)
	}
}

// ---------------------------------------------------------------------------
// renderScenePlanBlock
// ---------------------------------------------------------------------------

func TestRenderScenePlanBlockEnglish(t *testing.T) {
	plan := &ScenePlan{
		TargetWords: 4000,
		Scenes: []Scene{
			{ID: 1, Title: "Arrival", Description: "Ka reaches the gate.", PoV: "Ka", WordsBudget: 1000},
			{ID: 2, Title: "Standoff", Description: "Guards block the way.", PoV: "Rin", WordsBudget: 3000},
		},
	}
	block := renderScenePlanBlock(plan, "en")
	for _, want := range []string{"[Scene plan for THIS chapter", "total 4000 words", "[1000 words · ends ≈25% of chapter]", "PoV: Ka", "never compress a scene into one paragraph"} {
		if !strings.Contains(block, want) {
			t.Fatalf("english block missing %q:\n%s", want, block)
		}
	}
}

func TestRenderScenePlanBlockSpanish(t *testing.T) {
	plan := &ScenePlan{
		TargetWords: 4000,
		Scenes:      []Scene{{ID: 1, Title: "Llegada", Description: "Ka llega a la puerta.", WordsBudget: 4000}},
	}
	block := renderScenePlanBlock(plan, "es")
	for _, want := range []string{"[Plan de escenas de ESTE capítulo", "4000 palabras", "nunca comprimas una escena"} {
		if !strings.Contains(block, want) {
			t.Fatalf("spanish block missing %q:\n%s", want, block)
		}
	}
}

func TestRenderScenePlanBlockEmpty(t *testing.T) {
	if got := renderScenePlanBlock(nil, "en"); got != "" {
		t.Fatalf("nil plan must render empty, got %q", got)
	}
	if got := renderScenePlanBlock(&ScenePlan{}, "en"); got != "" {
		t.Fatalf("scene-less plan must render empty, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// ensureScenePlanForWriting (the writing gate)
// ---------------------------------------------------------------------------

const scenePlanAnswer = `[{"title":"Arrival","description":"Ka reaches the gate and meets Rin","pov":"Ka","words_budget":1000},{"title":"Crossing","description":"They slip through the broken wall","pov":"Ka","words_budget":1500},{"title":"Ambush","description":"Hunters corner them; Rin turns on Ka","pov":"Rin","words_budget":1500}]`

func newScenePlanFixture(t *testing.T) (*config.Config, *Progress, string) {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Language = "en"
	state := &Progress{
		Phase:               "writing",
		CurrentChapterIndex: 0,
		Chapters: []ChapterState{
			{Num: 1, Title: "The Gate", Outline: "Ka reaches the gate, crosses with Rin, survives an ambush.", Status: StatusPending},
			{Num: 2, Title: "The City", Outline: "Ka enters the city.", Status: StatusPending},
		},
	}
	return cfg, state, filepath.Join(t.TempDir(), "progress.json")
}

func TestEnsureScenePlanAutoModeGeneratesAndApproves(t *testing.T) {
	api := batchAPI(t, func(string) string { return scenePlanAnswer })
	cfg, state, progressPath := newScenePlanFixture(t)
	state.AutoScenePlan = true
	logger := sse.NewLogBroadcaster()

	if err := ensureScenePlanForWriting(context.Background(), api, cfg, state, 0, progressPath, logger); err != nil {
		t.Fatalf("auto mode must not pause: %v", err)
	}
	plan := state.Chapters[0].ScenePlan
	if plan == nil || !plan.Approved || len(plan.Scenes) != 3 {
		t.Fatalf("expected approved 3-scene plan, got %+v", plan)
	}
	if sumBudgets(plan.Scenes) != plan.TargetWords {
		t.Fatalf("approved plan budgets must sum to target: %d vs %d", sumBudgets(plan.Scenes), plan.TargetWords)
	}
	// Plan persisted: reload and check it survived.
	reloaded, err := LoadProgress(progressPath)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Chapters[0].ScenePlan == nil || !reloaded.Chapters[0].ScenePlan.Approved {
		t.Fatal("approved plan must persist to progress file")
	}
}

func TestEnsureScenePlanManualModePausesForReview(t *testing.T) {
	api := batchAPI(t, func(string) string { return scenePlanAnswer })
	cfg, state, progressPath := newScenePlanFixture(t)
	state.AutoScenePlan = false
	logger := sse.NewLogBroadcaster()

	err := ensureScenePlanForWriting(context.Background(), api, cfg, state, 0, progressPath, logger)
	var pending *ScenePlanPendingError
	if !asScenePlanPending(err, &pending) {
		t.Fatalf("manual mode must pause with ScenePlanPendingError, got %v", err)
	}
	if pending.ChapterNum != 1 {
		t.Fatalf("pending error must carry chapter number, got %d", pending.ChapterNum)
	}
	plan := state.Chapters[0].ScenePlan
	if plan == nil || plan.Approved || len(plan.Scenes) != 3 {
		t.Fatalf("plan must exist unapproved for review, got %+v", plan)
	}

	// Approving clears the gate; second call proceeds without another LLM hit.
	if _, err := ApproveScenePlan(state, 1, progressPath); err != nil {
		t.Fatalf("approve: %v", err)
	}
	calls := 0
	api2 := batchAPI(t, func(string) string { calls++; return scenePlanAnswer })
	if err := ensureScenePlanForWriting(context.Background(), api2, cfg, state, 0, progressPath, logger); err != nil {
		t.Fatalf("approved plan must pass the gate: %v", err)
	}
	if calls != 0 {
		t.Fatal("approved plan must not trigger a regeneration call")
	}
}

func TestEnsureScenePlanFailureFallsBackToWriting(t *testing.T) {
	// Planner returns garbage: writing must NOT be blocked outright — the
	// single-pass length controller still applies as before the feature.
	api := batchAPI(t, func(string) string { return "sorry, I cannot plan chapters" })
	cfg, state, progressPath := newScenePlanFixture(t)
	state.AutoScenePlan = true
	logger := sse.NewLogBroadcaster()

	if err := ensureScenePlanForWriting(context.Background(), api, cfg, state, 0, progressPath, logger); err != nil {
		t.Fatalf("planner failure must fall back gracefully, got %v", err)
	}
	if state.Chapters[0].ScenePlan != nil {
		t.Fatal("failed planning must not store a plan")
	}
}

// ---------------------------------------------------------------------------
// Set / Clear / Regenerate / Auto toggle
// ---------------------------------------------------------------------------

func TestSetScenePlanScenesRequiresReApproval(t *testing.T) {
	api := batchAPI(t, func(string) string { return scenePlanAnswer })
	cfg, state, progressPath := newScenePlanFixture(t)
	state.AutoScenePlan = true
	if err := ensureScenePlanForWriting(context.Background(), api, cfg, state, 0, progressPath, sse.NewLogBroadcaster()); err != nil {
		t.Fatalf("gate: %v", err)
	}

	edited := []Scene{
		{Title: "Arrival", Description: "kept", PoV: "Ka", WordsBudget: 1200},
		{Title: "New middle", Description: "user-added scene", PoV: "Rin", WordsBudget: 2800},
	}
	plan, err := SetScenePlanScenes(state, cfg, 1, edited, true, progressPath)
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	if plan.Approved {
		t.Fatal("any edit must require re-approval")
	}
	if len(plan.Scenes) != 2 || plan.Scenes[0].WordsBudget != 1200 || plan.Scenes[1].WordsBudget != 2800 {
		t.Fatalf("keepBudgets=true must preserve author numbers verbatim, got %+v", plan.Scenes)
	}
	if plan.Scenes[0].ID != 1 || plan.Scenes[1].ID != 2 {
		t.Fatal("edited scenes must be renumbered")
	}
}

func TestSetScenePlanScenesRescalesWhenNotKeepingBudgets(t *testing.T) {
	cfg, state, progressPath := newScenePlanFixture(t)
	target := state.Chapters[0].ScenePlan == nil // no plan yet; created below
	_ = target
	edited := []Scene{
		{Title: "A", Description: "d", WordsBudget: 100},
		{Title: "B", Description: "d", WordsBudget: 100},
	}
	plan, err := SetScenePlanScenes(state, cfg, 1, edited, false, progressPath)
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	if sumBudgets(plan.Scenes) != plan.TargetWords {
		t.Fatalf("rescaled budgets must sum to target %d, got %d", plan.TargetWords, sumBudgets(plan.Scenes))
	}
}

func TestSetScenePlanRejectsEmptyList(t *testing.T) {
	cfg, state, progressPath := newScenePlanFixture(t)
	if _, err := SetScenePlanScenes(state, cfg, 1, []Scene{}, false, progressPath); err == nil {
		t.Fatal("empty scene list must be rejected")
	}
}

func TestClearAndRegenerateScenePlan(t *testing.T) {
	api := batchAPI(t, func(string) string { return scenePlanAnswer })
	cfg, state, progressPath := newScenePlanFixture(t)
	state.AutoScenePlan = true
	logger := sse.NewLogBroadcaster()
	if err := ensureScenePlanForWriting(context.Background(), api, cfg, state, 0, progressPath, logger); err != nil {
		t.Fatalf("gate: %v", err)
	}
	if err := ClearScenePlan(state, 1, progressPath); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if state.Chapters[0].ScenePlan != nil {
		t.Fatal("cleared plan must be nil")
	}
	plan, err := RegenerateScenePlan(context.Background(), api, cfg, state, 1, "merge scenes 2 and 3", progressPath, logger)
	if err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	if len(plan.Scenes) != 3 || !plan.Approved {
		t.Fatalf("regenerated plan in auto mode must be approved, got %+v", plan)
	}
}

func TestSetAutoScenePlanPersists(t *testing.T) {
	_, state, progressPath := newScenePlanFixture(t)
	if err := SetAutoScenePlan(state, true, progressPath); err != nil {
		t.Fatalf("toggle: %v", err)
	}
	reloaded, err := LoadProgress(progressPath)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !reloaded.AutoScenePlan {
		t.Fatal("auto flag must persist")
	}
}

func TestApproveScenePlanRequiresExistingPlan(t *testing.T) {
	_, state, progressPath := newScenePlanFixture(t)
	if _, err := ApproveScenePlan(state, 1, progressPath); err == nil {
		t.Fatal("approving a nonexistent plan must fail")
	}
}

func TestGetScenePlanUnknownChapter(t *testing.T) {
	_, state, _ := newScenePlanFixture(t)
	if _, _, err := GetScenePlan(state, 99); err == nil {
		t.Fatal("unknown chapter number must error")
	}
}

// asScenePlanPending keeps errors.As usage local so the test file does not
// need the errors import twice; mirrors errors.As semantics for our type.
func asScenePlanPending(err error, target **ScenePlanPendingError) bool {
	for err != nil {
		if p, ok := err.(*ScenePlanPendingError); ok {
			*target = p
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// Compile-time sanity: the plan serializes to/from the JSON shape the frontend
// consumes (snake_case keys used by ScenePlanEditor.svelte).
func TestScenePlanJSONShape(t *testing.T) {
	data, err := json.Marshal(Scene{ID: 1, Title: "A", Description: "d", PoV: "p", WordsBudget: 500})
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, key := range []string{`"id"`, `"title"`, `"description"`, `"pov"`, `"words_budget"`} {
		if !strings.Contains(s, key) {
			t.Fatalf("scene JSON missing key %s: %s", key, s)
		}
	}
}
