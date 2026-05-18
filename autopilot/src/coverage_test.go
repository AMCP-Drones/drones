package autopilot

import (
	"context"
	"testing"

	securitymonitor "github.com/AMCP-Drones/drones/systems/deliverydron/security_monitor/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/tests/testutil"
)

func TestNormalizeSteps(t *testing.T) {
	s := normalizeSteps([]map[string]interface{}{{"lat": 1}})
	if len(s) != 1 {
		t.Fatalf("len=%d", len(s))
	}
	if normalizeSteps(nil) != nil {
		t.Fatal("nil")
	}
}

func TestGetFloatAndComputeVelocity(t *testing.T) {
	m := map[string]interface{}{"x": float64(3.5), "alt_m": 10, "h": int64(90)}
	if v := getFloat(m, "x"); v != 3.5 {
		t.Fatalf("getFloat %v", v)
	}
	if getFloat(m, "alt_m") != 10 {
		t.Fatal("int alt")
	}
	if getFloat(m, "h") != 90 {
		t.Fatal("int64")
	}
	vx, vy, vz := computeVelocity(90, 5, 10, 20)
	if vx == 0 && vy == 0 {
		t.Fatal("expected velocity")
	}
	if vz <= 0 {
		t.Fatal("expected climb vz")
	}
	_, _, vz2 := computeVelocity(0, 5, 10, 10)
	if vz2 != 0 {
		t.Fatal("expected zero vz at same alt")
	}
}

func TestStepControl_ExecutingAndComplete(t *testing.T) {
	mem := testutil.NewMemoryBus()
	ctx := context.Background()
	prefix := testutil.TopicPrefix()
	a := New(testutil.Config("autopilot"), mem)
	a.motorsTopic = prefix + ".motors"
	a.cargoTopic = prefix + ".cargo"
	a.limiterTopic = prefix + ".limiter"
	a.emergencyTopic = prefix + ".emergency"
	_ = mem.Subscribe(ctx, prefix+".motors", func(map[string]interface{}) {})
	_ = mem.Subscribe(ctx, prefix+".cargo", func(map[string]interface{}) {})
	_ = mem.Subscribe(ctx, prefix+".limiter", func(msg map[string]interface{}) {
		replyTo, _ := msg["reply_to"].(string)
		cid, _ := msg["correlation_id"].(string)
		_ = mem.Publish(ctx, replyTo, map[string]interface{}{
			"action": "response", "success": true, "correlation_id": cid,
			"payload": map[string]interface{}{"ok": true},
		})
	})
	_ = mem.Subscribe(ctx, prefix+".emergency", func(msg map[string]interface{}) {
		replyTo, _ := msg["reply_to"].(string)
		cid, _ := msg["correlation_id"].(string)
		_ = mem.Publish(ctx, replyTo, map[string]interface{}{
			"action": "response", "success": true, "correlation_id": cid,
			"payload": map[string]interface{}{"ok": true},
		})
	})

	a.mu.Lock()
	a.state = StateExecuting
	a.mission = map[string]interface{}{
		"mission_id": "m-sc",
		"steps": []interface{}{
			map[string]interface{}{"lat": 56.0, "lon": 37.7, "alt_m": 100.0, "speed_mps": 5.0, "drop": true},
		},
	}
	a.lastNavState = map[string]interface{}{"lat": 55.75, "lon": 37.61, "alt_m": 100.0, "heading_deg": 45.0}
	a.currentStepIndex = 0
	a.mu.Unlock()
	a.stepControl(ctx)

	a.mu.Lock()
	a.lastNavState = map[string]interface{}{"lat": 56.0, "lon": 37.7, "alt_m": 100.0, "heading_deg": 0.0}
	a.mu.Unlock()
	a.stepControl(ctx)
	a.mu.RLock()
	st := a.state
	a.mu.RUnlock()
	if st != StateCompleted {
		t.Fatalf("state=%s", st)
	}
}

func TestSafeActuatorStop(t *testing.T) {
	mem := testutil.NewMemoryBus()
	ctx := context.Background()
	prefix := testutil.TopicPrefix()
	a := New(testutil.Config("autopilot"), mem)
	a.motorsTopic = prefix + ".motors"
	a.cargoTopic = prefix + ".cargo"
	_ = mem.Subscribe(ctx, prefix+".motors", func(map[string]interface{}) {})
	_ = mem.Subscribe(ctx, prefix+".cargo", func(map[string]interface{}) {})
	a.safeActuatorStop(ctx)
}

func TestCheckLimiterAuthorization_Pending(t *testing.T) {
	mem := testutil.NewMemoryBus()
	ctx := context.Background()
	prefix := testutil.TopicPrefix()
	t.Setenv("SECURITY_POLICIES", `[{"sender":"autopilot","topic":"`+prefix+`.limiter","action":"get_state"}]`)
	sm := securitymonitor.New(testutil.Config("security_monitor"), mem)
	_ = sm.Start(ctx)
	defer func() { _ = sm.Stop(ctx) }()
	_ = mem.Subscribe(ctx, prefix+".limiter", func(msg map[string]interface{}) {
		replyTo, _ := msg["reply_to"].(string)
		cid, _ := msg["correlation_id"].(string)
		_ = mem.Publish(ctx, replyTo, map[string]interface{}{
			"action": "response", "success": true, "correlation_id": cid,
			"payload": map[string]interface{}{"orvd_status": "PENDING"},
		})
	})
	a := New(testutil.Config("autopilot"), mem)
	a.mu.Lock()
	a.mission = map[string]interface{}{"mission_id": "m1"}
	a.mu.Unlock()
	if got := a.checkLimiterAuthorization(ctx); got != preflightPending {
		t.Fatalf("got %q", got)
	}
}

func TestNotifyHelpers(t *testing.T) {
	mem := testutil.NewMemoryBus()
	ctx := context.Background()
	prefix := testutil.TopicPrefix()
	a := New(testutil.Config("autopilot"), mem)
	a.limiterTopic = prefix + ".limiter"
	a.emergencyTopic = prefix + ".emergency"
	_ = mem.Subscribe(ctx, prefix+".limiter", func(msg map[string]interface{}) {
		replyTo, _ := msg["reply_to"].(string)
		cid, _ := msg["correlation_id"].(string)
		_ = mem.Publish(ctx, replyTo, map[string]interface{}{
			"action": "response", "success": true, "correlation_id": cid,
			"payload": map[string]interface{}{"ok": true},
		})
	})
	_ = mem.Subscribe(ctx, prefix+".emergency", func(msg map[string]interface{}) {
		replyTo, _ := msg["reply_to"].(string)
		cid, _ := msg["correlation_id"].(string)
		_ = mem.Publish(ctx, replyTo, map[string]interface{}{
			"action": "response", "success": true, "correlation_id": cid,
			"payload": map[string]interface{}{"ok": true},
		})
	})
	a.notifyORVDComplete(ctx, "m1")
	a.notifyDroneportLand(ctx, "m1")
}

func TestHandleCmd_PauseKoverAbort(t *testing.T) {
	mem := testutil.NewMemoryBus()
	ctx := context.Background()
	prefix := testutil.TopicPrefix()
	a := New(testutil.Config("autopilot"), mem)
	a.motorsTopic = prefix + ".motors"
	a.cargoTopic = prefix + ".cargo"
	_ = mem.Subscribe(ctx, prefix+".motors", func(map[string]interface{}) {})
	_ = mem.Subscribe(ctx, prefix+".cargo", func(map[string]interface{}) {})
	a.mu.Lock()
	a.state = StateExecuting
	a.mission = map[string]interface{}{
		"mission_id": "m-cmd",
		"steps":      []interface{}{map[string]interface{}{"lat": 1.0, "lon": 2.0, "alt_m": 3.0}},
	}
	a.lastNavState = map[string]interface{}{"lat": 1.0, "lon": 2.0, "alt_m": 3.0, "heading_deg": 0.0}
	a.mu.Unlock()

	cmd := func(c string) {
		_, _ = a.handleCmd(ctx, map[string]interface{}{
			"sender": "security_monitor",
			"payload": map[string]interface{}{"command": c},
		})
	}
	cmd("PAUSE")
	cmd("RESUME")
	cmd("KOVER")
	cmd("ABORT")
	cmd("RESET")
}

func TestDoKover(t *testing.T) {
	mem := testutil.NewMemoryBus()
	ctx := context.Background()
	prefix := testutil.TopicPrefix()
	a := New(testutil.Config("autopilot"), mem)
	a.motorsTopic = prefix + ".motors"
	a.koverActive = true
	a.state = StateExecuting
	_ = mem.Subscribe(ctx, prefix+".motors", func(map[string]interface{}) {})
	a.doKover(ctx, map[string]interface{}{"lat": 1.0, "lon": 2.0, "alt_m": 10.0, "heading_deg": 0.0})
}
