package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/AMCP-Drones/drones/src/journal"
	securitymonitor "github.com/AMCP-Drones/drones/src/security_monitor"
	"github.com/AMCP-Drones/drones/tests/testutil"
)

func TestModule_SecurityMonitor_ProxyPublishDeniedWithoutPolicy(t *testing.T) {
	t.Setenv("SECURITY_POLICIES", "")
	t.Cleanup(func() { _ = os.Unsetenv("SECURITY_POLICIES") })

	mem := testutil.NewMemoryBus()
	ctx := context.Background()
	cfg := testutil.Config("security_monitor")
	sm := securitymonitor.New(cfg, mem)
	if err := sm.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sm.Stop(ctx) }()

	journalTopic := testutil.Config("journal").BrokerTopicFor("journal")
	calledCh := make(chan struct{}, 1)
	_ = mem.Subscribe(ctx, journalTopic, func(map[string]interface{}) {
		select {
		case calledCh <- struct{}{}:
		default:
		}
	})

	// No reply_to: denied proxy returns (nil, nil) and the monitor does not send a response.
	err := mem.Publish(ctx, cfg.BrokerTopicFor("security_monitor"), map[string]interface{}{
		"action": "proxy_publish",
		"sender": "autopilot",
		"payload": map[string]interface{}{
			"target": map[string]interface{}{"topic": journalTopic, "action": "LOG_EVENT"},
			"data": map[string]interface{}{
				"event": "X", "source": "t",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Wait briefly to ensure no handler is called
	select {
	case <-calledCh:
		t.Fatal("journal handler should not run")
	case <-time.After(100 * time.Millisecond):
		// Expected: no call received
	}
}

func TestModule_SecurityMonitor_ProxyPublishForwardsToJournal(t *testing.T) {
	prefix := testutil.TopicPrefix()
	journalTopic := prefix + ".journal"
	policy := []map[string]string{{
		"sender": "autopilot", "topic": journalTopic, "action": "LOG_EVENT",
	}}
	raw, _ := json.Marshal(policy)
	t.Setenv("SECURITY_POLICIES", string(raw))
	t.Cleanup(func() { _ = os.Unsetenv("SECURITY_POLICIES") })

	mem := testutil.NewMemoryBus()
	ctx := context.Background()
	dir := t.TempDir()
	path := fmt.Sprintf("%s/j.ndjson", dir)
	t.Setenv("JOURNAL_FILE_PATH", path)
	t.Cleanup(func() { _ = os.Unsetenv("JOURNAL_FILE_PATH") })

	journalCfg := testutil.Config("journal")
	j := journal.New(journalCfg, mem)
	if err := j.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = j.Stop(ctx) }()

	smCfg := testutil.Config("security_monitor")
	sm := securitymonitor.New(smCfg, mem)
	if err := sm.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sm.Stop(ctx) }()

	resp, err := mem.Request(ctx, smCfg.BrokerTopicFor("security_monitor"), map[string]interface{}{
		"action": "proxy_publish",
		"sender": "autopilot",
		"payload": map[string]interface{}{
			"target": map[string]interface{}{"topic": journalTopic, "action": "LOG_EVENT"},
			"data": map[string]interface{}{
				"event": "SM_TEST", "source": "t",
			},
		},
	}, 2.0)
	if err != nil {
		t.Fatal(err)
	}
	pl, _ := resp["payload"].(map[string]interface{})
	if pl == nil || pl["published"] != true {
		t.Fatalf("expected published true, got %#v", resp)
	}
	b, err := os.ReadFile(path)
	if err != nil || len(b) == 0 {
		t.Fatalf("journal file: %v %q", err, string(b))
	}
}
