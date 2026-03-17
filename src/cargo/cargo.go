// Package cargo implements the cargo bay component: OPEN, CLOSE, get_state; logs state changes via security_monitor to journal.
package cargo

import (
	"context"
	"log"
	"os"
	"sync"
	"time"

	"github.com/AMCP-Drones/drones/src/bus"
	"github.com/AMCP-Drones/drones/src/component"
	"github.com/AMCP-Drones/drones/src/config"
)

// Cargo door state constants.
const (
	StateClosed = "CLOSED"
	StateOpen   = "OPEN"
)

// Cargo implements the cargo bay. OPEN/CLOSE only from security_monitor; get_state for telemetry.
type Cargo struct {
	*component.BaseComponent
	systemName           string
	securityMonitorTopic string
	journalTopic         string
	mu                   sync.RWMutex
	state                string
	lastChangeTs         float64
}

// New creates a Cargo component. Call Start after creation.
func New(cfg *config.Config, b bus.Bus) *Cargo {
	systemName := cfg.SystemName
	if systemName == "" {
		systemName = "deliverydron"
	}
	topic := cfg.ComponentTopic
	if topic == "" {
		topic = config.TopicFor(systemName, "cargo")
	}
	base := component.NewBaseComponent(cfg.ComponentID, "cargo", topic, b)
	secTopic := os.Getenv("SECURITY_MONITOR_TOPIC")
	if secTopic == "" {
		secTopic = config.TopicFor(systemName, "security_monitor")
	}
	journalTopic := config.TopicFor(systemName, "journal")
	c := &Cargo{
		BaseComponent:        base,
		systemName:           systemName,
		securityMonitorTopic: secTopic,
		journalTopic:         journalTopic,
		state:                StateClosed,
		lastChangeTs:         float64(time.Now().UnixNano()) / 1e9,
	}
	c.registerHandlers()
	return c
}

func (c *Cargo) registerHandlers() {
	c.RegisterHandler("OPEN", c.handleOpen)
	c.RegisterHandler("CLOSE", c.handleClose)
	c.RegisterHandler("get_state", c.handleGetState)
}

func (c *Cargo) setState(ctx context.Context, newState string) {
	c.mu.Lock()
	old := c.state
	c.state = newState
	c.lastChangeTs = float64(time.Now().UnixNano()) / 1e9
	c.mu.Unlock()
	if old != newState {
		c.logStateChange(ctx, old, newState)
	}
}

func (c *Cargo) logStateChange(ctx context.Context, oldState, newState string) {
	msg := map[string]interface{}{
		"action": "proxy_publish",
		"sender": c.ComponentID,
		"payload": map[string]interface{}{
			"target": map[string]interface{}{
				"topic":  c.journalTopic,
				"action": "LOG_EVENT",
			},
			"data": map[string]interface{}{
				"event":   "CARGO_STATE_CHANGED",
				"source":  "cargo",
				"details": map[string]string{"old_state": oldState, "new_state": newState},
			},
		},
	}
	if err := c.Bus.Publish(ctx, c.securityMonitorTopic, msg); err != nil {
		log.Printf("[%s] failed to log state change: %v", c.ComponentID, err)
	}
}

func (c *Cargo) handleOpen(ctx context.Context, message map[string]interface{}) (map[string]interface{}, error) {
	if !component.IsTrustedSender(message, "security_monitor") {
		return nil, nil
	}
	c.setState(ctx, StateOpen)
	return map[string]interface{}{"ok": true, "state": c.state}, nil
}

func (c *Cargo) handleClose(ctx context.Context, message map[string]interface{}) (map[string]interface{}, error) {
	if !component.IsTrustedSender(message, "security_monitor") {
		return nil, nil
	}
	c.setState(ctx, StateClosed)
	return map[string]interface{}{"ok": true, "state": c.state}, nil
}

func (c *Cargo) handleGetState(_ context.Context, _ map[string]interface{}) (map[string]interface{}, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return map[string]interface{}{
		"state":          c.state,
		"last_change_ts": c.lastChangeTs,
	}, nil
}
