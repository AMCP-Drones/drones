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
	ComponentTopic string // COMPONENT_TOPIC or SYSTEM_NAME.COMPONENT_ID
	SystemName     string // SYSTEM_NAME (default deliverydron)
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
	componentTopic := strings.TrimSpace(os.Getenv("COMPONENT_TOPIC"))
	if componentTopic == "" {
		componentTopic = systemName + "." + componentID
	}
	healthPort := os.Getenv("HEALTH_PORT")
	if healthPort == "" {
		healthPort = "8080"
	}

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
		if v, err := strconv.Atoi(p); err == nil {
			mqttPort = v
		}
	}
	mqttQoS := 1
	if q := os.Getenv("MQTT_QOS"); q != "" {
		if v, err := strconv.Atoi(q); err == nil {
			mqttQoS = v
		}
	}

	return &Config{
		BrokerType:     brokerType,
		ComponentID:    componentID,
		ComponentTopic: componentTopic,
		SystemName:     systemName,
		HealthPort:     healthPort,
		KafkaBootstrap: kafkaBootstrap,
		KafkaGroupID:   kafkaGroupID,
		BrokerUser:     os.Getenv("BROKER_USER"),
		BrokerPassword: os.Getenv("BROKER_PASSWORD"),
		MQTTBroker:     mqttBroker,
		MQTTPort:       mqttPort,
		MQTTQoS:        mqttQoS,
	}
}
