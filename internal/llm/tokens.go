package llm

import (
	"context"
	"showmethestory/internal/sse"
	"sync"
	"time"
	"unicode/utf8"
)

type taskTokenCtxKey struct{}

// EstimateTokensFromRunes is the shared rune→token estimate (~1.5 tokens per
// rune) used when the API returns no usage data.
func EstimateTokensFromRunes(runes int) int {
	return int(float64(runes) * 1.5)
}

const tokenEmitInterval = 2 * time.Second

// TaskTokenUsage accumulates prompt/completion tokens for one async task.
type TaskTokenUsage struct {
	mu sync.Mutex

	committedPrompt     int
	committedCompletion int
	pendingPrompt       int
	pendingCompletion   int

	logger   *sse.LogBroadcaster
	lastEmit time.Time
}

func newTaskTokenUsage(logger *sse.LogBroadcaster) *TaskTokenUsage {
	return &TaskTokenUsage{logger: logger}
}

// NewTaskTokenUsage builds a standalone tracker (exported wrapper around the
// private constructor) for detached runners that must own their token counts —
// see StartDetachedPrePlanGenerate.
func NewTaskTokenUsage(logger *sse.LogBroadcaster) *TaskTokenUsage {
	return newTaskTokenUsage(logger)
}

func WithTaskTokens(ctx context.Context, logger *sse.LogBroadcaster) (context.Context, *TaskTokenUsage) {
	usage := newTaskTokenUsage(logger)
	return context.WithValue(ctx, taskTokenCtxKey{}, usage), usage
}

// AttachTaskTokens re-uses an existing tracker for ctx: it returns a derived
// context whose LLM calls accumulate into usage. When usage is nil the context
// is returned unchanged (calls then report no usage, same as before). Used by
// detached background work (Generate buttons) so its tokens land in the same
// badge the UI reads via /api/status instead of an orphaned tracker.
func AttachTaskTokens(ctx context.Context, usage *TaskTokenUsage) context.Context {
	if usage == nil {
		return ctx
	}
	return context.WithValue(ctx, taskTokenCtxKey{}, usage)
}

// MaxTaskDuration is the DEFAULT hard wall-clock ceiling for a single
// background generation task. Without it, a persistently failing or hanging
// API endpoint could keep a task (and its retry loops) alive forever,
// saturating CPU, connections and log channels — the root cause of machine
// freezes during generation. Callers must invoke the returned cancel func when
// the task ends normally so resources are released early.
const MaxTaskDuration = 30 * time.Minute

// NewTaskContext builds the root context for a background generation task:
// cancellable on demand (manual stop) and bounded by maxDuration. A
// non-positive maxDuration means NO wall-clock ceiling — the task runs until
// it completes or the user presses Stop. This is what the configurable
// task_duration_minutes setting maps to: long chapter batches that include
// several rewrite passes legitimately exceed 30 minutes, and previously the
// deadline killed them mid-flight even though every individual LLM call was
// healthy (per-call HTTP timeouts still apply either way).
func NewTaskContext(parent context.Context, maxDuration time.Duration) (context.Context, context.CancelFunc) {
	if maxDuration <= 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, maxDuration)
}

func TaskTokensFromContext(ctx context.Context) *TaskTokenUsage {
	if ctx == nil {
		return nil
	}
	usage, _ := ctx.Value(taskTokenCtxKey{}).(*TaskTokenUsage)
	return usage
}

func countMessageRunes(messages []Message) int {
	n := 0
	for _, m := range messages {
		n += utf8.RuneCountInString(m.Content)
	}
	return n
}

func (t *TaskTokenUsage) beginCall(messages []Message) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.pendingPrompt = EstimateTokensFromRunes(countMessageRunes(messages))
	t.pendingCompletion = 0
	t.mu.Unlock()
	t.maybeEmit(true)
}

func (t *TaskTokenUsage) updateStreamContent(content string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.pendingCompletion = EstimateTokensFromRunes(utf8.RuneCountInString(content))
	t.mu.Unlock()
	t.maybeEmit(false)
}

func (t *TaskTokenUsage) finishCall(promptTokens, completionTokens int, hasUsage bool, messages []Message, output string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	if hasUsage {
		t.committedPrompt += promptTokens
		t.committedCompletion += completionTokens
	} else {
		t.committedPrompt += EstimateTokensFromRunes(countMessageRunes(messages))
		t.committedCompletion += EstimateTokensFromRunes(utf8.RuneCountInString(output))
	}
	t.pendingPrompt = 0
	t.pendingCompletion = 0
	t.mu.Unlock()
	t.maybeEmit(true)
}

func (t *TaskTokenUsage) Snapshot() (prompt, completion int) {
	if t == nil {
		return 0, 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.committedPrompt + t.pendingPrompt, t.committedCompletion + t.pendingCompletion
}

func (t *TaskTokenUsage) maybeEmit(force bool) {
	if t == nil || t.logger == nil {
		return
	}
	t.mu.Lock()
	now := time.Now()
	if !force && !t.lastEmit.IsZero() && now.Sub(t.lastEmit) < tokenEmitInterval {
		t.mu.Unlock()
		return
	}
	prompt := t.committedPrompt + t.pendingPrompt
	completion := t.committedCompletion + t.pendingCompletion
	t.lastEmit = now
	t.mu.Unlock()
	t.logger.TokenUsage(prompt, completion)
}
