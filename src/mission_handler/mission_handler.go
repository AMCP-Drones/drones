// Package mission_handler implements LOAD_MISSION (WPL/JSON), VALIDATE_ONLY, get_state; sends mission to autopilot via security_monitor, logs to journal.
package mission_handler

import (
	"context"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/AMCP-Drones/drones/src/bus"
	"github.com/AMCP-Drones/drones/src/component"
	"github.com/AMCP-Drones/drones/src/config"
)

// MissionHandler accepts WPL or JSON missions, validates, sends to autopilot, logs events.
type MissionHandler struct {
	*component.BaseComponent
	systemName        string
	secMonitorTopic   string
	autopilotTopic    string
	journalTopic      string
	limiterTopic      string
	requestTimeoutSec float64
	lastMission       map[string]interface{}
	lastError         string
}

// New creates a MissionHandler. Call Start after creation.
func New(cfg *config.Config, b bus.Bus) *MissionHandler {
	systemName := cfg.SystemName
	if systemName == "" {
		systemName = "deliverydron"
	}
	topic := cfg.ComponentTopic
	if topic == "" {
		topic = config.TopicFor(systemName, "mission_handler")
	}
	base := component.NewBaseComponent(cfg.ComponentID, "mission_handler", topic, b)
	secTopic := os.Getenv("SECURITY_MONITOR_TOPIC")
	if secTopic == "" {
		secTopic = config.TopicFor(systemName, "security_monitor")
	}
	autopilotTopic := os.Getenv("AUTOPILOT_TOPIC")
	if autopilotTopic == "" {
		autopilotTopic = config.TopicFor(systemName, "autopilot")
	}
	journalTopic := config.TopicFor(systemName, "journal")
	limiterTopic := config.TopicFor(systemName, "limiter")
	timeout := 10.0
	if s := os.Getenv("MISSION_HANDLER_REQUEST_TIMEOUT_S"); s != "" {
		if v, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil && v > 0 {
			timeout = v
		}
	}
	m := &MissionHandler{
		BaseComponent:     base,
		systemName:        systemName,
		secMonitorTopic:   secTopic,
		autopilotTopic:    autopilotTopic,
		journalTopic:      journalTopic,
		limiterTopic:      limiterTopic,
		requestTimeoutSec: timeout,
		lastMission:       nil,
		lastError:         "",
	}
	m.registerHandlers()
	return m
}

func (m *MissionHandler) registerHandlers() {
	m.RegisterHandler("LOAD_MISSION", m.handleLoadMission)
	m.RegisterHandler("VALIDATE_ONLY", m.handleValidateOnly)
	m.RegisterHandler("get_state", m.handleGetState)
}

func (m *MissionHandler) handleLoadMission(ctx context.Context, message map[string]interface{}) (map[string]interface{}, error) {
	if !component.IsTrustedSender(message, "security_monitor") {
		return nil, nil
	}
	payload, _ := message["payload"].(map[string]interface{})
	if payload == nil {
		m.lastError = "invalid_input"
		m.logToJournal(ctx, "MISSION_HANDLER_VALIDATION_ERROR", map[string]interface{}{"error": m.lastError})
		return map[string]interface{}{"ok": false, "error": m.lastError}, nil
	}
	mission, errMsg := m.parseMission(payload)
	if mission == nil {
		m.lastError = errMsg
		m.logToJournal(ctx, "MISSION_HANDLER_VALIDATION_ERROR", map[string]interface{}{"error": m.lastError})
		return map[string]interface{}{"ok": false, "error": m.lastError}, nil
	}
	if ok, err := validateMission(mission); !ok {
		m.lastError = err
		m.logToJournal(ctx, "MISSION_HANDLER_VALIDATION_ERROR", map[string]interface{}{"error": m.lastError})
		return map[string]interface{}{"ok": false, "error": m.lastError}, nil
	}
	m.lastMission = mission
	m.lastError = ""
	mid, _ := mission["mission_id"].(string)
	m.logToJournal(ctx, "MISSION_HANDLER_MISSION_RECEIVED", map[string]interface{}{"mission_id": mid})

	proxyMsg := map[string]interface{}{
		"action": "proxy_request",
		"sender": m.ComponentID,
		"payload": map[string]interface{}{
			"target": map[string]interface{}{"topic": m.autopilotTopic, "action": "mission_load"},
			"data":   map[string]interface{}{"mission": mission},
		},
	}
	resp, err := m.Bus.Request(ctx, m.secMonitorTopic, proxyMsg, m.requestTimeoutSec)
	if err != nil {
		m.lastError = "autopilot_no_response"
		m.logToJournal(ctx, "MISSION_HANDLER_AUTOPILOT_ERROR", map[string]interface{}{"error": m.lastError, "mission_id": mid})
		return map[string]interface{}{"ok": false, "error": m.lastError}, nil
	}
	pl, _ := resp["payload"].(map[string]interface{})
	tr, _ := pl["target_response"].(map[string]interface{})
	if tr != nil {
		trPl, _ := tr["payload"].(map[string]interface{})
		if trPl != nil && trPl["ok"] != true {
			m.lastError, _ = trPl["error"].(string)
			if m.lastError == "" {
				m.lastError = "autopilot_error"
			}
			m.logToJournal(ctx, "MISSION_HANDLER_AUTOPILOT_ERROR", map[string]interface{}{"error": m.lastError, "mission_id": mid})
			return map[string]interface{}{"ok": false, "error": m.lastError}, nil
		}
	}
	m.logToJournal(ctx, "MISSION_HANDLER_MISSION_SENT_TO_AUTOPILOT", map[string]interface{}{"mission_id": mid})
	// Send mission to limiter for geofence (policy allows mission_handler -> limiter mission_load)
	limiterMsg := map[string]interface{}{
		"action": "proxy_publish",
		"sender": m.ComponentID,
		"payload": map[string]interface{}{
			"target": map[string]interface{}{"topic": m.limiterTopic, "action": "mission_load"},
			"data":   map[string]interface{}{"mission": mission},
		},
	}
	if err := m.Bus.Publish(ctx, m.secMonitorTopic, limiterMsg); err != nil {
		log.Printf("[%s] send mission to limiter: %v", m.ComponentID, err)
	}
	return map[string]interface{}{"ok": true}, nil
}

func (m *MissionHandler) parseMission(payload map[string]interface{}) (map[string]interface{}, string) {
	if wpl, ok := payload["wpl_content"].(string); ok && wpl != "" {
		missionID, _ := payload["mission_id"].(string)
		mission, err := ParseWPL(wpl, missionID)
		return mission, err
	}
	if raw, ok := payload["mission"]; ok {
		if mission, ok := raw.(map[string]interface{}); ok {
			return mission, ""
		}
	}
	return nil, "invalid_input_wpl_or_mission_required"
}

func validateMission(mission map[string]interface{}) (bool, string) {
	if mission == nil {
		return false, "mission_not_dict"
	}
	mid, _ := mission["mission_id"].(string)
	if mid == "" {
		return false, "invalid_mission_id"
	}
	steps, _ := mission["steps"].([]interface{})
	if len(steps) == 0 {
		return false, "empty_steps"
	}
	for i, s := range steps {
		step, ok := s.(map[string]interface{})
		if !ok {
			return false, "invalid_step_" + strconv.Itoa(i)
		}
		if _, ok := step["lat"]; !ok {
			return false, "missing_lat_in_step_" + strconv.Itoa(i)
		}
		if _, ok := step["lon"]; !ok {
			return false, "missing_lon_in_step_" + strconv.Itoa(i)
		}
		if _, ok := step["alt_m"]; !ok {
			return false, "missing_alt_m_in_step_" + strconv.Itoa(i)
		}
	}
	return true, ""
}

func (m *MissionHandler) handleValidateOnly(ctx context.Context, message map[string]interface{}) (map[string]interface{}, error) {
	if !component.IsTrustedSender(message, "security_monitor") {
		return nil, nil
	}
	payload, _ := message["payload"].(map[string]interface{})
	if payload == nil {
		m.lastError = "invalid_input"
		m.logToJournal(ctx, "MISSION_HANDLER_VALIDATION_ERROR", map[string]interface{}{"error": m.lastError})
		return map[string]interface{}{"ok": false, "error": m.lastError}, nil
	}
	mission, errMsg := m.parseMission(payload)
	if mission == nil {
		m.lastError = errMsg
		m.logToJournal(ctx, "MISSION_HANDLER_VALIDATION_ERROR", map[string]interface{}{"error": m.lastError})
		return map[string]interface{}{"ok": false, "error": m.lastError}, nil
	}
	if ok, err := validateMission(mission); !ok {
		m.lastError = err
		m.logToJournal(ctx, "MISSION_HANDLER_VALIDATION_ERROR", map[string]interface{}{"error": m.lastError})
		return map[string]interface{}{"ok": false, "error": m.lastError}, nil
	}
	m.lastError = ""
	return map[string]interface{}{"ok": true}, nil
}

func (m *MissionHandler) handleGetState(_ context.Context, message map[string]interface{}) (map[string]interface{}, error) {
	if !component.IsTrustedSender(message, "security_monitor") {
		return nil, nil
	}
	return map[string]interface{}{
		"last_mission": m.lastMission,
		"last_error":   m.lastError,
	}, nil
}

func (m *MissionHandler) logToJournal(ctx context.Context, event string, details map[string]interface{}) {
	msg := map[string]interface{}{
		"action": "proxy_publish",
		"sender": m.ComponentID,
		"payload": map[string]interface{}{
			"target": map[string]interface{}{"topic": m.journalTopic, "action": "LOG_EVENT"},
			"data": map[string]interface{}{
				"event":   event,
				"source":  "mission_handler",
				"details": details,
			},
		},
	}
	if err := m.Bus.Publish(ctx, m.secMonitorTopic, msg); err != nil {
		log.Printf("[%s] log to journal: %v", m.ComponentID, err)
	}
}
