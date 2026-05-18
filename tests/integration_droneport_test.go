//go:build droneport
// +build droneport

package tests

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/AMCP-Drones/drones/systems/deliverydron/bus/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/config/src"
)

const (
	dronePortOrchestratorTopic = "v1.drone_port.1.orchestrator"
	dronePortDroneManagerTopic = "v1.drone_port.1.drone_manager"
	dronePortRegistryTopic     = "v1.drone_port.1.registry"
	dronePortPortManagerTopic  = "v1.drone_port.1.port_manager"
	dronePortStateStoreTopic   = "v1.drone_port.1.state_store"
	dronePortSITLHomeTopic     = "sitl-drone-home"
	dronePortTestSender        = "delivery_drone_droneport_contract_test"
)

type dronePortClient struct {
	bus bus.Bus
}

func newDronePortClient(t *testing.T, id string) *dronePortClient {
	t.Helper()
	b, err := bus.New(&config.Config{
		BrokerType:     "mqtt",
		ComponentID:    id,
		MQTTBroker:     "localhost",
		MQTTPort:       1883,
		MQTTQoS:        1,
		BrokerUser:     "admin",
		BrokerPassword: "admin_secret_123",
	})
	if err != nil {
		t.Fatalf("create DronePort mqtt bus: %v", err)
	}

	ctx := context.Background()
	if err := b.Start(ctx); err != nil {
		t.Fatalf("start DronePort mqtt bus: %v", err)
	}
	t.Cleanup(func() { _ = b.Stop(ctx) })

	return &dronePortClient{bus: b}
}

func (c *dronePortClient) requestRaw(t *testing.T, topic, action string, payload map[string]interface{}, timeout float64) map[string]interface{} {
	t.Helper()
	resp, err := c.bus.Request(context.Background(), topic, map[string]interface{}{
		"action":  action,
		"sender":  dronePortTestSender,
		"payload": payload,
	}, timeout)
	if err != nil {
		t.Fatalf("DronePort %s request to %s failed: %v", action, topic, err)
	}
	return resp
}

func (c *dronePortClient) request(t *testing.T, topic, action string, payload map[string]interface{}, timeout float64) map[string]interface{} {
	t.Helper()
	resp := c.requestRaw(t, topic, action, payload, timeout)
	if success, _ := resp["success"].(bool); !success {
		t.Fatalf("DronePort %s returned unsuccessful response: %#v", action, resp)
	}
	pl, ok := resp["payload"].(map[string]interface{})
	if !ok || pl == nil {
		t.Fatalf("DronePort %s response has no payload: %#v", action, resp)
	}
	return pl
}

func (c *dronePortClient) publish(t *testing.T, topic, action string, payload map[string]interface{}) {
	t.Helper()
	if err := c.bus.Publish(context.Background(), topic, map[string]interface{}{
		"action":  action,
		"sender":  dronePortTestSender,
		"payload": payload,
	}); err != nil {
		t.Fatalf("publish DronePort %s to %s: %v", action, topic, err)
	}
}

func dronePortUniqueID(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
}

func resetDronePortState(t *testing.T, c *dronePortClient) {
	t.Helper()
	ports := []struct {
		id      string
		droneID interface{}
		status  string
	}{
		{id: "P-01", droneID: "drone_001", status: "reserved"},
		{id: "P-02", droneID: nil, status: "free"},
		{id: "P-03", droneID: nil, status: "free"},
		{id: "P-04", droneID: nil, status: "free"},
	}
	for _, port := range ports {
		c.publish(t, dronePortStateStoreTopic, "update_port", map[string]interface{}{
			"port_id":  port.id,
			"drone_id": port.droneID,
			"status":   port.status,
		})
	}
	time.Sleep(500 * time.Millisecond)
}

func waitDronePortDrone(t *testing.T, c *dronePortClient, droneID string, predicate func(map[string]interface{}) bool) map[string]interface{} {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	var last map[string]interface{}
	for time.Now().Before(deadline) {
		pl := c.request(t, dronePortRegistryTopic, "get_drone", map[string]interface{}{"drone_id": droneID}, 5.0)
		if _, hasError := pl["error"]; !hasError {
			last = pl
			if predicate == nil || predicate(pl) {
				return pl
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("DronePort drone %s did not reach expected state, last=%#v", droneID, last)
	return nil
}

func dronePortPorts(t *testing.T, c *dronePortClient) []interface{} {
	t.Helper()
	pl := c.request(t, dronePortPortManagerTopic, "get_port_status", map[string]interface{}{}, 10.0)
	ports, ok := pl["ports"].([]interface{})
	if !ok {
		t.Fatalf("get_port_status payload has no ports list: %#v", pl)
	}
	return ports
}

func findDronePortPort(ports []interface{}, portID string) map[string]interface{} {
	for _, item := range ports {
		port, _ := item.(map[string]interface{})
		if port["port_id"] == portID {
			return port
		}
	}
	return nil
}

func TestDronePort_GCS015_GetAvailableDrones(t *testing.T) {
	c := newDronePortClient(t, "droneport_contract_gcs015")
	resetDronePortState(t, c)

	droneID := dronePortUniqueID("dp_ready")
	c.publish(t, dronePortRegistryTopic, "register_drone", map[string]interface{}{
		"drone_id": droneID,
		"model":    "DeliveryDrone",
		"port_id":  "P-02",
	})
	c.publish(t, dronePortRegistryTopic, "update_battery", map[string]interface{}{
		"drone_id": droneID,
		"battery":  100.0,
	})

	waitDronePortDrone(t, c, droneID, func(drone map[string]interface{}) bool {
		return drone["status"] == "ready"
	})

	pl := c.request(t, dronePortOrchestratorTopic, "get_available_drones", map[string]interface{}{}, 10.0)
	drones, ok := pl["drones"].([]interface{})
	if !ok || len(drones) == 0 {
		t.Fatalf("get_available_drones returned no drones: %#v", pl)
	}
	for _, item := range drones {
		drone, _ := item.(map[string]interface{})
		if drone["drone_id"] == droneID && drone["status"] == "ready" {
			return
		}
	}
	t.Fatalf("available drones do not include registered ready drone %s: %#v", droneID, drones)
}

func TestDronePort_GCS016_GetPortStatus(t *testing.T) {
	c := newDronePortClient(t, "droneport_contract_gcs016")
	resetDronePortState(t, c)

	ports := dronePortPorts(t, c)
	if len(ports) < 4 {
		t.Fatalf("expected at least 4 ports, got %#v", ports)
	}
	for _, item := range ports {
		port, _ := item.(map[string]interface{})
		if port["port_id"] == nil || port["status"] == nil || port["lat"] == nil || port["lon"] == nil {
			t.Fatalf("port status is missing required fields: %#v", port)
		}
	}
}

func TestDronePort_GCS017_RequestLandingAssignsFreePort(t *testing.T) {
	c := newDronePortClient(t, "droneport_contract_gcs017")
	resetDronePortState(t, c)

	droneID := dronePortUniqueID("dp_landing")
	pl := c.request(t, dronePortDroneManagerTopic, "request_landing", map[string]interface{}{
		"drone_id": droneID,
		"model":    "DeliveryDrone",
		"battery":  100.0,
	}, 10.0)
	if pl["approved"] != true || pl["port_id"] == "" || pl["drone_id"] != droneID {
		t.Fatalf("request_landing did not approve landing with assigned port: %#v", pl)
	}

	portID, _ := pl["port_id"].(string)

	ports := dronePortPorts(t, c)
	port := findDronePortPort(ports, portID)
	if port == nil || port["status"] != "reserved" || port["drone_id"] != droneID {
		t.Fatalf("landing did not reserve assigned port %s for drone %s: %#v", portID, droneID, ports)
	}
}

func TestDronePort_GCS018_RequestLandingWhenNoFreePorts(t *testing.T) {
	c := newDronePortClient(t, "droneport_contract_gcs018")
	resetDronePortState(t, c)
	t.Cleanup(func() { resetDronePortState(t, c) })

	for i, portID := range []string{"P-01", "P-02", "P-03", "P-04"} {
		c.publish(t, dronePortStateStoreTopic, "update_port", map[string]interface{}{
			"port_id":  portID,
			"drone_id": fmt.Sprintf("occupied_%d", i),
			"status":   "reserved",
		})
	}
	time.Sleep(500 * time.Millisecond)

	pl := c.request(t, dronePortDroneManagerTopic, "request_landing", map[string]interface{}{
		"drone_id": dronePortUniqueID("dp_no_port"),
		"model":    "DeliveryDrone",
		"battery":  100.0,
	}, 10.0)
	if pl["error"] != "No free ports" {
		t.Fatalf("request_landing without free ports should return No free ports, got %#v", pl)
	}
}

func TestDronePort_GCS019_RequestDepartureFreesPortAndPublishesSITLHome(t *testing.T) {
	c := newDronePortClient(t, "droneport_contract_gcs019")
	resetDronePortState(t, c)

	sitlHome := make(chan map[string]interface{}, 5)
	if err := c.bus.Subscribe(context.Background(), dronePortSITLHomeTopic, func(message map[string]interface{}) {
		select {
		case sitlHome <- message:
		default:
		}
	}); err != nil {
		t.Fatalf("subscribe SITL home topic: %v", err)
	}
	t.Cleanup(func() { _ = c.bus.Unsubscribe(context.Background(), dronePortSITLHomeTopic) })

	droneID := dronePortUniqueID("dp_takeoff")
	landing := c.request(t, dronePortDroneManagerTopic, "request_landing", map[string]interface{}{
		"drone_id": droneID,
		"model":    "DeliveryDrone",
		"battery":  100.0,
	}, 10.0)
	portID, _ := landing["port_id"].(string)
	if portID == "" {
		t.Fatalf("landing response has no port_id: %#v", landing)
	}
	waitDronePortDrone(t, c, droneID, func(drone map[string]interface{}) bool {
		return drone["drone_id"] == droneID && drone["port_id"] == portID
	})
	c.publish(t, dronePortRegistryTopic, "update_battery", map[string]interface{}{
		"drone_id": droneID,
		"battery":  100.0,
	})
	waitDronePortDrone(t, c, droneID, func(drone map[string]interface{}) bool {
		return drone["status"] == "ready" && drone["port_id"] == portID
	})

	pl := c.request(t, dronePortDroneManagerTopic, "request_takeoff", map[string]interface{}{
		"drone_id": droneID,
	}, 10.0)
	if pl["approved"] != true || pl["port_id"] != portID || pl["drone_id"] != droneID {
		t.Fatalf("request_takeoff did not approve departure from assigned port: %#v", pl)
	}
	coords, _ := pl["port_coordinates"].(map[string]interface{})
	if coords == nil || coords["lat"] == nil || coords["lon"] == nil {
		t.Fatalf("request_takeoff response has no port coordinates: %#v", pl)
	}

	select {
	case msg := <-sitlHome:
		if msg["drone_id"] != droneID || msg["home_lat"] == nil || msg["home_lon"] == nil {
			t.Fatalf("SITL home event has wrong payload: %#v", msg)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("request_takeoff did not publish SITL home event")
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		port := findDronePortPort(dronePortPorts(t, c), portID)
		if port != nil && port["status"] == "free" && port["drone_id"] == "" {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("request_takeoff did not free port %s", portID)
}

func TestDronePort_GCS020_RequestChargingViaDroneManager(t *testing.T) {
	c := newDronePortClient(t, "droneport_contract_gcs020")
	resetDronePortState(t, c)

	droneID := dronePortUniqueID("dp_charge")
	landing := c.request(t, dronePortDroneManagerTopic, "request_landing", map[string]interface{}{
		"drone_id": droneID,
		"model":    "DeliveryDrone",
		"battery":  100.0,
	}, 10.0)
	if landing["approved"] != true {
		t.Fatalf("precondition landing failed: %#v", landing)
	}

	resp := c.requestRaw(t, dronePortDroneManagerTopic, "request_charging", map[string]interface{}{
		"drone_id": droneID,
		"battery":  80.0,
	}, 10.0)
	if success, _ := resp["success"].(bool); !success {
		t.Fatalf("DroneManager should support request_charging and start charging, got response: %#v", resp)
	}

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		drone := waitDronePortDrone(t, c, droneID, nil)
		if drone["status"] == "charging" || fmt.Sprint(drone["battery"]) == "100" || fmt.Sprint(drone["battery"]) == "100.0" {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("request_charging did not update drone charging state")
}
