package llm

import (
	"context"
	"testing"
	"time"

	"showmethestory/internal/sse"
)

// Brainstorm calls must report on their own SSE channel so they never touch
// the background task's token_usage badge.
func TestBrainstormTokensUseDedicatedChannel(t *testing.T) {
	logger := sse.NewLogBroadcaster()
	defer logger.Close()
	ch := logger.Subscribe()
	defer logger.Unsubscribe(ch)

	ctx, usage := WithBrainstormTokens(context.Background(), logger, "bs-1", 2)
	if TaskTokensFromContext(ctx) != usage {
		t.Fatal("tracker must travel in the context")
	}
	usage.beginCall([]Message{{Role: "user", Content: "hola"}})
	usage.finishCall(120, 45, true, nil, "")

	var sawBrainstorm bool
	deadline := time.After(2 * time.Second)
	for !sawBrainstorm {
		select {
		case msg := <-ch:
			switch msg.Event {
			case "token_usage":
				t.Fatalf("brainstorm call leaked into the task token channel: %#v", msg.Data)
			case "brainstorm_tokens":
				data, ok := msg.Data.(map[string]interface{})
				if !ok {
					t.Fatalf("unexpected payload %#v", msg.Data)
				}
				if data["request_id"] != "bs-1" || data["variant"] != 2 {
					t.Fatalf("wrong request scope: %#v", data)
				}
				// beginCall emits a provisional estimate first; keep reading
				// until the committed usage of the finished call arrives.
				if data["prompt_tokens"] == 120 && data["completion_tokens"] == 45 {
					sawBrainstorm = true
				}
			}
		case <-deadline:
			t.Fatal("no brainstorm_tokens event emitted")
		}
	}
	if p, c := usage.Snapshot(); p != 120 || c != 45 {
		t.Fatalf("snapshot = %d/%d, want 120/45", p, c)
	}
}
