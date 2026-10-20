package story

import (
"fmt"
"strings"
"testing"
)

func TestArcDebug11(t *testing.T) {
raw := "Edmund's debut is premature, it belongs to Chapter 3; Edmund introduced early again in Chapter 3; unrelated name inconsistency with Lady Beatrice"
parts := strings.Split(raw, "; ")
if len(parts) == 1 {
parts = strings.Split(raw, ";")
}
for i, p := range parts {
parts[i] = strings.TrimRight(strings.TrimSpace(p), ".")
}
merged := mergeUniqueIssues(parts)
fmt.Println("MERGED:", merged)
infos := mapArcIssueInfos(merged)
for _, f := range infos {
fmt.Printf("INFO %q key=%v cat=%s names=%v chrs=%v contra=%v\n", Truncate(f.text, 50), f.key, f.cat, f.names, f.chrs, f.isContradiction)
}
out := dedupeArcIssuesInfo(infos)
fmt.Println("OUT:", out)
got := splitFactCheckIssues(raw)
fmt.Println("SPLITFN:", got)
}
