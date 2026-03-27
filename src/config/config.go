// Package config reads broker and component settings from environment (aligned with platform).
package config

import (
	"os"
	"strconv"
	"strings"
)

// Config holds broker and component configuration from env.
type Config struct {
	BrokerType     string // kafka | mqtt
	ComponentID    string // COMPONENT_ID or SYSTEM_ID
	ComponentTopic string // COMPONENT_TOPIC or hierarchical default
	SystemName     string // SYSTEM_NAME (default deliverydron)
	TopicVersion   string // TOPIC_VERSION (default v1) — first segment of hierarchical topics
	InstanceID     string // INSTANCE_ID (default Delivery001) — third segment
	HealthPort     string // HEALTH_PORT for HTTP health endpoint

	// Kafka
	KafkaBootstrap string
	KafkaGroupID   string
	BrokerUser     string
	BrokerPassword string

	// MQTT
	MQTTBroker string
	MQTTPort   int
	MQTTQoS    int
}

// FromEnv loads configuration from environment variables.
func FromEnv() *Config {
	brokerType := os.Getenv("BROKER_TYPE")
	if brokerType == "" {
		brokerType = "kafka"
	}
	componentID := os.Getenv("COMPONENT_ID")
	if componentID == "" {
		componentID = os.Getenv("SYSTEM_ID")
	}
	if componentID == "" {
		componentID = "delivery_drone"
	}
	systemName := strings.TrimSpace(os.Getenv("SYSTEM_NAME"))
	if systemName == "" {
		systemName = "deliverydron"
	}
	topicVersion := strings.TrimSpace(os.Getenv("TOPIC_VERSION"))
	if topicVersion == "" {
		topicVersion = "v1"
	}
	instanceID := strings.TrimSpace(os.Getenv("INSTANCE_ID"))
	if instanceID == "" {
		instanceID = "Delivery001"
	}
	componentTopic := strings.TrimSpace(os.Getenv("COMPONENT_TOPIC"))
	cfg := &Config{
		BrokerType:     brokerType,
		ComponentID:    componentID,
		SystemName:     systemName,
		TopicVersion:   topicVersion,
		InstanceID:     instanceID,
		ComponentTopic: componentTopic,
	}
	if cfg.ComponentTopic == "" {
		cfg.ComponentTopic = cfg.BrokerTopicFor(componentID)
	}
	healthPort := os.Getenv("HEALTH_PORT")
	if healthPort == "" {
		healthPort = "8080"
	}
	cfg.HealthPort = healthPort

	kafkaBootstrap := os.Getenv("KAFKA_BOOTSTRAP_SERVERS")
	if kafkaBootstrap == "" {
		host := os.Getenv("KAFKA_HOST")
		if host == "" {
			host = "localhost"
		}
		port := os.Getenv("KAFKA_PORT")
		if port == "" {
			port = "9092"
		}
		kafkaBootstrap = host + ":" + port
	}
	kafkaGroupID := os.Getenv("KAFKA_GROUP_ID")
	if kafkaGroupID == "" {
		kafkaGroupID = componentID + "_group"
	}

	mqttBroker := os.Getenv("MQTT_BROKER")
	if mqttBroker == "" {
		mqttBroker = os.Getenv("MQTT_HOST")
	}
	if mqttBroker == "" {
		mqttBroker = "localhost"
	}
	mqttPort := 1883
	if p := os.Getenv("MQTT_PORT"); p != "" {
		if v, err := strconv.Atoi(strings.TrimSpace(p)); err == nil {
			mqttPort = v
		}
	}
	mqttQoS := 1
	if q := os.Getenv("MQTT_QOS"); q != "" {
		if v, err := strconv.Atoi(strings.TrimSpace(q)); err == nil {
			mqttQoS = v
		}
	}

	cfg.KafkaBootstrap = kafkaBootstrap
	cfg.KafkaGroupID = kafkaGroupID
	cfg.BrokerUser = os.Getenv("BROKER_USER")
	cfg.BrokerPassword = os.Getenv("BROKER_PASSWORD")
	cfg.MQTTBroker = mqttBroker
	cfg.MQTTPort = mqttPort
	cfg.MQTTQoS = mqttQoS
	return cfg
}

// TopicPrefix returns TOPIC_VERSION.SYSTEM_NAME.INSTANCE_ID (no trailing dot).
// Policy placeholders ${SYSTEM_NAME} and ${TOPIC_PREFIX} expand to this string (three segments, no trailing component).
func (c *Config) TopicPrefix() string {
	v := strings.TrimSpace(c.TopicVersion)
	if v == "" {
		v = "v1"
	}
	sys := strings.TrimSpace(c.SystemName)
	if sys == "" {
		sys = "deliverydron"
	}
	inst := strings.TrimSpace(c.InstanceID)
	if inst == "" {
		inst = "Delivery001"
	}
	return v + "." + sys + "." + inst
}

// BrokerTopicFor returns the full broker topic for an internal component segment, e.g.
// "autopilot" -> "v1.deliverydron.Delivery001.autopilot".
func (c *Config) BrokerTopicFor(component string) string {
	return c.TopicPrefix() + "." + strings.TrimSpace(component)
}
