//go:build gcs
// +build gcs

package tests

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/AMCP-Drones/drones/systems/deliverydron/bus/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/config/src"
	missionhandler "github.com/AMCP-Drones/drones/systems/deliverydron/mission_handler/src"
)

const (
	gcsOrchestratorTopic  = "v1.gcs.1.orchestrator"
	gcsMissionStoreTopic  = "v1.gcs.1.mission_store"
	gcsDroneStoreTopic    = "v1.gcs.1.drone_store"
	gcsSecurityMonitor    = "v1.Agrodron.Agrodron001.security_monitor"
	gcsMissionHandler     = "v1.Agrodron.Agrodron001.mission_handler"
	gcsAutopilot          = "v1.Agrodron.Agrodron001.autopilot"
	gcsTelemetry          = "v1.Agrodron.Agrodron001.telemetry"
	gcsContractTestSender = "delivery_drone_gcs_contract_test"
)

type gcsClient struct {
	bus bus.Bus
}

type gcsDroneRequest struct {
	Action       string
	TargetTopic  string
	TargetAction string
	Data         map[string]interface{}
	Raw          map[string]interface{}
}

func newGCSClient(t *testing.T, id string) *gcsClient {
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
		t.Fatalf("create GCS mqtt bus: %v", err)
	}

	ctx := context.Background()
	if err := b.Start(ctx); err != nil {
		t.Fatalf("start GCS mqtt bus: %v", err)
	}
	t.Cleanup(func() { _ = b.Stop(ctx) })

	return &gcsClient{bus: b}
}

func (c *gcsClient) request(t *testing.T, topic, action string, payload map[string]interface{}, timeout float64) map[string]interface{} {
	t.Helper()
	resp, err := c.bus.Request(context.Background(), topic, map[string]interface{}{
		"action":  action,
		"sender":  gcsContractTestSender,
		"payload": payload,
	}, timeout)
	if err != nil {
		t.Fatalf("GCS %s request to %s failed: %v", action, topic, err)
	}
	if success, _ := resp["success"].(bool); !success {
		t.Fatalf("GCS %s returned unsuccessful response: %#v", action, resp)
	}
	pl, ok := resp["payload"].(map[string]interface{})
	if !ok || pl == nil {
		t.Fatalf("GCS %s response has no payload: %#v", action, resp)
	}
	return pl
}

func gcsTaskPayload() map[string]interface{} {
	return map[string]interface{}{
		"waypoints": []interface{}{
			map[string]interface{}{"lat": 55.751244, "lon": 37.618423, "alt_m": 120.0},
			map[string]interface{}{"lat": 55.761244, "lon": 37.628423, "alt_m": 130.0},
		},
	}
}

func submitGCSMission(t *testing.T, c *gcsClient) string {
	t.Helper()
	pl := c.request(t, gcsOrchestratorTopic, "task.submit", gcsTaskPayload(), 15.0)
	missionID, _ := pl["mission_id"].(string)
	if missionID == "" {
		t.Fatalf("task.submit response has no mission_id: %#v", pl)
	}
	waypoints, ok := pl["waypoints"].([]interface{})
	if !ok || len(waypoints) < 4 {
		t.Fatalf("task.submit did not return expanded route: %#v", pl)
	}
	return missionID
}

func waitGCSMission(t *testing.T, c *gcsClient, missionID string, predicate func(map[string]interface{}) bool) map[string]interface{} {
	t.Helper()
	deadline := time.Now().Add(12 * time.Second)
	var last map[string]interface{}
	for time.Now().Before(deadline) {
		pl := c.request(t, gcsMissionStoreTopic, "store.get_mission", map[string]interface{}{"mission_id": missionID}, 5.0)
		mission, _ := pl["mission"].(map[string]interface{})
		if mission != nil {
			last = mission
			if predicate == nil || predicate(mission) {
				return mission
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("mission %s did not reach expected state, last=%#v", missionID, last)
	return nil
}

func waitGCSDrone(t *testing.T, c *gcsClient, droneID string, predicate func(map[string]interface{}) bool) map[string]interface{} {
	t.Helper()
	deadline := time.Now().Add(12 * time.Second)
	var last map[string]interface{}
	for time.Now().Before(deadline) {
		pl := c.request(t, gcsDroneStoreTopic, "store.get_drone", map[string]interface{}{"drone_id": droneID}, 5.0)
		drone, _ := pl["drone"].(map[string]interface{})
		if drone != nil {
			last = drone
			if predicate == nil || predicate(drone) {
				return drone
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("drone %s did not reach expected state, last=%#v", droneID, last)
	return nil
}

func respondToGCSRequest(t *testing.T, b bus.Bus, request map[string]interface{}, targetResponse map[string]interface{}) {
	t.Helper()
	replyTo, _ := request["reply_to"].(string)
	correlationID, _ := request["correlation_id"].(string)
	if replyTo == "" || correlationID == "" {
		t.Fatalf("GCS request has no reply_to/correlation_id: %#v", request)
	}
	if err := b.Publish(context.Background(), replyTo, map[string]interface{}{
		"action":         "response",
		"sender":         "fake_delivery_drone_security_monitor",
		"correlation_id": correlationID,
		"success":        true,
		"payload": map[string]interface{}{
			"target_response": targetResponse,
		},
	}); err != nil {
		t.Fatalf("publish fake drone response: %v", err)
	}
}

func startFakeDroneSecurityMonitor(t *testing.T, c *gcsClient) <-chan gcsDroneRequest {
	return startFakeDroneSecurityMonitorWithAutopilot(t, c, true)
}

func startFakeDroneSecurityMonitorWithAutopilot(t *testing.T, c *gcsClient, autopilotOK bool) <-chan gcsDroneRequest {
	t.Helper()
	requests := make(chan gcsDroneRequest, 20)

	err := c.bus.Subscribe(context.Background(), gcsSecurityMonitor, func(message map[string]interface{}) {
		payload, _ := message["payload"].(map[string]interface{})
		target, _ := payload["target"].(map[string]interface{})
		data, _ := payload["data"].(map[string]interface{})
		targetTopic, _ := target["topic"].(string)
		targetAction, _ := target["action"].(string)

		req := gcsDroneRequest{
			Action:       messageAction(message),
			TargetTopic:  targetTopic,
			TargetAction: targetAction,
			Data:         data,
			Raw:          message,
		}
		select {
		case requests <- req:
		default:
		}

		var targetResponse map[string]interface{}
		switch {
		case targetTopic == gcsMissionHandler && targetAction == "load_mission":
			if data["mission_id"] == nil || data["drone_id"] == nil || data["wpl_content"] == nil {
				targetResponse = map[string]interface{}{"success": false, "payload": map[string]interface{}{"ok": false, "error": "missing mission upload fields"}}
			} else {
				targetResponse = map[string]interface{}{"success": true, "payload": map[string]interface{}{"ok": true}}
			}
		case targetTopic == gcsAutopilot && targetAction == "cmd":
			payload := map[string]interface{}{"ok": autopilotOK && data["command"] == "START"}
			if !autopilotOK {
				payload["error"] = "autopilot rejected start"
			}
			targetResponse = map[string]interface{}{"success": true, "payload": payload}
		case targetTopic == gcsTelemetry && targetAction == "get_state":
			targetResponse = map[string]interface{}{
				"success": true,
				"payload": map[string]interface{}{
					"telemetry": map[string]interface{}{
						"drone_id":  data["drone_id"],
						"battery":   87.0,
						"latitude":  55.7558,
						"longitude": 37.6173,
						"altitude":  120.0,
					},
				},
			}
		default:
			targetResponse = map[string]interface{}{"success": false, "payload": map[string]interface{}{"ok": false, "error": "unexpected target"}}
		}

		respondToGCSRequest(t, c.bus, message, targetResponse)
	})
	if err != nil {
		t.Fatalf("subscribe fake drone security monitor: %v", err)
	}
	t.Cleanup(func() { _ = c.bus.Unsubscribe(context.Background(), gcsSecurityMonitor) })

	return requests
}

func messageAction(message map[string]interface{}) string {
	action, _ := message["action"].(string)
	return action
}

func waitFakeDroneRequest(t *testing.T, requests <-chan gcsDroneRequest, predicate func(gcsDroneRequest) bool) gcsDroneRequest {
	t.Helper()
	deadline := time.After(15 * time.Second)
	for {
		select {
		case req := <-requests:
			if predicate(req) {
				return req
			}
		case <-deadline:
			t.Fatalf("did not receive expected fake drone request")
		}
	}
}

func TestGCS_R001_TaskSubmitBuildsRouteAndPersistsMission(t *testing.T) {
	c := newGCSClient(t, "gcs_contract_r001")
	missionID := submitGCSMission(t, c)

	mission := waitGCSMission(t, c, missionID, func(mission map[string]interface{}) bool {
		return mission["status"] == "created"
	})
	if mission["mission_id"] != missionID {
		t.Fatalf("stored mission_id mismatch: %#v", mission)
	}
	if waypoints, ok := mission["waypoints"].([]interface{}); !ok || len(waypoints) < 4 {
		t.Fatalf("stored mission has no expanded waypoints: %#v", mission)
	}
}

func TestGCS_R002_TaskAssignUploadsMissionToDroneAndReservesDrone(t *testing.T) {
	c := newGCSClient(t, "gcs_contract_r002")
	requests := startFakeDroneSecurityMonitor(t, c)
	missionID := submitGCSMission(t, c)
	droneID := fmt.Sprintf("DELIVERY-GCS-%d", time.Now().UnixNano())

	pl := c.request(t, gcsOrchestratorTopic, "task.assign", map[string]interface{}{
		"mission_id": missionID,
		"drone_id":   droneID,
	}, 30.0)
	if ok, _ := pl["ok"].(bool); !ok {
		t.Fatalf("task.assign did not return ok=true: %#v", pl)
	}

	req := waitFakeDroneRequest(t, requests, func(req gcsDroneRequest) bool {
		return req.TargetTopic == gcsMissionHandler && req.TargetAction == "load_mission"
	})
	if req.Action != "proxy_request" {
		t.Fatalf("GCS should use proxy_request through security monitor, got %#v", req.Raw)
	}
	if req.Data["mission_id"] != missionID || req.Data["drone_id"] != droneID {
		t.Fatalf("mission upload payload mismatch: %#v", req.Data)
	}
	wpl, _ := req.Data["wpl_content"].(string)
	if !strings.HasPrefix(wpl, "QGC WPL 110") {
		t.Fatalf("mission upload should contain QGC WPL 110 content, got: %#v", req.Data)
	}

	waitGCSMission(t, c, missionID, func(mission map[string]interface{}) bool {
		return mission["status"] == "assigned" && mission["assigned_drone"] == droneID
	})
	waitGCSDrone(t, c, droneID, func(drone map[string]interface{}) bool {
		return drone["status"] == "reserved"
	})
}

func TestGCS_R003_TaskStartCommandsAutopilotAndSavesTelemetry(t *testing.T) {
	c := newGCSClient(t, "gcs_contract_r003")
	requests := startFakeDroneSecurityMonitor(t, c)
	missionID := submitGCSMission(t, c)
	droneID := fmt.Sprintf("DELIVERY-GCS-START-%d", time.Now().UnixNano())

	c.request(t, gcsOrchestratorTopic, "task.assign", map[string]interface{}{
		"mission_id": missionID,
		"drone_id":   droneID,
	}, 30.0)
	waitFakeDroneRequest(t, requests, func(req gcsDroneRequest) bool {
		return req.TargetTopic == gcsMissionHandler && req.TargetAction == "load_mission"
	})

	pl := c.request(t, gcsOrchestratorTopic, "task.start", map[string]interface{}{
		"mission_id": missionID,
		"drone_id":   droneID,
	}, 20.0)
	if ok, _ := pl["ok"].(bool); !ok {
		t.Fatalf("task.start did not return ok=true: %#v", pl)
	}

	autopilotReq := waitFakeDroneRequest(t, requests, func(req gcsDroneRequest) bool {
		return req.TargetTopic == gcsAutopilot && req.TargetAction == "cmd"
	})
	if autopilotReq.Data["command"] != "START" {
		t.Fatalf("GCS should command autopilot START, got %#v", autopilotReq.Data)
	}

	waitGCSMission(t, c, missionID, func(mission map[string]interface{}) bool {
		return mission["status"] == "running"
	})
	waitGCSDrone(t, c, droneID, func(drone map[string]interface{}) bool {
		return drone["status"] == "busy"
	})

	waitFakeDroneRequest(t, requests, func(req gcsDroneRequest) bool {
		return req.TargetTopic == gcsTelemetry && req.TargetAction == "get_state"
	})
	drone := waitGCSDrone(t, c, droneID, func(drone map[string]interface{}) bool {
		position, _ := drone["last_position"].(map[string]interface{})
		return drone["battery"] != nil && position != nil && position["latitude"] != nil && position["longitude"] != nil
	})
	if battery, ok := drone["battery"].(float64); !ok || battery != 87.0 {
		t.Fatalf("GCS should persist telemetry battery=87, got %#v", drone)
	}
}

func TestGCS_R004_TaskSubmitRejectsInvalidRoute(t *testing.T) {
	c := newGCSClient(t, "gcs_contract_r004")

	pl := c.request(t, gcsOrchestratorTopic, "task.submit", map[string]interface{}{
		"waypoints": []interface{}{
			map[string]interface{}{"lat": 55.751244, "lon": 37.618423, "alt_m": 120.0},
		},
	}, 15.0)
	if errMsg, _ := pl["error"].(string); errMsg != "failed to build route" {
		t.Fatalf("task.submit should reject invalid route, got %#v", pl)
	}
	if missionID, _ := pl["mission_id"].(string); missionID != "" {
		t.Fatalf("invalid route should not return mission_id, got %#v", pl)
	}
}

func TestGCS_R005_TaskAssignRejectsUnknownMission(t *testing.T) {
	c := newGCSClient(t, "gcs_contract_r005")
	requests := startFakeDroneSecurityMonitor(t, c)
	missionID := fmt.Sprintf("missing-%d", time.Now().UnixNano())
	droneID := fmt.Sprintf("DELIVERY-GCS-MISSING-%d", time.Now().UnixNano())

	pl := c.request(t, gcsOrchestratorTopic, "task.assign", map[string]interface{}{
		"mission_id": missionID,
		"drone_id":   droneID,
	}, 30.0)
	if ok, _ := pl["ok"].(bool); ok {
		t.Fatalf("task.assign should reject unknown mission, got %#v", pl)
	}
	if errMsg, _ := pl["error"].(string); errMsg != "mission_prepare_failed" {
		t.Fatalf("unexpected unknown mission error: %#v", pl)
	}

	select {
	case req := <-requests:
		if req.TargetTopic == gcsMissionHandler && req.TargetAction == "load_mission" {
			t.Fatalf("GCS must not upload unknown mission to drone: %#v", req)
		}
	case <-time.After(1 * time.Second):
	}
}

func TestGCS_R006_TaskStartDoesNotRunMissionWhenAutopilotRejects(t *testing.T) {
	c := newGCSClient(t, "gcs_contract_r006")
	requests := startFakeDroneSecurityMonitorWithAutopilot(t, c, false)
	missionID := submitGCSMission(t, c)
	droneID := fmt.Sprintf("DELIVERY-GCS-REJECT-%d", time.Now().UnixNano())

	c.request(t, gcsOrchestratorTopic, "task.assign", map[string]interface{}{
		"mission_id": missionID,
		"drone_id":   droneID,
	}, 30.0)
	waitFakeDroneRequest(t, requests, func(req gcsDroneRequest) bool {
		return req.TargetTopic == gcsMissionHandler && req.TargetAction == "load_mission"
	})
	waitGCSMission(t, c, missionID, func(mission map[string]interface{}) bool {
		return mission["status"] == "assigned"
	})

	pl := c.request(t, gcsOrchestratorTopic, "task.start", map[string]interface{}{
		"mission_id": missionID,
		"drone_id":   droneID,
	}, 20.0)
	if ok, _ := pl["ok"].(bool); !ok {
		t.Fatalf("task.start should acknowledge forwarding command to drone_manager, got %#v", pl)
	}
	waitFakeDroneRequest(t, requests, func(req gcsDroneRequest) bool {
		return req.TargetTopic == gcsAutopilot && req.TargetAction == "cmd"
	})
	time.Sleep(1 * time.Second)

	missionPL := c.request(t, gcsMissionStoreTopic, "store.get_mission", map[string]interface{}{"mission_id": missionID}, 5.0)
	mission, _ := missionPL["mission"].(map[string]interface{})
	if mission == nil {
		t.Fatalf("mission disappeared after autopilot rejection: %#v", missionPL)
	}
	if mission["status"] == "running" {
		t.Fatalf("mission must not become running after autopilot rejection: %#v", mission)
	}

	dronePL := c.request(t, gcsDroneStoreTopic, "store.get_drone", map[string]interface{}{"drone_id": droneID}, 5.0)
	drone, _ := dronePL["drone"].(map[string]interface{})
	if drone == nil {
		t.Fatalf("drone disappeared after autopilot rejection: %#v", dronePL)
	}
	if drone["status"] == "busy" {
		t.Fatalf("drone must not become busy after autopilot rejection: %#v", drone)
	}
}

func TestGCS_R007_GCSGeneratedWPLIsAcceptedByDroneMissionParser(t *testing.T) {
	c := newGCSClient(t, "gcs_contract_r007")
	requests := startFakeDroneSecurityMonitor(t, c)
	missionID := submitGCSMission(t, c)
	droneID := fmt.Sprintf("DELIVERY-GCS-WPL-%d", time.Now().UnixNano())

	c.request(t, gcsOrchestratorTopic, "task.assign", map[string]interface{}{
		"mission_id": missionID,
		"drone_id":   droneID,
	}, 30.0)
	req := waitFakeDroneRequest(t, requests, func(req gcsDroneRequest) bool {
		return req.TargetTopic == gcsMissionHandler && req.TargetAction == "load_mission"
	})

	wpl, _ := req.Data["wpl_content"].(string)
	if !strings.HasPrefix(wpl, "QGC WPL 110") {
		t.Fatalf("GCS should send QGC WPL 110 content, got %#v", req.Data)
	}
	mission, errMsg := missionhandler.ParseWPL(wpl, missionID)
	if errMsg != "" {
		t.Fatalf("delivery drone mission_handler parser rejected GCS WPL: %s, wpl=%q", errMsg, wpl)
	}
	if mission["mission_id"] != missionID {
		t.Fatalf("parsed mission_id mismatch: %#v", mission)
	}
	steps, _ := mission["steps"].([]interface{})
	if len(steps) == 0 {
		t.Fatalf("parsed GCS WPL should contain mission steps: %#v", mission)
	}
}

func TestGCS_R008_DroneMissionParserRejectsInvalidWPL(t *testing.T) {
	mission, errMsg := missionhandler.ParseWPL("QGC WPL 110\n0\t1\t3\t16\t0\t0\t0\t0\t55.75\t37.61\t120\t1", "invalid-wpl")
	if errMsg != "no_waypoints_in_wpl" {
		t.Fatalf("expected no_waypoints_in_wpl, got mission=%#v err=%q", mission, errMsg)
	}
}
