package story

import "testing"
import "fmt"

func TestDbgBlocks(t *testing.T) {
ch := &ChapterState{Num: 1, Title: "t", Content: "段落一。\n\n“对话。”\n\n---\n\n段落二。"}
SyncChapterBlocks(ch)
fmt.Printf("nblocks=%d sep=%q\n", len(ch.Blocks), ch.BlockSep)
for _, b := range ch.Blocks {
fmt.Printf("  id=%d type=%s scene=%d text=%q\n", b.ID, b.Type, b.ScenePos, b.Text)
}
}
