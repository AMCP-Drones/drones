package component

import (
	"context"
	"testing"

	"github.com/AMCP-Drones/drones/systems/deliverydron/tests/testutil"
)

func TestGetString(t *testing.T) {
	if getString(map[string]interface{}{"k": "v"}, "k") != "v" {
		t.Fatal("string")
	}
	if getString(map[string]interface{}{"k": 1}, "k") != "" {
		t.Fatal("non-string")
	}
}

func TestBaseComponent_StartStop_Idempotent(t *testing.T) {
	mem := testutil.NewMemoryBus()
	ctx := context.Background()
	b := NewBaseComponent("id", "type", "topic", mem)
	if err := b.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := b.Start(ctx); err != nil {
		t.Fatal("second start")
	}
	if err := b.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := b.Stop(ctx); err != nil {
		t.Fatal("second stop")
	}
}

func TestBaseComponent_Stop(t *testing.T) {
	mem := testutil.NewMemoryBus()
	ctx := context.Background()
	b := NewBaseComponent("id", "type", "topic", mem)
	_ = b.Start(ctx)
	if err := b.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if b.Running() {
		t.Fatal("expected stopped")
	}
}

func TestBaseComponent_UnknownAction(t *testing.T) {
	mem := testutil.NewMemoryBus()
	ctx := context.Background()
	b := NewBaseComponent("id", "type", "topic", mem)
	_ = b.Start(ctx)
	defer func() { _ = b.Stop(ctx) }()
	resp, err := mem.Request(ctx, "topic", map[string]interface{}{
		"action": "missing", "sender": "security_monitor",
	}, 2.0)
	if err != nil {
		t.Fatal(err)
	}
	if resp["success"] != false {
		t.Fatalf("%#v", resp)
	}
}

func TestBaseComponent_HandleMessage(t *testing.T) {
	mem := testutil.NewMemoryBus()
	ctx := context.Background()
	b := NewBaseComponent("id", "type", "topic", mem)
	b.RegisterHandler("ping", func(ctx context.Context, msg map[string]interface{}) (map[string]interface{}, error) {
		return map[string]interface{}{"pong": true}, nil
	})
	_ = b.Start(ctx)
	defer func() { _ = b.Stop(ctx) }()

	resp, err := mem.Request(ctx, "topic", map[string]interface{}{
		"action": "ping", "sender": "security_monitor", "payload": map[string]interface{}{},
	}, 2.0)
	if err != nil {
		t.Fatal(err)
	}
	pl, _ := resp["payload"].(map[string]interface{})
	if pl["pong"] != true {
		t.Fatalf("%#v", pl)
	}
}
