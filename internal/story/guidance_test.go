package story

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"showmethestory/internal/sse"
)

func TestAuthorGuidanceBlock(t *testing.T) {
	cases := []struct {
		name, lang, guidance, wantMarker string
		wantEmpty                        bool
	}{
		{name: "empty", lang: "en", guidance: "   ", wantEmpty: true},
		{name: "english", lang: "en", guidance: "a blue butterfly crosses the yard", wantMarker: "[AUTHOR GUIDANCE"},
		{name: "spanish", lang: "es", guidance: "una mariposa azul cruza el patio", wantMarker: "[INDICACIONES DEL AUTOR"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := AuthorGuidanceBlock(tc.guidance, tc.lang)
			if tc.wantEmpty {
				if got != "" {
					t.Fatalf("empty guidance must render no block, got %q", got)
				}
				return
			}
			if !strings.Contains(got, tc.wantMarker) || !strings.Contains(got, strings.TrimSpace(tc.guidance)) {
				t.Fatalf("block must carry marker %q and the author text, got %q", tc.wantMarker, got)
			}
		})
	}
}

func TestChapterGuidanceReachesPlanAndProse(t *testing.T) {
	cfg, state, _ := newScenePlanFixture(t)
	state.Chapters[0].WritingGuidance = "GUIDANCE_MARKER: she drops the glass"

	planPrompt, _ := buildChapterWritingPrompt(cfg, state, 0, nil, nil)
	if !strings.Contains(planPrompt, "GUIDANCE_MARKER") {
		t.Fatal("chapter prompt must carry the chapter writing guidance")
	}
	plan := &ScenePlan{Scenes: []Scene{{ID: 1, Title: "A", Description: "d", WordsBudget: 1000}}}
	scenePrompt, _ := buildSceneWritingPrompt(cfg, state, 0, nil, plan, 0, "", "", "", 1000)
	if !strings.Contains(scenePrompt, "GUIDANCE_MARKER") {
		t.Fatal("scene prompt must carry the chapter writing guidance")
	}
}

func TestScenePlanPromptCarriesGuidance(t *testing.T) {
	var captured string
	api := batchAPI(t, func(prompt string) string {
		captured = prompt
		return scenePlanAnswer
	})
	cfg, state, progressPath := newScenePlanFixture(t)
	state.AutoScenePlan = true
	state.Chapters[0].WritingGuidance = "GUIDANCE_MARKER: a blue butterfly"
	if err := ensureScenePlanForWriting(context.Background(), api, cfg, state, 0, progressPath, sse.NewLogBroadcaster()); err != nil {
		t.Fatalf("gate: %v", err)
	}
	if !strings.Contains(captured, "GUIDANCE_MARKER") {
		t.Fatal("scene planner prompt must carry the chapter writing guidance")
	}
}

func TestOutlineBatchPromptCarriesGuidance(t *testing.T) {
	var captured string
	api := batchAPI(t, func(prompt string) string {
		captured = prompt
		return batchAnswer(1, 1)
	})
	cfg, _, _ := newScenePlanFixture(t)
	state := &Progress{Phase: "writing"}
	path := filepath.Join(t.TempDir(), "progress.json")
	req := OutlineBatchRequest{ChapterCount: 1, Synopsis: "open on the funeral", Guidance: "GUIDANCE_MARKER: end with the sister at the door"}
	if err := GenerateOutlineBatch(context.Background(), api, cfg, state, nil, req, path, sse.NewLogBroadcaster()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(captured, "GUIDANCE_MARKER") {
		t.Fatal("batch outline prompt must carry the author guidance")
	}
}
