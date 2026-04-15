// Package bus provides the broker abstraction and factory.
package bus

import (
	"fmt"

	"github.com/AMCP-Drones/drones/components/bus/src/kafka"
	"github.com/AMCP-Drones/drones/components/bus/src/mqtt"
	"github.com/AMCP-Drones/drones/components/config/src"
)

// New creates a Bus (Kafka or MQTT) from config. BROKER_TYPE selects implementation.
func New(cfg *config.Config) (Bus, error) {
	switch cfg.BrokerType {
	case "kafka":
		return kafka.New(cfg.KafkaBootstrap, cfg.ComponentID, cfg.KafkaGroupID, cfg.BrokerUser, cfg.BrokerPassword), nil
	case "mqtt":
		return mqtt.New(cfg.MQTTBroker, cfg.MQTTPort, cfg.ComponentID, cfg.MQTTQoS, cfg.BrokerUser, cfg.BrokerPassword), nil
	default:
		return nil, fmt.Errorf("unknown broker type: %q (use kafka or mqtt)", cfg.BrokerType)
	}
}

// MustNew is like New but panics on error (for main).
func MustNew(cfg *config.Config) Bus {
	b, err := New(cfg)
	if err != nil {
		panic(err)
	}
	return b
}
