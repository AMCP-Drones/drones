package tests

import (
	"fmt"
	"sync"
	"time"
)

type droneportMockDrone struct {
	battery float64
	portID  string
	status  string // new, charging, ready
}

type droneportMock struct {
	mu           sync.Mutex
	drones       map[string]*droneportMockDrone
	denyTakeoff  map[string]bool
	denyLanding  bool
	chargeRate   float64 // percent per second
	nextPort     int
}

func newDroneportMock() *droneportMock {
	return &droneportMock{
		drones:      make(map[string]*droneportMockDrone),
		denyTakeoff: make(map[string]bool),
		chargeRate:  20.0,
		nextPort:    1,
	}
}

func (m *droneportMock) handle(topic string, msg map[string]interface{}) map[string]interface{} {
	action, _ := msg["action"].(string)
	pl, _ := msg["payload"].(map[string]interface{})
	if pl == nil {
		pl = map[string]interface{}{}
	}
	switch action {
	case "get_available_drones":
		return m.handleGetAvailable()
	case "request_landing":
		return m.handleLanding(pl)
	case "request_takeoff":
		return m.handleTakeoff(pl)
	default:
		return map[string]interface{}{"error": "unknown action"}
	}
}

func (m *droneportMock) handleGetAvailable() map[string]interface{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	list := []interface{}{}
	for id, d := range m.drones {
		if d.status == "ready" && d.battery > 60 {
			list = append(list, map[string]interface{}{
				"drone_id": id,
				"battery":  d.battery,
				"status":   d.status,
				"port_id":  d.portID,
			})
		}
	}
	return map[string]interface{}{"drones": list, "from": "orchestrator"}
}

func (m *droneportMock) handleLanding(pl map[string]interface{}) map[string]interface{} {
	droneID, _ := pl["drone_id"].(string)
	if droneID == "" {
		return map[string]interface{}{"error": "drone_id required", "from": "drone_manager"}
	}
	if m.denyLanding {
		return map[string]interface{}{"error": "No free ports", "from": "drone_manager"}
	}
	batt := 95.0
	if v, ok := pl["battery"].(float64); ok {
		batt = v
	}
	m.mu.Lock()
	port := m.nextPort
	m.nextPort++
	m.drones[droneID] = &droneportMockDrone{
		battery: batt,
		portID:  formatPort(port),
		status:  "charging",
	}
	m.mu.Unlock()
	if batt < 100 {
		go m.simulateCharge(droneID, batt)
	} else {
		m.mu.Lock()
		if d := m.drones[droneID]; d != nil {
			d.status = "ready"
			d.battery = 100
		}
		m.mu.Unlock()
	}
	return map[string]interface{}{
		"approved": true,
		"port_id":  formatPort(port),
		"drone_id": droneID,
		"from":     "drone_manager",
	}
}

func formatPort(n int) string {
	return fmt.Sprintf("P-%02d", n)
}

func (m *droneportMock) simulateCharge(droneID string, start float64) {
	batt := start
	for batt < 100 {
		time.Sleep(50 * time.Millisecond)
		batt += m.chargeRate * 0.05
		if batt > 100 {
			batt = 100
		}
		m.mu.Lock()
		if d := m.drones[droneID]; d != nil {
			d.battery = batt
			if batt >= 100 {
				d.status = "ready"
				d.battery = 100
			}
		}
		m.mu.Unlock()
	}
}

func (m *droneportMock) handleTakeoff(pl map[string]interface{}) map[string]interface{} {
	droneID, _ := pl["drone_id"].(string)
	if droneID == "" {
		return map[string]interface{}{"error": "drone_id required", "from": "drone_manager"}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.denyTakeoff[droneID] {
		return map[string]interface{}{"error": "port_busy", "from": "drone_manager"}
	}
	d, ok := m.drones[droneID]
	if !ok {
		return map[string]interface{}{"error": "Failed to get drone information", "from": "drone_manager"}
	}
	if d.battery <= 60 || d.status != "ready" {
		return map[string]interface{}{"error": "Not enough battery for takeoff", "from": "drone_manager"}
	}
	portID := d.portID
	batt := d.battery
	d.portID = ""
	d.status = "departed"
	return map[string]interface{}{
		"approved": true,
		"battery":  batt,
		"port_id":  portID,
		"drone_id": droneID,
		"from":     "drone_manager",
	}
}
