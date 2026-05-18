package emergency

import (
	"context"
	"testing"
	"time"

	securitymonitor "github.com/AMCP-Drones/drones/systems/deliverydron/security_monitor/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/tests/testutil"
)

func TestParseDroneportBattery_Variants(t *testing.T) {
	if v, ok := parseDroneportBattery("95.5"); !ok || v != 95.5 {
		t.Fatalf("string %v %v", v, ok)
	}
	if v, ok := parseDroneportBattery(float32(88)); !ok || v != 88 {
		t.Fatal("float32")
	}
	if v, ok := parseDroneportBattery(int(77)); !ok || v != 77 {
		t.Fatal("int")
	}
	if _, ok := parseDroneportBattery("unknown"); ok {
		t.Fatal("unknown")
	}
}

func TestDroneportBodyHelpers(t *testing.T) {
	if !droneportTakeoffOK(map[string]interface{}{
		"payload": map[string]interface{}{"port_id": "p1", "battery": 90.0},
	}) {
		t.Fatal("port+battery")
	}
	if !droneportLandingOK(map[string]interface{}{
		"payload": map[string]interface{}{"port_id": "p1"},
	}) {
		t.Fatal("port landing")
	}
	if unwrapDroneportBody(nil) != nil {
		t.Fatal("nil resp")
	}
}

func TestDroneportResponseHelpers(t *testing.T) {
	if !droneportLandingOK(map[string]interface{}{"payload": map[string]interface{}{"approved": true}}) {
		t.Fatal("landing approved")
	}
	if !droneportTakeoffOK(map[string]interface{}{"payload": map[string]interface{}{"approved": true, "battery": 90}}) {
		t.Fatal("takeoff approved")
	}
	if droneportApproved(map[string]interface{}{"error": "x"}) {
		t.Fatal("error should not approve")
	}
}

func TestHandleDroneportEvent(t *testing.T) {
	mem := testutil.NewMemoryBus()
	ctx := context.Background()
	e := New(testutil.Config("emergency"), mem)
	pl, err := e.handleDroneportEvent(ctx, map[string]interface{}{
		"sender":  "droneport",
		"payload": map[string]interface{}{"event": "port_ready"},
	})
	if err != nil || pl["ok"] != true {
		t.Fatalf("%#v err=%v", pl, err)
	}
}

func TestOptionalLandingBattery(t *testing.T) {
	if optionalLandingBattery(map[string]interface{}{"battery_pct": 40.0}, 95) == nil {
		t.Fatal("expected ptr")
	}
}

func TestHandleLimiterEvent_EmergencyLand(t *testing.T) {
	mem := testutil.NewMemoryBus()
	ctx := context.Background()
	e := New(testutil.Config("emergency"), mem)
	e.droneport.mockSuccess = true
	pl, err := e.handleLimiterEvent(ctx, map[string]interface{}{
		"sender": "limiter",
		"payload": map[string]interface{}{
			"event":      "EMERGENCY_LAND_REQUIRED",
			"mission_id": "m-em",
			"details":    map[string]interface{}{"reason": "test"},
		},
	})
	if err != nil || pl["ok"] != true {
		t.Fatalf("%#v err=%v", pl, err)
	}
}

func TestHandleDroneportLand_Mock(t *testing.T) {
	mem := testutil.NewMemoryBus()
	ctx := context.Background()
	e := New(testutil.Config("emergency"), mem)
	e.droneport.mockSuccess = true
	e.droneportPhase = DroneportPhaseDeparted
	pl, err := e.handleDroneportLand(ctx, map[string]interface{}{
		"sender":  "autopilot",
		"payload": map[string]interface{}{"mission_id": "m-land"},
	})
	if err != nil || pl["ok"] != true {
		t.Fatalf("%#v err=%v", pl, err)
	}
}

func TestIsDroneReady_AndRunLanding(t *testing.T) {
	ctx := context.Background()
	mem := testutil.NewMemoryBus()
	orch := "test.orch.em"
	dp := "test.dp.em"
	t.Setenv("DRONEPORT_ORCHESTRATOR_TOPIC", orch)
	t.Setenv("DRONEPORT_TOPIC", dp)
	t.Setenv("DRONEPORT_DRONE_ID", "drone_001")
	t.Setenv("DRONEPORT_CHARGE_TIMEOUT_S", "1")
	t.Setenv("EMERGENCY_DRONEPORT_MOCK_SUCCESS", "0")
	t.Setenv("SECURITY_POLICIES", `[
		{"sender":"emergency","topic":"`+orch+`","action":"get_available_drones"},
		{"sender":"emergency","topic":"`+dp+`","action":"request_landing"}
	]`)
	reply := func(msg map[string]interface{}, body map[string]interface{}) {
		replyTo, _ := msg["reply_to"].(string)
		cid, _ := msg["correlation_id"].(string)
		_ = mem.Publish(ctx, replyTo, map[string]interface{}{
			"action": "response", "success": true, "correlation_id": cid,
			"payload": body,
		})
	}
	_ = mem.Subscribe(ctx, orch, func(msg map[string]interface{}) {
		action, _ := msg["action"].(string)
		if action == "get_available_drones" {
			reply(msg, map[string]interface{}{
				"drones": []interface{}{
					map[string]interface{}{"drone_id": "drone_001", "battery": 92.0, "status": "ready"},
				},
			})
		}
	})
	_ = mem.Subscribe(ctx, dp, func(msg map[string]interface{}) {
		reply(msg, map[string]interface{}{"approved": true, "port_id": "p1"})
	})
	sm := securitymonitor.New(testutil.Config("security_monitor"), mem)
	_ = sm.Start(ctx)
	defer func() { _ = sm.Stop(ctx) }()
	e := New(testutil.Config("emergency"), mem)
	ready, _, err := e.isDroneReady(ctx)
	if err != nil || !ready {
		t.Fatalf("ready=%v err=%v", ready, err)
	}
	ok, errMsg, _ := e.runLanding(ctx, 95.0)
	if !ok || errMsg != "" {
		t.Fatalf("ok=%v err=%s", ok, errMsg)
	}
	e.droneportChargeWaitAt = time.Now().Add(-2 * time.Second)
	ready2, timedOut, err2 := e.checkChargeReady(ctx)
	if err2 != nil {
		t.Fatal(err2)
	}
	_ = ready2
	_ = timedOut
}

func TestRunPostMissionLand_NonMock(t *testing.T) {
	ctx := context.Background()
	mem := testutil.NewMemoryBus()
	dp := "test.dp.land"
	t.Setenv("DRONEPORT_TOPIC", dp)
	t.Setenv("EMERGENCY_DRONEPORT_MOCK_SUCCESS", "0")
	t.Setenv("SECURITY_POLICIES", `[{"sender":"emergency","topic":"`+dp+`","action":"request_landing"}]`)
	_ = mem.Subscribe(ctx, dp, func(msg map[string]interface{}) {
		replyTo, _ := msg["reply_to"].(string)
		cid, _ := msg["correlation_id"].(string)
		_ = mem.Publish(ctx, replyTo, map[string]interface{}{
			"action": "response", "success": true, "correlation_id": cid,
			"payload": map[string]interface{}{"approved": true, "port_id": "p2"},
		})
	})
	sm := securitymonitor.New(testutil.Config("security_monitor"), mem)
	_ = sm.Start(ctx)
	defer func() { _ = sm.Stop(ctx) }()
	e := New(testutil.Config("emergency"), mem)
	e.droneportPhase = DroneportPhaseDeparted
	ok, _ := e.runPostMissionLand(ctx, "m-post", nil)
	if !ok {
		t.Fatal("expected land ok")
	}
}

func TestRunPreflight_AlreadyDepartedSameMission(t *testing.T) {
	mem := testutil.NewMemoryBus()
	ctx := context.Background()
	e := New(testutil.Config("emergency"), mem)
	e.droneport.mockSuccess = true
	e.droneportPhase = DroneportPhaseDeparted
	e.droneportLastMissionID = "m1"
	r, _ := e.runPreflight(ctx, "m1", nil)
	if r != preflightOK {
		t.Fatalf("result=%v", r)
	}
}
