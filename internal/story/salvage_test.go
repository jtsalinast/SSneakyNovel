package story

import "testing"

func TestCandidateChapterJSONs(t *testing.T) {
	raw := "Here is the report:\n```json\n{\"summary\":\"ok\",\"suggestions\":[{\"id\":\"a\",\"title\":\"t\"}]}\n```\nDone."
	cands := candidateChapterJSONs(raw)
	if len(cands) == 0 {
		t.Fatal("no candidates")
	}
	if cands[0] != `{"summary":"ok","suggestions":[{"id":"a","title":"t"}]}` {
		t.Errorf("cand0=%q", cands[0])
	}
	trunc := `{"summary":"great chapter","suggestions":[{"id":"s1","title":"Punch up the opening`
	cands = candidateChapterJSONs(trunc)
	found := false
	for _, c := range cands {
		if len(c) > 0 && c[0] == '{' {
			found = true
		}
	}
	if !found {
		t.Errorf("no salvage candidate for truncated: %#v", cands)
	}
}
