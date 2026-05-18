package tests

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/AMCP-Drones/drones/systems/deliverydron/autopilot/src"
	emergency "github.com/AMCP-Drones/drones/systems/deliverydron/emergency/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/limiter/src"
	securitymonitor "github.com/AMCP-Drones/drones/systems/deliverydron/security_monitor/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/tests/testutil"
)

func droneportPolicies(prefix, dpTopic, orchTopic string) string {
	policies := []map[string]string{
		{"sender": "autopilot", "topic": prefix + ".emergency", "action": "droneport_takeoff"},
		{"sender": "autopilot", "topic": prefix + ".emergency", "action": "droneport_land"},
		{"sender": "autopilot", "topic": prefix + ".emergency", "action": "get_state"},
		{"sender": "autopilot", "topic": prefix + ".limiter", "action": "get_state"},
		{"sender": "autopilot", "topic": prefix + ".limiter", "action": "orvd_takeoff"},
		{"sender": "emergency", "topic": dpTopic, "action": "request_takeoff"},
		{"sender": "emergency", "topic": dpTopic, "action": "request_landing"},
		{"sender": "emergency", "topic": orchTopic, "action": "get_available_drones"},
		{"sender": "emergency", "topic": prefix + ".journal", "action": "LOG_EVENT"},
	}
	raw, _ := json.Marshal(policies)
	return string(raw)
}

func subscribeDroneportMock(ctx context.Context, mem *testutil.MemoryBus, dpTopic, orchTopic string, mock *droneportMock) {
	reply := func(msg map[string]interface{}) map[string]interface{} {
		body := mock.handle("", msg)
		return map[string]interface{}{
			"action":         "response",
			"sender":         "droneport",
			"success":        body["error"] == nil,
			"correlation_id": msg["correlation_id"],
			"payload":        body,
		}
	}
	_ = mem.Subscribe(ctx, dpTopic, func(msg map[string]interface{}) {
		replyTo, _ := msg["reply_to"].(string)
		if replyTo == "" {
			return
		}
		_ = mem.Publish(ctx, replyTo, reply(msg))
	})
	_ = mem.Subscribe(ctx, orchTopic, func(msg map[string]interface{}) {
		replyTo, _ := msg["reply_to"].(string)
		if replyTo == "" {
			return
		}
		_ = mem.Publish(ctx, replyTo, reply(msg))
	})
}

func TestModule_Droneport_EmergencyTakeoffMock(t *testing.T) {
	ctx := context.Background()
	mem := testutil.NewMemoryBus()
	prefix := testutil.TopicPrefix()
	dpTopic := "test.droneport.mock"
	orchTopic := "test.droneport.orch"
	t.Setenv("SECURITY_POLICIES", droneportPolicies(prefix, dpTopic, orchTopic))
	t.Setenv("DRONEPORT_TOPIC", dpTopic)
	t.Setenv("DRONEPORT_ORCHESTRATOR_TOPIC", orchTopic)
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

func TestModule_Droneport_FullPreflightLifecycle(t *testing.T) {
	ctx := context.Background()
	mem := testutil.NewMemoryBus()
	prefix := testutil.TopicPrefix()
	dpTopic := "test.droneport.full"
	orchTopic := "test.droneport.orch.full"
	mock := newDroneportMock()
	subscribeDroneportMock(ctx, mem, dpTopic, orchTopic, mock)

	t.Setenv("SECURITY_POLICIES", droneportPolicies(prefix, dpTopic, orchTopic))
	t.Setenv("DRONEPORT_TOPIC", dpTopic)
	t.Setenv("DRONEPORT_ORCHESTRATOR_TOPIC", orchTopic)
	t.Setenv("DRONEPORT_DRONE_ID", "drone_001")
	t.Setenv("EMERGENCY_DRONEPORT_MOCK_SUCCESS", "0")
	t.Setenv("LIMITER_ORVD_MOCK_SUCCESS", "1")
	t.Setenv("JOURNAL_FILE_PATH", t.TempDir()+"/dp-full.ndjson")

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

	mid := "m-dp-full"
	_, _ = mem.Request(ctx, prefix+".limiter", map[string]interface{}{
		"action": "mission_load", "sender": "security_monitor",
		"payload": map[string]interface{}{"mission": sampleMission(mid)},
	}, 2.0)
	_, _ = mem.Request(ctx, prefix+".autopilot", map[string]interface{}{
		"action": "mission_load", "sender": "security_monitor",
		"payload": map[string]interface{}{"mission": sampleMission(mid)},
	}, 2.0)

	_, _ = mem.Request(ctx, prefix+".autopilot", map[string]interface{}{
		"action": "cmd", "sender": "security_monitor",
		"payload": map[string]interface{}{"command": "START"},
	}, 2.0)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		st, err := mem.Request(ctx, prefix+".autopilot", map[string]interface{}{
			"action": "get_state", "sender": "security_monitor", "payload": map[string]interface{}{},
		}, 2.0)
		if err != nil {
			t.Fatal(err)
		}
		stPl, _ := st["payload"].(map[string]interface{})
		if stPl["state"] == autopilot.StateExecuting {
			break
		}
		if stPl["state"] == autopilot.StateAborted {
			t.Fatalf("aborted: %#v", stPl)
		}
		time.Sleep(50 * time.Millisecond)
	}
	st, err := mem.Request(ctx, prefix+".autopilot", map[string]interface{}{
		"action": "get_state", "sender": "security_monitor", "payload": map[string]interface{}{},
	}, 2.0)
	if err != nil {
		t.Fatal(err)
	}
	stPl, _ := st["payload"].(map[string]interface{})
	if stPl["state"] != autopilot.StateExecuting {
		t.Fatalf("expected EXECUTING, got %#v", stPl)
	}
}

func TestModule_Droneport_ChargingPendingThenSuccess(t *testing.T) {
	ctx := context.Background()
	mem := testutil.NewMemoryBus()
	prefix := testutil.TopicPrefix()
	dpTopic := "test.droneport.pending"
	orchTopic := "test.droneport.orch.pending"
	mock := newDroneportMock()
	mock.chargeRate = 5.0
	subscribeDroneportMock(ctx, mem, dpTopic, orchTopic, mock)

	t.Setenv("SECURITY_POLICIES", droneportPolicies(prefix, dpTopic, orchTopic))
	t.Setenv("DRONEPORT_TOPIC", dpTopic)
	t.Setenv("DRONEPORT_ORCHESTRATOR_TOPIC", orchTopic)
	t.Setenv("DRONEPORT_DRONE_ID", "drone_001")
	t.Setenv("EMERGENCY_DRONEPORT_MOCK_SUCCESS", "0")
	t.Setenv("JOURNAL_FILE_PATH", t.TempDir()+"/dp-pend.ndjson")

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

	resp1, err := mem.Request(ctx, prefix+".emergency", map[string]interface{}{
		"action": "droneport_takeoff", "sender": "security_monitor",
		"payload": map[string]interface{}{"mission_id": "m-pend"},
	}, 2.0)
	if err != nil {
		t.Fatal(err)
	}
	pl1, _ := resp1["payload"].(map[string]interface{})
	if pl1["pending"] != true {
		t.Fatalf("expected pending on first call, got %#v", pl1)
	}

	deadline := time.Now().Add(5 * time.Second)
	var pl2 map[string]interface{}
	for time.Now().Before(deadline) {
		resp2, err := mem.Request(ctx, prefix+".emergency", map[string]interface{}{
			"action": "droneport_takeoff", "sender": "security_monitor",
			"payload": map[string]interface{}{"mission_id": "m-pend"},
		}, 2.0)
		if err != nil {
			t.Fatal(err)
		}
		pl2, _ = resp2["payload"].(map[string]interface{})
		if pl2["ok"] == true {
			break
		}
		time.Sleep(80 * time.Millisecond)
	}
	if pl2["ok"] != true {
		t.Fatalf("expected ok after charge, got %#v", pl2)
	}
}

func TestModule_Droneport_AutopilotPreflightDenied(t *testing.T) {
	dpTopic := "test.droneport.deny"
	orchTopic := "test.droneport.orch.deny"
	prefix := testutil.TopicPrefix()
	ctx := context.Background()
	mem := testutil.NewMemoryBus()
	mock := newDroneportMock()
	mock.denyLanding = true
	subscribeDroneportMock(ctx, mem, dpTopic, orchTopic, mock)

	policies := orvdPolicies(prefix, "")
	pol := []map[string]string{}
	_ = json.Unmarshal([]byte(policies), &pol)
	pol = append(pol,
		map[string]string{"sender": "autopilot", "topic": prefix + ".emergency", "action": "droneport_takeoff"},
		map[string]string{"sender": "autopilot", "topic": prefix + ".emergency", "action": "get_state"},
		map[string]string{"sender": "emergency", "topic": dpTopic, "action": "request_takeoff"},
		map[string]string{"sender": "emergency", "topic": dpTopic, "action": "request_landing"},
		map[string]string{"sender": "emergency", "topic": orchTopic, "action": "get_available_drones"},
		map[string]string{"sender": "emergency", "topic": prefix + ".journal", "action": "LOG_EVENT"},
	)
	raw, _ := json.Marshal(pol)
	t.Setenv("SECURITY_POLICIES", string(raw))
	t.Setenv("DRONEPORT_TOPIC", dpTopic)
	t.Setenv("DRONEPORT_ORCHESTRATOR_TOPIC", orchTopic)
	t.Setenv("EMERGENCY_DRONEPORT_MOCK_SUCCESS", "0")
	t.Setenv("LIMITER_ORVD_MOCK_SUCCESS", "1")
	t.Setenv("JOURNAL_FILE_PATH", t.TempDir()+"/dp2.ndjson")

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
	if stPl["last_error"] != "droneport_denied" && stPl["last_error"] != "No free ports" {
		t.Fatalf("last_error=%#v", stPl["last_error"])
	}
}

func TestModule_Droneport_PostMissionLand(t *testing.T) {
	ctx := context.Background()
	mem := testutil.NewMemoryBus()
	prefix := testutil.TopicPrefix()
	dpTopic := "test.droneport.land"
	orchTopic := "test.droneport.orch.land"
	mock := newDroneportMock()
	subscribeDroneportMock(ctx, mem, dpTopic, orchTopic, mock)

	t.Setenv("SECURITY_POLICIES", droneportPolicies(prefix, dpTopic, orchTopic))
	t.Setenv("DRONEPORT_TOPIC", dpTopic)
	t.Setenv("DRONEPORT_ORCHESTRATOR_TOPIC", orchTopic)
	t.Setenv("DRONEPORT_DRONE_ID", "drone_001")
	t.Setenv("EMERGENCY_DRONEPORT_MOCK_SUCCESS", "0")
	t.Setenv("JOURNAL_FILE_PATH", t.TempDir()+"/dp-land.ndjson")

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
		"action": "droneport_land",
		"sender": "security_monitor",
		"payload": map[string]interface{}{
			"mission_id":  "m-land",
			"battery_pct": 40.0,
		},
	}, 2.0)
	if err != nil {
		t.Fatal(err)
	}
	pl, _ := resp["payload"].(map[string]interface{})
	if pl["ok"] != true {
		t.Fatalf("expected ok, got %#v", pl)
	}
	mock.mu.Lock()
	d := mock.drones["drone_001"]
	mock.mu.Unlock()
	if d == nil || d.portID == "" {
		t.Fatalf("expected drone registered at port after land")
	}
}
