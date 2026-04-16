package tests

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/AMCP-Drones/drones/systems/deliverydron/cargo/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/motors/src"
	securitymonitor "github.com/AMCP-Drones/drones/systems/deliverydron/security_monitor/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/telemetry/src"
	"github.com/AMCP-Drones/drones/tests/testutil"
)

// Integration: telemetry polls motors and cargo state through the security monitor.
func TestIntegration_Telemetry_AggregatesMotorsAndCargo(t *testing.T) {
	prefix := testutil.TopicPrefix()
	motorsTopic := prefix + ".motors"
	cargoTopic := prefix + ".cargo"
	telTopic := prefix + ".telemetry"

	policies := []map[string]string{
		{"sender": "telemetry", "topic": motorsTopic, "action": "get_state"},
		{"sender": "telemetry", "topic": cargoTopic, "action": "get_state"},
	}
	raw, _ := json.Marshal(policies)
	t.Setenv("SECURITY_POLICIES", string(raw))
	t.Cleanup(func() { _ = os.Unsetenv("SECURITY_POLICIES") })
	t.Setenv("TELEMETRY_POLL_INTERVAL_S", "0.05")
	t.Setenv("TELEMETRY_REQUEST_TIMEOUT_S", "2")
	t.Setenv("SITL_MODE", "mock")
	t.Cleanup(func() {
		_ = os.Unsetenv("TELEMETRY_POLL_INTERVAL_S")
		_ = os.Unsetenv("TELEMETRY_REQUEST_TIMEOUT_S")
		_ = os.Unsetenv("SITL_MODE")
	})

	mem := testutil.NewMemoryBus()
	ctx := context.Background()

	m := motors.New(testutil.Config("motors"), mem)
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = m.Stop(ctx) }()

	cg := cargo.New(testutil.Config("cargo"), mem)
	if err := cg.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cg.Stop(ctx) }()

	sm := securitymonitor.New(testutil.Config("security_monitor"), mem)
	if err := sm.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sm.Stop(ctx) }()

	tel := telemetry.New(testutil.Config("telemetry"), mem)
	if err := tel.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tel.Stop(ctx) }()

	// Poll for telemetry data instead of blind sleep
	var resp map[string]interface{}
	var err error
	timeout := time.After(3 * time.Second)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		resp, err = mem.Request(ctx, telTopic, map[string]interface{}{
			"action":  "get_state",
			"sender":  "security_monitor",
			"payload": map[string]interface{}{},
		}, 2.0)
		if err == nil {
			pl, _ := resp["payload"].(map[string]interface{})
			if pl != nil {
				mot, _ := pl["motors"].(map[string]interface{})
				car, _ := pl["cargo"].(map[string]interface{})
				if mot != nil && car != nil {
					break
				}
			}
		}
		select {
		case <-timeout:
			t.Fatalf("timeout waiting for telemetry data; last response: %#v, err: %v", resp, err)
		case <-ticker.C:
			continue
		}
	}

	pl, _ := resp["payload"].(map[string]interface{})
	if pl == nil {
		t.Fatalf("missing payload: %#v", resp)
	}
	mot, _ := pl["motors"].(map[string]interface{})
	car, _ := pl["cargo"].(map[string]interface{})
	if mot == nil || car == nil {
		t.Fatalf("expected motors and cargo snapshots: %#v", pl)
	}
}
