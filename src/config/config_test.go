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
	if cfg.HealthPort != "8080" {
		t.Errorf("HealthPort: got %s", cfg.HealthPort)
	}
}

func TestFromEnv_Override(t *testing.T) {
	os.Clearenv()
	os.Setenv("BROKER_TYPE", "mqtt")
	os.Setenv("COMPONENT_ID", "drone_1")
	os.Setenv("HEALTH_PORT", "9090")
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
