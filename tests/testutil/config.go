package testutil

import "github.com/AMCP-Drones/drones/systems/deliverydron/config/src"

// Config returns a config for in-memory tests: hierarchical topics v1.testsys.T001.<componentID>.
func Config(componentID string) *config.Config {
	return &config.Config{
		BrokerType:     "kafka",
		ComponentID:    componentID,
		SystemName:     "testsys",
		TopicVersion:   "v1",
		InstanceID:     "T001",
		KafkaBootstrap: "localhost:9092",
	}
}

// TopicPrefix is v1.testsys.T001 for building policy JSON.
func TopicPrefix() string {
	return Config("x").TopicPrefix()
}
