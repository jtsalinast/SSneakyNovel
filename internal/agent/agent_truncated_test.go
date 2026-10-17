package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"showmethestory/internal/config"
	"showmethestory/internal/llm"
	"strings"
	"testing"
)

func TestAgentPartialStreamDoesNotAppendFallback(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")
	}))
	defer srv.Close()
	var output strings.Builder
	_, err := callAgentAPI(context.Background(), &config.APIConfig{BaseURL: srv.URL}, nil, func(s string) { output.WriteString(s) })
	if err == nil || calls != 1 || output.String() != "partial" {
		t.Fatalf("err=%v calls=%d output=%q", err, calls, output.String())
	}
}

func TestParseToolCallTruncatedJSONNoRepair(t *testing.T) {
	// The closing tag is missing and the JSON is incomplete; parseToolCall should return nil.
	truncated := `brightwhite, open#3chapter. <tool_call> {"name":"revise_chapter","arguments":{"num":3,"feedback":"heavywrite#3chapter, untiehard: \n\n1.[offborrowshoeborrowsock]shoecode4-5code, borrowshoethroughnot upper; lifenot associationBAsockgivelife. thissectionwhiskerexcept. \n\n2.[newborrowmouth]outfit. hefrontinupperbuyitemclass bluehorsearmor, hand`

	tc := parseToolCall(truncated)
	if tc != nil {
		t.Fatalf("parseToolCall should not parse truncated JSON, got tool: %s", tc.Name)
	}
}

func TestParseToolCallCompleteTag(t *testing.T) {
	complete := `<tool_call>{"name":"search_project","arguments":{"query":""}}</tool_call>`
	tc := parseToolCall(complete)
	if tc == nil {
		t.Fatal("failed to parse complete tool call")
	}
	if tc.Name != "search_project" {
		t.Fatalf("tool name should be search_project, got: %s", tc.Name)
	}
}

func TestParseToolCallTextBeforeTag(t *testing.T) {
	content := `good , Icome. <tool_call>{"name":"revise_chapter","arguments":{"num":1,"feedback":"#onechapter"}}</tool_call>`
	tc := parseToolCall(content)
	if tc == nil {
		t.Fatal("failed to parse tool call preceded by text")
	}
	if tc.Name != "revise_chapter" {
		t.Fatalf("tool name should be revise_chapter, got: %s", tc.Name)
	}
}

func TestExtractJSONStringAware(t *testing.T) {
	content := `{"name":"x","arguments":{"feedback":"hesay: } thisNo."}}`
	got := llm.ExtractJSON(content)
	tc := parseToolCallFromJSON(got)
	if tc == nil {
		t.Fatalf("string-aware JSON extraction failed, got: %q", got)
	}
	if tc.Name != "x" {
		t.Fatalf("tool name should be x, got: %s", tc.Name)
	}
}

func TestHasUnclosedToolCall(t *testing.T) {
	if !hasUnclosedToolCall(`<tool_call>{"name":"x"}`) {
		t.Fatal("should recognize an unclosed tool_call")
	}
	if hasUnclosedToolCall(`<tool_call>{"name":"x"}</tool_call>`) {
		t.Fatal("closed tool_call should not be flagged as unclosed")
	}
	if hasUnclosedToolCall(`no tool here`) {
		t.Fatal("text without tool_call should return false")
	}
}

func TestIsAgentOutputTruncated(t *testing.T) {
	truncated := `front <tool_call> {"name":"revise_chapter","arguments":{"feedback":"hello`
	if !isAgentOutputTruncated("length", truncated, nil) {
		t.Fatal("finish_reason=length + unclosed tool_call + broken JSON should count as truncated")
	}
	if isAgentOutputTruncated("stop", truncated, nil) {
		t.Fatal("finish_reason=stop should not count as token truncation")
	}
	complete := `<tool_call>{"name":"x","arguments":{}}</tool_call>`
	tc := parseToolCall(complete)
	if isAgentOutputTruncated("length", complete, tc) {
		t.Fatal("complete tool_call with finish_reason=length should not be truncated (provider guarantees pass-through)")
	}
}

func TestIsFailedToolCallAttempt(t *testing.T) {
	unclosed := `<tool_call> {"name":"update_project_config","arguments":{"title":"`
	if !isFailedToolCallAttempt(unclosed, parseToolCall(unclosed)) {
		t.Fatal("unclosed tool_call with broken JSON should be a failed attempt")
	}
	badClosed := `<tool_call>{"name":</tool_call>`
	if !isFailedToolCallAttempt(badClosed, parseToolCall(badClosed)) {
		t.Fatal("closed tag with invalid JSON should be a failed attempt")
	}
	ok := `<tool_call>{"name":"search_project","arguments":{"query":"x"}}</tool_call>`
	if isFailedToolCallAttempt(ok, parseToolCall(ok)) {
		t.Fatal("valid closed tool_call should not be a failed attempt")
	}
	if isFailedToolCallAttempt("connectreturnuse, notool", nil) {
		t.Fatal("plain response without tool call should not be a failed attempt")
	}
}

func TestToolCallParseRetryFeedback(t *testing.T) {
	ctx := &AgentContext{Config: &config.Config{Language: "en"}, APICfg: &config.APIConfig{MaxTokens: 8192}}
	fb := toolCallParseRetryFeedback(ctx, "stop", `<tool_call>{"name":"x"`)
	if !strings.Contains(fb, "retry once") {
		t.Fatalf("feedback should ask for a retry, got: %s", fb)
	}
	if !strings.Contains(fb, "truncated_or_unclosed") {
		t.Fatalf("unclosed call should report reason truncated_or_unclosed, got: %s", fb)
	}
}

func TestAgentEffectiveMaxTokens(t *testing.T) {
	if agentEffectiveMaxTokens(&config.APIConfig{MaxTokens: 0}) != 8192 {
		t.Fatal("agent max_tokens floor should be 8192")
	}
	if agentEffectiveMaxTokens(&config.APIConfig{MaxTokens: 16000}) != 16000 {
		t.Fatal("should use the configured max_tokens")
	}
}
