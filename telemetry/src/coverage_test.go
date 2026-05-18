package telemetry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AMCP-Drones/drones/systems/deliverydron/tests/testutil"
)

func TestMapFloatAndAsFloat(t *testing.T) {
	m := map[string]interface{}{
		"lat": float32(55.5), "alt_m": 10, "heading_deg": int64(90),
	}
	if v, ok := mapFloat(m, "lat"); !ok || v != 55.5 {
		t.Fatalf("lat %v %v", v, ok)
	}
	if _, ok := mapFloat(nil, "x"); ok {
		t.Fatal("nil map")
	}
	if _, ok := asFloat("bad"); ok {
		t.Fatal("bad type")
	}
}

func TestCopyMap_Nil(t *testing.T) {
	if copyMap(nil) != nil {
		t.Fatal("expected nil")
	}
}

func TestPostAnalyticsTelemetry_MotorsFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	t.Setenv("ANALYTICS_ENABLED", "1")
	t.Setenv("ANALYTICS_BASE_URL", srv.URL)
	t.Setenv("ANALYTICS_API_KEY", "k")
	mem := testutil.NewMemoryBus()
	tel := New(testutil.Config("telemetry"), mem)
	motors := map[string]interface{}{
		"last_target": map[string]interface{}{"lat": 55.1, "lon": 37.1, "alt_m": 60.0},
	}
	tel.postAnalyticsTelemetry(context.Background(), 1, nil, motors, nil)
}

func TestPostAnalyticsTelemetry_FullPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	t.Setenv("ANALYTICS_ENABLED", "1")
	t.Setenv("ANALYTICS_BASE_URL", srv.URL)
	t.Setenv("ANALYTICS_API_KEY", "k")

	mem := testutil.NewMemoryBus()
	ctx := context.Background()
	prefix := testutil.TopicPrefix()
	cfg := testutil.Config("telemetry")
	tel := New(cfg, mem)
	tel.journalTopic = prefix + ".journal"
	_ = mem.Subscribe(ctx, prefix+".journal", func(map[string]interface{}) {})

	nav := map[string]interface{}{"lat": 55.0, "lon": 37.0, "alt_m": 50.0, "heading_deg": 180.0}
	motors := map[string]interface{}{
		"last_target": map[string]interface{}{"lat": 55.1, "lon": 37.1, "alt_m": 60.0},
	}
	cargo := map[string]interface{}{"battery_pct": 88.0}
	tel.postAnalyticsTelemetry(ctx, 1, nav, motors, cargo)
}

func TestHandleGetState_Untrusted(t *testing.T) {
	tel := &Telemetry{}
	if pl, _ := tel.handleGetState(context.Background(), map[string]interface{}{"sender": "x"}); pl != nil {
		t.Fatalf("%#v", pl)
	}
}

func TestProxyGetState_Error(t *testing.T) {
	mem := testutil.NewMemoryBus()
	cfg := testutil.Config("telemetry")
	tel := New(cfg, mem)
	if v := tel.proxyGetState(context.Background(), "missing.topic", "get_state"); v != nil {
		t.Fatal("expected nil on error")
	}
}

func TestStart_PollLoop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	t.Setenv("ANALYTICS_ENABLED", "1")
	t.Setenv("ANALYTICS_BASE_URL", srv.URL)
	t.Setenv("ANALYTICS_API_KEY", "k")
	t.Setenv("TELEMETRY_POLL_INTERVAL_S", "0.05")

	mem := testutil.NewMemoryBus()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	prefix := testutil.TopicPrefix()
	cfg := testutil.Config("telemetry")
	tel := New(cfg, mem)
	tel.navigationTopic = prefix + ".navigation"
	tel.motorsTopic = prefix + ".motors"
	tel.cargoTopic = prefix + ".cargo"
	tel.journalTopic = prefix + ".journal"
	nav := map[string]interface{}{"lat": 55.0, "lon": 37.0, "alt_m": 10.0, "heading_deg": 0.0}
	reply := func(msg map[string]interface{}) {
		replyTo, _ := msg["reply_to"].(string)
		cid, _ := msg["correlation_id"].(string)
		_ = mem.Publish(ctx, replyTo, map[string]interface{}{
			"action": "response", "success": true, "correlation_id": cid,
			"payload": nav,
		})
	}
	_ = mem.Subscribe(ctx, prefix+".navigation", reply)
	_ = mem.Subscribe(ctx, prefix+".motors", reply)
	_ = mem.Subscribe(ctx, prefix+".cargo", func(msg map[string]interface{}) {
		replyTo, _ := msg["reply_to"].(string)
		cid, _ := msg["correlation_id"].(string)
		_ = mem.Publish(ctx, replyTo, map[string]interface{}{
			"action": "response", "success": true, "correlation_id": cid,
			"payload": map[string]interface{}{"battery_pct": 90.0},
		})
	})
	_ = mem.Subscribe(ctx, prefix+".journal", func(map[string]interface{}) {})

	if err := tel.Start(ctx); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	_ = tel.Stop(ctx)

	if v, ok := asFloat(float32(1.5)); !ok || v != 1.5 {
		t.Fatalf("float32 %v %v", v, ok)
	}
	if _, ok := asFloat(int64(2)); !ok {
		t.Fatal("int64")
	}
}
