package tests

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/AMCP-Drones/drones/systems/deliverydron/autopilot/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/limiter/src"
	securitymonitor "github.com/AMCP-Drones/drones/systems/deliverydron/security_monitor/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/tests/testutil"
)

func coverageStackPolicies(prefix, orvdTopic string) string {
	pol := []map[string]string{}
	_ = json.Unmarshal([]byte(orvdPolicies(prefix, orvdTopic)), &pol)
	extra := []map[string]string{
		{"sender": "autopilot", "topic": prefix + ".navigation", "action": "get_state"},
		{"sender": "autopilot", "topic": prefix + ".motors", "action": "SET_TARGET"},
		{"sender": "autopilot", "topic": prefix + ".cargo", "action": "CLOSE"},
		{"sender": "autopilot", "topic": prefix + ".emergency", "action": "droneport_land"},
		{"sender": "autopilot", "topic": prefix + ".limiter", "action": "orvd_complete"},
		{"sender": "limiter", "topic": orvdTopic, "action": "complete_mission"},
	}
	pol = append(pol, extra...)
	raw, _ := json.Marshal(pol)
	return string(raw)
}

func TestCoverage_AutopilotExecutesMissionToCompleted(t *testing.T) {
	ctx := context.Background()
	orvdTopic := "test.orvd.exec"
	mock := newOpbdMock()
	nav := map[string]interface{}{"lat": 55.75, "lon": 37.61, "alt_m": 100.0, "heading_deg": 0.0}
	mem, _, lim, _, _ := startORVDStackWithNav(t, orvdTopic, false, mock.handle, nav)
	prefix := testutil.TopicPrefix()

	t.Setenv("AUTOPILOT_CONTROL_INTERVAL_S", "0.05")
	t.Setenv("AUTOPILOT_NAV_POLL_INTERVAL_S", "0.05")
	t.Setenv("AUTOPILOT_PREFLIGHT_TIMEOUT_S", "10")

	_ = mem.Subscribe(ctx, prefix+".motors", func(map[string]interface{}) {})
	_ = mem.Subscribe(ctx, prefix+".cargo", func(map[string]interface{}) {})

	_, err := mem.Request(ctx, prefix+".mission_handler", map[string]interface{}{
		"action": "LOAD_MISSION",
		"sender": "security_monitor",
		"payload": map[string]interface{}{
			"mission": sampleMission("m-exec"),
		},
	}, 5.0)
	if err != nil {
		t.Fatal(err)
	}

	_, err = mem.Request(ctx, prefix+".autopilot", map[string]interface{}{
		"action": "cmd", "sender": "security_monitor",
		"payload": map[string]interface{}{"command": "START"},
	}, 5.0)
	if err != nil {
		t.Fatal(err)
	}

	gotCompleted := false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		st, err := mem.Request(ctx, prefix+".autopilot", map[string]interface{}{
			"action": "get_state", "sender": "security_monitor", "payload": map[string]interface{}{},
		}, 2.0)
		if err != nil {
			t.Fatal(err)
		}
		stPl, _ := st["payload"].(map[string]interface{})
		if stPl["state"] == autopilot.StateCompleted {
			gotCompleted = true
			break
		}
		if stPl["state"] == autopilot.StateAborted {
			t.Fatalf("aborted: %#v", stPl)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !gotCompleted {
		t.Fatal("autopilot did not reach COMPLETED")
	}
	_ = lim
}

func TestCoverage_LimiterORVDCompleteHandler(t *testing.T) {
	ctx := context.Background()
	mem := testutil.NewMemoryBus()
	prefix := testutil.TopicPrefix()
	orvdTopic := "test.orvd.complete.h"

	t.Setenv("SECURITY_POLICIES", orvdPolicies(prefix, orvdTopic))
	t.Setenv("ORVD_TOPIC", orvdTopic)
	t.Setenv("LIMITER_ORVD_MOCK_SUCCESS", "0")
	t.Setenv("JOURNAL_FILE_PATH", t.TempDir()+"/oc.ndjson")

	mock := newOpbdMock()
	_ = mem.Subscribe(ctx, orvdTopic, func(msg map[string]interface{}) {
		replyTo, _ := msg["reply_to"].(string)
		cid, _ := msg["correlation_id"].(string)
		_ = mem.Publish(ctx, replyTo, map[string]interface{}{
			"action": "response", "sender": "orvd", "success": true, "correlation_id": cid,
			"payload": mock.handle(msg),
		})
	})

	sm := securitymonitor.New(testutil.Config("security_monitor"), mem)
	_ = sm.Start(ctx)
	defer func() { _ = sm.Stop(ctx) }()

	lim := limiter.New(testutil.Config("limiter"), mem)
	_ = lim.Start(ctx)
	defer func() { _ = lim.Stop(ctx) }()

	_, _ = mem.Request(ctx, prefix+".limiter", map[string]interface{}{
		"action": "mission_load", "sender": "security_monitor",
		"payload": map[string]interface{}{"mission": sampleMission("m-oc")},
	}, 2.0)

	resp, err := mem.Request(ctx, prefix+".limiter", map[string]interface{}{
		"action": "orvd_complete", "sender": "security_monitor",
		"payload": map[string]interface{}{"mission_id": "m-oc", "result": "success"},
	}, 2.0)
	if err != nil {
		t.Fatal(err)
	}
	pl, _ := resp["payload"].(map[string]interface{})
	if pl["ok"] != true {
		t.Fatalf("%#v", pl)
	}
}

func TestCoverage_AutopilotCommands(t *testing.T) {
	ctx := context.Background()
	mem := testutil.NewMemoryBus()
	prefix := testutil.TopicPrefix()
	t.Setenv("SECURITY_POLICIES", orvdPolicies(prefix, ""))
	t.Setenv("LIMITER_ORVD_MOCK_SUCCESS", "1")
	t.Setenv("JOURNAL_FILE_PATH", t.TempDir()+"/cmd.ndjson")

	_, _, _, _, ap := startORVDStack(t, "", false, nil)
	_ = ap

	mission := sampleMission("m-cmd")
	_, _ = mem.Request(ctx, prefix+".autopilot", map[string]interface{}{
		"action": "mission_load", "sender": "security_monitor",
		"payload": map[string]interface{}{"mission": mission},
	}, 2.0)

	for _, cmd := range []string{"PAUSE", "RESUME", "KOVER", "ABORT", "RESET"} {
		_, _ = mem.Request(ctx, prefix+".autopilot", map[string]interface{}{
			"action": "cmd", "sender": "security_monitor",
			"payload": map[string]interface{}{"command": cmd},
		}, 2.0)
	}
}
