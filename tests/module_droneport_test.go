package tests

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/AMCP-Drones/drones/systems/deliverydron/autopilot/src"
	emergency "github.com/AMCP-Drones/drones/systems/deliverydron/emergency/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/limiter/src"
	securitymonitor "github.com/AMCP-Drones/drones/systems/deliverydron/security_monitor/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/tests/testutil"
)

func droneportPolicies(prefix, dpTopic string) string {
	policies := []map[string]string{
		{"sender": "autopilot", "topic": prefix + ".emergency", "action": "droneport_takeoff"},
		{"sender": "autopilot", "topic": prefix + ".limiter", "action": "get_state"},
		{"sender": "emergency", "topic": dpTopic, "action": "request_takeoff"},
		{"sender": "emergency", "topic": prefix + ".journal", "action": "LOG_EVENT"},
	}
	raw, _ := json.Marshal(policies)
	return string(raw)
}

func TestModule_Droneport_EmergencyTakeoffMock(t *testing.T) {
	ctx := context.Background()
	mem := testutil.NewMemoryBus()
	prefix := testutil.TopicPrefix()
	dpTopic := "test.droneport.mock"
	t.Setenv("SECURITY_POLICIES", droneportPolicies(prefix, dpTopic))
	t.Setenv("DRONEPORT_TOPIC", dpTopic)
	t.Setenv("EMERGENCY_DRONEPORT_MOCK_SUCCESS", "1")
	t.Setenv("JOURNAL_FILE_PATH", t.TempDir()+"/dp.ndjson")

	sm := securitymonitor.New(testutil.Config("security_monitor"), mem)
	if err := sm.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sm.Stop(ctx) })

	em := emergency.New(testutil.Config("emergency"), mem)
	if err := em.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = em.Stop(ctx) })

	resp, err := mem.Request(ctx, prefix+".emergency", map[string]interface{}{
		"action": "droneport_takeoff",
		"sender": "security_monitor",
		"payload": map[string]interface{}{
			"mission_id": "m-dp",
		},
	}, 2.0)
	if err != nil {
		t.Fatal(err)
	}
	pl, _ := resp["payload"].(map[string]interface{})
	if pl["ok"] != true {
		t.Fatalf("expected ok, got %#v", pl)
	}
}

func TestModule_Droneport_AutopilotPreflightDenied(t *testing.T) {
	dpTopic := "test.droneport.deny"
	prefix := testutil.TopicPrefix()
	ctx := context.Background()
	mem := testutil.NewMemoryBus()

	policies := orvdPolicies(prefix, "")
	pol := []map[string]string{}
	_ = json.Unmarshal([]byte(policies), &pol)
	pol = append(pol,
		map[string]string{"sender": "autopilot", "topic": prefix + ".emergency", "action": "droneport_takeoff"},
		map[string]string{"sender": "emergency", "topic": dpTopic, "action": "request_takeoff"},
		map[string]string{"sender": "emergency", "topic": prefix + ".journal", "action": "LOG_EVENT"},
	)
	raw, _ := json.Marshal(pol)
	t.Setenv("SECURITY_POLICIES", string(raw))
	t.Setenv("DRONEPORT_TOPIC", dpTopic)
	t.Setenv("EMERGENCY_DRONEPORT_MOCK_SUCCESS", "0")
	t.Setenv("JOURNAL_FILE_PATH", t.TempDir()+"/dp2.ndjson")

	_ = mem.Subscribe(ctx, dpTopic, func(msg map[string]interface{}) {
		replyTo, _ := msg["reply_to"].(string)
		cid, _ := msg["correlation_id"].(string)
		_ = mem.Publish(ctx, replyTo, map[string]interface{}{
			"action": "response", "sender": "droneport", "success": true, "correlation_id": cid,
			"payload": map[string]interface{}{"error": "port_busy"},
		})
	})
	_ = mem.Subscribe(ctx, prefix+".journal", func(map[string]interface{}) {})

	sm := securitymonitor.New(testutil.Config("security_monitor"), mem)
	if err := sm.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sm.Stop(ctx) })

	lim := limiter.New(testutil.Config("limiter"), mem)
	if err := lim.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lim.Stop(ctx) })

	em := emergency.New(testutil.Config("emergency"), mem)
	if err := em.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = em.Stop(ctx) })

	ap := autopilot.New(testutil.Config("autopilot"), mem)
	if err := ap.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ap.Stop(ctx) })

	_, _ = mem.Request(ctx, prefix+".limiter", map[string]interface{}{
		"action": "mission_load", "sender": "security_monitor",
		"payload": map[string]interface{}{"mission": sampleMission("m-dp-deny")},
	}, 2.0)
	_, _ = mem.Request(ctx, prefix+".autopilot", map[string]interface{}{
		"action": "mission_load", "sender": "security_monitor",
		"payload": map[string]interface{}{"mission": sampleMission("m-dp-deny")},
	}, 2.0)

	resp, err := mem.Request(ctx, prefix+".autopilot", map[string]interface{}{
		"action": "cmd", "sender": "security_monitor",
		"payload": map[string]interface{}{"command": "START"},
	}, 2.0)
	if err != nil {
		t.Fatal(err)
	}
	pl, _ := resp["payload"].(map[string]interface{})
	if pl["state"] != autopilot.StateAborted {
		t.Fatalf("expected ABORTED, got %#v", pl)
	}
	st, err := mem.Request(ctx, prefix+".autopilot", map[string]interface{}{
		"action": "get_state", "sender": "security_monitor", "payload": map[string]interface{}{},
	}, 2.0)
	if err != nil {
		t.Fatal(err)
	}
	stPl, _ := st["payload"].(map[string]interface{})
	if stPl["last_error"] != "droneport_denied" {
		t.Fatalf("last_error=%#v", stPl["last_error"])
	}
}
