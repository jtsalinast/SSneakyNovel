package story

import "testing"

func TestMultiInterventionSameBeat(t *testing.T) {
	fresh := []TropeIntervention{
		{ChapterNum: 3, ScenePos: 0, TropeIndex: 1, Status: "pending", Feedback: "Show dont tell here", Quote: "he was sad"},
		{ChapterNum: 3, ScenePos: 0, TropeIndex: 1, Status: "pending", Feedback: "Tighten the dialogue tag", Quote: "he was sad"},
		{ChapterNum: 3, ScenePos: 0, TropeIndex: 1, Status: "pending", Feedback: "Show dont tell here", Quote: "he was sad"},
	}
	merged := mergeInterventions([]TropeIntervention{}, fresh)
	if len(merged) != 2 {
		t.Fatalf("expected 2 distinct interventions on same beat, got %d", len(merged))
	}
}
