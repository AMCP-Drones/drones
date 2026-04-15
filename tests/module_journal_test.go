package tests

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/AMCP-Drones/drones/components/journal/src"
	"github.com/AMCP-Drones/drones/tests/testutil"
)

func TestModule_Journal_LOG_EVENT_WritesNDJSON(t *testing.T) {
	mem := testutil.NewMemoryBus()
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "events.ndjson")
	t.Setenv("JOURNAL_FILE_PATH", path)
	t.Cleanup(func() { _ = os.Unsetenv("JOURNAL_FILE_PATH") })

	cfg := testutil.Config("journal")
	j := journal.New(cfg, mem)
	if err := j.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = j.Stop(ctx) }()

	err := mem.Publish(ctx, cfg.BrokerTopicFor("journal"), map[string]interface{}{
		"action": "LOG_EVENT",
		"sender": "security_monitor",
		"payload": map[string]interface{}{
			"event":   "TEST_EVENT",
			"source":  "unit",
			"details": map[string]interface{}{"n": 1},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) == 0 || b[len(b)-1] != '\n' {
		t.Fatalf("expected non-empty NDJSON line, got %q", string(b))
	}
}

func TestModule_Journal_RejectsUntrustedSender(t *testing.T) {
	mem := testutil.NewMemoryBus()
	ctx := context.Background()
	dir := t.TempDir()
	t.Setenv("JOURNAL_FILE_PATH", filepath.Join(dir, "j.ndjson"))
	t.Cleanup(func() { _ = os.Unsetenv("JOURNAL_FILE_PATH") })

	cfg := testutil.Config("journal")
	j := journal.New(cfg, mem)
	if err := j.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = j.Stop(ctx) }()

	_ = mem.Publish(ctx, cfg.BrokerTopicFor("journal"), map[string]interface{}{
		"action": "LOG_EVENT",
		"sender": "intruder",
		"payload": map[string]interface{}{
			"event": "BAD",
		},
	})
	b, _ := os.ReadFile(filepath.Join(dir, "j.ndjson"))
	if len(b) > 0 {
		t.Fatalf("expected no write for untrusted sender, file: %q", string(b))
	}
}
