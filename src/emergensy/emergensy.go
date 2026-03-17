// Package emergensy implements the emergency protocol: on limiter_event (EMERGENCY_LAND_REQUIRED) starts isolation, closes cargo, commands LAND to motors, logs to journal.
package emergensy

import (
	"context"
	"log"
	"os"

	"github.com/AMCP-Drones/drones/src/bus"
	"github.com/AMCP-Drones/drones/src/component"
	"github.com/AMCP-Drones/drones/src/config"
)

// Emergensy handles limiter_event (EMERGENCY_LAND_REQUIRED): isolation, cargo close, motors LAND, journal.
type Emergensy struct {
	*component.BaseComponent
	systemName       string
	secMonitorTopic  string
	journalTopic     string
	motorsTopic      string
	cargoTopic       string
	active           bool
}

// New creates an Emergensy component. Call Start after creation.
func New(cfg *config.Config, b bus.Bus) *Emergensy {
	systemName := cfg.SystemName
	if systemName == "" {
		systemName = "deliverydron"
	}
	topic := cfg.ComponentTopic
	if topic == "" {
		topic = config.TopicFor(systemName, "emergensy")
	}
	base := component.NewBaseComponent(cfg.ComponentID, "emergensy", topic, b)
	secTopic := os.Getenv("SECURITY_MONITOR_TOPIC")
	if secTopic == "" {
		secTopic = config.TopicFor(systemName, "security_monitor")
	}
	journalTopic := config.TopicFor(systemName, "journal")
	motorsTopic := config.TopicFor(systemName, "motors")
	cargoTopic := config.TopicFor(systemName, "cargo")
	e := &Emergensy{
		BaseComponent:   base,
		systemName:      systemName,
		secMonitorTopic: secTopic,
		journalTopic:    journalTopic,
		motorsTopic:     motorsTopic,
		cargoTopic:      cargoTopic,
		active:          false,
	}
	e.registerHandlers()
	return e
}

func (e *Emergensy) registerHandlers() {
	e.RegisterHandler("limiter_event", e.handleLimiterEvent)
	e.RegisterHandler("get_state", e.handleGetState)
}

func (e *Emergensy) handleLimiterEvent(ctx context.Context, message map[string]interface{}) (map[string]interface{}, error) {
	if !component.IsTrustedSender(message, "security_monitor") {
		return nil, nil
	}
	payload, _ := message["payload"].(map[string]interface{})
	if payload == nil {
		return map[string]interface{}{"ok": false, "ignored": true}, nil
	}
	event, _ := payload["event"].(string)
	if event != "EMERGENCY_LAND_REQUIRED" {
		return map[string]interface{}{"ok": false, "ignored": true}, nil
	}
	missionID, _ := payload["mission_id"].(string)
	details, _ := payload["details"].(map[string]interface{})
	if details == nil {
		details = map[string]interface{}{}
	}
	e.active = true

	// 1. ISOLATION_START to security_monitor
	isolationMsg := map[string]interface{}{
		"action":  "ISOLATION_START",
		"sender":  e.ComponentID,
		"payload": map[string]interface{}{"reason": "LIMITER_EMERGENCY", "mission_id": missionID},
	}
	if err := e.Bus.Publish(ctx, e.secMonitorTopic, isolationMsg); err != nil {
		log.Printf("[%s] ISOLATION_START: %v", e.ComponentID, err)
	}

	// 2. Cargo CLOSE via proxy_publish
	cargoMsg := map[string]interface{}{
		"action": "proxy_publish",
		"sender": e.ComponentID,
		"payload": map[string]interface{}{
			"target": map[string]interface{}{"topic": e.cargoTopic, "action": "CLOSE"},
			"data":   map[string]interface{}{"reason": "emergency"},
		},
	}
	if err := e.Bus.Publish(ctx, e.secMonitorTopic, cargoMsg); err != nil {
		log.Printf("[%s] cargo CLOSE: %v", e.ComponentID, err)
	}

	// 3. Motors LAND via proxy_publish
	motorsMsg := map[string]interface{}{
		"action": "proxy_publish",
		"sender": e.ComponentID,
		"payload": map[string]interface{}{
			"target": map[string]interface{}{"topic": e.motorsTopic, "action": "LAND"},
			"data":   map[string]interface{}{"mode": "AUTO_LAND", "reason": "emergency"},
		},
	}
	if err := e.Bus.Publish(ctx, e.secMonitorTopic, motorsMsg); err != nil {
		log.Printf("[%s] motors LAND: %v", e.ComponentID, err)
	}

	// 4. Journal LOG_EVENT via proxy_publish
	journalMsg := map[string]interface{}{
		"action": "proxy_publish",
		"sender": e.ComponentID,
		"payload": map[string]interface{}{
			"target": map[string]interface{}{"topic": e.journalTopic, "action": "LOG_EVENT"},
			"data": map[string]interface{}{
				"event":      "EMERGENCY_PROTOCOL_STARTED",
				"mission_id": missionID,
				"details":    details,
			},
		},
	}
	if err := e.Bus.Publish(ctx, e.secMonitorTopic, journalMsg); err != nil {
		log.Printf("[%s] journal LOG_EVENT: %v", e.ComponentID, err)
	}

	return map[string]interface{}{"ok": true}, nil
}

func (e *Emergensy) handleGetState(_ context.Context, _ map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{"active": e.active}, nil
}
