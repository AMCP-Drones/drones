// Package telemetry aggregates state from motors and cargo via security_monitor proxy_request; serves get_state.
package telemetry

import (
	"context"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AMCP-Drones/drones/systems/deliverydron/bus/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/component/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/config/src"
)

// Telemetry aggregates motors and cargo state; get_state only from security_monitor.
type Telemetry struct {
	*component.BaseComponent
	systemName        string
	secMonitorTopic   string
	motorsTopic       string
	cargoTopic        string
	pollIntervalSec   float64
	requestTimeoutSec float64
	mu                sync.RWMutex
	lastMotors        map[string]interface{}
	lastCargo         map[string]interface{}
	lastPollTs        float64
}

// New creates a Telemetry component. Call Start after creation.
func New(cfg *config.Config, b bus.Bus) *Telemetry {
	systemName := cfg.SystemName
	if systemName == "" {
		systemName = "deliverydron"
	}
	topic := cfg.ComponentTopic
	if topic == "" {
		topic = cfg.BrokerTopicFor("telemetry")
	}
	base := component.NewBaseComponent(cfg.ComponentID, "telemetry", topic, b)
	secTopic := os.Getenv("SECURITY_MONITOR_TOPIC")
	if secTopic == "" {
		secTopic = cfg.BrokerTopicFor("security_monitor")
	}
	motorsTopic := os.Getenv("MOTORS_TOPIC")
	if motorsTopic == "" {
		motorsTopic = cfg.BrokerTopicFor("motors")
	}
	cargoTopic := os.Getenv("CARGO_TOPIC")
	if cargoTopic == "" {
		cargoTopic = cfg.BrokerTopicFor("cargo")
	}
	pollInterval := 1.0
	if s := os.Getenv("TELEMETRY_POLL_INTERVAL_S"); s != "" {
		if v, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil && v > 0 {
			pollInterval = v
		}
	}
	requestTimeout := 5.0
	if s := os.Getenv("TELEMETRY_REQUEST_TIMEOUT_S"); s != "" {
		if v, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil && v > 0 {
			requestTimeout = v
		}
	}
	t := &Telemetry{
		BaseComponent:     base,
		systemName:        systemName,
		secMonitorTopic:   secTopic,
		motorsTopic:       motorsTopic,
		cargoTopic:        cargoTopic,
		pollIntervalSec:   pollInterval,
		requestTimeoutSec: requestTimeout,
		lastMotors:        nil,
		lastCargo:         nil,
		lastPollTs:        0,
	}
	t.registerHandlers()
	return t
}

func (t *Telemetry) registerHandlers() {
	t.RegisterHandler("get_state", t.handleGetState)
}

// Start subscribes and starts the poll loop.
func (t *Telemetry) Start(ctx context.Context) error {
	if err := t.BaseComponent.Start(ctx); err != nil {
		return err
	}
	go t.pollLoop(ctx)
	return nil
}

func (t *Telemetry) pollLoop(ctx context.Context) {
	for t.Running() {
		t.pollOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(t.pollIntervalSec * float64(time.Second))):
		}
	}
}

func (t *Telemetry) pollOnce(ctx context.Context) {
	motors := t.proxyGetState(ctx, t.motorsTopic, "get_state")
	cargo := t.proxyGetState(ctx, t.cargoTopic, "get_state")
	t.mu.Lock()
	if motors != nil {
		t.lastMotors = motors
	}
	if cargo != nil {
		t.lastCargo = cargo
	}
	t.lastPollTs = float64(time.Now().UnixNano()) / 1e9
	t.mu.Unlock()
}

func (t *Telemetry) proxyGetState(ctx context.Context, targetTopic, action string) map[string]interface{} {
	msg := map[string]interface{}{
		"action": "proxy_request",
		"sender": t.ComponentID,
		"payload": map[string]interface{}{
			"target": map[string]interface{}{"topic": targetTopic, "action": action},
			"data":   map[string]interface{}{},
		},
	}
	resp, err := t.Bus.Request(ctx, t.secMonitorTopic, msg, t.requestTimeoutSec)
	if err != nil {
		log.Printf("[%s] proxy_request %s: %v", t.ComponentID, targetTopic, err)
		return nil
	}
	// Response payload is the proxy_request handler result: { target_topic, target_action, target_response }
	payload, _ := resp["payload"].(map[string]interface{})
	if payload == nil {
		return nil
	}
	tr, _ := payload["target_response"].(map[string]interface{})
	if tr == nil {
		return nil
	}
	pl, _ := tr["payload"].(map[string]interface{})
	return pl
}

func (t *Telemetry) handleGetState(_ context.Context, message map[string]interface{}) (map[string]interface{}, error) {
	if !component.IsTrustedSender(message, "security_monitor") {
		return nil, nil
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := map[string]interface{}{
		"motors":       copyMap(t.lastMotors),
		"cargo":        copyMap(t.lastCargo),
		"last_poll_ts": t.lastPollTs,
	}
	return out, nil
}

func copyMap(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	c := make(map[string]interface{})
	for k, v := range m {
		c[k] = v
	}
	return c
}
