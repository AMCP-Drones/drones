package missionhandler

import (
	"context"
	"testing"

	securitymonitor "github.com/AMCP-Drones/drones/systems/deliverydron/security_monitor/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/tests/testutil"
)

func TestParseMission_WPLAndJSON(t *testing.T) {
	m := New(testutil.Config("mission_handler"), testutil.NewMemoryBus())
	wpl := "QGC WPL 110\n" +
		"0\t1\t0\t16\t0\t0\t0\t0\t55.75\t37.62\t100\t1\n" +
		"1\t0\t0\t16\t0\t0\t0\t0\t55.76\t37.63\t120\t1\n"
	mission, err := m.parseMission(map[string]interface{}{
		"wpl_content": wpl,
		"mission_id":  "m-wpl",
	})
	if mission == nil || err != "" {
		t.Fatalf("mission=%#v err=%q", mission, err)
	}
	mission2, err2 := m.parseMission(map[string]interface{}{
		"mission": map[string]interface{}{
			"mission_id": "m-json",
			"steps":      []interface{}{map[string]interface{}{"lat": 1.0, "lon": 2.0, "alt_m": 3.0}},
		},
	})
	if mission2 == nil || err2 != "" {
		t.Fatalf("json mission=%#v err=%q", mission2, err2)
	}
	_, err3 := m.parseMission(map[string]interface{}{"foo": "bar"})
	if err3 != "invalid_input_wpl_or_mission_required" {
		t.Fatalf("err=%q", err3)
	}
}

func TestHandleLoadMission_Success(t *testing.T) {
	ctx := context.Background()
	mem := testutil.NewMemoryBus()
	prefix := testutil.TopicPrefix()
	t.Setenv("SECURITY_POLICIES", `[
		{"sender":"mission_handler","topic":"`+prefix+`.limiter","action":"mission_load"},
		{"sender":"mission_handler","topic":"`+prefix+`.autopilot","action":"mission_load"},
		{"sender":"mission_handler","topic":"`+prefix+`.journal","action":"LOG_EVENT"}
	]`)
	sm := securitymonitor.New(testutil.Config("security_monitor"), mem)
	_ = sm.Start(ctx)
	defer func() { _ = sm.Stop(ctx) }()

	m := New(testutil.Config("mission_handler"), mem)
	_ = m.Start(ctx)
	defer func() { _ = m.Stop(ctx) }()
	_ = mem.Subscribe(ctx, prefix+".journal", func(map[string]interface{}) {})
	replyOK := func(msg map[string]interface{}) {
		replyTo, _ := msg["reply_to"].(string)
		cid, _ := msg["correlation_id"].(string)
		_ = mem.Publish(ctx, replyTo, map[string]interface{}{
			"action": "response", "success": true, "correlation_id": cid,
			"payload": map[string]interface{}{"ok": true},
		})
	}
	_ = mem.Subscribe(ctx, prefix+".limiter", replyOK)
	_ = mem.Subscribe(ctx, prefix+".autopilot", replyOK)

	pl, err := m.handleLoadMission(ctx, map[string]interface{}{
		"sender": "security_monitor",
		"payload": map[string]interface{}{
			"mission": map[string]interface{}{
				"mission_id": "m-load",
				"steps":      []interface{}{map[string]interface{}{"lat": 1.0, "lon": 2.0, "alt_m": 3.0}},
			},
		},
	})
	if err != nil || pl["ok"] != true {
		t.Fatalf("%#v err=%v", pl, err)
	}
}

func TestHandleValidateOnly_ValidMission(t *testing.T) {
	m := New(testutil.Config("mission_handler"), testutil.NewMemoryBus())
	pl, err := m.handleValidateOnly(context.Background(), map[string]interface{}{
		"sender": "security_monitor",
		"payload": map[string]interface{}{
			"mission": map[string]interface{}{
				"mission_id": "ok",
				"steps":      []interface{}{map[string]interface{}{"lat": 1.0, "lon": 2.0, "alt_m": 3.0}},
			},
		},
	})
	if err != nil || pl["ok"] != true {
		t.Fatalf("%#v err=%v", pl, err)
	}
}

func TestHandleLoadMission_LimiterReject(t *testing.T) {
	ctx := context.Background()
	mem := testutil.NewMemoryBus()
	prefix := testutil.TopicPrefix()
	t.Setenv("SECURITY_POLICIES", `[{"sender":"mission_handler","topic":"`+prefix+`.limiter","action":"mission_load"},{"sender":"mission_handler","topic":"`+prefix+`.journal","action":"LOG_EVENT"}]`)
	sm := securitymonitor.New(testutil.Config("security_monitor"), mem)
	_ = sm.Start(ctx)
	defer func() { _ = sm.Stop(ctx) }()
	m := New(testutil.Config("mission_handler"), mem)
	_ = m.Start(ctx)
	defer func() { _ = m.Stop(ctx) }()
	_ = mem.Subscribe(ctx, prefix+".journal", func(map[string]interface{}) {})
	_ = mem.Subscribe(ctx, prefix+".limiter", func(msg map[string]interface{}) {
		replyTo, _ := msg["reply_to"].(string)
		cid, _ := msg["correlation_id"].(string)
		_ = mem.Publish(ctx, replyTo, map[string]interface{}{
			"action": "response", "success": true, "correlation_id": cid,
			"payload": map[string]interface{}{"ok": false, "error": "rejected"},
		})
	})
	pl, _ := m.handleLoadMission(ctx, map[string]interface{}{
		"sender": "security_monitor",
		"payload": map[string]interface{}{
			"mission": map[string]interface{}{
				"mission_id": "bad",
				"steps":      []interface{}{map[string]interface{}{"lat": 1.0, "lon": 2.0, "alt_m": 3.0}},
			},
		},
	})
	if pl["ok"] == true {
		t.Fatalf("%#v", pl)
	}
}

func TestValidateMission_AllErrors(t *testing.T) {
	cases := []struct {
		mission map[string]interface{}
		err     string
	}{
		{nil, "mission_not_dict"},
		{map[string]interface{}{"steps": []interface{}{}}, "invalid_mission_id"},
		{map[string]interface{}{"mission_id": "x", "steps": []interface{}{}}, "empty_steps"},
		{map[string]interface{}{"mission_id": "x", "steps": []interface{}{"bad"}}, "invalid_step_0"},
		{map[string]interface{}{"mission_id": "x", "steps": []interface{}{map[string]interface{}{"lon": 1.0, "alt_m": 2.0}}}, "missing_lat_in_step_0"},
		{map[string]interface{}{"mission_id": "x", "steps": []interface{}{map[string]interface{}{"lat": 1.0, "alt_m": 2.0}}}, "missing_lon_in_step_0"},
		{map[string]interface{}{"mission_id": "x", "steps": []interface{}{map[string]interface{}{"lat": 1.0, "lon": 2.0}}}, "missing_alt_m_in_step_0"},
	}
	for _, tc := range cases {
		ok, err := validateMission(tc.mission)
		if ok || err != tc.err {
			t.Fatalf("mission=%#v ok=%v err=%q want %q", tc.mission, ok, err, tc.err)
		}
	}
}

func TestHandleGetState_AndValidateWPL(t *testing.T) {
	m := New(testutil.Config("mission_handler"), testutil.NewMemoryBus())
	st, _ := m.handleGetState(context.Background(), map[string]interface{}{
		"sender": "security_monitor",
	})
	if st == nil {
		t.Fatal("nil state")
	}
	pl, _ := m.handleValidateOnly(context.Background(), map[string]interface{}{
		"sender": "security_monitor",
		"payload": map[string]interface{}{
			"wpl_content": "bad\n1 2 3\n",
			"mission_id":  "x",
		},
	})
	if pl["ok"] == true {
		t.Fatalf("%#v", pl)
	}
}

func TestHandleLoadMission_AutopilotNoResponse(t *testing.T) {
	ctx := context.Background()
	mem := testutil.NewMemoryBus()
	prefix := testutil.TopicPrefix()
	t.Setenv("SECURITY_POLICIES", `[
		{"sender":"mission_handler","topic":"`+prefix+`.limiter","action":"mission_load"},
		{"sender":"mission_handler","topic":"`+prefix+`.autopilot","action":"mission_load"},
		{"sender":"mission_handler","topic":"`+prefix+`.journal","action":"LOG_EVENT"}
	]`)
	sm := securitymonitor.New(testutil.Config("security_monitor"), mem)
	_ = sm.Start(ctx)
	defer func() { _ = sm.Stop(ctx) }()
	m := New(testutil.Config("mission_handler"), mem)
	_ = m.Start(ctx)
	defer func() { _ = m.Stop(ctx) }()
	_ = mem.Subscribe(ctx, prefix+".journal", func(map[string]interface{}) {})
	_ = mem.Subscribe(ctx, prefix+".limiter", func(msg map[string]interface{}) {
		replyTo, _ := msg["reply_to"].(string)
		cid, _ := msg["correlation_id"].(string)
		_ = mem.Publish(ctx, replyTo, map[string]interface{}{
			"action": "response", "success": true, "correlation_id": cid,
			"payload": map[string]interface{}{"ok": true},
		})
	})
	pl, _ := m.handleLoadMission(ctx, map[string]interface{}{
		"sender": "security_monitor",
		"payload": map[string]interface{}{
			"mission": map[string]interface{}{
				"mission_id": "m-ap",
				"steps":      []interface{}{map[string]interface{}{"lat": 1.0, "lon": 2.0, "alt_m": 3.0}},
			},
		},
	})
	if pl["error"] != "autopilot_no_response" {
		t.Fatalf("%#v", pl)
	}
}

func TestHandleValidateOnly_InvalidStep(t *testing.T) {
	m := New(testutil.Config("mission_handler"), testutil.NewMemoryBus())
	pl, _ := m.handleValidateOnly(context.Background(), map[string]interface{}{
		"sender": "security_monitor",
		"payload": map[string]interface{}{
			"mission": map[string]interface{}{
				"mission_id": "bad",
				"steps":      []interface{}{map[string]interface{}{"lat": 1}},
			},
		},
	})
	if pl["ok"] == true {
		t.Fatalf("%#v", pl)
	}
}
