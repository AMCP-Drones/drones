//go:build orvd
// +build orvd

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
	orvdExternalTopic = "v1.ORVD.ORVD001.main"
	orvdSystemTopic   = "systems.orvd_system"
)

func newORVDClient(t *testing.T, id string) bus.Bus {
	t.Helper()
	b, err := bus.New(&config.Config{
		BrokerType:     "kafka",
		ComponentID:    id,
		KafkaBootstrap: "localhost:9092",
		KafkaGroupID:   id,
		BrokerUser:     "admin",
		BrokerPassword: "admin_secret_123",
	})
	if err != nil {
		t.Fatalf("create mqtt bus: %v", err)
	}

	ctx := context.Background()
	if err := b.Start(ctx); err != nil {
		t.Fatalf("start mqtt bus: %v", err)
	}
	t.Cleanup(func() { _ = b.Stop(ctx) })

	return b
}

func requestORVD(t *testing.T, b bus.Bus, topic, action string, payload map[string]interface{}) map[string]interface{} {
	t.Helper()
	resp, err := b.Request(context.Background(), topic, map[string]interface{}{
		"action":  action,
		"sender":  "delivery_drone_contract_test",
		"payload": payload,
	}, 10.0)
	if err != nil {
		t.Fatalf("ORVD %s request failed: %v", action, err)
	}
	if success, _ := resp["success"].(bool); !success {
		t.Fatalf("ORVD %s returned unsuccessful response: %#v", action, resp)
	}
	pl, ok := resp["payload"].(map[string]interface{})
	if !ok || pl == nil {
		t.Fatalf("ORVD %s response has no payload: %#v", action, resp)
	}
	return pl
}

func expectORVDStatus(t *testing.T, payload map[string]interface{}, want string) {
	t.Helper()
	got, _ := payload["status"].(string)
	if got != want {
		t.Fatalf("unexpected ORVD status: got %q, want %q, payload=%#v", got, want, payload)
	}
}

func TestORVD_R000_SystemTopicIsReachable(t *testing.T) {
	b := newORVDClient(t, "orvd_contract_r000")

	pl := requestORVD(t, b, orvdSystemTopic, "get_status", map[string]interface{}{})
	running, _ := pl["running"].(bool)
	if !running {
		t.Fatalf("ORVD gateway on system topic is not running: %#v", pl)
	}
}

func TestORVD_R001_ExternalAPIHappyPath(t *testing.T) {
	b := newORVDClient(t, "orvd_contract_r001")
	now := time.Now().UnixNano()
	droneID := fmt.Sprintf("DELIVERY-DRONE-%d", now)
	missionID := float64(now % 1000000000)

	pl := requestORVD(t, b, orvdExternalTopic, "register_drone", map[string]interface{}{
		"drone_id": droneID,
		"model":    "DeliveryDrone-v2",
		"operator": "AMCP-Drones",
	})
	expectORVDStatus(t, pl, "registered")
	if pl["drone_id"] != droneID {
		t.Fatalf("registered drone_id mismatch: %#v", pl)
	}

	pl = requestORVD(t, b, orvdExternalTopic, "register_mission", map[string]interface{}{
		"mission_id": missionID,
		"drone_id":   droneID,
		"route": []interface{}{
			map[string]interface{}{"lat": 55.7558, "lon": 37.6173},
			map[string]interface{}{"lat": 55.7560, "lon": 37.6180},
		},
		"time":     time.Now().UTC().Format(time.RFC3339),
		"velocity": 10.5,
	})
	expectORVDStatus(t, pl, "mission_registered")

	pl = requestORVD(t, b, orvdExternalTopic, "authorize_mission", map[string]interface{}{
		"mission_id": missionID,
	})
	expectORVDStatus(t, pl, "authorized")

	pl = requestORVD(t, b, orvdExternalTopic, "request_takeoff", map[string]interface{}{
		"drone_id":   droneID,
		"mission_id": missionID,
		"time":       time.Now().UTC().Format(time.RFC3339),
	})
	expectORVDStatus(t, pl, "takeoff_authorized")

	pl = requestORVD(t, b, orvdExternalTopic, "send_telemetry", map[string]interface{}{
		"drone_id": droneID,
		"coords":   map[string]interface{}{"lat": 55.7559, "lon": 37.6175},
		"altitude": 120.0,
		"speed":    10.0,
	})
	expectORVDStatus(t, pl, "telemetry_received")
}

func TestORVD_R002_TakeoffDeniedBeforeAuthorization(t *testing.T) {
	b := newORVDClient(t, "orvd_contract_r002")
	now := time.Now().UnixNano()
	droneID := fmt.Sprintf("DELIVERY-DRONE-DENIED-%d", now)
	missionID := float64((now % 1000000000) + 1000000000)

	expectORVDStatus(t, requestORVD(t, b, orvdExternalTopic, "register_drone", map[string]interface{}{
		"drone_id": droneID,
		"model":    "DeliveryDrone-v2",
	}), "registered")

	expectORVDStatus(t, requestORVD(t, b, orvdExternalTopic, "register_mission", map[string]interface{}{
		"mission_id": missionID,
		"drone_id":   droneID,
		"route": []interface{}{
			map[string]interface{}{"lat": 55.7558, "lon": 37.6173},
			map[string]interface{}{"lat": 55.7560, "lon": 37.6180},
		},
		"time":     time.Now().UTC().Format(time.RFC3339),
		"velocity": 10.5,
	}), "mission_registered")

	pl := requestORVD(t, b, orvdExternalTopic, "request_takeoff", map[string]interface{}{
		"drone_id":   droneID,
		"mission_id": missionID,
		"time":       time.Now().UTC().Format(time.RFC3339),
	})
	expectORVDStatus(t, pl, "takeoff_denied")
	if reason, _ := pl["reason"].(string); reason != "mission not authorized" {
		t.Fatalf("unexpected denial reason: %#v", pl)
	}
}

func TestORVD_R003_InternalAndExternalTopicsAreBothRouted(t *testing.T) {
	b := newORVDClient(t, "orvd_contract_r003")

	for _, topic := range []string{orvdExternalTopic, orvdSystemTopic} {
		pl := requestORVD(t, b, topic, "get_status", map[string]interface{}{})
		running, _ := pl["running"].(bool)
		if !running {
			t.Fatalf("ORVD gateway on topic %s is not running: %#v", topic, pl)
		}
	}
}
