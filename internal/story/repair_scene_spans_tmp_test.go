package story

import (
"strings"
"testing"
)

func TestRepairSceneSpansBreakAligned(t *testing.T) {
// 6-scene plan, prose with 5 "---" breaks => break-aligned repair.
var sb strings.Builder
for i := 0; i < 6; i++ {
if i > 0 {
sb.WriteString("---\n\n")
}
for p := 0; p < 4; p++ {
sb.WriteString("Scene paragraph body words keep going here for budget drift.\n\n")
}
}
ch := &ChapterState{Num: 1, Content: sb.String()}
scenes := make([]Scene, 6)
for i := range scenes {
scenes[i] = Scene{ID: i + 1, Title: "S", WordsBudget: 2000} // deliberately wrong budgets
}
ch.ScenePlan = &ScenePlan{Scenes: scenes}
n := ResyncChapterInterface(ch)
if n != 6 {
t.Fatalf("scenes=%d want 6", n)
}
if got := ch.LastInterfaceReport; !strings.Contains(got, "6 scenes") {
t.Fatalf("report=%q", got)
}
scenePos := map[int]bool{}
tropeCount := 0
for _, b := range ch.Blocks {
if b.ScenePos >= 0 {
scenePos[b.ScenePos] = true
}
if b.TropeIndex > 0 {
tropeCount++
}
}
if len(scenePos) != 6 {
t.Fatalf("distinct scene chips=%v want 6 (blocks=%d report=%s)", len(scenePos), len(ch.Blocks), ch.LastInterfaceReport)
}
if tropeCount != len(ch.Blocks) {
t.Fatalf("trope-indexed blocks=%d of %d", tropeCount, len(ch.Blocks))
}
}
