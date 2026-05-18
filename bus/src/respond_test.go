package bus

import (
	"context"
	"testing"

	"github.com/AMCP-Drones/drones/systems/deliverydron/tests/testutil"
)

func TestRespond_NoReplyTo(t *testing.T) {
	mem := testutil.NewMemoryBus()
	ctx := context.Background()
	err := Respond(ctx, mem, map[string]interface{}{"action": "ping"}, map[string]interface{}{"ok": true}, "test", true, "")
	if err != errNoReplyTo {
		t.Fatalf("expected errNoReplyTo, got %v", err)
	}
}

func TestRespond_PublishesResponse(t *testing.T) {
	mem := testutil.NewMemoryBus()
	ctx := context.Background()
	replyTopic := "test.reply.custom"
	got := make(chan map[string]interface{}, 1)
	_ = mem.Subscribe(ctx, replyTopic, func(msg map[string]interface{}) {
		got <- msg
	})
	orig := map[string]interface{}{
		"reply_to":       replyTopic,
		"correlation_id": "cid-1",
	}
	if err := Respond(ctx, mem, orig, map[string]interface{}{"x": 1}, "sender", true, ""); err != nil {
		t.Fatal(err)
	}
	msg := <-got
	if msg["correlation_id"] != "cid-1" {
		t.Fatalf("correlation_id=%v", msg["correlation_id"])
	}
	if msg["success"] != true {
		t.Fatalf("success=%v", msg["success"])
	}
}
