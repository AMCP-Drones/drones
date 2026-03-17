// Package component provides base component helpers: register handler by action, ping, get_status.
package component

import (
	"context"
	"fmt"
	"log"
	"sync/atomic"

	"github.com/AMCP-Drones/drones/src/bus"
	"github.com/AMCP-Drones/drones/src/sdk"
)

// Handler is a function that handles a message and optionally returns a response payload.
type Handler func(ctx context.Context, message map[string]interface{}) (map[string]interface{}, error)

// BaseComponent provides action-based routing and built-in ping/get_status.
type BaseComponent struct {
	ComponentID   string
	ComponentType string
	Topic         string
	Bus           bus.Bus
	handlers      map[string]Handler
	running       uint32
}

// NewBaseComponent creates a base component. Call RegisterHandler for actions, then Start().
func NewBaseComponent(componentID, componentType, topic string, b bus.Bus) *BaseComponent {
	c := &BaseComponent{
		ComponentID:   componentID,
		ComponentType: componentType,
		Topic:         topic,
		Bus:           b,
		handlers:      make(map[string]Handler),
	}
	c.registerBuiltinHandlers()
	return c
}

func (c *BaseComponent) registerBuiltinHandlers() {
	c.RegisterHandler("ping", c.handlePing)
	c.RegisterHandler("get_status", c.handleGetStatus)
}

// RegisterHandler registers a handler for the given action.
func (c *BaseComponent) RegisterHandler(action string, h Handler) {
	c.handlers[action] = h
}

// Running returns whether the component is started and subscribed.
func (c *BaseComponent) Running() bool {
	return atomic.LoadUint32(&c.running) == 1
}

// IsTrustedSender returns true if the message sender is the security monitor (or starts with the given prefix).
// Components that accept commands only from the security monitor should use prefix "security_monitor".
func IsTrustedSender(message map[string]interface{}, prefix string) bool {
	s, _ := message["sender"].(string)
	if s == "" {
		return false
	}
	return len(prefix) <= len(s) && s[:len(prefix)] == prefix
}

func (c *BaseComponent) handlePing(_ context.Context, _ map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{
		"pong":         true,
		"component_id": c.ComponentID,
	}, nil
}

func (c *BaseComponent) handleGetStatus(_ context.Context, _ map[string]interface{}) (map[string]interface{}, error) {
	actions := make([]string, 0, len(c.handlers))
	for a := range c.handlers {
		actions = append(actions, a)
	}
	return map[string]interface{}{
		"component_id":   c.ComponentID,
		"component_type": c.ComponentType,
		"topic":          c.Topic,
		"running":        c.Running(),
		"handlers":       actions,
	}, nil
}

func (c *BaseComponent) handleMessage(ctx context.Context, message map[string]interface{}) {
	action, _ := message["action"].(string)
	if action == "" {
		log.Printf("[%s] message without action", c.ComponentID)
		return
	}
	h := c.handlers[action]
	if h == nil {
		log.Printf("[%s] unknown action: %s", c.ComponentID, action)
		if replyTo, _ := message["reply_to"].(string); replyTo != "" {
			if err := bus.Respond(ctx, c.Bus, message, map[string]interface{}{"error": "unknown action: " + action}, c.ComponentID, false, "unknown action"); err != nil {
				log.Printf("[%s] respond unknown action: %v", c.ComponentID, err)
			}
		}
		return
	}
	result, err := h(ctx, message)
	if replyTo, _ := message["reply_to"].(string); replyTo != "" {
		if err != nil {
			if errResp := bus.Respond(ctx, c.Bus, message, map[string]interface{}{}, c.ComponentID, false, err.Error()); errResp != nil {
				log.Printf("[%s] respond error: %v", c.ComponentID, errResp)
			}
			return
		}
		if result != nil {
			payload := result
			resp := sdk.CreateResponse(
				getString(message, "correlation_id"),
				payload,
				c.ComponentID,
				true,
				"",
			)
			if err := c.Bus.Publish(ctx, replyTo, resp); err != nil {
				log.Printf("[%s] publish response: %v", c.ComponentID, err)
			}
		}
	}
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// Start subscribes to the component topic and starts the bus. Call after all RegisterHandler calls.
func (c *BaseComponent) Start(ctx context.Context) error {
	if atomic.LoadUint32(&c.running) == 1 {
		return nil
	}
	handler := func(message map[string]interface{}) {
		c.handleMessage(ctx, message)
	}
	if err := c.Bus.Subscribe(ctx, c.Topic, handler); err != nil {
		return err
	}
	if err := c.Bus.Start(ctx); err != nil {
		return err
	}
	atomic.StoreUint32(&c.running, 1)
	log.Printf("[%s] started, listening on topic %s", c.ComponentID, c.Topic)
	return nil
}

// Stop unsubscribes and stops the bus.
func (c *BaseComponent) Stop(ctx context.Context) error {
	if atomic.LoadUint32(&c.running) == 0 {
		return nil
	}
	atomic.StoreUint32(&c.running, 0)
	var firstErr error
	if err := c.Bus.Unsubscribe(ctx, c.Topic); err != nil {
		log.Printf("[%s] unsubscribe: %v", c.ComponentID, err)
		if firstErr == nil {
			firstErr = err
		}
	}
	if err := c.Bus.Stop(ctx); err != nil {
		log.Printf("[%s] bus stop: %v", c.ComponentID, err)
		if firstErr == nil {
			firstErr = err
		}
	}
	log.Printf("[%s] stopped", c.ComponentID)
	return firstErr
}

// Request sends a request to another topic and waits for response.
func (c *BaseComponent) Request(ctx context.Context, topic string, action string, payload map[string]interface{}, timeoutSec float64) (map[string]interface{}, error) {
	msg := map[string]interface{}{
		"action":  action,
		"payload": payload,
		"sender":  c.ComponentID,
	}
	resp, err := c.Bus.Request(ctx, topic, msg, timeoutSec)
	if err != nil {
		return nil, err
	}
	if success, _ := resp["success"].(bool); !success {
		errMsg, _ := resp["error"].(string)
		return nil, fmt.Errorf("remote error: %s", errMsg)
	}
	pl, _ := resp["payload"].(map[string]interface{})
	return pl, nil
}
