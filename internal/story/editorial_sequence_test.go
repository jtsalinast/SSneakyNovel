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

func TestMergeCritiquePartsNormalizesSuggestionTypeDrift(t *testing.T) {
	overall := `{"summary":123,"overall_score":"70"}`
	parts := []string{
		`{"pillar_scores":[{"key":"tone","score":50.5,"criteria_levels":[{"key":"tone_consistency","level":"2"}]}],"suggestions":[{"title":123,"detail":"d","action":"a","category":7,"pillar":"tone","criteria":["tone_consistency",{"key":"voice"}],"score_impact":"+12","applies_to_chapters":"2, 4","interventions":{"chapter_num":2},"recommendations":"nope"}]}`,
		`{"pillar_scores":[{"key":"reader_profile","score":55}],"suggestions":["just text",42]}`,
	}
	raw, err := mergeCritiqueParts(overall, parts)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Summary      string              `json:"summary"`
		OverallScore int                 `json:"overall_score"`
		PillarScores []PillarAssessment  `json:"pillar_scores"`
		Suggestions  []DraftedSuggestion `json:"suggestions"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("one malformed field must not fail the whole report: %v", err)
	}
	if got.Summary != "123" || got.OverallScore != 70 {
		t.Fatalf("overall fields not coerced: summary=%q score=%d", got.Summary, got.OverallScore)
	}
	if len(got.PillarScores) != 2 || got.PillarScores[0].Score != 50 {
		t.Fatalf("pillar score not coerced: %+v", got.PillarScores)
	}
	if len(got.PillarScores[0].CriteriaLevels) != 1 || got.PillarScores[0].CriteriaLevels[0].Level != 2 {
		t.Fatalf("criteria levels not coerced: %+v", got.PillarScores[0].CriteriaLevels)
	}
	if len(got.Suggestions) != 1 {
		t.Fatalf("expected one suggestion, got %d", len(got.Suggestions))
	}
	s := got.Suggestions[0]
	if s.Title != "123" || s.ScoreImpact != 12 {
		t.Fatalf("scalar fields not coerced: %+v", s)
	}
	if len(s.AppliesToChapters) != 2 || s.AppliesToChapters[0] != 2 || s.AppliesToChapters[1] != 4 {
		t.Fatalf("chapter list not coerced: %v", s.AppliesToChapters)
	}
	if len(s.Criteria) != 2 || s.Criteria[0].Key != "tone_consistency" || s.Criteria[1].Key != "voice" {
		t.Fatalf("criteria not coerced: %+v", s.Criteria)
	}
	if len(s.Interventions) != 0 {
		t.Fatalf("advice-only pass must drop bundled interventions: %+v", s.Interventions)
	}
}

func TestEditorialPillarCallRetriesWeakPillarWithoutSuggestions(t *testing.T) {
	levels := func(pk string) []map[string]any {
		out := []map[string]any{}
		for _, c := range PillarChecklist(pk) {
			out = append(out, map[string]any{"key": c.Key, "level": 50})
		}
		return out
	}
	var calls atomic.Int32
	api := batchAPI(t, func(prompt string) string {
		calls.Add(1)
		if !strings.Contains(prompt, "ZERO suggestions") {
			b, _ := json.Marshal(map[string]any{
				"pillar_scores": []any{map[string]any{"key": "tone", "score": 50, "criteria_levels": levels("tone")}},
				"suggestions":   []any{},
			})
			return string(b)
		}
		b, _ := json.Marshal(map[string]any{
			"pillar_scores": []any{map[string]any{"key": "tone", "score": 50, "criteria_levels": levels("tone")}},
			"suggestions":   []any{map[string]any{"title": "fix tone drift", "evidence": "she laughed at the funeral"}},
		})
		return string(b)
	})
	part, ok := editorialPillarCall(context.Background(), api, nil, "system", "en", "tone", "grade tone", "")
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

func TestEditorialPillarCallRetriesMissingEvidence(t *testing.T) {
	levels := []map[string]any{}
	for _, c := range PillarChecklist("tone") {
		levels = append(levels, map[string]any{"key": c.Key, "level": 70})
	}
	payload := func(evidence string) string {
		b, _ := json.Marshal(map[string]any{
			"pillar_scores": []any{map[string]any{"key": "tone", "score": 70, "criteria_levels": levels}},
			"suggestions":   []any{map[string]any{"title": "fix tone", "evidence": evidence}},
		})
		return string(b)
	}
	var calls atomic.Int32
	api := batchAPI(t, func(prompt string) string {
		calls.Add(1)
		if !strings.Contains(prompt, "EMPTY \"evidence\"") {
			return payload("")
		}
		return payload("she laughed at the funeral")
	})
	part, ok := editorialPillarCall(context.Background(), api, nil, "system", "en", "tone", "grade tone", "")
	if !ok {
		t.Fatal("expected the retried pillar call to succeed")
	}
	if calls.Load() != 2 {
		t.Fatalf("expected one retry for missing evidence, got %d calls", calls.Load())
	}
	if !strings.Contains(part, "she laughed at the funeral") {
		t.Fatalf("expected the evidence-backed answer, got %s", part)
	}
}

func TestCritiquePromptsCarrySpanishSchemaExamples(t *testing.T) {
	es := draftedJSONSchema("es")
	if strings.Contains(es, "short imperative headline") || !strings.Contains(es, "titular corto e imperativo") {
		t.Fatalf("Spanish act schema must not seed English examples: %s", es)
	}
	if ContentLanguageName("es") != "español" || ContentLanguageName("en") != "English" {
		t.Fatalf("unexpected language names: %q %q", ContentLanguageName("es"), ContentLanguageName("en"))
	}
	cfg := config.DefaultConfigForLang("es")
	state := &Progress{Chapters: []ChapterState{{Num: 1, Title: "Uno", Outline: "Zara abre el cuaderno.", Status: StatusPending}}}
	outline := buildOutlineEditorPrompt(cfg, state, nil, "es")
	if !strings.Contains(outline, "in español") || strings.Contains(outline, "in es\"") {
		t.Fatalf("outline schema must name the output language: %s", outline)
	}
}

func TestEvidenceGroundedAcceptsParaphraseAndRejectsInvention(t *testing.T) {
	material := "La habitación era la misma, pero algo en el aire la volvía más densa. Zara cerró el cuaderno y las palabras siguieron escribiéndose solas sobre la mesa."
	cases := []struct {
		name     string
		evidence string
		want     bool
	}{
		{"verbatim", "Zara cerró el cuaderno y las palabras siguieron escribiéndose solas sobre la mesa.", true},
		{"grounded paraphrase", "La habitación seguía igual, pero el aire se sentía más denso; Zara cerró el cuaderno y las palabras continuaron escribiéndose solas.", true},
		{"invented scene", "Mateo descubre el mapa del farero y huye a Lisboa en tren.", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		if got := evidenceGrounded(tc.evidence, material); got != tc.want {
			t.Fatalf("%s: evidenceGrounded(%q) = %v, want %v", tc.name, tc.evidence, got, tc.want)
		}
	}
}

func TestEditorialPillarCallCorrectsInventedEvidence(t *testing.T) {
	levels := []map[string]any{}
	for _, c := range PillarChecklist("tone") {
		levels = append(levels, map[string]any{"key": c.Key, "level": 70})
	}
	payload := func(evidence string) string {
		b, _ := json.Marshal(map[string]any{
			"pillar_scores": []any{map[string]any{"key": "tone", "score": 70, "criteria_levels": levels}},
			"suggestions":   []any{map[string]any{"title": "fix tone", "evidence": evidence}},
		})
		return string(b)
	}
	var calls atomic.Int32
	api := batchAPI(t, func(prompt string) string {
		calls.Add(1)
		if !strings.Contains(prompt, "invented quotation") {
			return payload("El farero incendia la biblioteca al amanecer.")
		}
		return payload("Zara cerró el cuaderno.")
	})
	material := "Zara cerró el cuaderno y las palabras siguieron escribiéndose solas."
	part, ok := editorialPillarCall(context.Background(), api, nil, "system", "en", "tone", "grade tone", material)
	if !ok {
		t.Fatal("expected the retried pillar call to succeed")
	}
	if calls.Load() != 2 {
		t.Fatalf("expected one corrective retry, got %d calls", calls.Load())
	}
	if !strings.Contains(part, "Zara cerró el cuaderno.") {
		t.Fatalf("expected the grounded evidence, got %s", part)
	}
}

func TestEditorialPillarCallDropsEvidenceStillInventedAfterRetry(t *testing.T) {
	levels := []map[string]any{}
	for _, c := range PillarChecklist("tone") {
		levels = append(levels, map[string]any{"key": c.Key, "level": 70})
	}
	api := batchAPI(t, func(prompt string) string {
		b, _ := json.Marshal(map[string]any{
			"pillar_scores": []any{map[string]any{"key": "tone", "score": 70, "criteria_levels": levels}},
			"suggestions":   []any{map[string]any{"title": "fix tone", "evidence": "El farero incendia la biblioteca al amanecer."}},
		})
		return string(b)
	})
	part, ok := editorialPillarCall(context.Background(), api, nil, "system", "en", "tone", "grade tone", "Zara cerró el cuaderno.")
	if !ok {
		t.Fatal("the point itself must survive even when its quote cannot be verified")
	}
	var probe struct {
		Suggestions []DraftedSuggestion `json:"suggestions"`
	}
	if err := json.Unmarshal([]byte(part), &probe); err != nil {
		t.Fatal(err)
	}
	if len(probe.Suggestions) != 1 || probe.Suggestions[0].Title != "fix tone" {
		t.Fatalf("expected the suggestion to be kept, got %+v", probe.Suggestions)
	}
	if probe.Suggestions[0].Evidence != "" {
		t.Fatalf("invented quotation must not be persisted, got %q", probe.Suggestions[0].Evidence)
	}
}

func TestEditorialPillarCallRetriesMissingCriteria(t *testing.T) {
	complete := func(pk string) string {
		levels := []map[string]any{}
		for _, c := range PillarChecklist(pk) {
			levels = append(levels, map[string]any{"key": c.Key, "level": 60})
		}
		b, _ := json.Marshal(map[string]any{
			"pillar_scores": []any{map[string]any{"key": pk, "score": 60, "criteria_levels": levels}},
			"suggestions":   []any{map[string]any{"title": "fix", "evidence": "quote"}},
		})
		return string(b)
	}
	var calls atomic.Int32
	api := batchAPI(t, func(prompt string) string {
		calls.Add(1)
		if !strings.Contains(prompt, "UNGRADED") {
			return `{"pillar_scores":[{"key":"tone","score":60,"criteria_levels":[{"key":"tone_consistency","level":60}]}],"suggestions":[{"title":"fix","evidence":"quote"}]}`
		}
		return complete("tone")
	})
	part, ok := editorialPillarCall(context.Background(), api, nil, "system", "en", "tone", "grade tone", "")
	if !ok {
		t.Fatal("expected the retried pillar call to succeed")
	}
	if calls.Load() != 2 {
		t.Fatalf("expected one retry listing the missing criteria, got %d calls", calls.Load())
	}
	var probe struct {
		PillarScores []PillarAssessment `json:"pillar_scores"`
	}
	if err := json.Unmarshal([]byte(part), &probe); err != nil {
		t.Fatal(err)
	}
	if len(probe.PillarScores) != 1 || len(probe.PillarScores[0].CriteriaLevels) != len(PillarChecklist("tone")) {
		t.Fatalf("expected every checklist item graded, got %+v", probe.PillarScores)
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
	}, "")
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

// TestAnalyzeFinishedActReusesBetaSessionWithoutRerunningReaders pins the
// separation: the expert pass reuses the saved beta session and never invokes
// the readers itself.
func TestAnalyzeFinishedActReusesBetaSessionWithoutRerunningReaders(t *testing.T) {
	var calls atomic.Int32
	api := batchAPI(t, func(prompt string) string {
		calls.Add(1)
		if !strings.Contains(prompt, "ONLY the pillar") {
			return `{"summary":"editorial letter","overall_score":70}`
		}
		start := strings.Index(prompt, "ONLY the pillar \"") + len("ONLY the pillar \"")
		pk := prompt[start:]
		if i := strings.IndexByte(pk, '"'); i >= 0 {
			pk = pk[:i]
		}
		levels := []map[string]any{}
		for _, c := range PillarChecklist(pk) {
			levels = append(levels, map[string]any{"key": c.Key, "level": 70})
		}
		payload := map[string]any{
			"pillar_scores": []any{map[string]any{"key": pk, "score": 70, "criteria_levels": levels}},
			"suggestions": []any{map[string]any{
				"category": "tone", "title": "fix " + pk, "detail": "d", "action": "a",
				"evidence": "Mara opened the letter.", "pillar": pk, "score_impact": 10,
				"criteria": []any{map[string]any{"key": levels[0]["key"].(string)}},
			}},
		}
		b, _ := json.Marshal(payload)
		return string(b)
	})
	cfg := config.DefaultConfigForLang("en")
	state := betaTestState()
	content := actDraftedContentFingerprint(state, 1, 2)
	state.BetaReports = map[int][]*BetaReaderReport{1: {{
		ContentFingerprint: content,
		PanelSize:          1,
		AverageEngagement:  50,
		Evaluations:        []BetaReaderEvaluation{{PersonaID: "r1", PersonaName: "Reader", Engagement: 50}},
	}}}
	if err := AnalyzeFinishedAct(context.Background(), api, cfg, state, nil, 1, "", sse.NewLogBroadcaster()); err != nil {
		t.Fatal(err)
	}
	// 1 overall + 4 pillar calls. Any beta reader call would add 18+ calls.
	if got := calls.Load(); got != 5 {
		t.Fatalf("expert pass must reuse the beta session and never rerun readers, got %d API calls", got)
	}
	rep := state.ActAnalyses[1]
	if rep == nil {
		t.Fatal("act report not stored")
	}
	if missing := ungradedPillarKeys(rep.PillarScores); len(missing) > 0 {
		t.Fatalf("persisted report left ungraded pillars: %v", missing)
	}
	for _, p := range rep.PillarScores {
		if p.Key == "reader_profile" && !strings.Contains(p.Note, "Beta panel reconciliation") {
			t.Fatalf("large beta/expert gap must be reconciled in the note, got %q", p.Note)
		}
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
				"evidence": "The hero opens the door.", "pillar": pk, "score_impact": 0,
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
		if s.Evidence != "The hero opens the door." {
			t.Fatalf("evidence lost during post-processing: %+v", s)
		}
	}
}
