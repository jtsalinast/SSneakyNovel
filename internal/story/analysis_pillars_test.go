package story

import (
	"os"
	"path/filepath"
	"testing"
)

// --- Rubric derivation ------------------------------------------------------

func TestDeriveScoreFromCriteria(t *testing.T) {
	if got := deriveScoreFromCriteria(nil); got != -1 {
		t.Fatalf("empty levels must yield -1, got %d", got)
	}
	if got := deriveScoreFromCriteria([]int{100, 100, 100}); got != 100 {
		t.Fatalf("all-perfect average = 100, got %d", got)
	}
	if got := deriveScoreFromCriteria([]int{0, 50}); got != 25 {
		t.Fatalf("average of 0 and 50 = 25, got %d", got)
	}
	// clamping: out-of-range model answers never break the derivation
	if got := deriveScoreFromCriteria([]int{-40, 160}); got != 50 {
		t.Fatalf("clamped (-40,160) -> (0,100) avg = 50, got %d", got)
	}
}

func TestFinalizePillarReport_ScoreDerivedFromChecklist(t *testing.T) {
	in := []PillarAssessment{{
		Key:   "story_structure",
		Score: 12, // bogus score the model claims — must be overridden
		Note:  "structure note",
		CriteriaLevels: []struct {
			Key   string `json:"key"`
			Level int    `json:"level"`
		}{
			{"hook_opening", 40}, {"beat_coverage", 40}, {"pacing_curve", 40},
			{"stakes_escalation", 40}, {"arc_payoffs", 40}, {"chapter_end_hooks", 40},
			{"scene_economy", 40},
		},
	}}
	pillars, _ := finalizePillarReport(in, nil)
	if len(pillars) != 4 {
		t.Fatalf("expected all 4 pillars backfilled, got %d", len(pillars))
	}
	var ss PillarAssessment
	for _, p := range pillars {
		if p.Key == "story_structure" {
			ss = p
		}
	}
	if ss.Score != 40 {
		t.Fatalf("score must be DERIVED from checklist (avg 40), got %d", ss.Score)
	}
	if len(ss.Checklist) != 7 {
		t.Fatalf("checklist attached with fixed items, got %d", len(ss.Checklist))
	}
	for _, c := range ss.Checklist {
		if c.Level != 40 {
			t.Fatalf("criterion %s level = %d, want 40", c.Key, c.Level)
		}
	}
	// Ungraded pillar keeps a below-strong default so the UI never claims
	// "no changes needed" without proof.
	for _, p := range pillars {
		if p.Key == "tone" && p.Score >= strongPillarScore {
			t.Fatalf("missing pillar must not read as strong, got %d", p.Score)
		}
	}
}

func TestFinalizePillarReport_ClosingAllItemsDrivesTo100(t *testing.T) {
	lv := func(v int) []struct {
		Key   string `json:"key"`
		Level int    `json:"level"`
	} {
		out := make([]struct {
			Key   string `json:"key"`
			Level int    `json:"level"`
		}, 0, len(PillarChecklist("tone")))
		for _, c := range PillarChecklist("tone") {
			e := struct {
				Key   string `json:"key"`
				Level int    `json:"level"`
			}{c.Key, v}
			out = append(out, e)
		}
		return out
	}
	pillars, _ := finalizePillarReport([]PillarAssessment{{Key: "tone", Score: 30, CriteriaLevels: lv(100)}}, nil)
	for _, p := range pillars {
		if p.Key == "tone" && p.Score != 100 {
			t.Fatalf("every item at 100 must derive tone=100, got %d", p.Score)
		}
	}
}

func TestFinalizePillarReport_ImpactCappedAndPriorityDerived(t *testing.T) {
	in := []PillarAssessment{{
		Key: "story_structure", Score: 50,
		CriteriaLevels: []struct {
			Key   string `json:"key"`
			Level int    `json:"level"`
		}{{"hook_opening", 50}},
	}}
	sugs := []FinalizableSuggestion{
		{Category: "pacing", Status: "pending", Pillar: "story_structure", Impact: 999,
			Criteria: []SuggestionCriterion{{Key: "pacing_curve"}, {Key: "hook_opening"}}},
		{Category: "tone", Status: "pending", Pillar: "", Impact: 5,
			Criteria: []SuggestionCriterion{{Key: "bogus_criterion"}}},
		{Category: "other", Status: "pending", Pillar: "", Impact: 0, Criteria: nil},
	}
	_, out := finalizePillarReport(in, sugs)
	// Point 1: impact capped to remaining headroom (100-50=50), high priority
	if out[0].Impact != 50 {
		t.Fatalf("impact must cap at headroom 50, got %d", out[0].Impact)
	}
	if out[0].Priority != suggestionPriorityHigh {
		t.Fatalf("impact 50 + 2 criteria => high, got %q", out[0].Priority)
	}
	// bogus criterion dropped; pillar inferred from category "tone"
	if len(out[1].Criteria) != 0 {
		t.Fatalf("unknown criterion must be sanitized away, got %+v", out[1].Criteria)
	}
	if out[1].Pillar != "tone" {
		t.Fatalf("pillar inferred from category, got %q", out[1].Pillar)
	}
	if out[1].Priority != suggestionPriorityMedium {
		t.Fatalf("impact 5 => medium, got %q", out[1].Priority)
	}
	// No pillar / no impact => polish
	if out[2].Priority != suggestionPriorityLow || out[2].Impact != 0 {
		t.Fatalf("unattributed point => low priority, got %+v", out[2])
	}
}

func TestDeriveSuggestionPriorityAppliedIsLow(t *testing.T) {
	if got := deriveSuggestionPriority("applied", 40, 3); got != suggestionPriorityLow {
		t.Fatalf("applied points drop to low, got %q", got)
	}
	if got := deriveSuggestionPriority("pending", 8, 2); got != suggestionPriorityHigh {
		t.Fatalf("impact>=8 with 2 criteria => high, got %q", got)
	}
	if got := deriveSuggestionPriority("pending", 3, 0); got != suggestionPriorityLow {
		t.Fatalf("tiny unattributed point => low, got %q", got)
	}
}

func TestRenderPreviousChecklistBlock(t *testing.T) {
	rep := &OutlineAnalysisReport{PillarScores: []PillarAssessment{{
		Key:       "tone",
		Score:     60,
		Checklist: []PillarCriterion{{Key: "tone_consistency", Name: "Consistent tonal register", Level: 60}},
	}}}
	block := renderPreviousChecklistBlock(rep, "THE OUTLINE")
	if block == "" {
		t.Fatal("expected previous-checklist block for an existing report")
	}
	for _, want := range []string{"PREVIOUS EDITORIAL PASS ON THE OUTLINE", "tone_consistency=60"} {
		if !contains(block, want) {
			t.Fatalf("block missing %q:\n%s", want, block)
		}
	}
	if renderPreviousChecklistBlock(nil, "THE OUTLINE") != "" {
		t.Fatal("nil report yields empty block")
	}
}

func contains(hay, needle string) bool {
	return len(hay) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(hay); i++ {
			if hay[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}

// --- Real scene merges ------------------------------------------------------

func newMergeTestState() (*Progress, string) {
	dir, _ := os.MkdirTemp("", "merge-test")
	path := filepath.Join(dir, "progress.json")
	state := &Progress{Chapters: []ChapterState{
		{Num: 1, Status: StatusPending, ScenePlan: &ScenePlan{TargetWords: 3000, Scenes: []Scene{
			{ID: 1, Title: "A", Description: "first beat", WordsBudget: 1000},
			{ID: 2, Title: "B", Description: "redundant beat", WordsBudget: 900},
			{ID: 3, Title: "C", Description: "third beat", WordsBudget: 1100},
		}}},
	}}
	rep := &ScenePlanAnalysisReport{Suggestions: []SceneSuggestion{{
		ID: "s1", Category: "scene_economy", Status: "pending",
		Title: "Fuse redundant beats", Detail: "d", Action: "a",
		Pillar: "story_structure",
		Recommendations: []ScenePlanRecommendation{
			// Scene 2 is absorbed INTO scene 1; survivor carries folded content.
			{ChapterNum: 1, SceneID: 2, Present: true, MergeIntoSceneID: 1,
				NewTitle: "A+B fused", NewDescription: "folded beats", NewWordsBudget: 1900},
		},
	}}}
	state.ScenePlanAnalyses = map[int]*ScenePlanAnalysisReport{1: rep}
	return state, path
}

// scenePlanTestRep returns the chapter-1 report used by the merge tests.
func scenePlanTestRep(state *Progress) *ScenePlanAnalysisReport {
	return state.ScenePlanAnalyses[1]
}

func TestApplyScenePlanSuggestion_RealMergeDeletesAbsorbedScene(t *testing.T) {
	state, path := newMergeTestState()
	defer os.RemoveAll(filepath.Dir(path))

	n, err := ApplyScenePlanSuggestion(state, "s1", path)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if n == 0 {
		t.Fatal("expected at least one scene changed")
	}
	scenes := state.Chapters[0].ScenePlan.Scenes
	if len(scenes) != 2 {
		t.Fatalf("absorbed scene must be DELETED: expected 2 scenes, got %d (%+v)", len(scenes), scenes)
	}
	for _, sc := range scenes {
		if sc.ID == 2 {
			t.Fatal("scene 2 still present after merge")
		}
	}
	surv := scenes[0]
	if surv.ID != 1 || surv.Title != "A+B fused" || surv.Description != "folded beats" || surv.WordsBudget != 1900 {
		t.Fatalf("survivor must carry folded content, got %+v", surv)
	}
	if scenePlanTestRep(state).Suggestions[0].Status != "applied" {
		t.Fatalf("suggestion should be marked applied, got %q", scenePlanTestRep(state).Suggestions[0].Status)
	}
	if state.Chapters[0].ScenePlan.Approved {
		t.Fatal("plan must be un-approved after editing")
	}
}

func TestApplyScenePlanSuggestion_InvalidMergeTargetDegradesToReplacement(t *testing.T) {
	state, path := newMergeTestState()
	defer os.RemoveAll(filepath.Dir(path))

	// Target scene 99 does not exist: degrade to plain replacement, delete nothing.
	scenePlanTestRep(state).Suggestions[0].Recommendations[0].MergeIntoSceneID = 99
	n, err := ApplyScenePlanSuggestion(state, "s1", path)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 in-place replacement, got %d", n)
	}
	scenes := state.Chapters[0].ScenePlan.Scenes
	if len(scenes) != 3 {
		t.Fatalf("invalid merge target must NOT delete scenes, got %d", len(scenes))
	}
	if scenes[1].ID != 2 || scenes[1].Title != "A+B fused" {
		t.Fatalf("degraded replacement should update scene 2 in place, got %+v", scenes[1])
	}
}

func TestEditScenePlanRecommendation_CancelMerge(t *testing.T) {
	state, path := newMergeTestState()
	defer os.RemoveAll(filepath.Dir(path))

	rec := &scenePlanTestRep(state).Suggestions[0].Recommendations[0]
	if rec.MergeIntoSceneID != 1 {
		t.Fatalf("precondition: merge target 1, got %d", rec.MergeIntoSceneID)
	}
	if err := EditScenePlanRecommendation(state, "s1", 1, 2, "merge_into_scene_id", "", 0, path); err != nil {
		t.Fatalf("edit failed: %v", err)
	}
	if rec.MergeIntoSceneID != 0 {
		t.Fatalf("merge_into_scene_id=0 must cancel the merge, got %d", rec.MergeIntoSceneID)
	}
	// And re-target it onto scene 3.
	if err := EditScenePlanRecommendation(state, "s1", 1, 2, "merge_into_scene_id", "", 3, path); err != nil {
		t.Fatalf("re-target failed: %v", err)
	}
	if rec.MergeIntoSceneID != 3 {
		t.Fatalf("re-target to scene 3 failed, got %d", rec.MergeIntoSceneID)
	}
}
