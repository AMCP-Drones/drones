package tests

import (
	"context"
	"testing"

	"github.com/AMCP-Drones/drones/components/navigation/src"
	"github.com/AMCP-Drones/drones/tests/testutil"
)

func TestModule_Navigation_GetState_ViaRequest(t *testing.T) {
	mem := testutil.NewMemoryBus()
	ctx := context.Background()
	cfg := testutil.Config("navigation")
	n := navigation.New(cfg, mem)
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = n.Stop(ctx) }()

	resp, err := mem.Request(ctx, cfg.BrokerTopicFor("navigation"), map[string]interface{}{
		"action":  "get_state",
		"sender":  "security_monitor",
		"payload": map[string]interface{}{},
	}, 2.0)
	if err != nil {
		t.Fatal(err)
	}
	pl, _ := resp["payload"].(map[string]interface{})
	if pl == nil {
		t.Fatalf("payload missing: %#v", resp)
	}
	if _, ok := pl["lat"]; !ok {
		t.Fatalf("expected lat in nav state: %#v", pl)
	}
}

func TestModule_Navigation_NavState_Update(t *testing.T) {
	mem := testutil.NewMemoryBus()
	ctx := context.Background()
	cfg := testutil.Config("navigation")
	n := navigation.New(cfg, mem)
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = n.Stop(ctx) }()

	_ = mem.Publish(ctx, cfg.BrokerTopicFor("navigation"), map[string]interface{}{
		"action": "nav_state",
		"sender": "security_monitor",
		"payload": map[string]interface{}{
			"lat": 10.0, "lon": 20.0, "alt_m": 30.0,
		},
	})
	resp, err := mem.Request(ctx, cfg.BrokerTopicFor("navigation"), map[string]interface{}{
		"action":  "get_state",
		"sender":  "security_monitor",
		"payload": map[string]interface{}{},
	}, 2.0)
	if err != nil {
		t.Fatal(err)
	}
	pl, _ := resp["payload"].(map[string]interface{})
	if pl["lat"].(float64) != 10.0 {
		t.Fatalf("lat: %#v", pl)
	}
}
