package tests

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/AMCP-Drones/drones/systems/deliverydron/autopilot/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/limiter/src"
	emergency "github.com/AMCP-Drones/drones/systems/deliverydron/emergency/src"
	missionhandler "github.com/AMCP-Drones/drones/systems/deliverydron/mission_handler/src"
	securitymonitor "github.com/AMCP-Drones/drones/systems/deliverydron/security_monitor/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/tests/testutil"
)

func orvdPolicies(prefix, orvdTopic string) string {
	policies := []map[string]string{
		{"sender": "mission_handler", "topic": prefix + ".autopilot", "action": "mission_load"},
		{"sender": "mission_handler", "topic": prefix + ".limiter", "action": "mission_load"},
		{"sender": "mission_handler", "topic": prefix + ".journal", "action": "LOG_EVENT"},
		{"sender": "limiter", "topic": prefix + ".navigation", "action": "get_state"},
		{"sender": "limiter", "topic": prefix + ".telemetry", "action": "get_state"},
		{"sender": "limiter", "topic": prefix + ".journal", "action": "LOG_EVENT"},
		{"sender": "limiter", "topic": prefix + ".emergency", "action": "limiter_event"},
		{"sender": "autopilot", "topic": prefix + ".limiter", "action": "get_state"},
		{"sender": "autopilot", "topic": prefix + ".limiter", "action": "orvd_takeoff"},
		{"sender": "autopilot", "topic": prefix + ".limiter", "action": "orvd_complete"},
		{"sender": "autopilot", "topic": prefix + ".emergency", "action": "droneport_takeoff"},
		{"sender": "emergency", "topic": prefix + ".journal", "action": "LOG_EVENT"},
		{"sender": "orvd", "topic": prefix + ".limiter", "action": "update_config"},
		{"sender": "orvd", "topic": prefix + ".limiter", "action": "revoke_takeoff"},
		{"sender": "orvd", "topic": prefix + ".navigation", "action": "get_state"},
		{"sender": "autopilot", "topic": prefix + ".navigation", "action": "get_state"},
		{"sender": "autopilot", "topic": prefix + ".motors", "action": "SET_TARGET"},
		{"sender": "autopilot", "topic": prefix + ".cargo", "action": "CLOSE"},
		{"sender": "autopilot", "topic": prefix + ".journal", "action": "LOG_EVENT"},
	}
	if orvdTopic != "" {
		for _, action := range []string{
			"register_drone", "register_mission", "authorize_mission", "request_takeoff",
			"send_telemetry", "complete_mission", "report_incident", "get_mission_status",
		} {
			policies = append(policies, map[string]string{
				"sender": "limiter", "topic": orvdTopic, "action": action,
			})
		}
	}
	raw, _ := json.Marshal(policies)
	return string(raw)
}

func sampleMission(id string) map[string]interface{} {
	return map[string]interface{}{
		"mission_id": id,
		"steps": []interface{}{
			map[string]interface{}{"lat": 55.75, "lon": 37.61, "alt_m": 100.0},
		},
	}
}

func startORVDStack(t *testing.T, orvdTopic string, stubAutopilot bool, orvdHandler func(map[string]interface{}) map[string]interface{}) (*testutil.MemoryBus, *securitymonitor.SecurityMonitor, *limiter.Limiter, *missionhandler.MissionHandler, *autopilot.Autopilot) {
	return startORVDStackWithNav(t, orvdTopic, stubAutopilot, orvdHandler, nil)
}

func startORVDStackWithNav(t *testing.T, orvdTopic string, stubAutopilot bool, orvdHandler func(map[string]interface{}) map[string]interface{}, navState map[string]interface{}) (*testutil.MemoryBus, *securitymonitor.SecurityMonitor, *limiter.Limiter, *missionhandler.MissionHandler, *autopilot.Autopilot) {
	t.Helper()
	ctx := context.Background()
	mem := testutil.NewMemoryBus()
	prefix := testutil.TopicPrefix()

	t.Setenv("SECURITY_POLICIES", orvdPolicies(prefix, orvdTopic))
	t.Setenv("ORVD_TOPIC", orvdTopic)
	t.Setenv("JOURNAL_FILE_PATH", t.TempDir()+"/orvd_journal.ndjson")
	t.Setenv("LIMITER_ORVD_MOCK_SUCCESS", "0")
	t.Setenv("EMERGENCY_DRONEPORT_MOCK_SUCCESS", "1")
	t.Setenv("LIMITER_CONTROL_INTERVAL_S", "60")
	t.Setenv("LIMITER_NAV_POLL_INTERVAL_S", "60")
	t.Setenv("LIMITER_TELEMETRY_POLL_INTERVAL_S", "60")

	if orvdTopic != "" && orvdHandler != nil {
		_ = mem.Subscribe(ctx, orvdTopic, func(msg map[string]interface{}) {
			replyTo, _ := msg["reply_to"].(string)
			cid, _ := msg["correlation_id"].(string)
			resp := orvdHandler(msg)
			if resp == nil {
				resp = map[string]interface{}{"status": "denied"}
			}
			_ = mem.Publish(ctx, replyTo, map[string]interface{}{
				"action":         "response",
				"sender":         "orvd",
				"success":        true,
				"correlation_id": cid,
				"payload":        resp,
			})
		})
	}

	if stubAutopilot {
		_ = mem.Subscribe(ctx, prefix+".autopilot", func(msg map[string]interface{}) {
			if msg["action"] != "mission_load" && msg["action"] != "get_state" && msg["action"] != "cmd" {
				return
			}
			replyTo, _ := msg["reply_to"].(string)
			if replyTo == "" {
				return
			}
			cid, _ := msg["correlation_id"].(string)
			var payload map[string]interface{}
			switch msg["action"] {
			case "mission_load":
				payload = map[string]interface{}{"ok": true, "state": autopilot.StateMissionLoaded}
			case "get_state":
				payload = map[string]interface{}{"state": autopilot.StateMissionLoaded}
			case "cmd":
				pl, _ := msg["payload"].(map[string]interface{})
				cmd, _ := pl["command"].(string)
				if cmd == "START" {
					payload = map[string]interface{}{"ok": true, "state": autopilot.StateExecuting}
				} else {
					payload = map[string]interface{}{"ok": true, "state": autopilot.StateMissionLoaded}
				}
			default:
				payload = map[string]interface{}{"ok": true}
			}
			_ = mem.Publish(ctx, replyTo, map[string]interface{}{
				"action": "response", "sender": "autopilot", "success": true,
				"correlation_id": cid, "payload": payload,
			})
		})
	}
	_ = mem.Subscribe(ctx, prefix+".journal", func(map[string]interface{}) {})

	if navState != nil {
		_ = mem.Subscribe(ctx, prefix+".navigation", func(msg map[string]interface{}) {
			if msg["action"] != "get_state" {
				return
			}
			replyTo, _ := msg["reply_to"].(string)
			if replyTo == "" {
				return
			}
			cid, _ := msg["correlation_id"].(string)
			_ = mem.Publish(ctx, replyTo, map[string]interface{}{
				"action": "response", "sender": "navigation", "success": true,
				"correlation_id": cid, "payload": navState,
			})
		})
	}

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

	mh := missionhandler.New(testutil.Config("mission_handler"), mem)
	if err := mh.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mh.Stop(ctx) })

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

	return mem, sm, lim, mh, ap
}

func TestModule_ORVD_LimiterBoundsFail(t *testing.T) {
	ctx := context.Background()
	mem := testutil.NewMemoryBus()
	cfg := testutil.Config("limiter")
	t.Setenv("LIMITER_ORVD_MOCK_SUCCESS", "1")
	t.Setenv("LIMITER_MAX_MISSION_ALT_M", "100")

	lim := limiter.New(cfg, mem)
	if err := lim.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lim.Stop(ctx) })

	resp, err := mem.Request(ctx, cfg.BrokerTopicFor("limiter"), map[string]interface{}{
		"action": "mission_load",
		"sender": "security_monitor",
		"payload": map[string]interface{}{
			"mission": map[string]interface{}{
				"mission_id": "m-bounds",
				"steps": []interface{}{
					map[string]interface{}{"lat": 55.0, "lon": 37.0, "alt_m": 500.0},
				},
			},
		},
	}, 2.0)
	if err != nil {
		t.Fatal(err)
	}
	pl, _ := resp["payload"].(map[string]interface{})
	if pl["ok"] == true {
		t.Fatalf("expected bounds failure, got %#v", pl)
	}
	if pl["error"] != "mission_out_of_bounds" {
		t.Fatalf("error=%#v", pl["error"])
	}
}

func TestModule_ORVD_LimiterApproveAndDeny(t *testing.T) {
	orvdTopic := "test.orvd.approve"
	mock := newOpbdMock()
	mock.rejectMission("m-deny")
	mem, _, _, _, _ := startORVDStack(t, orvdTopic, true, mock.handle)

	ctx := context.Background()
	prefix := testutil.TopicPrefix()
	mhTopic := prefix + ".mission_handler"

	load := func(missionID string) map[string]interface{} {
		resp, err := mem.Request(ctx, mhTopic, map[string]interface{}{
			"action": "LOAD_MISSION",
			"sender": "security_monitor",
			"payload": map[string]interface{}{
				"mission": sampleMission(missionID),
			},
		}, 5.0)
		if err != nil {
			t.Fatal(err)
		}
		pl, _ := resp["payload"].(map[string]interface{})
		return pl
	}

	pl := load("m-ok")
	if pl["ok"] != true {
		t.Fatalf("approve load failed: %#v", pl)
	}

	pl = load("m-deny")
	if pl["ok"] == true {
		t.Fatalf("deny load should fail: %#v", pl)
	}
}

func TestModule_ORVD_LimiterMockSuccess(t *testing.T) {
	ctx := context.Background()
	mem := testutil.NewMemoryBus()
	cfg := testutil.Config("limiter")
	t.Setenv("LIMITER_ORVD_MOCK_SUCCESS", "1")

	lim := limiter.New(cfg, mem)
	if err := lim.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lim.Stop(ctx) })

	resp, err := mem.Request(ctx, cfg.BrokerTopicFor("limiter"), map[string]interface{}{
		"action": "mission_load",
		"sender": "security_monitor",
		"payload": map[string]interface{}{
			"mission": sampleMission("m-mock"),
		},
	}, 2.0)
	if err != nil {
		t.Fatal(err)
	}
	pl, _ := resp["payload"].(map[string]interface{})
	if pl["ok"] != true {
		t.Fatalf("mock load failed: %#v", pl)
	}
	st, err := mem.Request(ctx, cfg.BrokerTopicFor("limiter"), map[string]interface{}{
		"action": "get_state", "sender": "security_monitor", "payload": map[string]interface{}{},
	}, 2.0)
	if err != nil {
		t.Fatal(err)
	}
	stPl, _ := st["payload"].(map[string]interface{})
	if stPl["orvd_status"] != limiter.ORVDStatusAuthorized {
		t.Fatalf("orvd_status=%#v", stPl["orvd_status"])
	}
}

func TestModule_ORVD_MissionHandlerSkipsAutopilotOnLimiterDeny(t *testing.T) {
	orvdTopic := "test.orvd.skip"
	prefix := testutil.TopicPrefix()
	ctx := context.Background()

	autopilotLoads := 0
	mem := testutil.NewMemoryBus()
	_ = mem.Subscribe(ctx, prefix+".autopilot", func(msg map[string]interface{}) {
		if msg["action"] == "mission_load" {
			autopilotLoads++
		}
	})

	t.Setenv("SECURITY_POLICIES", orvdPolicies(prefix, orvdTopic))
	t.Setenv("ORVD_TOPIC", orvdTopic)
	t.Setenv("JOURNAL_FILE_PATH", t.TempDir()+"/j.ndjson")
	t.Setenv("LIMITER_ORVD_MOCK_SUCCESS", "0")

	mock := newOpbdMock()
	mock.rejectMission("m-skip")
	_ = mem.Subscribe(ctx, orvdTopic, func(msg map[string]interface{}) {
		replyTo, _ := msg["reply_to"].(string)
		cid, _ := msg["correlation_id"].(string)
		_ = mem.Publish(ctx, replyTo, map[string]interface{}{
			"action": "response", "sender": "orvd", "success": true, "correlation_id": cid,
			"payload": mock.handle(msg),
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

	mh := missionhandler.New(testutil.Config("mission_handler"), mem)
	if err := mh.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mh.Stop(ctx) })

	_, err := mem.Request(ctx, prefix+".mission_handler", map[string]interface{}{
		"action": "LOAD_MISSION",
		"sender": "security_monitor",
		"payload": map[string]interface{}{
			"mission": sampleMission("m-skip"),
		},
	}, 5.0)
	if err != nil {
		t.Fatal(err)
	}
	if autopilotLoads != 0 {
		t.Fatalf("autopilot mission_load count=%d, want 0", autopilotLoads)
	}
}

func TestModule_ORVD_AutopilotStartBlockedWithoutLimiterAuth(t *testing.T) {
	ctx := context.Background()
	mem := testutil.NewMemoryBus()
	prefix := testutil.TopicPrefix()
	t.Setenv("SECURITY_POLICIES", orvdPolicies(prefix, ""))
	t.Setenv("JOURNAL_FILE_PATH", t.TempDir()+"/j.ndjson")

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

	ap := autopilot.New(testutil.Config("autopilot"), mem)
	if err := ap.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ap.Stop(ctx) })

	_, _ = mem.Request(ctx, prefix+".autopilot", map[string]interface{}{
		"action": "mission_load",
		"sender": "security_monitor",
		"payload": map[string]interface{}{
			"mission": sampleMission("m-no-lim"),
		},
	}, 2.0)

	resp, err := mem.Request(ctx, prefix+".autopilot", map[string]interface{}{
		"action": "cmd",
		"sender": "security_monitor",
		"payload": map[string]interface{}{
			"command": "START",
		},
	}, 2.0)
	if err != nil {
		t.Fatal(err)
	}
	pl, _ := resp["payload"].(map[string]interface{})
	if pl["ok"] != true {
		t.Fatalf("START failed: %#v", pl)
	}
	if pl["state"] != autopilot.StateAborted {
		t.Fatalf("expected ABORTED without limiter auth, got state=%#v", pl["state"])
	}
}

func TestModule_ORVD_AutopilotStartAfterLimiterAuth(t *testing.T) {
	orvdTopic := "test.orvd.start"
	prefix := testutil.TopicPrefix()
	ctx := context.Background()
	mock := newOpbdMock()
	nav := map[string]interface{}{"lat": 55.75, "lon": 37.61, "alt_m": 100.0, "heading_deg": 0.0}
	mem, _, _, _, _ := startORVDStackWithNav(t, orvdTopic, false, mock.handle, nav)

	_, err := mem.Request(ctx, prefix+".mission_handler", map[string]interface{}{
		"action": "LOAD_MISSION",
		"sender": "security_monitor",
		"payload": map[string]interface{}{
			"mission": sampleMission("m-start"),
		},
	}, 5.0)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := mem.Request(ctx, prefix+".autopilot", map[string]interface{}{
		"action": "cmd",
		"sender": "security_monitor",
		"payload": map[string]interface{}{
			"command": "START",
		},
	}, 5.0)
	if err != nil {
		t.Fatal(err)
	}
	pl, _ := resp["payload"].(map[string]interface{})
	if pl["ok"] != true {
		t.Fatalf("START failed: %#v", pl)
	}
	if pl["state"] != autopilot.StateExecuting {
		t.Fatalf("expected EXECUTING after preflight, got state=%#v", pl["state"])
	}
}

func TestModule_ORVD_LimiterUpdateConfigFromORVD(t *testing.T) {
	ctx := context.Background()
	mem := testutil.NewMemoryBus()
	cfg := testutil.Config("limiter")
	t.Setenv("JOURNAL_FILE_PATH", t.TempDir()+"/orvd_push.ndjson")

	lim := limiter.New(cfg, mem)
	if err := lim.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lim.Stop(ctx) })

	_ = mem.Publish(ctx, cfg.BrokerTopicFor("limiter"), map[string]interface{}{
		"action": "update_config",
		"sender": "orvd",
		"payload": map[string]interface{}{
			"constraints": map[string]interface{}{
				"max_distance_from_path_m": 25.0,
				"max_alt_deviation_m":      12.0,
			},
		},
	})

	st, err := mem.Request(ctx, cfg.BrokerTopicFor("limiter"), map[string]interface{}{
		"action": "get_state", "sender": "security_monitor", "payload": map[string]interface{}{},
	}, 2.0)
	if err != nil {
		t.Fatal(err)
	}
	pl, _ := st["payload"].(map[string]interface{})
	if pl["max_distance_from_path_m"] != 25.0 || pl["max_alt_deviation_m"] != 12.0 {
		t.Fatalf("constraints not applied: %#v", pl)
	}
}

func TestModule_ORVD_PolicyBlocksLimiterWithoutORVDRule(t *testing.T) {
	ctx := context.Background()
	mem := testutil.NewMemoryBus()
	prefix := testutil.TopicPrefix()
	orvdTopic := "test.orvd.policy"

	policies := []map[string]string{
		{"sender": "limiter", "topic": prefix + ".journal", "action": "LOG_EVENT"},
	}
	raw, _ := json.Marshal(policies)
	t.Setenv("SECURITY_POLICIES", string(raw))
	t.Setenv("ORVD_TOPIC", orvdTopic)

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

	t.Setenv("LIMITER_ORVD_MOCK_SUCCESS", "0")
	_ = os.Setenv("LIMITER_ORVD_MOCK_SUCCESS", "0")

	resp, err := mem.Request(ctx, testutil.Config("security_monitor").BrokerTopicFor("security_monitor"), map[string]interface{}{
		"action": "proxy_request",
		"sender": "limiter",
		"payload": map[string]interface{}{
			"target": map[string]interface{}{"topic": orvdTopic, "action": "register_drone"},
			"data":   map[string]interface{}{"drone_id": "drone_001"},
		},
	}, 2.0)
	if err != nil {
		t.Fatal(err)
	}
	pl, _ := resp["payload"].(map[string]interface{})
	if pl["error"] != "forbidden" {
		t.Fatalf("expected forbidden, got %#v", pl)
	}
}

func TestModule_ORVD_RevokeTakeoffTriggersEmergency(t *testing.T) {
	ctx := context.Background()
	mem := testutil.NewMemoryBus()
	prefix := testutil.TopicPrefix()
	cfg := testutil.Config("limiter")

	policies := []map[string]string{
		{"sender": "limiter", "topic": prefix + ".emergency", "action": "limiter_event"},
		{"sender": "limiter", "topic": prefix + ".journal", "action": "LOG_EVENT"},
	}
	raw, _ := json.Marshal(policies)
	t.Setenv("SECURITY_POLICIES", string(raw))
	t.Setenv("JOURNAL_FILE_PATH", t.TempDir()+"/revoke.ndjson")
	t.Setenv("LIMITER_ORVD_MOCK_SUCCESS", "1")

	emergencyEvents := 0
	_ = mem.Subscribe(ctx, prefix+".emergency", func(msg map[string]interface{}) {
		if msg["action"] == "limiter_event" {
			emergencyEvents++
		}
	})

	sm := securitymonitor.New(testutil.Config("security_monitor"), mem)
	if err := sm.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sm.Stop(ctx) })

	lim := limiter.New(cfg, mem)
	if err := lim.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lim.Stop(ctx) })

	_ = mem.Publish(ctx, cfg.BrokerTopicFor("limiter"), map[string]interface{}{
		"action": "revoke_takeoff",
		"sender": "orvd",
		"payload": map[string]interface{}{
			"drone_id": "drone_001",
		},
	})

	time.Sleep(50 * time.Millisecond)
	if emergencyEvents < 1 {
		t.Fatalf("expected limiter_event on revoke, got %d", emergencyEvents)
	}
}

func TestModule_ORVD_SendTelemetryEmergency(t *testing.T) {
	orvdTopic := "test.orvd.telemetry"
	mock := newOpbdMock()
	mock.setEmergencyCoords(55.75, 37.61)
	nav := map[string]interface{}{"lat": 55.75, "lon": 37.61, "alt_m": 100.0}

	ctx := context.Background()
	mem := testutil.NewMemoryBus()
	prefix := testutil.TopicPrefix()

	t.Setenv("SECURITY_POLICIES", orvdPolicies(prefix, orvdTopic))
	t.Setenv("ORVD_TOPIC", orvdTopic)
	t.Setenv("JOURNAL_FILE_PATH", t.TempDir()+"/tel.ndjson")
	t.Setenv("LIMITER_ORVD_MOCK_SUCCESS", "0")
	t.Setenv("LIMITER_ORVD_TELEMETRY_INTERVAL_S", "0.05")
	t.Setenv("LIMITER_NAV_POLL_INTERVAL_S", "0.05")
	t.Setenv("LIMITER_CONTROL_INTERVAL_S", "0.05")

	_ = mem.Subscribe(ctx, orvdTopic, func(msg map[string]interface{}) {
		replyTo, _ := msg["reply_to"].(string)
		cid, _ := msg["correlation_id"].(string)
		_ = mem.Publish(ctx, replyTo, map[string]interface{}{
			"action": "response", "sender": "orvd", "success": true,
			"correlation_id": cid, "payload": mock.handle(msg),
		})
	})
	_ = mem.Subscribe(ctx, prefix+".journal", func(map[string]interface{}) {})
	navPolls := 0
	_ = mem.Subscribe(ctx, prefix+".navigation", func(msg map[string]interface{}) {
		if msg["action"] != "get_state" {
			return
		}
		navPolls++
		replyTo, _ := msg["reply_to"].(string)
		cid, _ := msg["correlation_id"].(string)
		_ = mem.Publish(ctx, replyTo, map[string]interface{}{
			"action": "response", "sender": "navigation", "success": true,
			"correlation_id": cid, "payload": nav,
		})
	})

	emergencyEvents := 0
	_ = mem.Subscribe(ctx, prefix+".emergency", func(msg map[string]interface{}) {
		if msg["action"] == "limiter_event" {
			emergencyEvents++
		}
	})

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

	loadResp, err := mem.Request(ctx, prefix+".limiter", map[string]interface{}{
		"action": "mission_load",
		"sender": "security_monitor",
		"payload": map[string]interface{}{
			"mission": sampleMission("m-tel"),
		},
	}, 5.0)
	if err != nil {
		t.Fatal(err)
	}
	pl, _ := loadResp["payload"].(map[string]interface{})
	if pl["ok"] != true {
		t.Fatalf("mission_load failed: %#v", pl)
	}

	for i := 0; i < 40 && navPolls < 1; i++ {
		time.Sleep(25 * time.Millisecond)
	}

	takeoffResp, err := mem.Request(ctx, prefix+".limiter", map[string]interface{}{
		"action": "orvd_takeoff",
		"sender": "security_monitor",
		"payload": map[string]interface{}{
			"mission_id": "m-tel",
		},
	}, 5.0)
	if err != nil {
		t.Fatal(err)
	}
	tPl, _ := takeoffResp["payload"].(map[string]interface{})
	if tPl["ok"] != true {
		t.Fatalf("orvd_takeoff failed: %#v", tPl)
	}

	stResp, err := mem.Request(ctx, prefix+".limiter", map[string]interface{}{
		"action": "get_state", "sender": "security_monitor", "payload": map[string]interface{}{},
	}, 2.0)
	if err != nil {
		t.Fatal(err)
	}
	stPl, _ := stResp["payload"].(map[string]interface{})
	if stPl["orvd_takeoff_authorized"] != true {
		t.Fatalf("takeoff not authorized in limiter state: %#v", stPl)
	}
	if stPl["mission_loaded"] != true {
		t.Fatalf("mission not loaded in limiter state: %#v", stPl)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && emergencyEvents < 1 {
		time.Sleep(50 * time.Millisecond)
	}
	if navPolls < 1 {
		t.Fatalf("expected navigation polls, got %d", navPolls)
	}
	if mock.telemetryCalls < 1 {
		t.Fatalf("expected send_telemetry calls, got %d (nav_polls=%d)", mock.telemetryCalls, navPolls)
	}
	if emergencyEvents < 1 {
		t.Fatalf("expected emergency from ORVD telemetry, got %d events (telemetry_calls=%d)", emergencyEvents, mock.telemetryCalls)
	}
}
