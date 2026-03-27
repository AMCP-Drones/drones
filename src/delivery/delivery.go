// Package delivery implements the delivery drone component (platform-facing, broker-based).
package delivery

import (
	"context"
	"log"
	"sync"

	"github.com/AMCP-Drones/drones/src/bus"
	"github.com/AMCP-Drones/drones/src/component"
)

const defaultTopic = "components.delivery_drone"

// Drone implements the delivery drone component: same message protocol as platform, handlers for delivery actions.
type Drone struct {
	*component.BaseComponent
	name    string
	state   map[string]interface{}
	stateMu sync.RWMutex
}

// New creates a delivery drone component. Call Start() after creation.
func New(componentID, name, topic string, b bus.Bus) *Drone {
	if topic == "" {
		topic = defaultTopic
	}
	base := component.NewBaseComponent(componentID, "delivery_drone", topic, b)
	d := &Drone{
		BaseComponent: base,
		name:          name,
		state:         map[string]interface{}{"status": "idle", "deliveries": 0},
	}
	d.registerHandlers()
	return d
}

func (d *Drone) registerHandlers() {
	d.RegisterHandler("echo", d.handleEcho)
	d.RegisterHandler("deliver_package", d.handleDeliverPackage)
	d.RegisterHandler("get_delivery_status", d.handleGetDeliveryStatus)
}

func (d *Drone) handleEcho(_ context.Context, message map[string]interface{}) (map[string]interface{}, error) {
	payload := message["payload"]
	if payload == nil {
		payload = map[string]interface{}{}
	}
	pl, _ := payload.(map[string]interface{})
	if pl == nil {
		pl = map[string]interface{}{"value": payload}
	}
	return map[string]interface{}{
		"echo":         pl,
		"from":         d.ComponentID,
		"component_id": d.ComponentID,
	}, nil
}

func (d *Drone) handleDeliverPackage(_ context.Context, message map[string]interface{}) (map[string]interface{}, error) {
	payload, _ := message["payload"].(map[string]interface{})
	dest := ""
	if payload != nil {
		if v, ok := payload["destination"].(string); ok {
			dest = v
		}
	}
	d.stateMu.Lock()
	deliveries, _ := d.state["deliveries"].(int)
	deliveries++
	d.state["deliveries"] = deliveries
	d.state["status"] = "delivering"
	d.state["last_destination"] = dest
	d.stateMu.Unlock()
	log.Printf("[%s] deliver_package destination=%s", d.ComponentID, dest)
	return map[string]interface{}{
		"accepted":     true,
		"destination":  dest,
		"deliveries":   deliveries,
		"from":         d.ComponentID,
		"component_id": d.ComponentID,
	}, nil
}

func (d *Drone) handleGetDeliveryStatus(_ context.Context, _ map[string]interface{}) (map[string]interface{}, error) {
	d.stateMu.RLock()
	defer d.stateMu.RUnlock()
	out := make(map[string]interface{})
	for k, v := range d.state {
		out[k] = v
	}
	out["from"] = d.ComponentID
	out["component_id"] = d.ComponentID
	return out, nil
}

// State returns a copy of current state (for tests).
func (d *Drone) State() map[string]interface{} {
	d.stateMu.RLock()
	defer d.stateMu.RUnlock()
	out := make(map[string]interface{})
	for k, v := range d.state {
		out[k] = v
	}
	return out
}
