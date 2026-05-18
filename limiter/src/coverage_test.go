package limiter

import (
	"context"
	"testing"

	securitymonitor "github.com/AMCP-Drones/drones/systems/deliverydron/security_monitor/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/tests/testutil"
)

func TestApplyORVDConstraints(t *testing.T) {
	l := &Limiter{maxDistanceFromPathM: 10, maxAltDeviationM: 5}
	applyORVDConstraints(l, map[string]interface{}{
		"constraints": map[string]interface{}{
			"max_distance_from_path_m": 25.0,
			"max_alt_deviation_m":      15.0,
		},
	})
	if l.maxDistanceFromPathM != 25 || l.maxAltDeviationM != 15 {
		t.Fatalf("dist=%v alt=%v", l.maxDistanceFromPathM, l.maxAltDeviationM)
	}
	applyORVDConstraints(l, map[string]interface{}{})
	if l.maxDistanceFromPathM != 25 {
		t.Fatal("nil constraints should not change")
	}
}

func TestGetFloat_Variants(t *testing.T) {
	if v := getFloat(map[string]interface{}{"x": int(3)}, "x"); v != 3 {
		t.Fatalf("int %v", v)
	}
	if v := getFloat(map[string]interface{}{"x": int64(4)}, "x"); v != 4 {
		t.Fatalf("int64 %v", v)
	}
}

func TestRunORVDMissionLoad_Mock(t *testing.T) {
	l := New(testutil.Config("limiter"), testutil.NewMemoryBus())
	l.orvd.mockSuccess = true
	status, err := l.runORVDMissionLoad(context.Background(), map[string]interface{}{
		"mission_id": "m-ml",
		"steps":      []interface{}{map[string]interface{}{"lat": 1.0, "lon": 2.0, "alt_m": 10.0}},
	})
	if status != ORVDStatusAuthorized || err != "" {
		t.Fatalf("status=%s err=%s", status, err)
	}
}

func TestStepFloat_Variants(t *testing.T) {
	if v, ok := stepFloat(map[string]interface{}{"lat": float32(1.5)}, "lat"); !ok || v != 1.5 {
		t.Fatalf("float32: %v %v", v, ok)
	}
	if _, ok := stepFloat(map[string]interface{}{"lat": "x"}, "lat"); ok {
		t.Fatal("expected fail for string")
	}
}

func TestHandleMissionLoad_OutOfBounds(t *testing.T) {
	ctx := context.Background()
	mem := testutil.NewMemoryBus()
	prefix := testutil.TopicPrefix()
	t.Setenv("LIMITER_MAX_MISSION_ALT_M", "50")
	sm := securitymonitor.New(testutil.Config("security_monitor"), mem)
	_ = sm.Start(ctx)
	defer func() { _ = sm.Stop(ctx) }()
	l := New(testutil.Config("limiter"), mem)
	_ = l.Start(ctx)
	defer func() { _ = l.Stop(ctx) }()
	resp, err := mem.Request(ctx, prefix+".limiter", map[string]interface{}{
		"action": "mission_load", "sender": "security_monitor",
		"payload": map[string]interface{}{
			"mission": map[string]interface{}{
				"mission_id": "hi",
				"steps":      []interface{}{map[string]interface{}{"lat": 1.0, "lon": 2.0, "alt_m": 200.0}},
			},
		},
	}, 2.0)
	if err != nil {
		t.Fatal(err)
	}
	pl, _ := resp["payload"].(map[string]interface{})
	if pl["error"] != "mission_out_of_bounds" {
		t.Fatalf("%#v", pl)
	}
}

func TestRunORVDRequestTakeoff_Mock(t *testing.T) {
	l := New(testutil.Config("limiter"), testutil.NewMemoryBus())
	l.orvd.mockSuccess = true
	l.orvdStatus = ORVDStatusAuthorized
	ok, pending, err := l.runORVDRequestTakeoff(context.Background(), "m-tk2")
	if !ok || pending || err != "" {
		t.Fatalf("ok=%v pending=%v err=%s", ok, pending, err)
	}
}

func TestRunORVDCompleteMission_Mock(t *testing.T) {
	ctx := context.Background()
	mem := testutil.NewMemoryBus()
	l := New(testutil.Config("limiter"), mem)
	l.orvd.mockSuccess = true
	l.orvdPhase = ORVDPhaseTakeoffAuthorized
	l.runORVDCompleteMission(ctx, "m1", "success")
	if l.orvdPhase != ORVDPhaseCompleted {
		t.Fatalf("phase=%s", l.orvdPhase)
	}
}

func TestHandleORVDTakeoff_ViaHandler(t *testing.T) {
	ctx := context.Background()
	mem := testutil.NewMemoryBus()
	prefix := testutil.TopicPrefix()
	sm := securitymonitor.New(testutil.Config("security_monitor"), mem)
	_ = sm.Start(ctx)
	defer func() { _ = sm.Stop(ctx) }()
	l := New(testutil.Config("limiter"), mem)
	_ = l.Start(ctx)
	defer func() { _ = l.Stop(ctx) }()
	l.orvd.mockSuccess = true
	l.orvdStatus = ORVDStatusAuthorized
	l.orvdMissionID = "m-tk"
	l.orvdTakeoffAuthorized = false

	resp, err := mem.Request(ctx, prefix+".limiter", map[string]interface{}{
		"action": "orvd_takeoff", "sender": "security_monitor",
		"payload": map[string]interface{}{"mission_id": "m-tk"},
	}, 2.0)
	if err != nil {
		t.Fatal(err)
	}
	pl, _ := resp["payload"].(map[string]interface{})
	if pl["ok"] != true {
		t.Fatalf("%#v", pl)
	}
}

func TestHandleORVDComplete_NoMission(t *testing.T) {
	ctx := context.Background()
	mem := testutil.NewMemoryBus()
	prefix := testutil.TopicPrefix()
	sm := securitymonitor.New(testutil.Config("security_monitor"), mem)
	_ = sm.Start(ctx)
	defer func() { _ = sm.Stop(ctx) }()
	l := New(testutil.Config("limiter"), mem)
	_ = l.Start(ctx)
	defer func() { _ = l.Stop(ctx) }()
	resp, err := mem.Request(ctx, prefix+".limiter", map[string]interface{}{
		"action": "orvd_complete", "sender": "security_monitor",
		"payload": map[string]interface{}{"result": "success"},
	}, 2.0)
	if err != nil {
		t.Fatal(err)
	}
	pl, _ := resp["payload"].(map[string]interface{})
	if pl["error"] != "no_mission" {
		t.Fatalf("%#v", pl)
	}
}

func TestHandleORVDComplete_ViaHandler(t *testing.T) {
	ctx := context.Background()
	mem := testutil.NewMemoryBus()
	prefix := testutil.TopicPrefix()
	orvdTopic := "test.orvd.complete"
	t.Setenv("ORVD_TOPIC", orvdTopic)
	t.Setenv("SECURITY_POLICIES", `[{"sender":"limiter","topic":"`+orvdTopic+`","action":"complete_mission"}]`)
	_ = mem.Subscribe(ctx, orvdTopic, func(msg map[string]interface{}) {
		replyTo, _ := msg["reply_to"].(string)
		cid, _ := msg["correlation_id"].(string)
		_ = mem.Publish(ctx, replyTo, map[string]interface{}{
			"action": "response", "sender": "orvd", "success": true, "correlation_id": cid,
			"payload": map[string]interface{}{"status": "mission_completed"},
		})
	})
	sm := securitymonitor.New(testutil.Config("security_monitor"), mem)
	_ = sm.Start(ctx)
	defer func() { _ = sm.Stop(ctx) }()
	l := New(testutil.Config("limiter"), mem)
	_ = l.Start(ctx)
	defer func() { _ = l.Stop(ctx) }()
	l.orvdPhase = ORVDPhaseTakeoffAuthorized

	_, err := mem.Request(ctx, prefix+".limiter", map[string]interface{}{
		"action": "orvd_complete", "sender": "security_monitor",
		"payload": map[string]interface{}{"mission_id": "m-complete", "result": "success"},
	}, 2.0)
	if err != nil {
		t.Fatal(err)
	}
	if l.orvdPhase != ORVDPhaseCompleted {
		t.Fatalf("phase=%s", l.orvdPhase)
	}
}
