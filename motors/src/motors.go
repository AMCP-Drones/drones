// Package motors implements the actuator component: SET_TARGET, LAND, get_state; publishes to SITL_COMMANDS_TOPIC.
package motors

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AMCP-Drones/drones/systems/deliverydron/bus/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/component/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/config/src"
)

// Motors mode constants.
const (
	ModeIDLE     = "IDLE"
	ModeTRACKING = "TRACKING"
	ModeLANDING  = "LANDING"

	defaultSITLDroneID = "drone_001"
	sitlLandVz         = -0.5
)

var sitlDroneIDPattern = regexp.MustCompile(`^drone_[0-9]{3,4}$`)

// Motors implements the motors actuator. Commands only from security_monitor.
type Motors struct {
	*component.BaseComponent
	systemName string
	sitlTopic  string
	sitlMode   string
	droneID    string
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
		topic = cfg.BrokerTopicFor("motors")
	}
	base := component.NewBaseComponent(cfg.ComponentID, "motors", topic, b)
	sitlTopic := strings.TrimSpace(os.Getenv("SITL_COMMANDS_TOPIC"))
	if sitlTopic == "" {
		sitlTopic = "sitl.commands"
	}
	sitlMode := strings.TrimSpace(strings.ToLower(os.Getenv("SITL_MODE")))
	if sitlMode == "" {
		sitlMode = "mock"
	}
	droneID := strings.TrimSpace(os.Getenv("SITL_DRONE_ID"))
	if droneID == "" {
		droneID = defaultSITLDroneID
	}
	if !sitlDroneIDPattern.MatchString(droneID) {
		log.Printf("[%s] invalid SITL_DRONE_ID %q, using %s", cfg.ComponentID, droneID, defaultSITLDroneID)
		droneID = defaultSITLDroneID
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
		droneID:       droneID,
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

func (m *Motors) handleSetTarget(_ context.Context, message map[string]interface{}) (map[string]interface{}, error) {
	if !component.IsTrustedSender(message, "security_monitor") {
		return nil, nil
	}
	payload, _ := message["payload"].(map[string]interface{})
	if payload == nil {
		return map[string]interface{}{"ok": false, "error": "invalid_payload"}, nil
	}
	target, err := sanitizeTarget(payload)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}, nil
	}
	sitlCmd, err := targetToSITLCommand(m.droneID, target)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}, nil
	}
	m.mu.Lock()
	m.lastTarget = target
	m.mode = ModeTRACKING
	m.lastCmdTs = float64(time.Now().UnixNano()) / 1e9
	mode := m.mode
	m.mu.Unlock()
	m.emitSITL(context.Background(), sitlCmd)
	return map[string]interface{}{"ok": true, "mode": mode}, nil
}

func (m *Motors) handleLand(_ context.Context, message map[string]interface{}) (map[string]interface{}, error) {
	if !component.IsTrustedSender(message, "security_monitor") {
		return nil, nil
	}
	m.mu.Lock()
	m.mode = ModeLANDING
	m.lastCmdTs = float64(time.Now().UnixNano()) / 1e9
	mode := m.mode
	m.mu.Unlock()
	m.emitSITL(context.Background(), landSITLCommand(m.droneID))
	return map[string]interface{}{"ok": true, "mode": mode}, nil
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
		"sitl_drone_id": m.droneID,
	}
	return out, nil
}

func (m *Motors) emitSITL(ctx context.Context, sitlCmd map[string]interface{}) {
	if m.sitlMode != "mock" {
		log.Printf("[%s] sitl_mode=%s: publishing schema command to %s", m.ComponentID, m.sitlMode, m.sitlTopic)
	}
	if err := m.Bus.Publish(ctx, m.sitlTopic, sitlCmd); err != nil {
		log.Printf("[%s] SITL publish: %v", m.ComponentID, err)
	}
}

func landSITLCommand(droneID string) map[string]interface{} {
	return map[string]interface{}{
		"drone_id":    droneID,
		"vx":          0.0,
		"vy":          0.0,
		"vz":          sitlLandVz,
		"mag_heading": 0.0,
	}
}

func targetToSITLCommand(droneID string, target map[string]interface{}) (map[string]interface{}, error) {
	vx, hasVX := floatFromMap(target, "vx")
	vy, hasVY := floatFromMap(target, "vy")
	vz, hasVZ := floatFromMap(target, "vz")
	heading, hasHeading := floatFromMap(target, "heading_deg")
	speed, hasSpeed := floatFromMap(target, "ground_speed_mps")

	if !hasVX && !hasVY && hasHeading && hasSpeed {
		rad := heading * math.Pi / 180
		vx = speed * math.Sin(rad)
		vy = speed * math.Cos(rad)
		hasVX, hasVY = true, true
	}
	if !hasVX {
		vx = 0
	}
	if !hasVY {
		vy = 0
	}
	if !hasVZ {
		vz = 0
	}
	if !hasHeading {
		heading = 0
	}

	return map[string]interface{}{
		"drone_id":    droneID,
		"vx":          clamp(vx, -50, 50),
		"vy":          clamp(vy, -50, 50),
		"vz":          clamp(vz, -10, 10),
		"mag_heading": clamp(heading, 0, 359.9),
	}, nil
}

func floatFromMap(m map[string]interface{}, key string) (float64, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	f, ok := toFloat(v)
	return f, ok
}

func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func sanitizeTarget(payload map[string]interface{}) (map[string]interface{}, error) {
	target := make(map[string]interface{})
	for _, k := range []string{"heading_deg", "ground_speed_mps", "alt_m", "vx", "vy", "vz", "lat", "lon"} {
		if v, ok := payload[k]; ok {
			if _, ok := toFloat(v); !ok {
				return nil, fmt.Errorf("invalid_%s", k)
			}
			target[k] = v
		}
	}
	if drop, ok := payload["drop"]; ok {
		if _, ok := drop.(bool); !ok {
			return nil, fmt.Errorf("invalid_drop")
		}
		target["drop"] = drop
	}
	if len(target) == 0 {
		return nil, fmt.Errorf("empty_target")
	}
	return target, nil
}

func toFloat(v interface{}) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	default:
		return 0, false
	}
}
