package config

import (
	"os"
	"testing"
)

func TestFromEnv_KafkaHostPortFallback(t *testing.T) {
	_ = os.Unsetenv("KAFKA_BOOTSTRAP_SERVERS")
	t.Setenv("KAFKA_HOST", "kafka.local")
	t.Setenv("KAFKA_PORT", "9093")
	t.Setenv("BROKER_TYPE", "kafka")
	cfg := FromEnv()
	if cfg.KafkaBootstrap != "kafka.local:9093" {
		t.Fatalf("bootstrap=%q", cfg.KafkaBootstrap)
	}
}

func TestFromEnv_MQTTDefaults(t *testing.T) {
	t.Setenv("BROKER_TYPE", "mqtt")
	t.Setenv("MQTT_BROKER", "")
	t.Setenv("MQTT_HOST", "mqtt.local")
	t.Setenv("MQTT_PORT", "1884")
	t.Setenv("MQTT_QOS", "2")
	cfg := FromEnv()
	if cfg.MQTTBroker != "mqtt.local" || cfg.MQTTPort != 1884 || cfg.MQTTQoS != 2 {
		t.Fatalf("mqtt=%s:%d qos=%d", cfg.MQTTBroker, cfg.MQTTPort, cfg.MQTTQoS)
	}
}

func TestFromEnv_ComponentIDFallbacks(t *testing.T) {
	_ = os.Unsetenv("COMPONENT_ID")
	t.Setenv("SYSTEM_ID", "sys_id")
	cfg := FromEnv()
	if cfg.ComponentID != "sys_id" {
		t.Fatalf("component_id=%q", cfg.ComponentID)
	}
}

func TestBrokerTopicFor_TrimsComponent(t *testing.T) {
	cfg := &Config{TopicVersion: "v1", SystemName: "s", InstanceID: "i"}
	got := cfg.BrokerTopicFor("  nav  ")
	if got != "v1.s.i.nav" {
		t.Fatalf("got %q", got)
	}
}
