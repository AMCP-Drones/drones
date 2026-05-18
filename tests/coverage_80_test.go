package tests

import (
	"context"
	"testing"
	"time"

	"github.com/AMCP-Drones/drones/systems/deliverydron/autopilot/src"
	emergency "github.com/AMCP-Drones/drones/systems/deliverydron/emergency/src"
	limiter "github.com/AMCP-Drones/drones/systems/deliverydron/limiter/src"
	securitymonitor "github.com/AMCP-Drones/drones/systems/deliverydron/security_monitor/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/tests/testutil"
)

func TestCoverage80_AutopilotMissionCompleted(t *testing.T) {
	orvdTopic := "test.orvd.complete80"
	ctx := context.Background()
	mock := newOpbdMock()
	nav := map[string]interface{}{"lat": 55.75, "lon": 37.61, "alt_m": 100.0, "heading_deg": 0.0}
	mem, _, _, _, _ := startORVDStackWithNav(t, orvdTopic, false, mock.handle, nav)
	prefix := testutil.TopicPrefix()

	t.Setenv("AUTOPILOT_CONTROL_INTERVAL_S", "0.05")
	t.Setenv("AUTOPILOT_NAV_POLL_INTERVAL_S", "0.05")
	t.Setenv("AUTOPILOT_PREFLIGHT_TIMEOUT_S", "10")

	_, err := mem.Request(ctx, prefix+".mission_handler", map[string]interface{}{
		"action": "LOAD_MISSION",
		"sender": "security_monitor",
		"payload": map[string]interface{}{
			"mission": sampleMission("m-complete80"),
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
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		st, err := mem.Request(ctx, prefix+".autopilot", map[string]interface{}{
			"action": "get_state", "sender": "security_monitor", "payload": map[string]interface{}{},
		}, 2.0)
		if err != nil {
			t.Fatal(err)
		}
		stPl, _ := st["payload"].(map[string]interface{})
		switch stPl["state"] {
		case autopilot.StateCompleted:
			gotCompleted = true
		case autopilot.StateAborted:
			t.Fatalf("aborted: %#v", stPl)
		}
		if gotCompleted {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !gotCompleted {
		t.Fatal("expected COMPLETED state")
	}
}

func TestCoverage80_MissionHandlerWPLLoad(t *testing.T) {
	orvdTopic := "test.orvd.wpl80"
	ctx := context.Background()
	mock := newOpbdMock()
	mem, _, _, _, _ := startORVDStack(t, orvdTopic, true, mock.handle)
	prefix := testutil.TopicPrefix()
	wpl := "QGC WPL 110\n" +
		"0\t1\t0\t16\t0\t0\t0\t0\t55.75\t37.62\t100\t1\n" +
		"1\t0\t0\t16\t0\t0\t0\t0\t55.76\t37.63\t120\t1\n"
	resp, err := mem.Request(ctx, prefix+".mission_handler", map[string]interface{}{
		"action": "LOAD_MISSION",
		"sender": "security_monitor",
		"payload": map[string]interface{}{
			"wpl_content": wpl,
			"mission_id":  "m-wpl80",
		},
	}, 5.0)
	if err != nil {
		t.Fatal(err)
	}
	pl, _ := resp["payload"].(map[string]interface{})
	if pl["ok"] != true {
		t.Fatalf("%#v", pl)
	}
}

func TestCoverage80_DroneportPreflightMock(t *testing.T) {
	ctx := context.Background()
	mem := testutil.NewMemoryBus()
	prefix := testutil.TopicPrefix()
	dpTopic := "test.dp.cov80"
	orchTopic := "test.dp.orch80"
	mock := newDroneportMock()
	subscribeDroneportMock(ctx, mem, dpTopic, orchTopic, mock)

	t.Setenv("SECURITY_POLICIES", droneportPolicies(prefix, dpTopic, orchTopic))
	t.Setenv("DRONEPORT_TOPIC", dpTopic)
	t.Setenv("DRONEPORT_ORCHESTRATOR_TOPIC", orchTopic)
	t.Setenv("DRONEPORT_DRONE_ID", "drone_001")
	t.Setenv("EMERGENCY_DRONEPORT_MOCK_SUCCESS", "0")
	t.Setenv("JOURNAL_FILE_PATH", t.TempDir()+"/dp80.ndjson")
	_ = mem.Subscribe(ctx, prefix+".journal", func(map[string]interface{}) {})

	sm := securitymonitor.New(testutil.Config("security_monitor"), mem)
	_ = sm.Start(ctx)
	defer func() { _ = sm.Stop(ctx) }()

	em := emergency.New(testutil.Config("emergency"), mem)
	_ = em.Start(ctx)
	defer func() { _ = em.Stop(ctx) }()

	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := mem.Request(ctx, prefix+".emergency", map[string]interface{}{
			"action": "droneport_takeoff", "sender": "security_monitor",
			"payload": map[string]interface{}{"mission_id": "m-dp80"},
		}, 2.0)
		if err != nil {
			t.Fatal(err)
		}
		pl, _ := resp["payload"].(map[string]interface{})
		if pl["ok"] == true {
			return
		}
		if pl["pending"] != true {
			t.Fatalf("%#v", pl)
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("droneport takeoff did not complete")
}

func TestCoverage80_EmergencyLimiterLand(t *testing.T) {
	ctx := context.Background()
	mem := testutil.NewMemoryBus()
	prefix := testutil.TopicPrefix()
	t.Setenv("SECURITY_POLICIES", `[{"sender":"limiter","topic":"`+prefix+`.emergency","action":"get_state"},{"sender":"limiter","topic":"`+prefix+`.emergency","action":"droneport_land"},{"sender":"limiter","topic":"`+prefix+`.journal","action":"LOG_EVENT"}]`)
	t.Setenv("EMERGENCY_DRONEPORT_MOCK_SUCCESS", "1")
	t.Setenv("JOURNAL_FILE_PATH", t.TempDir()+"/el.ndjson")
	_ = mem.Subscribe(ctx, prefix+".journal", func(map[string]interface{}) {})
	sm := securitymonitor.New(testutil.Config("security_monitor"), mem)
	_ = sm.Start(ctx)
	defer func() { _ = sm.Stop(ctx) }()
	em := emergency.New(testutil.Config("emergency"), mem)
	_ = em.Start(ctx)
	defer func() { _ = em.Stop(ctx) }()
	_ = mem.Publish(ctx, prefix+".emergency", map[string]interface{}{
		"action": "limiter_event",
		"sender": "limiter",
		"payload": map[string]interface{}{
			"event": "EMERGENCY_LAND_REQUIRED", "mission_id": "m-el",
			"details": map[string]interface{}{"reason": "deviation"},
		},
	})
	time.Sleep(100 * time.Millisecond)
}

func TestCoverage80_LimiterORVDTakeoff(t *testing.T) {
	ctx := context.Background()
	mem := testutil.NewMemoryBus()
	prefix := testutil.TopicPrefix()
	orvdTopic := "test.orvd.takeoff80"
	mock := newOpbdMock()
	t.Setenv("SECURITY_POLICIES", orvdPolicies(prefix, orvdTopic))
	t.Setenv("ORVD_TOPIC", orvdTopic)
	t.Setenv("JOURNAL_FILE_PATH", t.TempDir()+"/tk.ndjson")
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
	_ = sm.Start(ctx)
	defer func() { _ = sm.Stop(ctx) }()

	lim := limiter.New(testutil.Config("limiter"), mem)
	_ = lim.Start(ctx)
	defer func() { _ = lim.Stop(ctx) }()

	_, err := mem.Request(ctx, prefix+".limiter", map[string]interface{}{
		"action": "mission_load", "sender": "security_monitor",
		"payload": map[string]interface{}{"mission": sampleMission("m-tk80")},
	}, 5.0)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := mem.Request(ctx, prefix+".limiter", map[string]interface{}{
		"action": "orvd_takeoff", "sender": "security_monitor",
		"payload": map[string]interface{}{"mission_id": "m-tk80"},
	}, 5.0)
	if err != nil {
		t.Fatal(err)
	}
	pl, _ := resp["payload"].(map[string]interface{})
	if pl["ok"] != true {
		t.Fatalf("%#v", pl)
	}
}
