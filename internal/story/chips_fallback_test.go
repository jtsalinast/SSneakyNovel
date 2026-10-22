package story

import (
"strings"
"testing"
)

func TestSyncFallbackChipsRender(t *testing.T) {
content := strings.Join([]string{
"First opening paragraph of the scene with some tension words here.",
"They walked in. \"Hello,\" she said quietly to the room.",
"---",
"A second scene begins with a brand new paragraph that is longer.",
}, "\n\n")
ch := &ChapterState{Num: 1, Content: content}
SyncChapterBlocks(ch)
if len(ch.Blocks) == 0 {
t.Fatal("no blocks synced")
}
sceneChips := 0
for _, b := range ch.Blocks {
if b.SceneHasMeta != nil && *b.SceneHasMeta {
sceneChips++
}
if b.TropeCount < 1 || b.TropeIndex < 1 {
t.Errorf("block %d missing trope chip fields: count=%d idx=%d label=%q", b.ID, b.TropeCount, b.TropeIndex, b.TropeLabel)
}
}
if sceneChips == 0 {
t.Fatalf("fallback pseudo-scenes produced no chip-carrying blocks (scene_has_meta all false)")
}
t.Logf("blocks=%d chipMetaTrue=%d", len(ch.Blocks), sceneChips)
}
