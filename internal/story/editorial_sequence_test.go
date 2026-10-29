package story

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"showmethestory/internal/config"
	"showmethestory/internal/sse"
)

func TestMergeCritiquePartsKeepsLetterAndConcatenatesPillars(t *testing.T) {
	overall := `{"summary":"the letter","overall_score":72,"pillar_scores":[{"key":"tone","score":99}],"suggestions":[{"title":"stray"}]}`
	parts := []string{
		`{"pillar_scores":[{"key":"reader_profile","score":60}],"suggestions":[{"title":"a"}]}`,
		`not json`,
		`{"pillar_scores":[{"key":"tone","score":55}],"suggestions":[{"title":"b"},{"title":"c"}]}`,
	}
	raw, err := mergeCritiqueParts(overall, parts)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Summary      string             `json:"summary"`
		OverallScore int                `json:"overall_score"`
		PillarScores []PillarAssessment `json:"pillar_scores"`
		Suggestions  []DraftedSuggestion
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	if got.Summary != "the letter" || got.OverallScore != 72 {
		t.Fatalf("letter/score not preserved: %+v", got)
	}
	if len(got.PillarScores) != 2 || got.PillarScores[0].Key != "reader_profile" || got.PillarScores[1].Key != "tone" {
		t.Fatalf("expected only the per-pillar entries, got %+v", got.PillarScores)
	}
	titles := []string{}
	for _, s := range got.Suggestions {
		titles = append(titles, s.Title)
	}
	if strings.Join(titles, ",") != "a,b,c" {
		t.Fatalf("expected pillar suggestions in order (stray overall suggestion dropped), got %v", titles)
	}
}

func TestEditorialPillarCallRetriesWeakPillarWithoutSuggestions(t *testing.T) {
	var calls atomic.Int32
	api := batchAPI(t, func(prompt string) string {
		calls.Add(1)
		if !strings.Contains(prompt, "ZERO suggestions") {
			return `{"pillar_scores":[{"key":"tone","score":50}],"suggestions":[]}`
		}
		return `{"pillar_scores":[{"key":"tone","score":50}],"suggestions":[{"title":"fix tone drift","evidence":"she laughed at the funeral"}]}`
	})
	part, ok := editorialPillarCall(context.Background(), api, nil, "system", "en", "tone", "grade tone")
	if !ok {
		t.Fatal("expected the retried pillar call to succeed")
	}
	if calls.Load() != 2 {
		t.Fatalf("expected one retry with the strict reminder, got %d calls", calls.Load())
	}
	if !strings.Contains(part, "fix tone drift") {
		t.Fatalf("expected the retried answer to be returned, got %s", part)
	}
}

func TestEditorialSequenceSurvivesOneFailedPillar(t *testing.T) {
	api := batchAPI(t, func(prompt string) string {
		switch {
		case strings.Contains(prompt, "SCOPE=reader_profile"):
			return `{"pillar_scores":[{"key":"reader_profile","score":65}],"suggestions":[{"title":"hook"}]}`
		case strings.Contains(prompt, "SCOPE=tone"):
			return `definitely not json`
		case strings.Contains(prompt, "SCOPE="):
			start := strings.Index(prompt, "SCOPE=")
			pk := prompt[start+len("SCOPE="):]
			if i := strings.IndexAny(pk, "\n\r"); i >= 0 {
				pk = pk[:i]
			}
			pk = strings.TrimSpace(pk)
			if pk == "" {
				return `{"summary":"ok","overall_score":70}`
			}
			return `{"pillar_scores":[{"key":"` + pk + `","score":80}],"suggestions":[]}`
		default:
			return `{}`
		}
	})
	raw, err := editorialSequence(context.Background(), api, nil, "system", "en", func(pillarKey string) string {
		return "SCOPE=" + pillarKey
	})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Summary      string             `json:"summary"`
		OverallScore int                `json:"overall_score"`
		PillarScores []PillarAssessment `json:"pillar_scores"`
		Suggestions  []DraftedSuggestion
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	if got.Summary != "ok" || got.OverallScore != 70 {
		t.Fatalf("overall call lost: %+v", got)
	}
	if len(got.PillarScores) != 3 {
		t.Fatalf("expected the three valid pillars, got %+v", got.PillarScores)
	}
	for _, p := range got.PillarScores {
		if p.Key == "tone" {
			t.Fatal("the failed pillar must not appear in the merged report")
		}
	}
}

func TestEditorialScopeOverride(t *testing.T) {
	overall := editorialScopeOverride("en", "")
	if !strings.Contains(overall, `"overall_score"`) || strings.Contains(overall, "pillar_scores") {
		t.Fatalf("overall call must ask for the letter only: %s", overall)
	}
	pillar := editorialScopeOverride("en", "tone")
	for _, want := range []string{"tone", "criteria_levels", "evidence", "SCOPE OVERRIDE", "ONLY the pillar"} {
		if !strings.Contains(pillar, want) {
			t.Fatalf("pillar call missing %q: %s", want, pillar)
		}
	}
	es := editorialScopeOverride("es", "tone")
	if !strings.Contains(es, "REGLA DE EVIDENCIA") || strings.Contains(es, "EVIDENCE RULE") {
		t.Fatalf("Spanish pillar call must use the Spanish rules: %s", es)
	}
}

// TestAnalyzeFullOutlineRunsEditorialSequence pins the wiring of one critic:
// the outline pass must go through the shared 1-overall + 4-pillar driver and
// persist the merged report (summary + all pillars + evidence), instead of a
// single mega call.
func TestAnalyzeFullOutlineRunsEditorialSequence(t *testing.T) {
	var calls atomic.Int32
	api := batchAPI(t, func(prompt string) string {
		calls.Add(1)
		if !strings.Contains(prompt, "ONLY the pillar") {
			return `{"summary":"editorial letter","overall_score":71}`
		}
		start := strings.Index(prompt, "ONLY the pillar \"") + len("ONLY the pillar \"")
		pk := prompt[start:]
		if i := strings.IndexByte(pk, '"'); i >= 0 {
			pk = pk[:i]
		}
		levels := []map[string]any{}
		for _, c := range PillarChecklist(pk) {
			levels = append(levels, map[string]any{"key": c.Key, "level": 100})
		}
		payload := map[string]any{
			"pillar_scores": []any{map[string]any{"key": pk, "score": 100, "note": "graded", "criteria_levels": levels}},
			"suggestions": []any{map[string]any{
				"category": "tone", "title": "tighten " + pk, "detail": "detail", "action": "action",
				"evidence": "the planned beat", "pillar": pk, "score_impact": 0,
				"criteria":            []any{map[string]any{"key": levels[0]["key"].(string)}},
				"applies_to_chapters": []int{1},
			}},
		}
		b, _ := json.Marshal(payload)
		return string(b)
	})
	cfg := config.DefaultConfigForLang("en")
	state := &Progress{Chapters: []ChapterState{
		{Num: 1, Title: "One", Outline: "The hero opens the door.", Status: StatusPending},
		{Num: 2, Title: "Two", Outline: "The hero closes the door.", Status: StatusPending},
	}}
	if err := AnalyzeFullOutline(context.Background(), api, cfg, state, nil, sse.NewLogBroadcaster()); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 5 {
		t.Fatalf("expected 1 overall + 4 pillar calls, got %d", got)
	}
	rep := state.OutlineAnalysis
	if rep == nil {
		t.Fatal("report not persisted")
	}
	if rep.Summary != "editorial letter" || rep.OverallScore != 71 {
		t.Fatalf("overall call lost: %+v", rep)
	}
	if len(rep.PillarScores) != 4 {
		t.Fatalf("expected all four pillars, got %+v", rep.PillarScores)
	}
	for _, p := range rep.PillarScores {
		if p.Score != 100 {
			t.Fatalf("pillar %s did not keep its rubric-derived score: %+v", p.Key, p)
		}
	}
	if len(rep.Suggestions) != 4 {
		t.Fatalf("expected one merged suggestion per pillar, got %d", len(rep.Suggestions))
	}
	for _, s := range rep.Suggestions {
		if s.Evidence != "the planned beat" {
			t.Fatalf("evidence lost during post-processing: %+v", s)
		}
	}
}
