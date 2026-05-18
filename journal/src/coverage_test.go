package journal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/AMCP-Drones/drones/systems/deliverydron/sdk/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/tests/testutil"
)

func TestInferSeverityAndEventType(t *testing.T) {
	if inferEventType("ORVD_EMERGENCY") != "safety_event" {
		t.Fatal("event type")
	}
	if inferSeverity("AUTOPILOT_ABORT", nil) != "emergency" {
		t.Fatal("abort severity")
	}
	if inferSeverity("DEVIATION_WARNING", nil) != "warning" {
		t.Fatal("warning")
	}
	if inferSeverity("INFO", map[string]interface{}{"severity": "Critical"}) != "critical" {
		t.Fatal("payload severity")
	}
	msg := buildAnalyticsMessage("src", "EVT", map[string]interface{}{
		"mission_id": "m1",
		"details":    map[string]interface{}{"k": 1},
	})
	if msg == "" {
		t.Fatal("empty message")
	}
}

func TestHandlePostTelemetry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	t.Setenv("ANALYTICS_ENABLED", "1")
	t.Setenv("ANALYTICS_BASE_URL", srv.URL)
	t.Setenv("ANALYTICS_API_KEY", "k")

	mem := testutil.NewMemoryBus()
	ctx := context.Background()
	dir := t.TempDir()
	t.Setenv("JOURNAL_FILE_PATH", filepath.Join(dir, "j.ndjson"))
	j := New(testutil.Config("journal"), mem)
	_ = j.Start(ctx)
	defer func() { _ = j.Stop(ctx) }()

	pl, err := j.handlePostTelemetry(ctx, map[string]interface{}{
		"sender": "security_monitor",
		"payload": map[string]interface{}{
			"telemetry_log": sdk.TelemetryLog{Latitude: 1, Longitude: 2},
		},
	})
	if err != nil || pl["ok"] != true {
		t.Fatalf("%#v err=%v", pl, err)
	}

	pl2, _ := j.handlePostTelemetry(ctx, map[string]interface{}{
		"sender":  "security_monitor",
		"payload": map[string]interface{}{},
	})
	if pl2["error"] != "missing_telemetry_log" {
		t.Fatalf("%#v", pl2)
	}

	pl3, _ := j.handlePostTelemetry(ctx, map[string]interface{}{
		"sender":  "intruder",
		"payload": map[string]interface{}{"telemetry_log": sdk.TelemetryLog{}},
	})
	if pl3 != nil {
		t.Fatalf("expected nil for untrusted: %#v", pl3)
	}
}

func TestPostAnalyticsEvent_Disabled(t *testing.T) {
	_ = os.Unsetenv("ANALYTICS_ENABLED")
	mem := testutil.NewMemoryBus()
	j := New(testutil.Config("journal"), mem)
	j.postAnalyticsEvent(context.Background(), "s", "E", map[string]interface{}{})
}

func TestHandleLogEvent_AnalyticsEnabled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	t.Setenv("ANALYTICS_ENABLED", "1")
	t.Setenv("ANALYTICS_BASE_URL", srv.URL)
	t.Setenv("ANALYTICS_API_KEY", "k")

	mem := testutil.NewMemoryBus()
	ctx := context.Background()
	dir := t.TempDir()
	t.Setenv("JOURNAL_FILE_PATH", filepath.Join(dir, "evt.ndjson"))
	j := New(testutil.Config("journal"), mem)
	_ = j.Start(ctx)
	defer func() { _ = j.Stop(ctx) }()

	pl, err := j.handleLogEvent(ctx, map[string]interface{}{
		"sender": "security_monitor",
		"payload": map[string]interface{}{
			"source":  "autopilot",
			"event":   "AUTOPILOT_ABORT",
			"payload": map[string]interface{}{"mission_id": "m1"},
		},
	})
	if err != nil || pl["ok"] != true {
		t.Fatalf("%#v err=%v", pl, err)
	}
}
