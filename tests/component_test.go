package tests

import (
	"context"
	"testing"

	"github.com/AMCP-Drones/drones/systems/deliverydron/delivery/src"
	"github.com/AMCP-Drones/drones/tests/testutil"
)

func TestModule_DeliveryDrone_Echo(t *testing.T) {
	mem := testutil.NewMemoryBus()
	ctx := context.Background()
	topic := testutil.Config("delivery_drone").BrokerTopicFor("delivery_drone")
	drone := delivery.New("test_drone", "Test", topic, mem)
	if err := mem.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := drone.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = drone.Stop(ctx) }()

	resp, err := mem.Request(ctx, topic, map[string]interface{}{
		"action":  "echo",
		"payload": map[string]interface{}{"message": "hello"},
		"sender":  "client",
	}, 2.0)
	if err != nil {
		t.Fatal(err)
	}
	pl, _ := resp["payload"].(map[string]interface{})
	if pl == nil {
		t.Fatalf("response: %#v", resp)
	}
	echo, _ := pl["echo"].(map[string]interface{})
	if echo == nil || echo["message"] != "hello" {
		t.Fatalf("echo payload: %#v", pl)
	}
}

func TestModule_DeliveryDrone_DeliverPackage(t *testing.T) {
	mem := testutil.NewMemoryBus()
	ctx := context.Background()
	topic := testutil.Config("delivery_drone").BrokerTopicFor("delivery_drone")
	drone := delivery.New("test_drone", "Test", topic, mem)
	if err := mem.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := drone.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = drone.Stop(ctx) }()

	_, err := mem.Request(ctx, topic, map[string]interface{}{
		"action":  "deliver_package",
		"payload": map[string]interface{}{"destination": "warehouse_1"},
		"sender":  "client",
	}, 2.0)
	if err != nil {
		t.Fatal(err)
	}

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

func TestModule_DeliveryDrone_GetDeliveryStatus(t *testing.T) {
	mem := testutil.NewMemoryBus()
	ctx := context.Background()
	topic := testutil.Config("delivery_drone").BrokerTopicFor("delivery_drone")
	drone := delivery.New("test_drone", "Test", topic, mem)
	if err := mem.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := drone.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = drone.Stop(ctx) }()

	resp, err := mem.Request(ctx, topic, map[string]interface{}{
		"action":  "get_delivery_status",
		"payload": map[string]interface{}{},
		"sender":  "client",
	}, 2.0)
	if err != nil {
		t.Fatal(err)
	}
	pl, _ := resp["payload"].(map[string]interface{})
	if pl == nil {
		t.Fatal("response payload missing")
	}
	if pl["component_id"] != "test_drone" {
		t.Errorf("expected component_id=test_drone, got %v", pl["component_id"])
	}
	if pl["status"] != "idle" {
		t.Errorf("expected status=idle, got %v", pl["status"])
	}
}
