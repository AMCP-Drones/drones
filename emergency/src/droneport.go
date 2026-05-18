package emergency

import (
	"context"
	"os"
	"strconv"
	"strings"

	"github.com/AMCP-Drones/drones/systems/deliverydron/component/src"
)

type droneportConfig struct {
	topic          string
	droneID        string
	mockSuccess    bool
	droneModel     string
	landingBattery float64
	proxy          *component.ProxyClient
}

func loadDroneportConfig(instanceID string, proxy *component.ProxyClient) droneportConfig {
	topic := strings.TrimSpace(os.Getenv("DRONEPORT_TOPIC"))
	if topic == "" {
		topic = strings.TrimSpace(os.Getenv("DRONEPORT_EXTERNAL_TOPIC"))
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
	return droneportConfig{
		topic:          topic,
		droneID:        droneID,
		mockSuccess:    parseBoolEnvDroneport(os.Getenv("EMERGENCY_DRONEPORT_MOCK_SUCCESS")),
		droneModel:     model,
		landingBattery: landingBatt,
		proxy:          proxy,
	}
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
		if _, ok := cur["port_id"]; ok {
			return cur
		}
		if _, ok := cur["battery"]; ok {
			return cur
		}
		if pl, ok := cur["payload"].(map[string]interface{}); ok {
			if pl["error"] != nil || pl["port_id"] != nil || pl["battery"] != nil {
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

func droneportTakeoffOK(resp map[string]interface{}) bool {
	body := unwrapDroneportBody(resp)
	if body == nil || body["error"] != nil {
		return false
	}
	_, hasPort := body["port_id"]
	_, hasBatt := body["battery"]
	return hasPort || hasBatt
}

func (e *Emergency) requestDroneportTakeoff(ctx context.Context, missionID string) (bool, map[string]interface{}) {
	if e.droneport.topic == "" {
		return true, nil
	}
	if e.droneport.mockSuccess {
		e.logToJournal(ctx, "DRONEPORT_TAKEOFF_APPROVED", map[string]interface{}{
			"mission_id": missionID,
			"stub":       true,
			"reason":     "EMERGENCY_DRONEPORT_MOCK_SUCCESS",
		})
		return true, nil
	}
	payload := map[string]interface{}{
		"drone_id": e.droneport.droneID,
	}
	if missionID != "" {
		payload["mission_id"] = missionID
	}
	resp, err := e.droneport.proxy.ProxyRequest(ctx, e.droneport.topic, "request_takeoff", payload)
	if err != nil {
		e.logToJournal(ctx, "DRONEPORT_TAKEOFF_DENIED", map[string]interface{}{
			"mission_id": missionID,
			"error":      err.Error(),
		})
		return false, map[string]interface{}{"error": err.Error()}
	}
	if droneportTakeoffOK(resp) {
		e.logToJournal(ctx, "DRONEPORT_TAKEOFF_APPROVED", map[string]interface{}{"mission_id": missionID})
		return true, resp
	}
	e.logToJournal(ctx, "DRONEPORT_TAKEOFF_DENIED", map[string]interface{}{"mission_id": missionID, "response": resp})
	return false, resp
}

func (e *Emergency) logToJournal(ctx context.Context, event string, details map[string]interface{}) {
	if err := e.audit.LogEvent(ctx, event, details); err != nil {
		// logged by audit
		_ = err
	}
}
