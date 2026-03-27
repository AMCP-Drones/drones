package config

import (
	"os"
	"testing"
)

func TestFromEnv_Defaults(t *testing.T) {
	os.Clearenv()
	cfg := FromEnv()
	if cfg.BrokerType != "kafka" {
		t.Errorf("BrokerType: got %s", cfg.BrokerType)
	}
	if cfg.ComponentID != "delivery_drone" {
		t.Errorf("ComponentID: got %s", cfg.ComponentID)
	}
	if cfg.TopicVersion != "v1" {
		t.Errorf("TopicVersion: got %s", cfg.TopicVersion)
	}
	if cfg.InstanceID != "Delivery001" {
		t.Errorf("InstanceID: got %s", cfg.InstanceID)
	}
	if cfg.SystemName != "deliverydron" {
		t.Errorf("SystemName: got %s", cfg.SystemName)
	}
	wantTopic := "v1.deliverydron.Delivery001.delivery_drone"
	if cfg.ComponentTopic != wantTopic {
		t.Errorf("ComponentTopic: got %s want %s", cfg.ComponentTopic, wantTopic)
	}
	if cfg.TopicPrefix() != "v1.deliverydron.Delivery001" {
		t.Errorf("TopicPrefix: got %s", cfg.TopicPrefix())
	}
	if cfg.HealthPort != "8080" {
		t.Errorf("HealthPort: got %s", cfg.HealthPort)
	}
}

func TestFromEnv_Override(t *testing.T) {
	os.Clearenv()
	if err := os.Setenv("BROKER_TYPE", "mqtt"); err != nil {
		t.Fatal(err)
	}
	if err := os.Setenv("COMPONENT_ID", "drone_1"); err != nil {
		t.Fatal(err)
	}
	if err := os.Setenv("HEALTH_PORT", "9090"); err != nil {
		t.Fatal(err)
	}
	cfg := FromEnv()
	if cfg.BrokerType != "mqtt" {
		t.Errorf("BrokerType: got %s", cfg.BrokerType)
	}
	if cfg.ComponentID != "drone_1" {
		t.Errorf("ComponentID: got %s", cfg.ComponentID)
	}
	if cfg.HealthPort != "9090" {
		t.Errorf("HealthPort: got %s", cfg.HealthPort)
	}
}

func TestFromEnv_ComponentTopicOverride(t *testing.T) {
	os.Clearenv()
	_ = os.Setenv("COMPONENT_TOPIC", "custom.flat.topic")
	_ = os.Setenv("COMPONENT_ID", "autopilot")
	cfg := FromEnv()
	if cfg.ComponentTopic != "custom.flat.topic" {
		t.Errorf("ComponentTopic: got %s", cfg.ComponentTopic)
	}
}
