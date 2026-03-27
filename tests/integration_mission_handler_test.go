package tests

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/AMCP-Drones/drones/src/bus"
	"github.com/AMCP-Drones/drones/src/journal"
	missionhandler "github.com/AMCP-Drones/drones/src/mission_handler"
	securitymonitor "github.com/AMCP-Drones/drones/src/security_monitor"
	"github.com/AMCP-Drones/drones/tests/testutil"
)

// Integration: mission_handler loads a JSON mission through the security monitor into stub autopilot + limiter, with journal logging.
func TestIntegration_MissionHandler_LoadMission(t *testing.T) {
	prefix := testutil.TopicPrefix()
	journalTopic := prefix + ".journal"
	apTopic := prefix + ".autopilot"
	limiterTopic := prefix + ".limiter"
	mhTopic := prefix + ".mission_handler"

	policies := []map[string]string{
		{"sender": "mission_handler", "topic": journalTopic, "action": "LOG_EVENT"},
		{"sender": "mission_handler", "topic": apTopic, "action": "mission_load"},
		{"sender": "mission_handler", "topic": limiterTopic, "action": "mission_load"},
	}
	raw, _ := json.Marshal(policies)
	t.Setenv("SECURITY_POLICIES", string(raw))
	t.Cleanup(func() { _ = os.Unsetenv("SECURITY_POLICIES") })
	t.Setenv("JOURNAL_FILE_PATH", t.TempDir()+"/j.ndjson")
	t.Cleanup(func() { _ = os.Unsetenv("JOURNAL_FILE_PATH") })
	t.Setenv("MISSION_HANDLER_REQUEST_TIMEOUT_S", "5")
	t.Cleanup(func() { _ = os.Unsetenv("MISSION_HANDLER_REQUEST_TIMEOUT_S") })

	mem := testutil.NewMemoryBus()
	ctx := context.Background()

	_ = mem.Subscribe(ctx, apTopic, func(msg map[string]interface{}) {
		if msg["action"] != "mission_load" {
			return
		}
		if _, ok := msg["reply_to"].(string); ok {
			_ = bus.Respond(ctx, mem, msg, map[string]interface{}{"ok": true}, "autopilot", true, "")
		}
	})
	limiterGot := false
	_ = mem.Subscribe(ctx, limiterTopic, func(msg map[string]interface{}) {
		if msg["action"] == "mission_load" {
			limiterGot = true
		}
	})

	j := journal.New(testutil.Config("journal"), mem)
	if err := j.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = j.Stop(ctx) }()

	sm := securitymonitor.New(testutil.Config("security_monitor"), mem)
	if err := sm.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sm.Stop(ctx) }()

	mh := missionhandler.New(testutil.Config("mission_handler"), mem)
	if err := mh.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = mh.Stop(ctx) }()

	mission := map[string]interface{}{
		"mission_id": "m-int-1",
		"steps": []interface{}{
			map[string]interface{}{"lat": 1.0, "lon": 2.0, "alt_m": 10.0},
		},
	}
	resp, err := mem.Request(ctx, mhTopic, map[string]interface{}{
		"action": "LOAD_MISSION",
		"sender": "security_monitor",
		"payload": map[string]interface{}{
			"mission": mission,
		},
	}, 5.0)
	if err != nil {
		t.Fatal(err)
	}
	outer, _ := resp["payload"].(map[string]interface{})
	if outer == nil || outer["ok"] != true {
		t.Fatalf("LOAD_MISSION failed: %#v", resp)
	}
	if !limiterGot {
		t.Fatal("limiter did not receive mission_load")
	}
}
