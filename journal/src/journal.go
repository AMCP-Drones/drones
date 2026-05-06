// Package journal implements the append-only event log (LOG_EVENT, NDJSON file).
package journal

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/AMCP-Drones/drones/systems/deliverydron/bus/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/component/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/config/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/sdk/src"
)

// Journal implements append-only NDJSON event log. Accepts LOG_EVENT only from security_monitor.
type Journal struct {
	*component.BaseComponent
	filePath   string
	analytics  *sdk.AnalyticsClient
	mu         sync.Mutex
}

// New creates a Journal. Call Start after creation.
func New(cfg *config.Config, b bus.Bus) *Journal {
	topic := cfg.ComponentTopic
	if topic == "" {
		topic = cfg.BrokerTopicFor("journal")
	}
	base := component.NewBaseComponent(cfg.ComponentID, "journal", topic, b)
	filePath := os.Getenv("JOURNAL_FILE_PATH")
	if filePath == "" {
		filePath = "/data/deliverydron_journal.ndjson"
	}
	j := &Journal{
		BaseComponent: base,
		filePath:      filePath,
		analytics:     sdk.NewAnalyticsClientFromEnv(),
	}
	j.registerHandlers()
	return j
}

func (j *Journal) registerHandlers() {
	j.RegisterHandler("LOG_EVENT", j.handleLogEvent)
}

func (j *Journal) handleLogEvent(_ context.Context, message map[string]interface{}) (map[string]interface{}, error) {
	if !component.IsTrustedSender(message, "security_monitor") {
		return nil, nil
	}
	payload, _ := message["payload"].(map[string]interface{})
	if payload == nil {
		return map[string]interface{}{"ok": false, "error": "invalid_payload"}, nil
	}
	source, _ := message["sender"].(string)
	if s, ok := payload["source"].(string); ok && s != "" {
		source = s
	}
	event, _ := payload["event"].(string)
	if event == "" {
		event = "UNKNOWN"
	}
	record := map[string]interface{}{
		"timestamp":        time.Now().UTC().Format(time.RFC3339Nano),
		"source_component": source,
		"source_action":    "LOG_EVENT",
		"event":            event,
		"payload":          payload,
	}
	line, err := json.Marshal(record)
	if err != nil {
		record["payload"] = map[string]interface{}{"error": "non-serializable payload: " + err.Error()}
		line, _ = json.Marshal(record)
	}
	line = append(line, '\n')

	dir := filepath.Dir(j.filePath)
	if dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Printf("[%s] failed to create journal dir: %v", j.ComponentID, err)
			return map[string]interface{}{"ok": false, "error": "write_failed"}, nil
		}
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	f, err := os.OpenFile(j.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("[%s] failed to open journal: %v", j.ComponentID, err)
		return map[string]interface{}{"ok": false, "error": "write_failed"}, nil
	}
	defer func() { _ = f.Close() }()
	_, err = f.Write(line)
	if err != nil {
		log.Printf("[%s] failed to write journal: %v", j.ComponentID, err)
		return map[string]interface{}{"ok": false, "error": "write_failed"}, nil
	}
	j.postAnalyticsEvent(context.Background(), source, event, payload)
	return map[string]interface{}{"ok": true}, nil
}

func (j *Journal) postAnalyticsEvent(ctx context.Context, source, event string, payload map[string]interface{}) {
	if !j.analytics.Enabled() {
		return
	}
	severity := "info"
	if s, ok := payload["severity"].(string); ok && s != "" {
		severity = s
	}
	eventType := "event"
	upper := strings.ToUpper(event)
	if strings.Contains(upper, "EMERGENCY") || strings.Contains(upper, "ISOLATION") || strings.Contains(upper, "LIMITER") {
		eventType = "safety_event"
	}
	message := event
	if source != "" {
		message = message + " source=" + source
	}
	item := sdk.EventLog{
		APIVersion: j.analytics.APIVersion(),
		Timestamp:  time.Now().UnixMilli(),
		EventType:  eventType,
		Service:    j.analytics.Service(),
		ServiceID:  j.analytics.ServiceID(),
		Severity:   severity,
		Message:    message,
	}
	if err := j.analytics.PostEvent(ctx, []sdk.EventLog{item}); err != nil {
		log.Printf("[%s] analytics event post: %v", j.ComponentID, err)
	}
}
