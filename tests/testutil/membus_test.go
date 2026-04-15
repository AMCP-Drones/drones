package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/AMCP-Drones/drones/components/bus/src"
	"github.com/AMCP-Drones/drones/components/sdk/src"
)

func TestMemoryBus_RequestResponse(t *testing.T) {
	b := NewMemoryBus()
	ctx := context.Background()
	if err := b.Start(ctx); err != nil {
		t.Fatal(err)
	}
	const svc = "v1.testsys.T001.echo_svc"
	_ = b.Subscribe(ctx, svc, func(msg map[string]interface{}) {
		_ = bus.Respond(ctx, b, msg, map[string]interface{}{"answer": 42}, "echo_svc", true, "")
	})
	resp, err := b.Request(ctx, svc, map[string]interface{}{
		"action":  "ping",
		"sender":  "client",
		"payload": map[string]interface{}{},
	}, 2.0)
	if err != nil {
		t.Fatal(err)
	}
	if !resp["success"].(bool) {
		t.Fatalf("expected success: %#v", resp)
	}
	pl, _ := resp["payload"].(map[string]interface{})
	if pl["answer"].(int) != 42 {
		t.Fatalf("payload: %#v", pl)
	}
}

func TestMemoryBus_PublishToHandler(t *testing.T) {
	b := NewMemoryBus()
	ctx := context.Background()
	var got map[string]interface{}
	_ = b.Subscribe(ctx, "t1", func(m map[string]interface{}) { got = m })
	_ = b.Publish(ctx, "t1", map[string]interface{}{"action": "x", "k": 1})
	if got == nil || got["k"].(int) != 1 {
		t.Fatalf("got %#v", got)
	}
}

func TestMemoryBus_ResponseRoutingUsesSDKShape(t *testing.T) {
	b := NewMemoryBus()
	ctx := context.Background()
	_ = b.Subscribe(ctx, "svc", func(msg map[string]interface{}) {
		cid, _ := msg["correlation_id"].(string)
		replyTo, _ := msg["reply_to"].(string)
		resp := sdk.CreateResponse(cid, map[string]interface{}{"ok": true}, "svc", true, "")
		_ = b.Publish(ctx, replyTo, resp)
	})
	out, err := b.Request(ctx, "svc", map[string]interface{}{"action": "a"}, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	if out["action"] != "response" {
		t.Fatalf("got %#v", out)
	}
}

func TestMemoryBus_RequestTimeout(t *testing.T) {
	b := NewMemoryBus()
	ctx := context.Background()
	_ = b.Subscribe(ctx, "slow", func(map[string]interface{}) { time.Sleep(200 * time.Millisecond) })
	_, err := b.Request(ctx, "slow", map[string]interface{}{"action": "x"}, 0.05)
	if err == nil {
		t.Fatal("expected timeout")
	}
}
