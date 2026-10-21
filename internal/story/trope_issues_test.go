package story

import "testing"

func TestRefreshTropeIssuesForChapter(t *testing.T) {
st := &Progress{
PendingWritingConflict: &WritingConflict{
ChapterNum:   3,
Issues:       []string{"The protagonist's eye color changed from brown to green."},
SceneTargets: [][2]int{{1, 0}},
},
}
ch := &ChapterState{Num: 3}
// Critic: one pending intervention on scene 2 trope 1, one already applied.
ch.Analysis = &ChapterAnalysisReport{
Suggestions: []DraftedSuggestion{{
Interventions: []TropeIntervention{
{ScenePos: 2, TropeIndex: 1, TropeLabel: "banter", Feedback: "beat lands too early", Status: "pending"},
{ScenePos: 2, TropeIndex: 0, Feedback: "already fixed", Status: "applied"},
},
}},
}
RefreshTropeIssuesForChapter(st, ch)
if len(ch.TropeIssues) != 2 {
t.Fatalf("want 2 groups (fact 1:0 + critic 2:1), got %d: %+v", len(ch.TropeIssues), ch.TropeIssues)
}
f0 := ch.TropeIssues[0]
if f0.ScenePos != 1 || f0.TropeIndex != 0 || len(f0.Fact) != 1 || len(f0.Critic) != 0 {
t.Errorf("first group wrong: %+v", f0)
}
c1 := ch.TropeIssues[1]
if c1.ScenePos != 2 || c1.TropeIndex != 1 || len(c1.Critic) != 1 {
t.Errorf("second group wrong: %+v", c1)
}

// Applied interventions drop out; conflict cleared -> dots disappear.
ch.Analysis.Suggestions[0].Interventions[0].Status = "applied"
st.PendingWritingConflict = nil
RefreshTropeIssuesForChapter(st, ch)
if ch.TropeIssues != nil {
t.Errorf("expected nil roll-up after all issues resolved, got %+v", ch.TropeIssues)
}
// Nil-safe on chapters with no analysis at all.
RefreshTropeIssuesForChapter(st, &ChapterState{Num: 9})
}
