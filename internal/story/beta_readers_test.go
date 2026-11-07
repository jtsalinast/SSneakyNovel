package story

import (
	"fmt"
	"strings"
	"testing"

	"showmethestory/internal/config"
)

func betaTestState() *Progress {
	return &Progress{
		PrePlanning: &PrePlanning{Acts: []PrePlanningAct{{Name: "The Break", StartCh: 1, EndCh: 2}}},
		Chapters: []ChapterState{
			{Num: 1, Title: "Opening", Content: "The rain hit the window. Mara opened the letter.", WordCount: 9, Status: StatusAccepted},
			{Num: 2, Title: "Turn", Content: "The letter named her father. She burned it before dawn.", WordCount: 10, Status: StatusAccepted},
		},
	}
}

func betaTestCfg(lang string) *config.Config {
	return &config.Config{Language: lang, Story: config.StoryConfig{
		Type: "drama", Subgenre: "metafiction", Theme: "memory", Tone: "melancholic",
		AudienceProfile: "adult readers of literary drama", StoryIdea: "A daughter reads her father's letters.",
		WritingStyle: "lyrical", WritingPOV: "third limited", Structure: "three-act", TargetAudience: "adult",
	}}
}

func TestBetaPromptsRenderWithoutFormatErrors(t *testing.T) {
	for _, lang := range []string{"en", "es"} {
		cfg := betaTestCfg(lang)
		state := betaTestState()
		panel := defaultBetaPanel(cfg)
		persona := panel.Personas[0]
		reading := betaReadingResult{ReadingNotes: "Notes", Memory: BetaReaderMemory{KnownEvents: []string{"a letter"}}}
		evals := []BetaReaderEvaluation{{PersonaID: "r1", PersonaName: persona.Name, Archetype: persona.Archetype, Engagement: 70, ContinueReading: 80}}

		prompts := map[string]string{
			"panel":      betaPanelPrompt(cfg, nil, lang),
			"reading":    betaReadingPrompt(cfg, &persona, nil, state, 1, 1, 2, lang),
			"evaluation": betaEvaluationPrompt(cfg, &persona, nil, state, 1, 1, 2, reading, lang),
			"experience": betaConsolidationPrompt(cfg, state, 1, 1, 2, evals, "experience", lang),
			"interp":     betaConsolidationPrompt(cfg, state, 1, 1, 2, evals, "interp", lang),
		}
		for name, p := range prompts {
			if strings.Contains(p, "%!") {
				t.Fatalf("%s/%s prompt has a format error:\n%s", lang, name, p)
			}
			if !strings.Contains(p, "r1") && name != "panel" {
				t.Fatalf("%s/%s prompt is missing the persona block", lang, name)
			}
		}
	}
}

func TestSanitizeBetaPanelAssignsIDsAndClamps(t *testing.T) {
	panel := &BetaPanel{Personas: make([]BetaPersona, 8)}
	for i := range panel.Personas {
		panel.Personas[i] = BetaPersona{Name: "Reader", Archetype: "unknown", Age: 200}
	}
	if !sanitizeBetaPanel(panel) {
		t.Fatal("panel with 8 personas must be accepted")
	}
	if len(panel.Personas) != 8 {
		t.Fatalf("want 8 personas, got %d", len(panel.Personas))
	}
	for i, p := range panel.Personas {
		if p.ID != fmt.Sprintf("r%d", i+1) {
			t.Fatalf("persona %d has id %q", i, p.ID)
		}
		if p.Archetype == "" || p.Age <= 0 || p.Age > 100 {
			t.Fatalf("persona %d not sanitized: %+v", i, p)
		}
		if p.Sensitivity.NeedForFastPacing < 1 || p.Sensitivity.NeedForFastPacing > 10 {
			t.Fatalf("sensitivity not clamped: %d", p.Sensitivity.NeedForFastPacing)
		}
		if p.Priorities.Plot < 1 || p.Priorities.Plot > 10 {
			t.Fatalf("priorities not clamped: %d", p.Priorities.Plot)
		}
	}
	if sanitizeBetaPanel(&BetaPanel{Personas: panel.Personas[:4]}) {
		t.Fatal("a panel with fewer than 5 personas must be rejected")
	}
}

func TestBetaQuoteGroundedRejectsInventedQuotes(t *testing.T) {
	chapters := map[int]string{1: "The rain hit the window. Mara opened the letter."}
	if got := betaQuoteGrounded("Mara opened the letter", 1, chapters); got == "" {
		t.Fatal("a real quote must survive grounding")
	}
	if got := betaQuoteGrounded("A dragon ate the moon", 1, chapters); got != "" {
		t.Fatalf("an invented quote must be cleared, got %q", got)
	}
}

func TestBuildBetaReportCountsOnlyKnownReadersAndFallsBack(t *testing.T) {
	evals := []BetaReaderEvaluation{
		{PersonaID: "r1", PersonaName: "Mara", Archetype: "target_audience", Engagement: 60, ContinueReading: 70,
			Worked: []string{"The letter scene landed."}, Concerns: []BetaConcern{{Description: "The turn felt rushed.", Chapter: 2, Kind: "narrative_problem"}},
			Memorable:           []BetaMoment{{Chapter: 1, Location: "window", Note: "the rain image"}},
			EngagementByChapter: []BetaChapterEngagement{{Chapter: 1, Engagement: 60}, {Chapter: 2, Engagement: 40}}},
		{PersonaID: "r2", PersonaName: "Ivan", Archetype: "literary", Engagement: 80, ContinueReading: 90,
			Summary: "Strong prose, thin middle."},
	}
	exp := betaConsolidationExperience{SharedConcerns: []BetaFinding{{Description: "Pacing sags", Readers: []string{"r1", "r2", "bogus"}, Severity: "high", Confidence: "high"}}}
	chapters := map[int]string{1: "The rain hit the window.", 2: "The letter named her father."}
	valid := map[string]bool{"r1": true, "r2": true}

	report := buildBetaReport(1, "The Break", 1, 2, "fp", evals, exp, betaConsolidationInterpretation{}, chapters, valid, "en")
	if len(report.SharedConcerns) != 1 || report.SharedConcerns[0].ReaderCount != 2 {
		t.Fatalf("reader count must be derived from known ids: %+v", report.SharedConcerns)
	}
	if report.AverageEngagement != 70 || report.AverageContinue != 80 {
		t.Fatalf("averages wrong: %d/%d", report.AverageEngagement, report.AverageContinue)
	}
	// The consolidation was partial: fallback must fill the missing categories.
	if len(report.EngagementPatterns) == 0 || len(report.IndividualObservations) == 0 || len(report.SharedPositives) == 0 {
		t.Fatalf("fallback did not fill missing categories: %+v", report)
	}
}

func TestBetaReportBlockTellsExpertToJudgeIndependently(t *testing.T) {
	report := &BetaReaderReport{PanelSize: 8, Summary: "Readers split on the ending.", AverageEngagement: 66, AverageContinue: 72,
		SharedConcerns: []BetaFinding{{Description: "The middle sags.", ReaderCount: 5, Severity: "high", Confidence: "medium", Kind: "narrative_problem"}},
		Evaluations:    []BetaReaderEvaluation{{PersonaID: "r1"}}}
	block := betaReportBlock(report, "en")
	if !strings.Contains(block, "judge independently") || !strings.Contains(block, "5/8") {
		t.Fatalf("expert block missing independence rule or reader count:\n%s", block)
	}
	// The expert must not copy the panel score, but must visibly reconcile it
	// with its own reader_profile grade and explain large gaps.
	for _, want := range []string{"RECONCILE", "reader_profile", "15 points", "not a score for you to copy"} {
		if !strings.Contains(block, want) {
			t.Fatalf("expert block missing %q:\n%s", want, block)
		}
	}
	if betaReportBlock(nil, "en") != "" {
		t.Fatal("nil report must render an empty block")
	}
	reconcile := betaReconciliationBlock(report, "en")
	for _, want := range []string{"MANDATORY", "66/100", "more than 15 points", "do NOT copy"} {
		if !strings.Contains(reconcile, want) {
			t.Fatalf("reconciliation block missing %q:\n%s", want, reconcile)
		}
	}
	if betaReconciliationBlock(nil, "en") != "" {
		t.Fatal("nil report must render no reconciliation block")
	}
}

func TestActContentFingerprintIsStableAndUnsalted(t *testing.T) {
	state := betaTestState()
	a := actDraftedContentFingerprint(state, 1, 2)
	if b := actDraftedContentFingerprint(state, 1, 2); a != b {
		t.Fatalf("content fingerprint must be stable across calls: %s != %s", a, b)
	}
	if salted := actDraftedFingerprint(state, 1, 2); salted == a {
		t.Fatal("content fingerprint must exclude the per-process grade salt")
	}
	state.Chapters[0].Content = "A different opening line."
	if changed := actDraftedContentFingerprint(state, 1, 2); changed == a {
		t.Fatal("content fingerprint must change when the prose changes")
	}
}

func TestLatestBetaReportMatchesContentAndLegacySessions(t *testing.T) {
	state := &Progress{BetaReports: map[int][]*BetaReaderReport{1: {
		{DraftFingerprint: "salted-old"}, // legacy session, no content fingerprint
	}}}
	if got := latestBetaReport(state, 1, "content"); got == nil {
		t.Fatal("a legacy session must be reused once after the upgrade")
	}
	state.BetaReports[1] = append(state.BetaReports[1], &BetaReaderReport{ContentFingerprint: "content"})
	if got := latestBetaReport(state, 1, "content"); got == nil || got.ContentFingerprint != "content" {
		t.Fatalf("expected the exact content match, got %+v", got)
	}
	if got := latestBetaReport(state, 1, "other"); got != nil {
		t.Fatalf("a stale session must not match changed prose, got %+v", got)
	}
}

func TestDefaultBetaPanelIsDiverseAndClamped(t *testing.T) {
	panel := defaultBetaPanel(betaTestCfg("en"))
	if len(panel.Personas) != 8 {
		t.Fatalf("want 8 personas, got %d", len(panel.Personas))
	}
	counts := map[string]int{}
	for _, p := range panel.Personas {
		counts[p.Archetype]++
	}
	if counts["target_audience"] != 3 {
		t.Fatalf("want 3 target-audience readers, got %d", counts["target_audience"])
	}
	for _, a := range []string{"literary", "entertainment", "psychological", "analytical", "general"} {
		if counts[a] != 1 {
			t.Fatalf("want exactly one %s reader, got %d", a, counts[a])
		}
	}
}
