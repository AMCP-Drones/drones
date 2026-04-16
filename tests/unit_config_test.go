package tests

import (
	"testing"

	"github.com/AMCP-Drones/drones/systems/deliverydron/config/src"
)

func TestUnit_Config_TopicPrefixAndBrokerTopic(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		ComponentID:  "autopilot",
		SystemName:   "mysys",
		TopicVersion: "v2",
		InstanceID:   "U2",
	}
	if cfg.TopicPrefix() != "v2.mysys.U2" {
		t.Fatalf("TopicPrefix: %q", cfg.TopicPrefix())
	}
	if cfg.BrokerTopicFor("motors") != "v2.mysys.U2.motors" {
		t.Fatalf("BrokerTopicFor: %q", cfg.BrokerTopicFor("motors"))
	}
}

func TestUnit_Config_TopicPrefixFillsEmptySegments(t *testing.T) {
	t.Parallel()
	c := &config.Config{SystemName: "", InstanceID: "", TopicVersion: ""}
	if c.TopicPrefix() != "v1.deliverydron.Delivery001" {
		t.Fatalf("got %q", c.TopicPrefix())
	}
}
