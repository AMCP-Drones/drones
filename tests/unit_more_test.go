package tests

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/AMCP-Drones/drones/systems/deliverydron/component/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/config/src"
	missionhandler "github.com/AMCP-Drones/drones/systems/deliverydron/mission_handler/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/sdk/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/tests/testutil"
)

func TestUnit_Config_FromEnv_DefaultsAndOverrides(t *testing.T) {
	t.Setenv("BROKER_TYPE", "mqtt")
	t.Setenv("COMPONENT_ID", "motors")
	t.Setenv("SYSTEM_NAME", "sysx")
	t.Setenv("TOPIC_SCHEME", "components")
	t.Setenv("TOPIC_PREFIX", "")
	t.Setenv("MQTT_BROKER", "broker")
	t.Setenv("MQTT_PORT", "2883")
	t.Setenv("MQTT_QOS", "2")
	t.Setenv("KAFKA_HOST", "khost")
	t.Setenv("KAFKA_PORT", "19092")
	t.Setenv("KAFKA_GROUP_ID", "g1")

	cfg := config.FromEnv()
	if cfg.BrokerType != "mqtt" || cfg.ComponentID != "motors" {
		t.Fatalf("unexpected identity config: %#v", cfg)
	}
	if cfg.TopicPrefix() != "components.sysx" || cfg.ComponentTopic != "components.sysx.motors" {
		t.Fatalf("unexpected topic config: prefix=%q topic=%q", cfg.TopicPrefix(), cfg.ComponentTopic)
	}
	if cfg.MQTTBroker != "broker" || cfg.MQTTPort != 2883 || cfg.MQTTQoS != 2 {
		t.Fatalf("unexpected mqtt config: %#v", cfg)
	}
	if cfg.KafkaBootstrap != "khost:19092" || cfg.KafkaGroupID != "g1" {
		t.Fatalf("unexpected kafka config: %#v", cfg)
	}
}

func TestUnit_SDK_MessageHelpers(t *testing.T) {
	msg := sdk.NewMessage("act", nil, "sender", "cid-1", "rep", "")
	if msg.Payload == nil || msg.Timestamp == "" {
		t.Fatalf("new message defaults are missing: %#v", msg)
	}
	asMap := msg.ToMap()
	if asMap["action"] != "act" || asMap["correlation_id"] != "cid-1" || asMap["reply_to"] != "rep" {
		t.Fatalf("unexpected map shape: %#v", asMap)
	}

	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := sdk.ParseMessage(raw)
	if err != nil || parsed.Action != "act" || parsed.Sender != "sender" {
		t.Fatalf("parse failed: err=%v parsed=%#v", err, parsed)
	}
}

func TestUnit_MissionHandler_ParseWPL_PositiveAndNegative(t *testing.T) {
	wpl := "QGC WPL 110\n" +
		"0\t1\t0\t16\t0\t0\t0\t0\t55.75\t37.62\t100\t1\n" +
		"1\t0\t0\t16\t0\t0\t0\t0\t55.76\t37.63\t120\t1\n"
	mission, errMsg := missionhandler.ParseWPL(wpl, "m-wpl")
	if errMsg != "" || mission == nil {
		t.Fatalf("expected parsed mission, err=%q mission=%#v", errMsg, mission)
	}
	steps, _ := mission["steps"].([]interface{})
	if len(steps) != 1 {
		t.Fatalf("expected one step, got %#v", mission["steps"])
	}

	badMission, badErr := missionhandler.ParseWPL("broken header\n1 2 3", "")
	if badMission != nil || badErr == "" {
		t.Fatalf("expected parse failure, got mission=%#v err=%q", badMission, badErr)
	}
}

func TestUnit_Component_BuiltinHandlersViaBus(t *testing.T) {
	ctx := context.Background()
	mem := testutil.NewMemoryBus()
	cfg := testutil.Config("base_component")
	c := component.NewBaseComponent("base_component", "base", cfg.BrokerTopicFor("base_component"), mem)
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Stop(ctx) })

	pingResp, err := mem.Request(ctx, c.Topic, map[string]interface{}{
		"action": "ping", "sender": "client",
	}, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	pingPayload, _ := pingResp["payload"].(map[string]interface{})
	if pingPayload["pong"] != true {
		t.Fatalf("unexpected ping payload: %#v", pingPayload)
	}

	statusResp, err := mem.Request(ctx, c.Topic, map[string]interface{}{
		"action": "get_status", "sender": "client",
	}, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	statusPayload, _ := statusResp["payload"].(map[string]interface{})
	if statusPayload["component_id"] != "base_component" || statusPayload["running"] != true {
		t.Fatalf("unexpected status payload: %#v", statusPayload)
	}

	unknownResp, err := mem.Request(ctx, c.Topic, map[string]interface{}{
		"action": "unknown_action", "sender": "client",
	}, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	if unknownResp["success"] != false {
		t.Fatalf("unknown action must fail: %#v", unknownResp)
	}
}
