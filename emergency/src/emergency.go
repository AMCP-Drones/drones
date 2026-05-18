// Package emergency implements the emergency protocol: on limiter_event (EMERGENCY_LAND_REQUIRED) starts isolation, closes cargo, commands LAND to motors, logs to journal.
package emergency

import (
	"context"
	"log"
	"os"
	"sync"
	"time"

	"github.com/AMCP-Drones/drones/systems/deliverydron/bus/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/component/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/config/src"
)

// Emergency handles limiter_event (EMERGENCY_LAND_REQUIRED): isolation, cargo close, motors LAND, journal.
type Emergency struct {
	*component.BaseComponent
	systemName      string
	secMonitorTopic string
	journalTopic    string
	motorsTopic     string
	cargoTopic      string
	proxy           *component.ProxyClient
	audit           *component.AuditLogger
	droneport              droneportConfig
	dpMu                   sync.Mutex
	droneportPhase         string
	droneportPortID        string
	droneportLastError     string
	droneportLastMissionID string
	droneportChargeWaitAt  time.Time
	active                 bool
}

// New creates an Emergency component. Call Start after creation.
func New(cfg *config.Config, b bus.Bus) *Emergency {
	systemName := cfg.SystemName
	if systemName == "" {
		systemName = "deliverydron"
	}
	topic := cfg.ComponentTopic
	if topic == "" {
		topic = cfg.BrokerTopicFor("emergency")
	}
	base := component.NewBaseComponent(cfg.ComponentID, "emergency", topic, b)
	secTopic := os.Getenv("SECURITY_MONITOR_TOPIC")
	if secTopic == "" {
		secTopic = cfg.BrokerTopicFor("security_monitor")
	}
	journalTopic := cfg.BrokerTopicFor("journal")
	motorsTopic := cfg.BrokerTopicFor("motors")
	cargoTopic := cfg.BrokerTopicFor("cargo")
	e := &Emergency{
		BaseComponent:   base,
		systemName:      systemName,
		secMonitorTopic: secTopic,
		journalTopic:    journalTopic,
		motorsTopic:     motorsTopic,
		cargoTopic:      cargoTopic,
		proxy: &component.ProxyClient{
			Bus:                  b,
			SenderID:             cfg.ComponentID,
			SecurityMonitorTopic: secTopic,
			TimeoutSec:           5.0,
		},
		active:          false,
	}
	e.audit = &component.AuditLogger{
		Proxy:        e.proxy,
		JournalTopic: journalTopic,
		Source:       "emergency",
	}
	e.droneport = loadDroneportConfig(cfg.InstanceID, e.proxy)
	if e.droneport.topic != "" || e.droneport.orchestratorTopic != "" {
		e.droneportPhase = DroneportPhaseNotRegistered
	} else {
		e.droneportPhase = DroneportPhaseDisabled
	}
	e.registerHandlers()
	return e
}

func (e *Emergency) registerHandlers() {
	e.RegisterHandler("limiter_event", e.handleLimiterEvent)
	e.RegisterHandler("droneport_takeoff", e.handleDroneportTakeoff)
	e.RegisterHandler("droneport_land", e.handleDroneportLand)
	e.RegisterHandler("droneport_event", e.handleDroneportEvent)
	e.RegisterHandler("get_state", e.handleGetState)
}

func (e *Emergency) handleDroneportTakeoff(ctx context.Context, message map[string]interface{}) (map[string]interface{}, error) {
	if !component.IsTrustedSender(message, "security_monitor") {
		return nil, nil
	}
	payload, _ := message["payload"].(map[string]interface{})
	if payload == nil {
		return map[string]interface{}{"ok": false, "error": "invalid_payload"}, nil
	}
	mid, _ := payload["mission_id"].(string)
	lb := optionalLandingBattery(payload, e.droneport.landingBattery)
	result, extra := e.runPreflight(ctx, mid, lb)
	switch result {
	case preflightOK:
		return map[string]interface{}{"ok": true, "extra": extra}, nil
	case preflightPending:
		return map[string]interface{}{"ok": false, "pending": true}, nil
	default:
		errKey := "droneport_denied"
		if extra != nil {
			if em, ok := extra["error"].(string); ok && em != "" {
				errKey = em
			}
		}
		return map[string]interface{}{"ok": false, "error": errKey, "extra": extra}, nil
	}
}

func (e *Emergency) handleDroneportLand(ctx context.Context, message map[string]interface{}) (map[string]interface{}, error) {
	if !component.IsTrustedSender(message, "security_monitor") && !component.IsTrustedSender(message, "autopilot") {
		return nil, nil
	}
	payload, _ := message["payload"].(map[string]interface{})
	if payload == nil {
		return map[string]interface{}{"ok": false, "error": "invalid_payload"}, nil
	}
	mid, _ := payload["mission_id"].(string)
	lb := optionalLandingBattery(payload, e.droneport.landingBattery)
	ok, extra := e.runPostMissionLand(ctx, mid, lb)
	if !ok {
		errKey := "droneport_land_denied"
		if extra != nil {
			if em, ok := extra["error"].(string); ok && em != "" {
				errKey = em
			}
		}
		return map[string]interface{}{"ok": false, "error": errKey}, nil
	}
	return map[string]interface{}{"ok": true}, nil
}

func (e *Emergency) handleDroneportEvent(ctx context.Context, message map[string]interface{}) (map[string]interface{}, error) {
	if !component.IsTrustedSender(message, "droneport") && !component.IsTrustedSender(message, "security_monitor") {
		return nil, nil
	}
	payload, _ := message["payload"].(map[string]interface{})
	if payload == nil {
		return map[string]interface{}{"ok": false, "error": "invalid_payload"}, nil
	}
	e.logToJournal(ctx, "DRONEPORT_EVENT_RECEIVED", payload)
	return map[string]interface{}{"ok": true}, nil
}

func (e *Emergency) handleLimiterEvent(ctx context.Context, message map[string]interface{}) (map[string]interface{}, error) {
	if !component.IsTrustedSender(message, "security_monitor") && !component.IsTrustedSender(message, "limiter") {
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
		e.active = false
		return map[string]interface{}{"ok": false, "error": "isolation_start_failed"}, nil
	}

	// 2. Cargo CLOSE via proxy_publish
	if err := e.proxy.ProxyPublishAsync(ctx, e.cargoTopic, "CLOSE", map[string]interface{}{"reason": "emergency"}); err != nil {
		log.Printf("[%s] cargo CLOSE: %v", e.ComponentID, err)
		e.active = false
		return map[string]interface{}{"ok": false, "error": "cargo_close_failed"}, nil
	}

	if err := e.proxy.ProxyPublishAsync(ctx, e.motorsTopic, "LAND", map[string]interface{}{"mode": "AUTO_LAND", "reason": "emergency"}); err != nil {
		log.Printf("[%s] motors LAND: %v", e.ComponentID, err)
		e.active = false
		return map[string]interface{}{"ok": false, "error": "motors_land_failed"}, nil
	}

	if err := e.audit.LogEvent(ctx, "EMERGENCY_PROTOCOL_STARTED", map[string]interface{}{
		"mission_id": missionID,
		"details":    details,
	}); err != nil {
		log.Printf("[%s] journal LOG_EVENT: %v", e.ComponentID, err)
		e.active = false
		return map[string]interface{}{"ok": false, "error": "journal_log_failed"}, nil
	}

	return map[string]interface{}{"ok": true}, nil
}

func (e *Emergency) handleGetState(_ context.Context, _ map[string]interface{}) (map[string]interface{}, error) {
	st := map[string]interface{}{"active": e.active}
	for k, v := range e.droneportStateSnapshot() {
		st[k] = v
	}
	return st, nil
}
