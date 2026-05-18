package emergency

import (
	"context"
	"os"
	"strconv"
	"strings"

	"github.com/AMCP-Drones/drones/systems/deliverydron/component/src"
)

type droneportConfig struct {
	topic                string
	orchestratorTopic    string
	droneID              string
	mockSuccess          bool
	droneModel           string
	landingBattery       float64
	minBatteryTakeoff    float64
	chargePollIntervalSec float64
	chargeTimeoutSec     float64
	proxy                *component.ProxyClient
}

func loadDroneportConfig(instanceID string, proxy *component.ProxyClient) droneportConfig {
	topic := strings.TrimSpace(os.Getenv("DRONEPORT_TOPIC"))
	if topic == "" {
		topic = strings.TrimSpace(os.Getenv("DRONEPORT_EXTERNAL_TOPIC"))
	}
	orchTopic := strings.TrimSpace(os.Getenv("DRONEPORT_ORCHESTRATOR_TOPIC"))
	if orchTopic == "" {
		orchTopic = "v1.drone_port.1.orchestrator"
	}
	droneID := strings.TrimSpace(os.Getenv("DRONEPORT_DRONE_ID"))
	if droneID == "" {
		droneID = strings.TrimSpace(instanceID)
	}
	if droneID == "" {
		droneID = "drone_001"
	}
	model := strings.TrimSpace(os.Getenv("DRONEPORT_DRONE_MODEL"))
	if model == "" {
		model = "deliverydron"
	}
	landingBatt := 95.0
	if s := os.Getenv("DRONEPORT_LANDING_BATTERY_DEFAULT"); s != "" {
		if v, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil && v >= 0 {
			landingBatt = v
		}
	}
	minBatt := 61.0
	if s := os.Getenv("DRONEPORT_MIN_BATTERY_TAKEOFF"); s != "" {
		if v, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil && v > 0 {
			minBatt = v
		}
	}
	chargePoll := 1.0
	if s := os.Getenv("DRONEPORT_CHARGE_POLL_INTERVAL_S"); s != "" {
		if v, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil && v > 0 {
			chargePoll = v
		}
	}
	chargeTimeout := 120.0
	if s := os.Getenv("DRONEPORT_CHARGE_TIMEOUT_S"); s != "" {
		if v, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil && v > 0 {
			chargeTimeout = v
		}
	}
	cfg := droneportConfig{
		topic:                 topic,
		orchestratorTopic:     orchTopic,
		droneID:               droneID,
		mockSuccess:           parseBoolEnvDroneport(os.Getenv("EMERGENCY_DRONEPORT_MOCK_SUCCESS")),
		droneModel:            model,
		landingBattery:        landingBatt,
		minBatteryTakeoff:     minBatt,
		chargePollIntervalSec: chargePoll,
		chargeTimeoutSec:      chargeTimeout,
		proxy:                 proxy,
	}
	return cfg
}

func parseBoolEnvDroneport(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func unwrapDroneportBody(resp map[string]interface{}) map[string]interface{} {
	cur := resp
	for i := 0; i < 6 && cur != nil; i++ {
		if cur["error"] != nil {
			return cur
		}
		if _, ok := cur["approved"]; ok {
			return cur
		}
		if _, ok := cur["port_id"]; ok {
			return cur
		}
		if _, ok := cur["battery"]; ok {
			return cur
		}
		if _, ok := cur["drones"]; ok {
			return cur
		}
		if pl, ok := cur["payload"].(map[string]interface{}); ok {
			if pl["error"] != nil || pl["approved"] != nil || pl["port_id"] != nil || pl["battery"] != nil || pl["drones"] != nil {
				return pl
			}
		}
		if tr, ok := cur["target_response"].(map[string]interface{}); ok {
			cur = tr
			continue
		}
		if pl, ok := cur["payload"].(map[string]interface{}); ok {
			if tr, ok := pl["target_response"].(map[string]interface{}); ok {
				cur = tr
				continue
			}
		}
		break
	}
	return cur
}

func (e *Emergency) logToJournal(ctx context.Context, event string, details map[string]interface{}) {
	if err := e.audit.LogEvent(ctx, event, details); err != nil {
		_ = err
	}
}

func optionalLandingBattery(payload map[string]interface{}, defaultBatt float64) *float64 {
	if payload == nil {
		return nil
	}
	if v, ok := payload["battery"]; ok {
		if f, ok := parseDroneportBattery(v); ok {
			return &f
		}
	}
	if v, ok := payload["battery_pct"]; ok {
		if f, ok := parseDroneportBattery(v); ok {
			return &f
		}
	}
	b := defaultBatt
	return &b
}
