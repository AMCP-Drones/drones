package tests

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	cargo "github.com/AMCP-Drones/drones/systems/deliverydron/cargo/src"
	emergency "github.com/AMCP-Drones/drones/systems/deliverydron/emergency/src"
	limiter "github.com/AMCP-Drones/drones/systems/deliverydron/limiter/src"
	motors "github.com/AMCP-Drones/drones/systems/deliverydron/motors/src"
	securitymonitor "github.com/AMCP-Drones/drones/systems/deliverydron/security_monitor/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/tests/testutil"
)

func TestModule_SecurityMonitor_PolicyAdminCRUD(t *testing.T) {
	ctx := context.Background()
	mem := testutil.NewMemoryBus()
	cfg := testutil.Config("security_monitor")
	t.Setenv("POLICY_ADMIN_SENDER", "qa_admin")
	t.Setenv("SECURITY_POLICIES", "")
	sm := securitymonitor.New(cfg, mem)
	if err := sm.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sm.Stop(ctx) })

	targetTopic := cfg.BrokerTopicFor("motors")
	policyPayload := map[string]interface{}{
		"sender": "autopilot",
		"topic":  targetTopic,
		"action": "SET_TARGET",
	}

	denyResp, err := mem.Request(ctx, cfg.BrokerTopicFor("security_monitor"), map[string]interface{}{
		"action": "set_policy", "sender": "intruder", "payload": policyPayload,
	}, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	denyPayload, _ := denyResp["payload"].(map[string]interface{})
	if denyPayload["updated"] != false {
		t.Fatalf("untrusted policy update must fail: %#v", denyPayload)
	}

	setResp, err := mem.Request(ctx, cfg.BrokerTopicFor("security_monitor"), map[string]interface{}{
		"action": "set_policy", "sender": "qa_admin", "payload": policyPayload,
	}, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	setPayload, _ := setResp["payload"].(map[string]interface{})
	if setPayload["updated"] != true {
		t.Fatalf("policy update must succeed: %#v", setPayload)
	}

	listResp, err := mem.Request(ctx, cfg.BrokerTopicFor("security_monitor"), map[string]interface{}{
		"action": "list_policies", "sender": "qa_admin",
	}, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	listPayload, _ := listResp["payload"].(map[string]interface{})
	if listPayload["count"] == 0 {
		t.Fatalf("expected non-empty policies: %#v", listPayload)
	}

	removeResp, err := mem.Request(ctx, cfg.BrokerTopicFor("security_monitor"), map[string]interface{}{
		"action": "remove_policy", "sender": "qa_admin", "payload": policyPayload,
	}, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	removePayload, _ := removeResp["payload"].(map[string]interface{})
	if removePayload["removed"] != true {
		t.Fatalf("policy removal must succeed: %#v", removePayload)
	}

	clearResp, err := mem.Request(ctx, cfg.BrokerTopicFor("security_monitor"), map[string]interface{}{
		"action": "clear_policies", "sender": "qa_admin",
	}, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	clearPayload, _ := clearResp["payload"].(map[string]interface{})
	if clearPayload["cleared"] != true {
		t.Fatalf("policy clear must succeed: %#v", clearPayload)
	}
}

func TestModule_SecurityMonitor_IsolationStartAndEmergencyPolicies(t *testing.T) {
	ctx := context.Background()
	mem := testutil.NewMemoryBus()
	cfg := testutil.Config("security_monitor")
	t.Setenv("SECURITY_POLICIES", "")
	sm := securitymonitor.New(cfg, mem)
	if err := sm.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sm.Stop(ctx) })

	var gotLand bool
	_ = mem.Subscribe(ctx, cfg.BrokerTopicFor("motors"), func(msg map[string]interface{}) {
		if msg["action"] == "LAND" && msg["sender"] == "security_monitor" {
			gotLand = true
		}
	})

	isoResp, err := mem.Request(ctx, cfg.BrokerTopicFor("security_monitor"), map[string]interface{}{
		"action": "ISOLATION_START", "sender": "emergency",
	}, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	isoPayload, _ := isoResp["payload"].(map[string]interface{})
	if isoPayload["activated"] != true {
		t.Fatalf("isolation must activate: %#v", isoPayload)
	}

	_, err = mem.Request(ctx, cfg.BrokerTopicFor("security_monitor"), map[string]interface{}{
		"action": "proxy_publish",
		"sender": "emergency",
		"payload": map[string]interface{}{
			"target": map[string]interface{}{
				"topic":  cfg.BrokerTopicFor("motors"),
				"action": "LAND",
			},
			"data": map[string]interface{}{},
		},
	}, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	if !gotLand {
		t.Fatal("expected LAND publish through emergency isolation policy")
	}

	statusResp, err := mem.Request(ctx, cfg.BrokerTopicFor("security_monitor"), map[string]interface{}{
		"action": "isolation_status", "sender": "emergency",
	}, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	statusPayload, _ := statusResp["payload"].(map[string]interface{})
	if statusPayload["mode"] != "ISOLATED" {
		t.Fatalf("expected isolated mode, got %#v", statusPayload)
	}
}

func TestModule_Motors_TrustedAndUntrustedCommands(t *testing.T) {
	ctx := context.Background()
	mem := testutil.NewMemoryBus()
	cfg := testutil.Config("motors")
	t.Setenv("SITL_COMMANDS_TOPIC", "sitl.commands")
	m := motors.New(cfg, mem)
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Stop(ctx) })

	var sitlCount int
	_ = mem.Subscribe(ctx, "sitl.commands", func(map[string]interface{}) { sitlCount++ })

	_ = mem.Publish(ctx, cfg.BrokerTopicFor("motors"), map[string]interface{}{
		"action": "SET_TARGET",
		"sender": "intruder",
		"payload": map[string]interface{}{
			"lat": 1.0, "lon": 2.0, "alt_m": 3.0,
		},
	})
	state1, err := mem.Request(ctx, cfg.BrokerTopicFor("motors"), map[string]interface{}{
		"action": "get_state", "sender": "security_monitor",
	}, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	payload1, _ := state1["payload"].(map[string]interface{})
	if payload1["mode"] != motors.ModeIDLE {
		t.Fatalf("untrusted set_target changed mode: %#v", payload1)
	}

	_, err = mem.Request(ctx, cfg.BrokerTopicFor("motors"), map[string]interface{}{
		"action": "SET_TARGET",
		"sender": "security_monitor",
		"payload": map[string]interface{}{
			"lat": 10.0, "lon": 20.0, "alt_m": 30.0, "heading_deg": 90.0,
		},
	}, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = mem.Request(ctx, cfg.BrokerTopicFor("motors"), map[string]interface{}{
		"action": "LAND", "sender": "security_monitor", "payload": map[string]interface{}{},
	}, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	state2, err := mem.Request(ctx, cfg.BrokerTopicFor("motors"), map[string]interface{}{
		"action": "get_state", "sender": "security_monitor",
	}, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	payload2, _ := state2["payload"].(map[string]interface{})
	if payload2["mode"] != motors.ModeLANDING || sitlCount < 2 {
		t.Fatalf("expected LANDING and sitl emits, got mode=%#v count=%d", payload2["mode"], sitlCount)
	}
}

func TestModule_Cargo_OpenCloseAndRejectUntrusted(t *testing.T) {
	ctx := context.Background()
	mem := testutil.NewMemoryBus()
	cfg := testutil.Config("cargo")
	c := cargo.New(cfg, mem)
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Stop(ctx) })

	_ = mem.Publish(ctx, cfg.BrokerTopicFor("cargo"), map[string]interface{}{
		"action": "CLOSE", "sender": "intruder",
	})
	st1, err := mem.Request(ctx, cfg.BrokerTopicFor("cargo"), map[string]interface{}{
		"action": "get_state", "sender": "security_monitor",
	}, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	p1, _ := st1["payload"].(map[string]interface{})
	if p1["state"] != cargo.StateClosed {
		t.Fatalf("unexpected state after untrusted close: %#v", p1)
	}

	_, err = mem.Request(ctx, cfg.BrokerTopicFor("cargo"), map[string]interface{}{
		"action": "OPEN", "sender": "security_monitor",
	}, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = mem.Request(ctx, cfg.BrokerTopicFor("cargo"), map[string]interface{}{
		"action": "CLOSE", "sender": "security_monitor",
	}, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	st2, err := mem.Request(ctx, cfg.BrokerTopicFor("cargo"), map[string]interface{}{
		"action": "get_state", "sender": "security_monitor",
	}, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	p2, _ := st2["payload"].(map[string]interface{})
	if p2["state"] != cargo.StateClosed {
		t.Fatalf("expected final closed state: %#v", p2)
	}
}

func TestModule_Emergency_ValidEventTriggersProtocol(t *testing.T) {
	ctx := context.Background()
	mem := testutil.NewMemoryBus()
	cfg := testutil.Config("emergency")
	e := emergency.New(cfg, mem)
	if err := e.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Stop(ctx) })

	var secMessages []string
	_ = mem.Subscribe(ctx, cfg.BrokerTopicFor("security_monitor"), func(msg map[string]interface{}) {
		a, _ := msg["action"].(string)
		secMessages = append(secMessages, a)
	})

	resp, err := mem.Request(ctx, cfg.BrokerTopicFor("emergency"), map[string]interface{}{
		"action": "limiter_event",
		"sender": "limiter",
		"payload": map[string]interface{}{
			"event":      "EMERGENCY_LAND_REQUIRED",
			"mission_id": "m-1",
			"details":    map[string]interface{}{"distance": 123.0},
		},
	}, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	pl, _ := resp["payload"].(map[string]interface{})
	if pl["ok"] != true {
		t.Fatalf("emergency protocol should succeed: %#v", pl)
	}

	stateResp, err := mem.Request(ctx, cfg.BrokerTopicFor("emergency"), map[string]interface{}{
		"action": "get_state", "sender": "security_monitor",
	}, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	statePayload, _ := stateResp["payload"].(map[string]interface{})
	if statePayload["active"] != true {
		t.Fatalf("emergency must be active: %#v", statePayload)
	}
	if len(secMessages) < 4 {
		t.Fatalf("expected protocol messages to security monitor, got %v", secMessages)
	}
}

func TestModule_Limiter_ConfigAndEmergencyTransition(t *testing.T) {
	ctx := context.Background()
	mem := testutil.NewMemoryBus()
	cfg := testutil.Config("limiter")

	prefix := testutil.TopicPrefix()
	policies := []map[string]string{
		{"sender": "limiter", "topic": prefix + ".navigation", "action": "get_state"},
		{"sender": "limiter", "topic": prefix + ".telemetry", "action": "get_state"},
		{"sender": "limiter", "topic": prefix + ".journal", "action": "LOG_EVENT"},
		{"sender": "limiter", "topic": prefix + ".emergency", "action": "limiter_event"},
	}
	rawPolicies, _ := json.Marshal(policies)
	t.Setenv("SECURITY_POLICIES", string(rawPolicies))
	t.Setenv("LIMITER_CONTROL_INTERVAL_S", "0.05")
	t.Setenv("LIMITER_NAV_POLL_INTERVAL_S", "0.01")
	t.Setenv("LIMITER_TELEMETRY_POLL_INTERVAL_S", "0.01")
	t.Setenv("LIMITER_MAX_DISTANCE_FROM_PATH_M", "5")
	t.Setenv("LIMITER_MAX_ALT_DEVIATION_M", "2")

	sm := securitymonitor.New(testutil.Config("security_monitor"), mem)
	if err := sm.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sm.Stop(ctx) })

	_ = mem.Subscribe(ctx, cfg.BrokerTopicFor("navigation"), func(msg map[string]interface{}) {
		if msg["action"] != "get_state" {
			return
		}
		replyTo, _ := msg["reply_to"].(string)
		cid, _ := msg["correlation_id"].(string)
		_ = mem.Publish(ctx, replyTo, map[string]interface{}{
			"action":         "response",
			"sender":         "navigation",
			"success":        true,
			"correlation_id": cid,
			"payload": map[string]interface{}{
				"lat": 55.0, "lon": 38.0, "alt_m": 200.0,
			},
		})
	})
	_ = mem.Subscribe(ctx, cfg.BrokerTopicFor("telemetry"), func(msg map[string]interface{}) {
		if msg["action"] != "get_state" {
			return
		}
		replyTo, _ := msg["reply_to"].(string)
		cid, _ := msg["correlation_id"].(string)
		_ = mem.Publish(ctx, replyTo, map[string]interface{}{
			"action":         "response",
			"sender":         "telemetry",
			"success":        true,
			"correlation_id": cid,
			"payload":        map[string]interface{}{"ok": true},
		})
	})

	l := limiter.New(cfg, mem)
	if err := l.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Stop(ctx) })

	_, err := mem.Request(ctx, cfg.BrokerTopicFor("limiter"), map[string]interface{}{
		"action": "mission_load",
		"sender": "security_monitor",
		"payload": map[string]interface{}{
			"mission": map[string]interface{}{
				"mission_id": "m-lim",
				"steps": []interface{}{
					map[string]interface{}{"lat": 55.75, "lon": 37.62, "alt_m": 100.0},
				},
			},
		},
	}, 1.0)
	if err != nil {
		t.Fatal(err)
	}

	_, err = mem.Request(ctx, cfg.BrokerTopicFor("limiter"), map[string]interface{}{
		"action": "update_config",
		"sender": "security_monitor",
		"payload": map[string]interface{}{
			"max_distance_from_path_m": 10.0,
			"max_alt_deviation_m":      5.0,
		},
	}, 1.0)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(200 * time.Millisecond)
	st, err := mem.Request(ctx, cfg.BrokerTopicFor("limiter"), map[string]interface{}{
		"action": "get_state", "sender": "security_monitor",
	}, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	pl, _ := st["payload"].(map[string]interface{})
	if pl["state"] != limiter.StateEmergency {
		t.Fatalf("expected emergency state, got %#v", pl)
	}
}
