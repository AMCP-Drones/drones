package tests

import (
	"context"
	"encoding/json"
	"testing"

	missionhandler "github.com/AMCP-Drones/drones/systems/deliverydron/mission_handler/src"
	securitymonitor "github.com/AMCP-Drones/drones/systems/deliverydron/security_monitor/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/tests/testutil"
)

func TestModule_MissionHandler_ValidateOnlyAndGetState(t *testing.T) {
	ctx := context.Background()
	mem := testutil.NewMemoryBus()
	cfg := testutil.Config("mission_handler")
	mh := missionhandler.New(cfg, mem)
	if err := mh.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mh.Stop(ctx) })

	// Negative: untrusted sender gets no response.
	_, err := mem.Request(ctx, cfg.BrokerTopicFor("mission_handler"), map[string]interface{}{
		"action":  "VALIDATE_ONLY",
		"sender":  "intruder",
		"payload": map[string]interface{}{"mission": map[string]interface{}{"mission_id": "x"}},
	}, 0.1)
	if err == nil {
		t.Fatal("expected timeout for untrusted sender")
	}

	invalidResp, err := mem.Request(ctx, cfg.BrokerTopicFor("mission_handler"), map[string]interface{}{
		"action":  "VALIDATE_ONLY",
		"sender":  "security_monitor",
		"payload": map[string]interface{}{"mission": map[string]interface{}{"mission_id": ""}},
	}, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	invalidPayload, _ := invalidResp["payload"].(map[string]interface{})
	if invalidPayload["ok"] != false {
		t.Fatalf("expected validation error, got %#v", invalidPayload)
	}

	validResp, err := mem.Request(ctx, cfg.BrokerTopicFor("mission_handler"), map[string]interface{}{
		"action": "VALIDATE_ONLY",
		"sender": "security_monitor",
		"payload": map[string]interface{}{
			"mission": map[string]interface{}{
				"mission_id": "m-valid",
				"steps": []interface{}{
					map[string]interface{}{"lat": 1.0, "lon": 2.0, "alt_m": 3.0},
				},
			},
		},
	}, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	validPayload, _ := validResp["payload"].(map[string]interface{})
	if validPayload["ok"] != true {
		t.Fatalf("expected valid mission, got %#v", validPayload)
	}

	stateResp, err := mem.Request(ctx, cfg.BrokerTopicFor("mission_handler"), map[string]interface{}{
		"action": "get_state", "sender": "security_monitor",
	}, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	statePayload, _ := stateResp["payload"].(map[string]interface{})
	if statePayload["last_error"] != "" {
		t.Fatalf("expected cleared last_error after valid validation, got %#v", statePayload)
	}
}

func TestModule_MissionHandler_LoadMission_AutopilotError(t *testing.T) {
	ctx := context.Background()
	mem := testutil.NewMemoryBus()
	prefix := testutil.TopicPrefix()

	// Policies required for mission_handler -> autopilot and journal.
	policies := []map[string]string{
		{"sender": "mission_handler", "topic": prefix + ".autopilot", "action": "mission_load"},
		{"sender": "mission_handler", "topic": prefix + ".journal", "action": "LOG_EVENT"},
		{"sender": "mission_handler", "topic": prefix + ".limiter", "action": "mission_load"},
	}
	raw, _ := json.Marshal(policies)
	t.Setenv("SECURITY_POLICIES", string(raw))
	sm := securitymonitor.New(testutil.Config("security_monitor"), mem)
	if err := sm.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sm.Stop(ctx) })

	// Autopilot explicitly returns error.
	_ = mem.Subscribe(ctx, prefix+".autopilot", func(msg map[string]interface{}) {
		replyTo, _ := msg["reply_to"].(string)
		cid, _ := msg["correlation_id"].(string)
		_ = mem.Publish(ctx, replyTo, map[string]interface{}{
			"action":         "response",
			"sender":         "autopilot",
			"success":        true,
			"correlation_id": cid,
			"payload": map[string]interface{}{
				"ok":    false,
				"error": "mission_rejected",
			},
		})
	})
	_ = mem.Subscribe(ctx, prefix+".limiter", func(map[string]interface{}) {})
	_ = mem.Subscribe(ctx, prefix+".journal", func(map[string]interface{}) {})

	mh := missionhandler.New(testutil.Config("mission_handler"), mem)
	if err := mh.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mh.Stop(ctx) })

	resp, err := mem.Request(ctx, prefix+".mission_handler", map[string]interface{}{
		"action": "LOAD_MISSION",
		"sender": "security_monitor",
		"payload": map[string]interface{}{
			"mission": map[string]interface{}{
				"mission_id": "m-fail",
				"steps": []interface{}{
					map[string]interface{}{"lat": 1.0, "lon": 2.0, "alt_m": 3.0},
				},
			},
		},
	}, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	pl, _ := resp["payload"].(map[string]interface{})
	if pl["ok"] != false || pl["error"] != "mission_rejected" {
		t.Fatalf("expected autopilot rejection, got %#v", pl)
	}
}
