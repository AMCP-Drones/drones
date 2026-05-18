package sdk

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestAnalyticsClient_DisabledByDefault(t *testing.T) {
	_ = os.Unsetenv("ANALYTICS_ENABLED")
	c := NewAnalyticsClientFromEnv()
	if c.Enabled() {
		t.Fatal("expected disabled without env")
	}
	if err := c.PostEvent(context.Background(), []EventLog{{Message: "x"}}); err != nil {
		t.Fatal(err)
	}
}

func TestAnalyticsClient_EnabledPostEventAndTelemetry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "secret" {
			t.Errorf("api key=%q", r.Header.Get("X-API-Key"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	t.Setenv("ANALYTICS_ENABLED", "1")
	t.Setenv("ANALYTICS_BASE_URL", srv.URL)
	t.Setenv("ANALYTICS_API_KEY", "secret")
	t.Setenv("ANALYTICS_DRONE", "d1")
	t.Setenv("ANALYTICS_DRONE_ID", "42")
	t.Setenv("ANALYTICS_SERVICE", "svc")
	t.Setenv("ANALYTICS_SERVICE_ID", "7")

	c := NewAnalyticsClientFromEnv()
	if !c.Enabled() {
		t.Fatal("expected enabled")
	}
	if c.Drone() != "d1" || c.DroneID() != 42 || c.Service() != "svc" || c.ServiceID() != 7 {
		t.Fatalf("accessors: drone=%s id=%d svc=%s sid=%d", c.Drone(), c.DroneID(), c.Service(), c.ServiceID())
	}
	batt := 80
	if err := c.PostTelemetry(context.Background(), []TelemetryLog{{
		Latitude: 1, Longitude: 2, Battery: &batt,
	}}); err != nil {
		t.Fatal(err)
	}
	if err := c.PostEvent(context.Background(), []EventLog{{Message: "hello"}}); err != nil {
		t.Fatal(err)
	}
}

func TestAnalyticsClient_PostErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()
	t.Setenv("ANALYTICS_ENABLED", "1")
	t.Setenv("ANALYTICS_BASE_URL", srv.URL)
	t.Setenv("ANALYTICS_API_KEY", "k")
	c := NewAnalyticsClientFromEnv()
	if err := c.PostEvent(context.Background(), []EventLog{{Message: "x"}}); err == nil {
		t.Fatal("expected error on bad status")
	}
}

func TestAnalyticsClient_NilSafeAccessors(t *testing.T) {
	var c *AnalyticsClient
	if c.APIVersion() == "" || c.Drone() == "" || c.DroneID() == 0 {
		t.Fatal("nil client accessors")
	}
}
