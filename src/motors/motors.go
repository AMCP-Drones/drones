// Package motors implements the actuator component: SET_TARGET, LAND, get_state; publishes to SITL_COMMANDS_TOPIC.
package motors

import (
	"context"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AMCP-Drones/drones/src/bus"
	"github.com/AMCP-Drones/drones/src/component"
	"github.com/AMCP-Drones/drones/src/config"
)

// Motors mode constants.
const (
	ModeIDLE     = "IDLE"
	ModeTRACKING = "TRACKING"
	ModeLANDING  = "LANDING"
)

// Motors implements the motors actuator. Commands only from security_monitor.
type Motors struct {
	*component.BaseComponent
	systemName string
	sitlTopic  string
	sitlMode   string
	mu         sync.RWMutex
	mode       string
	lastTarget map[string]interface{}
	lastCmdTs  float64
	tempC      float64
}

// New creates a Motors component. Call Start after creation.
func New(cfg *config.Config, b bus.Bus) *Motors {
	systemName := cfg.SystemName
	if systemName == "" {
		systemName = "deliverydron"
	}
	topic := cfg.ComponentTopic
	if topic == "" {
		topic = config.TopicFor(systemName, "motors")
	}
	base := component.NewBaseComponent(cfg.ComponentID, "motors", topic, b)
	sitlTopic := os.Getenv("SITL_COMMANDS_TOPIC")
	if sitlTopic == "" {
		sitlTopic = config.TopicFor(systemName, "sitl.commands")
	}
	sitlMode := strings.TrimSpace(strings.ToLower(os.Getenv("SITL_MODE")))
	if sitlMode == "" {
		sitlMode = "mock"
	}
	tempC := 25.0
	if t := os.Getenv("MOTORS_TEMPERATURE_C_DEFAULT"); t != "" {
		if v, err := strconv.ParseFloat(strings.TrimSpace(t), 64); err == nil {
			tempC = v
		}
	}
	m := &Motors{
		BaseComponent: base,
		systemName:    systemName,
		sitlTopic:     sitlTopic,
		sitlMode:      sitlMode,
		mode:          ModeIDLE,
		lastTarget:    nil,
		lastCmdTs:     0,
		tempC:         tempC,
	}
	m.registerHandlers()
	return m
}

func (m *Motors) registerHandlers() {
	m.RegisterHandler("SET_TARGET", m.handleSetTarget)
	m.RegisterHandler("LAND", m.handleLand)
	m.RegisterHandler("get_state", m.handleGetState)
}

func (m *Motors) handleSetTarget(ctx context.Context, message map[string]interface{}) (map[string]interface{}, error) {
	if !component.IsTrustedSender(message, "security_monitor") {
		return nil, nil
	}
	payload, _ := message["payload"].(map[string]interface{})
	if payload == nil {
		return map[string]interface{}{"ok": false, "error": "invalid_payload"}, nil
	}
	target := make(map[string]interface{})
	for _, k := range []string{"heading_deg", "ground_speed_mps", "alt_m", "vx", "vy", "vz", "lat", "lon", "drop"} {
		if v, ok := payload[k]; ok {
			target[k] = v
		}
	}
	m.mu.Lock()
	m.lastTarget = target
	m.mode = ModeTRACKING
	m.lastCmdTs = float64(time.Now().UnixNano()) / 1e9
	m.mu.Unlock()
	m.emitSITL(ctx, map[string]interface{}{"cmd": "SET_TARGET", "target": target})
	return map[string]interface{}{"ok": true, "mode": m.mode}, nil
}

func (m *Motors) handleLand(ctx context.Context, message map[string]interface{}) (map[string]interface{}, error) {
	if !component.IsTrustedSender(message, "security_monitor") {
		return nil, nil
	}
	m.mu.Lock()
	m.mode = ModeLANDING
	m.lastCmdTs = float64(time.Now().UnixNano()) / 1e9
	m.mu.Unlock()
	m.emitSITL(ctx, map[string]interface{}{"cmd": "LAND"})
	return map[string]interface{}{"ok": true, "mode": m.mode}, nil
}

func (m *Motors) handleGetState(_ context.Context, _ map[string]interface{}) (map[string]interface{}, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := map[string]interface{}{
		"mode":          m.mode,
		"last_target":   m.lastTarget,
		"last_cmd_ts":   m.lastCmdTs,
		"temperature_c": m.tempC,
		"sitl_mode":     m.sitlMode,
	}
	return out, nil
}

func (m *Motors) emitSITL(ctx context.Context, command map[string]interface{}) {
	msg := map[string]interface{}{
		"source":  "motors",
		"command": command,
	}
	if m.sitlMode != "mock" {
		msg["note"] = "sitl_mode=" + m.sitlMode + " not implemented, emitted as mock"
	}
	if err := m.Bus.Publish(ctx, m.sitlTopic, msg); err != nil {
		log.Printf("[%s] SITL publish: %v", m.ComponentID, err)
	}
}
