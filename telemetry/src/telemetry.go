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
	"github.com/AMCP-Drones/drones/systems/deliverydron/sdk/src"
)

// Telemetry aggregates motors and cargo state; get_state only from security_monitor.
type Telemetry struct {
	*component.BaseComponent
	systemName        string
	secMonitorTopic   string
	navigationTopic   string
	motorsTopic       string
	cargoTopic        string
	pollIntervalSec   float64
	requestTimeoutSec float64
	defaultBatteryPct int
	proxy             *component.ProxyClient
	analytics         *sdk.AnalyticsClient
	mu                sync.RWMutex
	lastMotors        map[string]interface{}
	lastCargo         map[string]interface{}
	lastNavigation    map[string]interface{}
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
	navigationTopic := os.Getenv("NAVIGATION_TOPIC")
	if navigationTopic == "" {
		navigationTopic = cfg.BrokerTopicFor("navigation")
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
	defaultBattery := 100
	if s := os.Getenv("TELEMETRY_BATTERY_PCT_DEFAULT"); s != "" {
		if v, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
			if v < 0 {
				v = 0
			}
			if v > 100 {
				v = 100
			}
			defaultBattery = v
		}
	}
	t := &Telemetry{
		BaseComponent:     base,
		systemName:        systemName,
		secMonitorTopic:   secTopic,
		navigationTopic:   navigationTopic,
		motorsTopic:       motorsTopic,
		cargoTopic:        cargoTopic,
		pollIntervalSec:   pollInterval,
		requestTimeoutSec: requestTimeout,
		defaultBatteryPct: defaultBattery,
		proxy: &component.ProxyClient{
			Bus:                  b,
			SenderID:             cfg.ComponentID,
			SecurityMonitorTopic: secTopic,
			TimeoutSec:           requestTimeout,
		},
		analytics: sdk.NewAnalyticsClientFromEnv(),
		lastMotors:        nil,
		lastCargo:         nil,
		lastNavigation:    nil,
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
	nav := t.proxyGetState(ctx, t.navigationTopic, "get_state")
	motors := t.proxyGetState(ctx, t.motorsTopic, "get_state")
	cargo := t.proxyGetState(ctx, t.cargoTopic, "get_state")
	nowMs := time.Now().UnixMilli()
	t.mu.Lock()
	if nav != nil {
		t.lastNavigation = nav
	}
	if motors != nil {
		t.lastMotors = motors
	}
	if cargo != nil {
		t.lastCargo = cargo
	}
	t.lastPollTs = float64(time.Now().UnixNano()) / 1e9
	t.mu.Unlock()
	t.postAnalyticsTelemetry(ctx, nowMs, nav, motors, cargo)
}

func (t *Telemetry) proxyGetState(ctx context.Context, targetTopic, action string) map[string]interface{} {
	pl, err := t.proxy.ProxyRequest(ctx, targetTopic, action, map[string]interface{}{})
	if err != nil {
		log.Printf("[%s] proxy_request %s: %v", t.ComponentID, targetTopic, err)
		return nil
	}
	return pl
}

func (t *Telemetry) handleGetState(_ context.Context, message map[string]interface{}) (map[string]interface{}, error) {
	if !component.IsTrustedSender(message, "security_monitor") {
		return nil, nil
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := map[string]interface{}{
		"navigation":   copyMap(t.lastNavigation),
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

func (t *Telemetry) postAnalyticsTelemetry(ctx context.Context, timestampMs int64, nav, motors, cargo map[string]interface{}) {
	if !t.analytics.Enabled() {
		return
	}
	var lat, lon float64
	var okLat, okLon bool
	if nav != nil {
		lat, okLat = mapFloat(nav, "lat")
		lon, okLon = mapFloat(nav, "lon")
	}
	if (!okLat || !okLon) && motors != nil {
		target, _ := motors["last_target"].(map[string]interface{})
		if target != nil {
			if !okLat {
				lat, okLat = mapFloat(target, "lat")
			}
			if !okLon {
				lon, okLon = mapFloat(target, "lon")
			}
		}
	}
	if !okLat || !okLon {
		return
	}

	var height *float64
	if nav != nil {
		if v, ok := mapFloat(nav, "alt_m", "height"); ok {
			height = &v
		}
	}
	if height == nil && motors != nil {
		if target, _ := motors["last_target"].(map[string]interface{}); target != nil {
			if v, ok := mapFloat(target, "alt_m", "height"); ok {
				height = &v
			}
		}
	}

	var course *float64
	if nav != nil {
		if v, ok := mapFloat(nav, "heading_deg", "course"); ok {
			course = &v
		}
	}
	if course == nil && motors != nil {
		if target, _ := motors["last_target"].(map[string]interface{}); target != nil {
			if v, ok := mapFloat(target, "heading_deg", "course"); ok {
				course = &v
			}
		}
	}

	var pitch *float64
	if nav != nil {
		if v, ok := mapFloat(nav, "pitch", "pitch_deg"); ok {
			pitch = &v
		}
	}
	if pitch == nil {
		// Explicit default avoids null values in dashboards when IMU values are unavailable.
		v := 0.0
		pitch = &v
	}

	var roll *float64
	if nav != nil {
		if v, ok := mapFloat(nav, "roll", "roll_deg"); ok {
			roll = &v
		}
	}
	if roll == nil {
		v := 0.0
		roll = &v
	}

	batteryVal := t.defaultBatteryPct
	if v, ok := mapFloat(cargo, "battery_pct", "battery"); ok {
		batteryVal = int(v)
	} else if v, ok := mapFloat(nav, "battery_pct", "battery"); ok {
		batteryVal = int(v)
	} else if v, ok := mapFloat(motors, "battery_pct", "battery"); ok {
		batteryVal = int(v)
	}
	if batteryVal < 0 {
		batteryVal = 0
	}
	if batteryVal > 100 {
		batteryVal = 100
	}
	battery := batteryVal

	logItem := sdk.TelemetryLog{
		APIVersion: t.analytics.APIVersion(),
		Timestamp:  timestampMs,
		Drone:      t.analytics.Drone(),
		DroneID:    t.analytics.DroneID(),
		Latitude:   lat,
		Longitude:  lon,
		Height:     height,
		Course:     course,
		Pitch:      pitch,
		Roll:       roll,
		Battery:    &battery,
	}
	if err := t.analytics.PostTelemetry(ctx, []sdk.TelemetryLog{logItem}); err != nil {
		log.Printf("[%s] analytics telemetry post: %v", t.ComponentID, err)
	}
}

func mapFloat(m map[string]interface{}, keys ...string) (float64, bool) {
	if m == nil {
		return 0, false
	}
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if f, ok := asFloat(v); ok {
				return f, true
			}
		}
	}
	return 0, false
}

func asFloat(v interface{}) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case int32:
		return float64(x), true
	default:
		return 0, false
	}
}
