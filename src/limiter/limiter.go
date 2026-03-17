// Package limiter implements the geofence component: mission_load, update_config, get_state; polls nav and telemetry, publishes limiter_event to emergensy and LOG_EVENT to journal on deviation.
package limiter

import (
	"context"
	"log"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AMCP-Drones/drones/src/bus"
	"github.com/AMCP-Drones/drones/src/component"
	"github.com/AMCP-Drones/drones/src/config"
)

const (
	StateNormal    = "NORMAL"
	StateWarning   = "WARNING"
	StateEmergency = "EMERGENCY"
)

// Limiter holds mission and last nav/telemetry; compares position to mission and triggers emergensy on breach.
type Limiter struct {
	*component.BaseComponent
	systemName               string
	secMonitorTopic          string
	journalTopic             string
	navigationTopic          string
	telemetryTopic           string
	emergensyTopic           string
	controlIntervalSec       float64
	navPollIntervalSec       float64
	telemetryPollIntervalSec float64
	requestTimeoutSec        float64
	maxDistanceFromPathM     float64
	maxAltDeviationM         float64
	mu                       sync.RWMutex
	mission                  map[string]interface{}
	lastNav                  map[string]interface{}
	lastTelemetry            map[string]interface{}
	state                    string
	lastNavPollTs            float64
	lastTelemetryPollTs      float64
}

// New creates a Limiter. Call Start after creation.
func New(cfg *config.Config, b bus.Bus) *Limiter {
	systemName := cfg.SystemName
	if systemName == "" {
		systemName = "deliverydron"
	}
	topic := cfg.ComponentTopic
	if topic == "" {
		topic = config.TopicFor(systemName, "limiter")
	}
	base := component.NewBaseComponent(cfg.ComponentID, "limiter", topic, b)
	secTopic := os.Getenv("SECURITY_MONITOR_TOPIC")
	if secTopic == "" {
		secTopic = config.TopicFor(systemName, "security_monitor")
	}
	journalTopic := config.TopicFor(systemName, "journal")
	navTopic := config.TopicFor(systemName, "navigation")
	telemetryTopic := config.TopicFor(systemName, "telemetry")
	emergensyTopic := config.TopicFor(systemName, "emergensy")
	controlInterval := 0.5
	navPollInterval := 0.2
	telemetryPollInterval := 0.5
	requestTimeout := 5.0
	maxDist := 50.0
	maxAlt := 20.0
	for _, p := range []struct {
		env string
		v   *float64
	}{
		{"LIMITER_CONTROL_INTERVAL_S", &controlInterval},
		{"LIMITER_NAV_POLL_INTERVAL_S", &navPollInterval},
		{"LIMITER_TELEMETRY_POLL_INTERVAL_S", &telemetryPollInterval},
		{"LIMITER_REQUEST_TIMEOUT_S", &requestTimeout},
		{"LIMITER_MAX_DISTANCE_FROM_PATH_M", &maxDist},
		{"LIMITER_MAX_ALT_DEVIATION_M", &maxAlt},
	} {
		if s := os.Getenv(p.env); s != "" {
			if v, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil && v > 0 {
				*p.v = v
			}
		}
	}
	l := &Limiter{
		BaseComponent:            base,
		systemName:               systemName,
		secMonitorTopic:          secTopic,
		journalTopic:             journalTopic,
		navigationTopic:          navTopic,
		telemetryTopic:           telemetryTopic,
		emergensyTopic:           emergensyTopic,
		controlIntervalSec:       controlInterval,
		navPollIntervalSec:       navPollInterval,
		telemetryPollIntervalSec: telemetryPollInterval,
		requestTimeoutSec:        requestTimeout,
		maxDistanceFromPathM:     maxDist,
		maxAltDeviationM:         maxAlt,
		state:                    StateNormal,
		lastNavPollTs:            0,
		lastTelemetryPollTs:      0,
	}
	l.registerHandlers()
	return l
}

func (l *Limiter) registerHandlers() {
	l.RegisterHandler("mission_load", l.handleMissionLoad)
	l.RegisterHandler("update_config", l.handleUpdateConfig)
	l.RegisterHandler("get_state", l.handleGetState)
}

// Start subscribes and starts the control loop.
func (l *Limiter) Start(ctx context.Context) error {
	if err := l.BaseComponent.Start(ctx); err != nil {
		return err
	}
	go l.controlLoop(ctx)
	return nil
}

func (l *Limiter) controlLoop(ctx context.Context) {
	for l.Running() {
		l.pollNavigationIfDue(ctx)
		l.pollTelemetryIfDue(ctx)
		l.recalculate(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(l.controlIntervalSec * float64(time.Second))):
		}
	}
}

func (l *Limiter) proxyRequest(ctx context.Context, targetTopic, action string) map[string]interface{} {
	msg := map[string]interface{}{
		"action": "proxy_request",
		"sender": l.ComponentID,
		"payload": map[string]interface{}{
			"target": map[string]interface{}{"topic": targetTopic, "action": action},
			"data":   map[string]interface{}{},
		},
	}
	resp, err := l.Bus.Request(ctx, l.secMonitorTopic, msg, l.requestTimeoutSec)
	if err != nil {
		return nil
	}
	pl, _ := resp["payload"].(map[string]interface{})
	tr, _ := pl["target_response"].(map[string]interface{})
	if tr == nil {
		return nil
	}
	payload, _ := tr["payload"].(map[string]interface{})
	return payload
}

func (l *Limiter) pollNavigationIfDue(ctx context.Context) {
	now := float64(time.Now().UnixNano()) / 1e9
	if now-l.lastNavPollTs < l.navPollIntervalSec {
		return
	}
	l.lastNavPollTs = now
	nav := l.proxyRequest(ctx, l.navigationTopic, "get_state")
	if nav != nil {
		l.mu.Lock()
		l.lastNav = nav
		l.mu.Unlock()
	}
}

func (l *Limiter) pollTelemetryIfDue(ctx context.Context) {
	now := float64(time.Now().UnixNano()) / 1e9
	if now-l.lastTelemetryPollTs < l.telemetryPollIntervalSec {
		return
	}
	l.lastTelemetryPollTs = now
	telem := l.proxyRequest(ctx, l.telemetryTopic, "get_state")
	if telem != nil {
		l.mu.Lock()
		l.lastTelemetry = telem
		l.mu.Unlock()
	}
}

func (l *Limiter) handleMissionLoad(_ context.Context, message map[string]interface{}) (map[string]interface{}, error) {
	if !component.IsTrustedSender(message, "security_monitor") {
		return nil, nil
	}
	payload, _ := message["payload"].(map[string]interface{})
	if payload == nil {
		return map[string]interface{}{"ok": false, "error": "invalid_mission"}, nil
	}
	mission, _ := payload["mission"].(map[string]interface{})
	if mission == nil {
		return map[string]interface{}{"ok": false, "error": "invalid_mission"}, nil
	}
	l.mu.Lock()
	l.mission = mission
	l.mu.Unlock()
	return map[string]interface{}{"ok": true}, nil
}

func (l *Limiter) handleUpdateConfig(_ context.Context, message map[string]interface{}) (map[string]interface{}, error) {
	if !component.IsTrustedSender(message, "security_monitor") {
		return nil, nil
	}
	payload, _ := message["payload"].(map[string]interface{})
	if payload == nil {
		return map[string]interface{}{"ok": false, "error": "invalid_payload"}, nil
	}
	l.mu.Lock()
	if v, ok := payload["max_distance_from_path_m"].(float64); ok {
		l.maxDistanceFromPathM = v
	}
	if v, ok := payload["max_alt_deviation_m"].(float64); ok {
		l.maxAltDeviationM = v
	}
	l.mu.Unlock()
	return map[string]interface{}{
		"ok":                       true,
		"max_distance_from_path_m": l.maxDistanceFromPathM,
		"max_alt_deviation_m":      l.maxAltDeviationM,
	}, nil
}

func (l *Limiter) handleGetState(_ context.Context, _ map[string]interface{}) (map[string]interface{}, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return map[string]interface{}{
		"state":                    l.state,
		"max_distance_from_path_m": l.maxDistanceFromPathM,
		"max_alt_deviation_m":      l.maxAltDeviationM,
	}, nil
}

func getFloat(m map[string]interface{}, k string) float64 {
	if v, ok := m[k]; ok {
		switch x := v.(type) {
		case float64:
			return x
		case int:
			return float64(x)
		case int64:
			return float64(x)
		}
	}
	return 0
}

func (l *Limiter) recalculate(ctx context.Context) {
	l.mu.Lock()
	mission := l.mission
	nav := l.lastNav
	l.mu.Unlock()
	if mission == nil || nav == nil {
		return
	}
	steps, _ := mission["steps"].([]interface{})
	if len(steps) == 0 {
		return
	}
	target, _ := steps[len(steps)-1].(map[string]interface{})
	if target == nil {
		return
	}
	lat := getFloat(nav, "lat")
	lon := getFloat(nav, "lon")
	alt := getFloat(nav, "alt_m")
	tLat := getFloat(target, "lat")
	tLon := getFloat(target, "lon")
	tAlt := getFloat(target, "alt_m")
	dLat := lat - tLat
	dLon := lon - tLon
	distanceM := math.Sqrt(dLat*dLat+dLon*dLon) * 111000
	altDev := math.Abs(alt - tAlt)

	l.mu.Lock()
	defer l.mu.Unlock()
	if distanceM > l.maxDistanceFromPathM || altDev > l.maxAltDeviationM {
		if l.state != StateEmergency {
			l.state = StateEmergency
			l.mu.Unlock()
			l.publishEmergency(ctx, distanceM, altDev)
			l.mu.Lock()
		}
	} else if distanceM > 0.5*l.maxDistanceFromPathM || altDev > 0.5*l.maxAltDeviationM {
		if l.state != StateWarning {
			l.mu.Unlock()
			l.logToJournal(ctx, "LIMITER_DEVIATION_WARNING", map[string]interface{}{"distance_m": distanceM, "alt_deviation_m": altDev})
			l.mu.Lock()
		}
		l.state = StateWarning
	} else {
		l.state = StateNormal
	}
}

func (l *Limiter) publishEmergency(ctx context.Context, distanceM, altDev float64) {
	details := map[string]interface{}{
		"distance_from_path_m":     distanceM,
		"max_distance_from_path_m": l.maxDistanceFromPathM,
		"alt_deviation_m":          altDev,
		"max_alt_deviation_m":      l.maxAltDeviationM,
	}
	l.logToJournal(ctx, "LIMITER_EMERGENCY_LAND_REQUIRED", details)
	eventPayload := map[string]interface{}{
		"event":   "EMERGENCY_LAND_REQUIRED",
		"details": details,
	}
	msg := map[string]interface{}{
		"action": "proxy_publish",
		"sender": l.ComponentID,
		"payload": map[string]interface{}{
			"target": map[string]interface{}{"topic": l.emergensyTopic, "action": "limiter_event"},
			"data":   eventPayload,
		},
	}
	if err := l.Bus.Publish(ctx, l.secMonitorTopic, msg); err != nil {
		log.Printf("[%s] publish emergensy: %v", l.ComponentID, err)
	}
}

func (l *Limiter) logToJournal(ctx context.Context, event string, details map[string]interface{}) {
	msg := map[string]interface{}{
		"action": "proxy_publish",
		"sender": l.ComponentID,
		"payload": map[string]interface{}{
			"target": map[string]interface{}{"topic": l.journalTopic, "action": "LOG_EVENT"},
			"data":   map[string]interface{}{"event": event, "source": "limiter", "details": details},
		},
	}
	if err := l.Bus.Publish(ctx, l.secMonitorTopic, msg); err != nil {
		log.Printf("[%s] log journal: %v", l.ComponentID, err)
	}
}
