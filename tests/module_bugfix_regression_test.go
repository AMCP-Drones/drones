package tests

import (
	"context"
	"testing"
	"time"

	cargo "github.com/AMCP-Drones/drones/systems/deliverydron/cargo/src"
	motors "github.com/AMCP-Drones/drones/systems/deliverydron/motors/src"
	navigation "github.com/AMCP-Drones/drones/systems/deliverydron/navigation/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/tests/testutil"
)

func TestRegression_BAG001_NavigationGetStateNotEmpty(t *testing.T) {
	ctx := context.Background()
	mem := testutil.NewMemoryBus()
	cfg := testutil.Config("navigation")
	nav := navigation.New(cfg, mem)
	if err := nav.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = nav.Stop(ctx) }()

	_ = mem.Publish(ctx, cfg.BrokerTopicFor("navigation"), map[string]interface{}{
		"action": "nav_state",
		"sender": "security_monitor",
		"payload": map[string]interface{}{
			"lat": 55.7558, "lon": 37.6173, "alt_m": 120.0,
		},
	})
	resp, err := mem.Request(ctx, cfg.BrokerTopicFor("navigation"), map[string]interface{}{
		"action": "get_state", "sender": "security_monitor", "payload": map[string]interface{}{},
	}, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	pl, _ := resp["payload"].(map[string]interface{})
	if pl == nil || len(pl) == 0 {
		t.Fatalf("navigation payload is empty: %#v", resp)
	}
	if pl["lat"] != 55.7558 || pl["lon"] != 37.6173 {
		t.Fatalf("unexpected coordinates: %#v", pl)
	}
}

func TestRegression_BAG002_CargoGetStateNotEmpty(t *testing.T) {
	ctx := context.Background()
	mem := testutil.NewMemoryBus()
	cfg := testutil.Config("cargo")
	c := cargo.New(cfg, mem)
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Stop(ctx) }()

	resp, err := mem.Request(ctx, cfg.BrokerTopicFor("cargo"), map[string]interface{}{
		"action": "get_state", "sender": "security_monitor", "payload": map[string]interface{}{},
	}, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	pl, _ := resp["payload"].(map[string]interface{})
	if pl == nil || pl["state"] == nil {
		t.Fatalf("cargo payload/state is empty: %#v", resp)
	}
}

func TestRegression_BAG003_MotorsSurvivesInvalidBurst(t *testing.T) {
	ctx := context.Background()
	mem := testutil.NewMemoryBus()
	cfg := testutil.Config("motors")
	t.Setenv("SITL_COMMANDS_TOPIC", "sitl.commands")
	m := motors.New(cfg, mem)
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = m.Stop(ctx) }()

	sitlCount := 0
	_ = mem.Subscribe(ctx, "sitl.commands", func(map[string]interface{}) { sitlCount++ })

	for i := 0; i < 80; i++ {
		_ = mem.Publish(ctx, cfg.BrokerTopicFor("motors"), map[string]interface{}{
			"action": "SET_TARGET",
			"sender": "security_monitor",
			"payload": map[string]interface{}{
				"vx": "invalid", "vy": nil, "vz": []int{1, 2},
			},
		})
	}
	time.Sleep(100 * time.Millisecond)
	if sitlCount != 0 {
		t.Fatalf("invalid commands must not be emitted to SITL, got %d", sitlCount)
	}

	pingResp, err := mem.Request(ctx, cfg.BrokerTopicFor("motors"), map[string]interface{}{
		"action": "ping", "sender": "security_monitor", "payload": map[string]interface{}{},
	}, 1.0)
	if err != nil {
		t.Fatalf("motors stopped responding after invalid burst: %v", err)
	}
	pingPl, _ := pingResp["payload"].(map[string]interface{})
	if pingPl["pong"] != true {
		t.Fatalf("unexpected ping response: %#v", pingResp)
	}
}
