package tests

import (
	"context"
	"sync"
	"testing"

	"github.com/AMCP-Drones/drones/src/delivery"
	"github.com/AMCP-Drones/drones/src/sdk"
)

// mockBus records published messages and invokes the subscribed handler when Deliver is called.
type mockBus struct {
	mu        sync.Mutex
	published []struct {
		topic   string
		message map[string]interface{}
	}
	handler    func(map[string]interface{})
	subscribed string
}

func (m *mockBus) Publish(_ context.Context, topic string, message map[string]interface{}) error {
	m.mu.Lock()
	m.published = append(m.published, struct {
		topic   string
		message map[string]interface{}
	}{topic, message})
	m.mu.Unlock()
	return nil
}

func (m *mockBus) Subscribe(_ context.Context, topic string, handler func(map[string]interface{})) error {
	m.mu.Lock()
	m.subscribed = topic
	m.handler = handler
	m.mu.Unlock()
	return nil
}

func (m *mockBus) Unsubscribe(_ context.Context, topic string) error {
	m.mu.Lock()
	m.handler = nil
	m.subscribed = ""
	m.mu.Unlock()
	return nil
}

func (m *mockBus) Request(_ context.Context, topic string, message map[string]interface{}, timeoutSec float64) (map[string]interface{}, error) {
	return sdk.CreateResponse("req1", map[string]interface{}{"ok": true}, "mock", true, ""), nil
}

func (m *mockBus) Start(_ context.Context) error {
	return nil
}

func (m *mockBus) Stop(_ context.Context) error {
	return nil
}

// Deliver simulates receiving a message (call after Start).
func (m *mockBus) Deliver(message map[string]interface{}) {
	m.mu.Lock()
	h := m.handler
	m.mu.Unlock()
	if h != nil {
		h(message)
	}
}

func (m *mockBus) Published() []struct {
	Topic   string
	Message map[string]interface{}
} {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]struct {
		Topic   string
		Message map[string]interface{}
	}, len(m.published))
	for i, p := range m.published {
		out[i].Topic = p.topic
		out[i].Message = p.message
	}
	return out
}

func TestDeliveryDrone_Echo(t *testing.T) {
	bus := &mockBus{}
	drone := delivery.New("test_drone", "Test", "components.delivery_drone", bus)
	ctx := context.Background()
	if err := drone.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = drone.Stop(ctx) }()

	bus.Deliver(map[string]interface{}{
		"action":         "echo",
		"payload":        map[string]interface{}{"message": "hello"},
		"sender":         "client",
		"reply_to":       "replies.client",
		"correlation_id": "c1",
	})

	pub := bus.Published()
	if len(pub) != 1 {
		t.Fatalf("expected 1 published message, got %d", len(pub))
	}
	if pub[0].Topic != "replies.client" {
		t.Errorf("reply_to topic: got %s", pub[0].Topic)
	}
	payload, _ := pub[0].Message["payload"].(map[string]interface{})
	if payload == nil {
		t.Fatal("response payload missing")
	}
	echo, _ := payload["echo"].(map[string]interface{})
	if echo == nil || echo["message"] != "hello" {
		t.Errorf("expected echo message hello, got %v", payload)
	}
}

func TestDeliveryDrone_DeliverPackage(t *testing.T) {
	bus := &mockBus{}
	drone := delivery.New("test_drone", "Test", "components.delivery_drone", bus)
	ctx := context.Background()
	if err := drone.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = drone.Stop(ctx) }()

	bus.Deliver(map[string]interface{}{
		"action":         "deliver_package",
		"payload":        map[string]interface{}{"destination": "warehouse_1"},
		"sender":         "client",
		"reply_to":       "replies.client",
		"correlation_id": "c1",
	})

	state := drone.State()
	if state["deliveries"].(int) != 1 {
		t.Errorf("expected deliveries=1, got %v", state["deliveries"])
	}
	if state["status"] != "delivering" {
		t.Errorf("expected status=delivering, got %v", state["status"])
	}
	if state["last_destination"] != "warehouse_1" {
		t.Errorf("expected last_destination=warehouse_1, got %v", state["last_destination"])
	}
}

func TestDeliveryDrone_GetDeliveryStatus(t *testing.T) {
	bus := &mockBus{}
	drone := delivery.New("test_drone", "Test", "components.delivery_drone", bus)
	ctx := context.Background()
	if err := drone.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = drone.Stop(ctx) }()

	bus.Deliver(map[string]interface{}{
		"action":         "get_delivery_status",
		"payload":        map[string]interface{}{},
		"sender":         "client",
		"reply_to":       "replies.client",
		"correlation_id": "c1",
	})

	pub := bus.Published()
	if len(pub) != 1 {
		t.Fatalf("expected 1 published message, got %d", len(pub))
	}
	payload, _ := pub[0].Message["payload"].(map[string]interface{})
	if payload == nil {
		t.Fatal("response payload missing")
	}
	if payload["component_id"] != "test_drone" {
		t.Errorf("expected component_id=test_drone, got %v", payload["component_id"])
	}
	if payload["status"] != "idle" {
		t.Errorf("expected status=idle, got %v", payload["status"])
	}
}
