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
