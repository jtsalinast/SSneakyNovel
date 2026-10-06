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
