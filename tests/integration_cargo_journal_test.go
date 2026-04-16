package tests

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/AMCP-Drones/drones/systems/deliverydron/cargo/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/journal/src"
	securitymonitor "github.com/AMCP-Drones/drones/systems/deliverydron/security_monitor/src"
	"github.com/AMCP-Drones/drones/tests/testutil"
)

// Integration: cargo OPEN triggers proxy_publish through security_monitor to journal (policy-gated).
func TestIntegration_CargoOpen_JournalLog(t *testing.T) {
	prefix := testutil.TopicPrefix()
	journalTopic := prefix + ".journal"
	cargoTopic := prefix + ".cargo"

	policies := []map[string]string{
		{"sender": "cargo", "topic": journalTopic, "action": "LOG_EVENT"},
	}
	raw, _ := json.Marshal(policies)
	t.Setenv("SECURITY_POLICIES", string(raw))
	t.Cleanup(func() { _ = os.Unsetenv("SECURITY_POLICIES") })
	path := t.TempDir() + "/j.ndjson"
	t.Setenv("JOURNAL_FILE_PATH", path)
	t.Cleanup(func() { _ = os.Unsetenv("JOURNAL_FILE_PATH") })

	mem := testutil.NewMemoryBus()
	ctx := context.Background()

	j := journal.New(testutil.Config("journal"), mem)
	if err := j.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = j.Stop(ctx) }()

	sm := securitymonitor.New(testutil.Config("security_monitor"), mem)
	if err := sm.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sm.Stop(ctx) }()

	c := cargo.New(testutil.Config("cargo"), mem)
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Stop(ctx) }()

	_ = mem.Publish(ctx, cargoTopic, map[string]interface{}{
		"action":  "OPEN",
		"sender":  "security_monitor",
		"payload": map[string]interface{}{},
	})

	b, err := os.ReadFile(path)
	if err != nil || len(b) == 0 {
		t.Fatalf("expected journal line: err=%v data=%q", err, string(b))
	}
}
