package bus

import (
	"testing"

	"github.com/AMCP-Drones/drones/systems/deliverydron/config/src"
)

func TestNew_UnknownBrokerType(t *testing.T) {
	cfg := &config.Config{BrokerType: "nats"}
	_, err := New(cfg)
	if err == nil {
		t.Fatal("expected error for unknown broker type")
	}
}

func TestMustNew_PanicsOnError(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic")
		}
	}()
	MustNew(&config.Config{BrokerType: "invalid"})
}

func TestNew_KafkaAndMQTT(t *testing.T) {
	k, err := New(&config.Config{
		BrokerType: "kafka", KafkaBootstrap: "localhost:9092",
		ComponentID: "c1", KafkaGroupID: "g1",
	})
	if err != nil || k == nil {
		t.Fatalf("kafka: err=%v bus=%v", err, k)
	}
	m, err := New(&config.Config{
		BrokerType: "mqtt", MQTTBroker: "localhost", MQTTPort: 1883,
		ComponentID: "c2", MQTTQoS: 1,
	})
	if err != nil || m == nil {
		t.Fatalf("mqtt: err=%v bus=%v", err, m)
	}
}

func TestMustNew_Success(t *testing.T) {
	b := MustNew(&config.Config{
		BrokerType: "kafka", KafkaBootstrap: "localhost:9092",
		ComponentID: "c3", KafkaGroupID: "g1",
	})
	if b == nil {
		t.Fatal("expected bus")
	}
}
