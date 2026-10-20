package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"showmethestory/internal/config"
	"showmethestory/internal/sse"
	"strings"
	"time"
)

type ChatRequest struct {
	Model         string         `json:"model"`
	Messages      []Message      `json:"messages"`
	Stream        bool           `json:"stream,omitempty"`
	StreamOptions *streamOptions `json:"stream_options,omitempty"`
	MaxTokens     int            `json:"max_tokens,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type tokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatResponse struct {
	Choices []struct {
		Message      Message `json:"message"`
		FinishReason string  `json:"finish_reason"`
	} `json:"choices"`
	Usage *tokenUsage `json:"usage,omitempty"`
}

// CompletionResult is the normalized result of a chat completion call.
type CompletionResult struct {
	Content      string
	FinishReason string // e.g. "stop", "length"
}

// A server-side output limit will not recover by retrying the same request.
type outputLimitError struct{ maxTokens int }

type contextBudgetError struct{ prompt, budget int }

func (e *contextBudgetError) Error() string {
	return fmt.Sprintf("estimated prompt size %d tokens exceeds context input budget %d; reduce project context or increase the configured context window", e.prompt, e.budget)
}

func (e *outputLimitError) Error() string {
	return fmt.Sprintf("API output truncated (finish_reason=length, requested max_tokens=%d); check provider output/reasoning limits or reduce the requested batch size", e.maxTokens)
}

func completionText(result CompletionResult, err error, maxTokens int) (string, error) {
	if err == nil && result.FinishReason == "length" {
		err = &outputLimitError{maxTokens: maxTokens}
	}
	return result.Content, err
}

func hasAPIVersionSegment(u string) bool {
	for _, seg := range strings.Split(u, "/") {
		if len(seg) >= 2 && seg[0] == 'v' && seg[1] >= '0' && seg[1] <= '9' {
			return true
		}
	}
	return false
}

// resolveChatCompletionsURL builds the POST endpoint from base_url and url_strict.
// Must stay in sync with frontend/src/lib/apiUrl.js.
func resolveChatCompletionsURL(base string, strict bool) string {
	base = strings.TrimSpace(base)
	base = strings.TrimSuffix(base, "/")
	if base == "" {
		return ""
	}
	if strings.HasSuffix(base, "/chat/completions") {
		return base
	}
	if strict {
		return base + "/chat/completions"
	}
	if hasAPIVersionSegment(base) {
		return base + "/chat/completions"
	}
	return base + "/v1/chat/completions"
}

func resolveAPIBase(base string, strict bool) string {
	u := resolveChatCompletionsURL(base, strict)
	return strings.TrimSuffix(u, "/chat/completions")
}

func normalizeURL(apiCfg *config.APIConfig) string {
	if apiCfg == nil {
		return ""
	}
	return resolveChatCompletionsURL(apiCfg.BaseURL, apiCfg.URLStrict)
}

// EnsureContextBudget fills an unset budget and clamps configured values to a
// smaller model-reported window. A deliberate smaller user limit is retained.
func EnsureContextBudget(apiCfg *config.APIConfig) {
	if apiCfg == nil {
		return
	}
	if window := FetchModelContextWindow(apiCfg); window > 0 {
		if apiCfg.ContextBudgetTokens <= 0 || apiCfg.ContextBudgetTokens > window {
			apiCfg.ContextBudgetTokens = window
		}
	} else if apiCfg.ContextBudgetTokens <= 0 {
		apiCfg.ContextBudgetTokens = config.DefaultContextBudgetTokens
	}
}

// FetchModelContextWindow from API /models dotwithdrawfinger upperlowerwindowmouthbigsmall.
// return context_window > 0, return 0(usesquareshouldmakeusevalue).
func FetchModelContextWindow(apiCfg *config.APIConfig) int {
	if apiCfg == nil || strings.TrimSpace(apiCfg.BaseURL) == "" || strings.TrimSpace(apiCfg.Model) == "" {
		return 0
	}
	modelsURL := resolveAPIBase(apiCfg.BaseURL, apiCfg.URLStrict) + "/models/" + apiCfg.Model

	req, err := http.NewRequest("GET", modelsURL, nil)
	if err != nil {
		return 0
	}
	if apiCfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiCfg.APIKey)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0
	}

	var result struct {
		ContextWindow int `json:"context_window"`
	}
	if err := json.Unmarshal(body, &result); err != nil || result.ContextWindow <= 0 {
		return 0
	}
	return result.ContextWindow
}

// sharedHTTPClient returns a client that reuses TCP connections (keep-alive)
// across calls. Creating a fresh http.Client per request prevented connection
// reuse, so retry storms opened hundreds of sockets and flooded the local
// network stack — one of the causes of machine freezes during generation.
func sharedHTTPClient(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConnsPerHost = 8
	return &http.Client{Timeout: timeout, Transport: transport}
}

func ValidateConfig(apiCfg *config.APIConfig) error {
	if strings.TrimSpace(apiCfg.BaseURL) == "" {
		return fmt.Errorf("API Base URL not configured")
	}
	if strings.TrimSpace(apiCfg.Model) == "" {
		return fmt.Errorf("model not configured")
	}
	return nil
}

// isStreamUnsupportedError reports whether err is the server rejecting the
// streaming request itself (HTTP 400 on a stream=true call). Only in that
// case does retrying with a plain non-streaming completion make sense; every
// other fatal error (auth, budget, oversized payload) would fail identically
// on the second call.
func isStreamUnsupportedError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	if !strings.Contains(msg, "status code: 400") {
		return false
	}
	// A context-budget rejection would fail identically without streaming;
	// never spend a second HTTP call on it.
	var budget *contextBudgetError
	if errors.As(err, &budget) {
		return false
	}
	return true
}

// IsOutputTruncation reports whether the model hit its output token cap
// (finish_reason=length). The partial text is NOT carried inside the error:
// callers receive it through CompletionResult.Content alongside this error.
// Chapter generation treats truncation as RECOVERABLE: the partial draft is
// handed to the length controller (which keeps, expands or regenerates it)
// rather than aborting the chapter outright.
func IsOutputTruncation(err error) bool {
	if err == nil {
		return false
	}
	var limit *outputLimitError
	return errors.As(err, &limit)
}

func IsFatalAPIError(err error) bool {
	if err == nil {
		return false
	}
	// Truncation is deliberately NOT fatal anymore: retrying with the same
	// request would truncate identically, but throwing the partial draft away
	// lost whole chapters that were merely slightly short of max_tokens. The
	// streaming caller returns the partial content plus this error so the
	// length-control loop can decide (keep / expand / regenerate).
	var limit *outputLimitError
	if errors.As(err, &limit) {
		return false
	}
	var budget *contextBudgetError
	if errors.As(err, &budget) {
		return true
	}
	msg := err.Error()
	// Note: we deliberately do NOT treat every "dial tcp" occurrence as fatal —
	// "dial tcp ... i/o timeout" is transient and should be retried.
	if strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "no such host") {
		return true
	}
	if strings.Contains(msg, "status code: 401") ||
		strings.Contains(msg, "status code: 403") ||
		strings.Contains(msg, "status code: 404") {
		return true
	}
	// Request-level 4xx errors will never succeed on retry: the server rejected
	// the payload itself (too large / malformed / unsupported parameter).
	// Retrying these used to hammer the API until the machine froze.
	for _, code := range []string{"400", "413", "422"} {
		if strings.Contains(msg, "status code: "+code) {
			return true
		}
	}
	if strings.Contains(msg, "context canceled") {
		return true
	}
	return false
}

// maxErrorBodyRunes bounds how much of an API error response body is kept in
// the returned error. Some providers echo the entire rejected prompt back in
// the error body; without a cap this giant string was copied into every log
// line and broadcast over SSE, saturating memory and UI channels.
const maxErrorBodyRunes = 600

func truncateErrorBody(body string) string {
	body = strings.TrimSpace(body)
	r := []rune(body)
	if len(r) <= maxErrorBodyRunes {
		return body
	}
	return string(r[:maxErrorBodyRunes]) + "… [truncated]"
}

// PromptInputBudget reserves output capacity and a small provider/tokenizer
// margin. Estimates deliberately use the project's conservative rune ratio.
func PromptInputBudget(apiCfg *config.APIConfig) int {
	window := config.DefaultContextBudgetTokens
	output := config.DefaultMaxTokens
	if apiCfg != nil {
		if apiCfg.ContextBudgetTokens > 0 {
			window = apiCfg.ContextBudgetTokens
		}
		if apiCfg.MaxTokens > 0 {
			output = apiCfg.MaxTokens
		}
	}
	margin := max(4096, window/20)
	return max(0, window-output-margin)
}

func validateContextBudget(apiCfg *config.APIConfig, messages []Message) error {
	prompt := EstimateTokensFromRunes(countMessageRunes(messages))
	budget := PromptInputBudget(apiCfg)
	if prompt > budget {
		return &contextBudgetError{prompt: prompt, budget: budget}
	}
	return nil
}

func CallAPI(ctx context.Context, apiCfg *config.APIConfig, system, user string) (string, error) {
	return CallAPIMessages(ctx, apiCfg, []Message{
		{Role: "system", Content: system},
		{Role: "user", Content: user},
	})
}

// CallAPIMessages calls the API with a full message list. It streams the
// response, and only when streaming yields no content at all and the error is
// transient does it fall back to one synchronous call.
func CallAPIMessages(ctx context.Context, apiCfg *config.APIConfig, messages []Message) (string, error) {
	messages = applyPromptAddon(ctx, messages)
	result, err := CallAPIStreamMessages(ctx, apiCfg, messages, nil)
	if err == nil && result.Content != "" {
		return completionText(result, nil, maxTokensFor(apiCfg))
	}
	if ctx.Err() != nil {
		if result.Content != "" {
			return result.Content, ctx.Err()
		}
		return "", ctx.Err()
	}
	if result.Content != "" {
		return result.Content, err
	}
	// Fall back to the synchronous endpoint only when the server explicitly
	// rejected the streaming request itself (HTTP 400 on the stream call).
	// Other fatal errors (auth, budget, oversized payload, 413/422) will also
	// fail synchronously, so they must not trigger a second HTTP call — that
	// doubled the connection pressure and log volume during generation storms.
	if !isStreamUnsupportedError(err) {
		return "", err
	}
	syncResult, syncErr := CallAPIMessagesSync(ctx, apiCfg, messages)
	if syncErr != nil {
		// If the non-streaming fallback hit the output limit, surface that
		// diagnostic instead of the original "stream unsupported" error.
		return "", syncErr
	}
	return completionText(syncResult, nil, maxTokensFor(apiCfg))
}

// CallAPIMessagesSync performs one plain (non-streaming) HTTP completion.
func CallAPIMessagesSync(ctx context.Context, apiCfg *config.APIConfig, messages []Message) (CompletionResult, error) {
	if err := validateContextBudget(apiCfg, messages); err != nil {
		return CompletionResult{}, err
	}
	fullURL := normalizeURL(apiCfg)
	tracker := TaskTokensFromContext(ctx)
	tracker.beginCall(messages)

	reqBody := ChatRequest{
		Model:     apiCfg.Model,
		Messages:  messages,
		MaxTokens: apiCfg.MaxTokens,
	}

	bts, err := json.Marshal(reqBody)
	if err != nil {
		return CompletionResult{}, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", fullURL, bytes.NewBuffer(bts))
	if err != nil {
		return CompletionResult{}, err
	}

	req.Header.Set("Content-Type", "application/json")
	if apiCfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiCfg.APIKey)
	}

	timeout := time.Duration(apiCfg.HTTPTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 300 * time.Second
	}
	client := sharedHTTPClient(timeout)
	resp, err := client.Do(req)
	if err != nil {
		return CompletionResult{}, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return CompletionResult{}, err
	}

	if resp.StatusCode != http.StatusOK {
		return CompletionResult{}, fmt.Errorf("API error, status code: %d, response body: %s", resp.StatusCode, truncateErrorBody(string(bodyBytes)))
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(bodyBytes, &chatResp); err != nil {
		return CompletionResult{}, err
	}

	if len(chatResp.Choices) > 0 {
		content := chatResp.Choices[0].Message.Content
		if tracker != nil {
			if chatResp.Usage != nil {
				tracker.finishCall(chatResp.Usage.PromptTokens, chatResp.Usage.CompletionTokens, true, messages, content)
			} else {
				tracker.finishCall(0, 0, false, messages, content)
			}
		}
		return CompletionResult{Content: content, FinishReason: chatResp.Choices[0].FinishReason}, nil
	}
	return CompletionResult{}, fmt.Errorf("API returned no valid Choices text")
}

// MaxAPIRetries bounds the retry loop in CallAPIWithRetry/CallAPIWithRetryLog.
// Without a ceiling, transient errors or empty responses caused an infinite
// retry loop that saturated CPU, connections and log channels (this was the
// root cause of machine freezes when a generation task kept hammering the API).
const MaxAPIRetries = 8

func callWithRetryLoop(ctx context.Context, apiCfg *config.APIConfig, system, user string, onRetry func(err error, attempt, waitSec int)) string {
	for retryCount := 1; ; retryCount++ {
		if ctx.Err() != nil {
			return ""
		}
		result, err := CallAPI(ctx, apiCfg, system, user)
		if err == nil && strings.TrimSpace(result) != "" {
			return result
		}
		if IsFatalAPIError(err) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return ""
		}
		if retryCount >= MaxAPIRetries {
			if onRetry != nil {
				e := err
				if e == nil {
					e = fmt.Errorf("API returned empty response")
				}
				onRetry(e, retryCount, 0)
			}
			return ""
		}
		waitTime := RetryWaitTime(retryCount)
		if onRetry != nil {
			e := err
			if e == nil {
				e = fmt.Errorf("API returned empty response")
			}
			onRetry(e, retryCount, waitTime)
		}
		select {
		case <-time.After(time.Duration(waitTime) * time.Second):
		case <-ctx.Done():
			return ""
		}
	}
}

func CallAPIWithRetry(ctx context.Context, apiCfg *config.APIConfig, system, user string) string {
	return callWithRetryLoop(ctx, apiCfg, system, user, func(err error, attempt, waitSec int) {
		fmt.Printf(" ⚠️ API call failed: %v. Retry %d/%d, waiting %ds...\n", err, attempt, MaxAPIRetries, waitSec)
	})
}

func CallAPIWithRetryLog(ctx context.Context, apiCfg *config.APIConfig, system, user string, logger *sse.LogBroadcaster) string {
	return callWithRetryLoop(ctx, apiCfg, system, user, func(err error, attempt, waitSec int) {
		if logger != nil {
			logger.WarnKey("log.api_retry", err, attempt, waitSec)
		} else {
			fmt.Printf(" ⚠️ API call failed: %v. Retry %d/%d, waiting %ds...\n", err, attempt, MaxAPIRetries, waitSec)
		}
	})
}

func RetryWaitTime(retry int) int {
	if retry > 4 {
		return 20
	}
	return retry * 5
}

type streamDelta struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *tokenUsage `json:"usage,omitempty"`
}

func CallAPIStream(ctx context.Context, apiCfg *config.APIConfig, system, user string, onChunk func(string)) (string, error) {
	result, err := CallAPIStreamMessages(ctx, apiCfg, []Message{
		{Role: "system", Content: system},
		{Role: "user", Content: user},
	}, onChunk)
	return completionText(result, err, maxTokensFor(apiCfg))
}

// maxTokensFor resolves the effective output cap actually sent to the API so
// truncation diagnostics report the real limit instead of a raw 0 when the
// project leaves MaxTokens unset (DefaultMaxTokens is applied per-request).
func maxTokensFor(apiCfg *config.APIConfig) int {
	if apiCfg != nil && apiCfg.MaxTokens > 0 {
		return apiCfg.MaxTokens
	}
	return config.DefaultMaxTokens
}

// CallAPIStreamMessages calls the streaming API with a full message list.
func CallAPIStreamMessages(ctx context.Context, apiCfg *config.APIConfig, messages []Message, onChunk func(string)) (CompletionResult, error) {
	if err := validateContextBudget(apiCfg, messages); err != nil {
		return CompletionResult{}, err
	}
	fullURL := normalizeURL(apiCfg)
	tracker := TaskTokensFromContext(ctx)
	tracker.beginCall(messages)

	reqBody := ChatRequest{
		Model:         apiCfg.Model,
		Messages:      messages,
		Stream:        true,
		StreamOptions: &streamOptions{IncludeUsage: true},
		MaxTokens:     apiCfg.MaxTokens,
	}

	bts, err := json.Marshal(reqBody)
	if err != nil {
		return CompletionResult{}, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", fullURL, bytes.NewBuffer(bts))
	if err != nil {
		return CompletionResult{}, err
	}

	req.Header.Set("Content-Type", "application/json")
	if apiCfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiCfg.APIKey)
	}

	timeout := time.Duration(apiCfg.HTTPTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 300 * time.Second
	}
	client := sharedHTTPClient(timeout)
	resp, err := client.Do(req)
	if err != nil {
		return CompletionResult{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return CompletionResult{}, fmt.Errorf("API error, status code: %d, response body: %s", resp.StatusCode, truncateErrorBody(string(bodyBytes)))
	}

	var fullContent strings.Builder
	reader := bufio.NewReader(resp.Body)
	var streamUsage *tokenUsage
	var finishReason string
	var readErr error
	done := false

	// Some OpenAI-compatible servers ignore "stream": true and answer with a
	// plain JSON completion. Detect that shape up front (content-type is not
	// text/event-stream) and parse the body as a non-stream response instead of
	// silently producing an empty stream error.
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		var chatResp ChatResponse
		if jerr := json.Unmarshal(bodyBytes, &chatResp); jerr == nil && len(chatResp.Choices) > 0 {
			content := chatResp.Choices[0].Message.Content
			fr := chatResp.Choices[0].FinishReason
			if tracker != nil {
				if chatResp.Usage != nil {
					tracker.finishCall(chatResp.Usage.PromptTokens, chatResp.Usage.CompletionTokens, true, messages, content)
				} else {
					tracker.finishCall(0, 0, false, messages, content)
				}
			}
			if content != "" && onChunk != nil {
				onChunk(content)
			}
			completion := CompletionResult{Content: content, FinishReason: fr}
			if ctx.Err() != nil {
				return completion, ctx.Err()
			}
			if content == "" {
				return completion, fmt.Errorf("non-streaming response was empty")
			}
			return completion, nil
		}
		// Not valid JSON either: fall through to the SSE loop, which will
		// surface a proper "stream ended" error.
		reader = bufio.NewReader(bytes.NewReader(bodyBytes))
	}

	for readErr == nil {
		if ctx.Err() != nil {
			return CompletionResult{Content: fullContent.String(), FinishReason: finishReason}, ctx.Err()
		}
		var line string
		line, readErr = reader.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			done = true
			break
		}

		var delta streamDelta
		if err := json.Unmarshal([]byte(data), &delta); err != nil {
			readErr = fmt.Errorf("invalid SSE JSON: %w", err)
			break
		}
		if delta.Usage != nil {
			streamUsage = delta.Usage
		}
		if len(delta.Choices) > 0 {
			if delta.Choices[0].FinishReason != "" {
				finishReason = delta.Choices[0].FinishReason
			}
			if delta.Choices[0].Delta.Content != "" {
				chunk := delta.Choices[0].Delta.Content
				fullContent.WriteString(chunk)
				if tracker != nil {
					tracker.updateStreamContent(fullContent.String())
				}
				if onChunk != nil {
					onChunk(chunk)
				}
			}
		}
	}

	result := fullContent.String()
	if tracker != nil {
		if streamUsage != nil {
			tracker.finishCall(streamUsage.PromptTokens, streamUsage.CompletionTokens, true, messages, result)
		} else {
			tracker.finishCall(0, 0, false, messages, result)
		}
	}
	completion := CompletionResult{Content: result, FinishReason: finishReason}
	if ctx.Err() != nil {
		return completion, ctx.Err()
	}
	if readErr != nil && readErr != io.EOF {
		return completion, fmt.Errorf("stream interrupted after %d bytes: %w", len(result), readErr)
	}
	if !done && finishReason == "" {
		return completion, fmt.Errorf("stream ended without finish_reason or [DONE] after %d bytes: %w", len(result), io.ErrUnexpectedEOF)
	}
	if result == "" {
		return completion, fmt.Errorf("streaming response was empty")
	}
	return completion, nil
}
