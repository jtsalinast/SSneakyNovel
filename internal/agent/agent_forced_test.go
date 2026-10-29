package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"showmethestory/internal/config"
	"showmethestory/internal/story"
)

// stubLLM returns a chat-completions handler serving fixed non-stream JSON.
func stubLLM(content string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":%q},"finish_reason":"stop"}]}`, content)
	}
}

func newForcedTestCtx(t *testing.T, baseURL string) *AgentContext {
	t.Helper()
	dir := t.TempDir()
	cfgPath := dir + "/config.json"
	cfg := &config.Config{Language: "en", Story: config.StoryConfig{Type: "fantasy"}}
	if err := config.SaveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save cfg: %v", err)
	}
	return &AgentContext{
		APICfg:  &config.APIConfig{BaseURL: baseURL, Model: "test-model", HTTPTimeoutSeconds: 5},
		CfgPath: cfgPath,
		State:   &story.Progress{},
	}
}

// The Generate buttons must trigger the tool through the CHAT AGENT even when
// the model answers plain text (or fails): step 0 executes generate_section
// directly from the [generate-section:X] tag, then the loop asks the LLM for
// the visible summary reply.
func TestRunAgentLoopForcedTagExecutesToolDespitePlainText(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		stubLLM("Sure! I have started generating the characters for you.")(w, r)
	}))
	defer srv.Close()

	var ran atomic.Value
	ctxx := newForcedTestCtx(t, srv.URL)
	ctxx.StartSectionGenerate = func(req SectionGenRequest) error {
		ran.Store(req.Section)
		return nil
	}

	reply, history, err := RunAgentLoop(context.Background(), ctxx,
		"[generate-section:characters] Please generate the characters for this novel.", nil, 5)
	if err != nil {
		t.Fatalf("loop error: %v", err)
	}
	if got := ran.Load(); got != "characters" {
		t.Fatalf("tool did not run with section=characters (got %v)", got)
	}
	if !strings.Contains(reply, "generating the characters") {
		t.Fatalf("reply should be the model summary, got %q", reply)
	}
	var sawCall, sawResult bool
	for _, st := range history {
		if st.Role == "assistant" && st.ToolCall != nil && st.ToolCall.Name == "generate_section" {
			sawCall = true
			var args map[string]string
			json.Unmarshal(st.ToolCall.Arguments, &args)
			if args["section"] != "characters" {
				t.Fatalf("forced call args wrong: %s", st.ToolCall.Arguments)
			}
		}
		if st.Role == "tool" && st.ToolResult != "" {
			sawResult = true
		}
	}
	if !sawCall || !sawResult {
		t.Fatalf("history missing forced tool call/result card: %+v", history)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected exactly 1 LLM call (summary), got %d", calls)
	}
}

// Even if the summary LLM call fails outright, the tagged turn must still
// execute the tool (the click always does something) and end without error.
func TestRunAgentLoopForcedTagSurvivesLLMFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	var ran atomic.Bool
	ctxx := newForcedTestCtx(t, srv.URL)
	ctxx.StartSectionGenerate = func(req SectionGenRequest) error {
		ran.Store(true)
		return nil
	}

	_, history, err := RunAgentLoop(context.Background(), ctxx,
		"[generate-section:style] please generate the style", nil, 4)
	if err != nil {
		t.Fatalf("tagged turn must not hard-fail after the tool ran: %v", err)
	}
	if !ran.Load() {
		t.Fatal("generate_section tool was never executed")
	}
	var sawCall bool
	for _, st := range history {
		if st.Role == "assistant" && st.ToolCall != nil {
			sawCall = true
		}
	}
	if !sawCall {
		t.Fatalf("history missing tool-call card: %+v", history)
	}
}

// Untagged messages keep the normal model-driven behavior untouched.
func TestRunAgentLoopUntaggedKeepsModelBehavior(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(stubLLM("Just a friendly plain-text answer.")))
	defer srv.Close()

	ctxx := newForcedTestCtx(t, srv.URL)
	var ranCalled bool
	ctxx.StartSectionGenerate = func(req SectionGenRequest) error { ranCalled = true; return nil }

	reply, _, err := RunAgentLoop(context.Background(), ctxx, "hello there", nil, 5)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if reply != "Just a friendly plain-text answer." {
		t.Fatalf("unexpected reply %q", reply)
	}
	if ranCalled {
		t.Fatal("tool must not run for untagged messages")
	}
}

// The creativity level must travel as a JSON number: generate_section decodes
// it into an int field, and the old string form made every slider level fall
// back to Balanced (or failed the tool call outright).
func TestParseGenerateSectionTagCreativityIsNumber(t *testing.T) {
	cases := map[string]int{
		"[generate-section:differentiating_idea;creativity=4]":                  4,
		"Please suggest [generate-section:differentiating_idea] [creativity=5]": 5,
	}
	for msg, want := range cases {
		call := ParseGenerateSectionTag(msg)
		if call == nil {
			t.Fatalf("%q: tag not parsed", msg)
		}
		var params struct {
			Creativity int `json:"creativity"`
		}
		if err := json.Unmarshal(call.Arguments, &params); err != nil {
			t.Fatalf("%q: creativity must decode as a number: %v (%s)", msg, err, call.Arguments)
		}
		if params.Creativity != want {
			t.Fatalf("%q: creativity = %d, want %d", msg, params.Creativity, want)
		}
	}
}

// A tagged turn has already run its tool on step 0: a failing summary endpoint
// must be retried a bounded number of times, not for every remaining step.
func TestRunAgentLoopForcedTagStopsAfterTwoAPIFailures(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	ctxx := newForcedTestCtx(t, srv.URL)
	ctxx.StartSectionGenerate = func(req SectionGenRequest) error { return nil }

	reply, _, err := RunAgentLoop(context.Background(), ctxx,
		"[generate-section:style] please generate the style", nil, 30)
	if err != nil {
		t.Fatalf("tagged turn must not hard-fail: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got > 2 {
		t.Fatalf("summary endpoint called %d times, want at most 2", got)
	}
	if reply == "" {
		t.Fatal("expected the deterministic reply after the API failures")
	}
}

// The generic Enhance wand tag carries the target StoryConfig key so the chat
// fast path can rewrite one Novel parameters field in place.
func TestParseGenerateSectionTagEnhanceField(t *testing.T) {
	call := ParseGenerateSectionTag("Please enhance the tone [generate-section:enhance;field=tone]")
	if call == nil {
		t.Fatal("enhance tag was not parsed")
	}
	var args map[string]string
	if err := json.Unmarshal(call.Arguments, &args); err != nil {
		t.Fatalf("args: %v", err)
	}
	if args["section"] != "enhance" || args["field"] != "tone" {
		t.Fatalf("unexpected args: %+v", args)
	}
}
