package tests

import (
	"fmt"
	"math"
	"strings"
)

// opbdMock implements a minimal stateful OpBD gateway for integration tests.
type opbdMock struct {
	dronesRegistered   map[string]bool
	rejectedMissions     map[string]bool
	unauthorizedTakeoff map[string]bool
	authorizedMissions   map[string]bool
	activeFlights        map[string]string
	emergencyLat         float64
	emergencyLon         float64
	telemetryCalls       int
}

func newOpbdMock() *opbdMock {
	return &opbdMock{
		dronesRegistered:   make(map[string]bool),
		rejectedMissions:   make(map[string]bool),
		unauthorizedTakeoff: make(map[string]bool),
		authorizedMissions: make(map[string]bool),
		activeFlights:      make(map[string]string),
		emergencyLat:       -999,
		emergencyLon:       -999,
	}
}

func (m *opbdMock) rejectMission(id string) {
	m.rejectedMissions[id] = true
}

func (m *opbdMock) denyTakeoff(id string) {
	m.unauthorizedTakeoff[id] = true
}

func (m *opbdMock) setEmergencyCoords(lat, lon float64) {
	m.emergencyLat = lat
	m.emergencyLon = lon
}

func (m *opbdMock) handle(msg map[string]interface{}) map[string]interface{} {
	action, _ := msg["action"].(string)
	pl, _ := msg["payload"].(map[string]interface{})
	if pl == nil {
		pl = map[string]interface{}{}
	}
	switch action {
	case "register_drone":
		droneID, _ := pl["drone_id"].(string)
		if droneID == "" {
			return map[string]interface{}{"status": "error", "message": "drone_id required"}
		}
		m.dronesRegistered[droneID] = true
		return map[string]interface{}{"status": "registered", "drone_id": droneID}
	case "register_mission":
		mid := missionIDString(pl["mission_id"])
		droneID, _ := pl["drone_id"].(string)
		if mid == "" || droneID == "" {
			return map[string]interface{}{"status": "error", "message": "mission_id and drone_id required"}
		}
		if !m.dronesRegistered[droneID] {
			return map[string]interface{}{"status": "error", "message": "drone not registered"}
		}
		if m.rejectedMissions[mid] {
			return map[string]interface{}{
				"status": "rejected",
				"reason": "route intersects no_fly_zone",
			}
		}
		return map[string]interface{}{"status": "mission_registered", "mission_id": mid}
	case "authorize_mission":
		mid := missionIDString(pl["mission_id"])
		if mid == "" {
			return map[string]interface{}{"status": "error", "message": "mission not found"}
		}
		if m.rejectedMissions[mid] {
			return map[string]interface{}{"status": "error", "message": "mission not found"}
		}
		m.authorizedMissions[mid] = true
		return map[string]interface{}{"status": "authorized", "mission_id": mid}
	case "request_takeoff":
		mid := missionIDString(pl["mission_id"])
		droneID, _ := pl["drone_id"].(string)
		if mid == "" {
			return map[string]interface{}{"status": "error", "message": "mission_id required"}
		}
		if m.unauthorizedTakeoff[mid] || !m.authorizedMissions[mid] {
			return map[string]interface{}{"status": "takeoff_denied", "reason": "mission not authorized"}
		}
		if droneID != "" {
			m.activeFlights[droneID] = mid
		}
		return map[string]interface{}{
			"status":     "takeoff_authorized",
			"mission_id": mid,
			"drone_id":   droneID,
		}
	case "send_telemetry":
		m.telemetryCalls++
		coords, _ := pl["coords"].(map[string]interface{})
		if coords != nil {
			lat, _ := coords["lat"].(float64)
			lon, _ := coords["lon"].(float64)
			if math.Abs(lat-m.emergencyLat) < 1e-6 && math.Abs(lon-m.emergencyLon) < 1e-6 {
				return map[string]interface{}{
					"status":  "emergency",
					"command": "LAND",
					"reason":  "entered no_fly_zone",
				}
			}
		}
		return map[string]interface{}{"status": "telemetry_received"}
	case "complete_mission":
		mid := missionIDString(pl["mission_id"])
		droneID, _ := pl["drone_id"].(string)
		delete(m.authorizedMissions, mid)
		if droneID != "" {
			delete(m.activeFlights, droneID)
		}
		return map[string]interface{}{"status": "mission_completed", "mission_id": mid}
	case "report_incident":
		return map[string]interface{}{"status": "incident_recorded", "incident_id": "INC-000001"}
	case "get_mission_status":
		mid := missionIDString(pl["mission_id"])
		st := "registered"
		if m.authorizedMissions[mid] {
			st = "authorized"
		}
		return map[string]interface{}{"status": "ok", "mission_status": st, "mission_id": mid}
	default:
		return map[string]interface{}{"status": "error", "message": "unknown action"}
	}
}

func missionIDString(v interface{}) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}
