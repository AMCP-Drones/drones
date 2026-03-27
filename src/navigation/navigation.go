// Package navigation implements the navigation component: holds last NAV_STATE, get_state; accepts nav_state updates (e.g. from SITL adapter via proxy).
package navigation

import (
	"context"
	"sync"

	"github.com/AMCP-Drones/drones/src/bus"
	"github.com/AMCP-Drones/drones/src/component"
	"github.com/AMCP-Drones/drones/src/config"
)

// Default mock NAV_STATE when no update has been received.
var defaultNavState = map[string]interface{}{
	"lat":              55.75,
	"lon":              37.62,
	"alt_m":            50.0,
	"ground_speed_mps": 0.0,
	"heading_deg":      0.0,
	"fix":              3,
	"satellites":       12,
	"gps_valid":        true,
	"hdop":             0.8,
}

// Navigation holds last nav state and serves get_state.
type Navigation struct {
	*component.BaseComponent
	systemName string
	mu         sync.RWMutex
	lastState  map[string]interface{}
}

// New creates a Navigation component. Call Start after creation.
func New(cfg *config.Config, b bus.Bus) *Navigation {
	systemName := cfg.SystemName
	if systemName == "" {
		systemName = "deliverydron"
	}
	topic := cfg.ComponentTopic
	if topic == "" {
		topic = cfg.BrokerTopicFor("navigation")
	}
	base := component.NewBaseComponent(cfg.ComponentID, "navigation", topic, b)
	// Start with a copy of default mock state
	initial := make(map[string]interface{})
	for k, v := range defaultNavState {
		initial[k] = v
	}
	n := &Navigation{
		BaseComponent: base,
		systemName:    systemName,
		lastState:     initial,
	}
	n.registerHandlers()
	return n
}

func (n *Navigation) registerHandlers() {
	n.RegisterHandler("nav_state", n.handleNavState)
	n.RegisterHandler("update_config", n.handleUpdateConfig)
	n.RegisterHandler("get_state", n.handleGetState)
}

func (n *Navigation) handleNavState(_ context.Context, message map[string]interface{}) (map[string]interface{}, error) {
	if !component.IsTrustedSender(message, "security_monitor") {
		return nil, nil
	}
	payload, _ := message["payload"].(map[string]interface{})
	if payload == nil {
		return map[string]interface{}{"ok": false, "error": "invalid_nav_payload"}, nil
	}
	n.mu.Lock()
	n.lastState = make(map[string]interface{})
	for k, v := range payload {
		n.lastState[k] = v
	}
	n.mu.Unlock()
	return map[string]interface{}{"ok": true}, nil
}

func (n *Navigation) handleUpdateConfig(_ context.Context, message map[string]interface{}) (map[string]interface{}, error) {
	if !component.IsTrustedSender(message, "security_monitor") {
		return nil, nil
	}
	payload, _ := message["payload"].(map[string]interface{})
	if payload == nil {
		return map[string]interface{}{"ok": false, "error": "invalid_config_payload"}, nil
	}
	n.mu.Lock()
	for k, v := range payload {
		n.lastState[k] = v
	}
	out := make(map[string]interface{})
	for k, v := range n.lastState {
		out[k] = v
	}
	n.mu.Unlock()
	return map[string]interface{}{"ok": true, "config": out}, nil
}

func (n *Navigation) handleGetState(_ context.Context, message map[string]interface{}) (map[string]interface{}, error) {
	if !component.IsTrustedSender(message, "security_monitor") {
		return nil, nil
	}
	n.mu.RLock()
	defer n.mu.RUnlock()
	out := make(map[string]interface{})
	for k, v := range n.lastState {
		out[k] = v
	}
	return out, nil
}
