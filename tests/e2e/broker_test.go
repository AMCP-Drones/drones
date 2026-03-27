//go:build e2e

package e2e

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/AMCP-Drones/drones/src/bus"
	"github.com/AMCP-Drones/drones/src/config"
)

// TestE2E_KafkaPubSub requires a running Kafka broker (e.g. docker compose from docker/).
// Run: E2E_KAFKA=1 KAFKA_BOOTSTRAP_SERVERS=localhost:9092 BROKER_USER=admin BROKER_PASSWORD=... go test -tags=e2e ./tests/e2e/... -v
func TestE2E_KafkaPubSub(t *testing.T) {
	if os.Getenv("E2E_KAFKA") != "1" {
		t.Skip("set E2E_KAFKA=1 to run against a real broker")
	}
	bootstrap := os.Getenv("KAFKA_BOOTSTRAP_SERVERS")
	if bootstrap == "" {
		t.Fatal("KAFKA_BOOTSTRAP_SERVERS is required for e2e kafka test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cfg := &config.Config{
		BrokerType:     "kafka",
		ComponentID:    "e2e_test_client",
		KafkaBootstrap: bootstrap,
		KafkaGroupID:   "e2e_test_group",
		BrokerUser:     os.Getenv("BROKER_USER"),
		BrokerPassword: os.Getenv("BROKER_PASSWORD"),
		SystemName:     "e2e",
		TopicVersion:   "v1",
		InstanceID:     "E2E001",
		ComponentTopic: "v1.e2e.E2E001.e2e_probe",
	}

	b, err := bus.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	received := make(chan map[string]interface{}, 1)
	if err := b.Subscribe(ctx, cfg.ComponentTopic, func(m map[string]interface{}) {
		if action, _ := m["action"].(string); action == "e2e_ping" {
			select {
			case received <- m:
			default:
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := b.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Stop(context.Background()) }()

	time.Sleep(2 * time.Second) // consumer join

	if err := b.Publish(ctx, cfg.ComponentTopic, map[string]interface{}{
		"action": "e2e_ping",
		"sender": "e2e",
		"payload": map[string]interface{}{
			"n": 42,
		},
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case m := <-received:
		pl, _ := m["payload"].(map[string]interface{})
		var n int
		switch v := pl["n"].(type) {
		case float64:
			n = int(v)
		case int:
			n = v
		case int64:
			n = int(v)
		default:
			t.Fatalf("unexpected n type %T: %#v", pl["n"], pl)
		}
		if n != 42 {
			t.Fatalf("payload n: %v", pl["n"])
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for kafka message")
	}
}
