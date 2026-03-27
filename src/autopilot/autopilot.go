// Package autopilot implements the control loop: mission_load, cmd (START/PAUSE/RESUME/ABORT/EMERGENCY_STOP/KOVER), get_state; polls navigation, sends SET_TARGET to motors and OPEN/CLOSE to cargo.
package autopilot

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

// Autopilot state constants.
const (
	StateIDLE          = "IDLE"
	StateMissionLoaded = "MISSION_LOADED"
	StateExecuting     = "EXECUTING"
	StatePaused        = "PAUSED"
	StateCompleted     = "COMPLETED"
	StateAborted       = "ABORTED"
	StateEmergencyStop = "EMERGENCY_STOP"
)

// Autopilot runs the mission control loop and commands motors and cargo.
type Autopilot struct {
	*component.BaseComponent
	systemName         string
	secMonitorTopic    string
	journalTopic       string
	navigationTopic    string
	motorsTopic        string
	cargoTopic         string
	controlIntervalSec float64
	navPollIntervalSec float64
	requestTimeoutSec  float64
	mu                 sync.RWMutex
	mission            map[string]interface{}
	state              string
	currentStepIndex   int
	lastNavState       map[string]interface{}
	cargoState         string
	koverActive        bool
	lastNavPollTs      float64
}

// New creates an Autopilot. Call Start after creation.
func New(cfg *config.Config, b bus.Bus) *Autopilot {
	systemName := cfg.SystemName
	if systemName == "" {
		systemName = "deliverydron"
	}
	topic := cfg.ComponentTopic
	if topic == "" {
		topic = cfg.BrokerTopicFor("autopilot")
	}
	base := component.NewBaseComponent(cfg.ComponentID, "autopilot", topic, b)
	secTopic := os.Getenv("SECURITY_MONITOR_TOPIC")
	if secTopic == "" {
		secTopic = cfg.BrokerTopicFor("security_monitor")
	}
	journalTopic := cfg.BrokerTopicFor("journal")
	navTopic := cfg.BrokerTopicFor("navigation")
	motorsTopic := cfg.BrokerTopicFor("motors")
	cargoTopic := cfg.BrokerTopicFor("cargo")
	controlInterval := 0.2
	navPollInterval := 0.1
	requestTimeout := 5.0
	for _, pair := range []struct {
		env string
		v   *float64
	}{
		{"AUTOPILOT_CONTROL_INTERVAL_S", &controlInterval},
		{"AUTOPILOT_NAV_POLL_INTERVAL_S", &navPollInterval},
		{"AUTOPILOT_REQUEST_TIMEOUT_S", &requestTimeout},
	} {
		if s := os.Getenv(pair.env); s != "" {
			if v, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil && v > 0 {
				*pair.v = v
			}
		}
	}
	a := &Autopilot{
		BaseComponent:      base,
		systemName:         systemName,
		secMonitorTopic:    secTopic,
		journalTopic:       journalTopic,
		navigationTopic:    navTopic,
		motorsTopic:        motorsTopic,
		cargoTopic:         cargoTopic,
		controlIntervalSec: controlInterval,
		navPollIntervalSec: navPollInterval,
		requestTimeoutSec:  requestTimeout,
		state:              StateIDLE,
		currentStepIndex:   0,
		lastNavState:       nil,
		cargoState:         "CLOSED",
		koverActive:        false,
		lastNavPollTs:      0,
	}
	a.registerHandlers()
	return a
}

func (a *Autopilot) registerHandlers() {
	a.RegisterHandler("mission_load", a.handleMissionLoad)
	a.RegisterHandler("cmd", a.handleCmd)
	a.RegisterHandler("get_state", a.handleGetState)
}

// Start subscribes and starts the control loop.
func (a *Autopilot) Start(ctx context.Context) error {
	if err := a.BaseComponent.Start(ctx); err != nil {
		return err
	}
	go a.controlLoop(ctx)
	return nil
}

func (a *Autopilot) controlLoop(ctx context.Context) {
	for a.Running() {
		a.pollNavigationIfDue(ctx)
		a.stepControl(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(a.controlIntervalSec * float64(time.Second))):
		}
	}
}

func (a *Autopilot) pollNavigationIfDue(ctx context.Context) {
	now := float64(time.Now().UnixNano()) / 1e9
	if now-a.lastNavPollTs < a.navPollIntervalSec {
		return
	}
	a.lastNavPollTs = now
	msg := map[string]interface{}{
		"action": "proxy_request",
		"sender": a.ComponentID,
		"payload": map[string]interface{}{
			"target": map[string]interface{}{"topic": a.navigationTopic, "action": "get_state"},
			"data":   map[string]interface{}{},
		},
	}
	resp, err := a.Bus.Request(ctx, a.secMonitorTopic, msg, a.requestTimeoutSec)
	if err != nil {
		return
	}
	pl, _ := resp["payload"].(map[string]interface{})
	tr, _ := pl["target_response"].(map[string]interface{})
	if tr == nil {
		return
	}
	navPl, _ := tr["payload"].(map[string]interface{})
	if navPl != nil {
		a.mu.Lock()
		a.lastNavState = navPl
		a.mu.Unlock()
	}
}

func normalizeSteps(stepsRaw interface{}) []interface{} {
	if steps, ok := stepsRaw.([]interface{}); ok {
		return steps
	}
	if stepsMap, ok := stepsRaw.([]map[string]interface{}); ok {
		result := make([]interface{}, len(stepsMap))
		for i, s := range stepsMap {
			result[i] = s
		}
		return result
	}
	return nil
}

func (a *Autopilot) handleMissionLoad(_ context.Context, message map[string]interface{}) (map[string]interface{}, error) {
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
	a.mu.Lock()
	a.mission = mission
	a.currentStepIndex = 0
	steps := normalizeSteps(mission["steps"])
	if len(steps) > 0 {
		a.currentStepIndex = 0
	} else {
		a.currentStepIndex = -1
	}
	a.state = StateMissionLoaded
	a.mu.Unlock()
	mid, _ := mission["mission_id"].(string)
	a.logToJournal(context.Background(), "AUTOPILOT_MISSION_LOADED", map[string]interface{}{"mission_id": mid, "state": a.state})
	return map[string]interface{}{"ok": true, "state": a.state}, nil
}

func (a *Autopilot) handleCmd(_ context.Context, message map[string]interface{}) (map[string]interface{}, error) {
	if !component.IsTrustedSender(message, "security_monitor") {
		return nil, nil
	}
	payload, _ := message["payload"].(map[string]interface{})
	if payload == nil {
		return map[string]interface{}{"ok": false, "error": "invalid_payload"}, nil
	}
	cmd, _ := payload["command"].(string)
	cmd = strings.TrimSpace(strings.ToUpper(cmd))
	a.mu.Lock()
	oldState := a.state
	switch cmd {
	case "START":
		if a.mission == nil {
			a.mu.Unlock()
			return map[string]interface{}{"ok": false, "error": "no_mission"}, nil
		}
		a.state = StateExecuting
	case "PAUSE":
		if a.state == StateExecuting {
			a.state = StatePaused
		}
	case "RESUME":
		if a.state == StatePaused {
			a.state = StateExecuting
		}
	case "ABORT":
		a.state = StateAborted
		a.mu.Unlock()
		// Safe actuator sequence: stop motors, close cargo
		if a.lastNavState != nil {
			lat := getFloat(a.lastNavState, "lat")
			lon := getFloat(a.lastNavState, "lon")
			alt := getFloat(a.lastNavState, "alt_m")
			heading := getFloat(a.lastNavState, "heading_deg")
			a.sendMotorsTarget(context.Background(), 0, 0, 0, alt, lat, lon, heading, false)
		}
		a.sendCargo(context.Background(), false)
		a.logToJournal(context.Background(), "AUTOPILOT_ABORTED", map[string]interface{}{"old_state": oldState})
		a.mu.Lock()
	case "RESET":
		a.mission = nil
		a.currentStepIndex = 0
		a.state = StateIDLE
	case "EMERGENCY_STOP":
		a.state = StateEmergencyStop
		a.mu.Unlock()
		// Safe actuator sequence: stop motors, close cargo
		if a.lastNavState != nil {
			lat := getFloat(a.lastNavState, "lat")
			lon := getFloat(a.lastNavState, "lon")
			alt := getFloat(a.lastNavState, "alt_m")
			heading := getFloat(a.lastNavState, "heading_deg")
			a.sendMotorsTarget(context.Background(), 0, 0, 0, alt, lat, lon, heading, false)
		}
		a.sendCargo(context.Background(), false)
		a.logToJournal(context.Background(), "AUTOPILOT_EMERGENCY_STOP", map[string]interface{}{"old_state": oldState})
		a.mu.Lock()
	case "KOVER":
		a.koverActive = true
		a.logToJournal(context.Background(), "AUTOPILOT_KOVER_ACTIVE", map[string]interface{}{})
		if a.state != StateExecuting && a.state != StatePaused {
			a.state = StateExecuting
		}
	default:
		a.mu.Unlock()
		return map[string]interface{}{"ok": false, "error": "unknown_command"}, nil
	}
	a.mu.Unlock()
	if oldState != a.state {
		a.logToJournal(context.Background(), "AUTOPILOT_STATE_CHANGE", map[string]interface{}{"old_state": oldState, "new_state": a.state, "command": cmd})
	}
	return map[string]interface{}{"ok": true, "state": a.state}, nil
}

func (a *Autopilot) handleGetState(_ context.Context, _ map[string]interface{}) (map[string]interface{}, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	totalSteps := 0
	mid := ""
	if a.mission != nil {
		steps, _ := a.mission["steps"].([]interface{})
		totalSteps = len(steps)
		mid, _ = a.mission["mission_id"].(string)
	}
	return map[string]interface{}{
		"state":              a.state,
		"mission_id":         mid,
		"current_step_index": a.currentStepIndex,
		"total_steps":        totalSteps,
		"cargo_state":        a.cargoState,
		"last_nav_state":     a.lastNavState,
	}, nil
}

func (a *Autopilot) stepControl(ctx context.Context) {
	a.mu.RLock()
	nav := a.lastNavState
	mission := a.mission
	state := a.state
	stepIndex := a.currentStepIndex
	kover := a.koverActive
	a.mu.RUnlock()
	if nav == nil {
		return
	}
	if kover {
		a.doKover(ctx, nav)
		return
	}
	if mission == nil || state != StateExecuting && state != StatePaused {
		return
	}
	steps, _ := mission["steps"].([]interface{})
	if len(steps) == 0 {
		return
	}
	if stepIndex < 0 {
		stepIndex = 0
	}
	if stepIndex >= len(steps) {
		a.mu.Lock()
		if a.state == StateExecuting {
			a.state = StateCompleted
			mid, _ := a.mission["mission_id"].(string)
			a.logToJournal(ctx, "AUTOPILOT_MISSION_COMPLETED", map[string]interface{}{"mission_id": mid})
		}
		a.mu.Unlock()
		return
	}
	step, _ := steps[stepIndex].(map[string]interface{})
	if step == nil {
		return
	}
	lat := getFloat(nav, "lat")
	lon := getFloat(nav, "lon")
	alt := getFloat(nav, "alt_m")
	tLat := getFloat(step, "lat")
	tLon := getFloat(step, "lon")
	tAlt := getFloat(step, "alt_m")
	dLat := tLat - lat
	dLon := tLon - lon
	distanceM := math.Sqrt(dLat*dLat+dLon*dLon) * 111000
	reachThreshold := 2.0
	if state == StatePaused {
		a.sendMotorsTarget(ctx, 0, 0, 0, alt, lat, lon, getFloat(nav, "heading_deg"), false)
		a.sendCargo(ctx, false)
		return
	}
	if distanceM <= reachThreshold {
		if stepIndex >= len(steps)-1 {
			a.mu.Lock()
			a.state = StateCompleted
			a.currentStepIndex = len(steps)
			mid, _ := a.mission["mission_id"].(string)
			a.mu.Unlock()
			a.logToJournal(ctx, "AUTOPILOT_MISSION_COMPLETED", map[string]interface{}{"mission_id": mid})
			a.sendMotorsTarget(ctx, 0, 0, 0, alt, lat, lon, getFloat(nav, "heading_deg"), false)
			a.sendCargo(ctx, false)
			return
		}
		a.mu.Lock()
		a.currentStepIndex = stepIndex + 1
		a.mu.Unlock()
		return
	}
	speedMps := getFloat(step, "speed_mps")
	if speedMps == 0 {
		speedMps = 5
	}
	headingDeg := 0.0
	if dLat != 0 || dLon != 0 {
		headingDeg = math.Mod(math.Atan2(dLon, dLat)*180/math.Pi+360, 360)
	}
	vx, vy, vz := computeVelocity(headingDeg, speedMps, alt, tAlt)
	drop := false
	if d, ok := step["drop"].(bool); ok {
		drop = d
	}
	a.sendMotorsTarget(ctx, vx, vy, vz, tAlt, lat, lon, headingDeg, drop)
	a.sendCargo(ctx, drop)
}

func (a *Autopilot) doKover(ctx context.Context, nav map[string]interface{}) {
	alt := getFloat(nav, "alt_m")
	lat := getFloat(nav, "lat")
	lon := getFloat(nav, "lon")
	heading := getFloat(nav, "heading_deg")
	a.sendMotorsTarget(ctx, 0, 0, -1, 0, lat, lon, heading, false)
	a.sendCargo(ctx, false)
	a.mu.Lock()
	a.cargoState = "CLOSED"
	a.mu.Unlock()
	if alt <= 0.5 {
		a.mu.Lock()
		a.koverActive = false
		a.state = StatePaused
		a.mu.Unlock()
		a.logToJournal(ctx, "AUTOPILOT_KOVER_LANDED", map[string]interface{}{})
	}
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

func computeVelocity(headingDeg, groundSpeedMps, currentAlt, targetAlt float64) (vx, vy, vz float64) {
	const maxClimbRate = 3.0
	rad := headingDeg * math.Pi / 180
	vx = groundSpeedMps * math.Sin(rad)
	vy = groundSpeedMps * math.Cos(rad)
	altDiff := targetAlt - currentAlt
	if math.Abs(altDiff) < 0.2 {
		vz = 0
	} else {
		vz = altDiff * 2
		if vz > maxClimbRate {
			vz = maxClimbRate
		}
		if vz < -maxClimbRate {
			vz = -maxClimbRate
		}
	}
	return vx, vy, vz
}

func (a *Autopilot) sendMotorsTarget(ctx context.Context, vx, vy, vz, altM, lat, lon, headingDeg float64, drop bool) {
	msg := map[string]interface{}{
		"action": "proxy_publish",
		"sender": a.ComponentID,
		"payload": map[string]interface{}{
			"target": map[string]interface{}{"topic": a.motorsTopic, "action": "SET_TARGET"},
			"data": map[string]interface{}{
				"vx": vx, "vy": vy, "vz": vz,
				"alt_m": altM, "lat": lat, "lon": lon,
				"heading_deg": headingDeg, "drop": drop,
			},
		},
	}
	if err := a.Bus.Publish(ctx, a.secMonitorTopic, msg); err != nil {
		log.Printf("[%s] send motors: %v", a.ComponentID, err)
	}
}

func (a *Autopilot) sendCargo(ctx context.Context, open bool) {
	action := "CLOSE"
	if open {
		action = "OPEN"
	}
	a.mu.Lock()
	a.cargoState = action
	a.mu.Unlock()
	msg := map[string]interface{}{
		"action": "proxy_publish",
		"sender": a.ComponentID,
		"payload": map[string]interface{}{
			"target": map[string]interface{}{"topic": a.cargoTopic, "action": action},
			"data":   map[string]interface{}{},
		},
	}
	if err := a.Bus.Publish(ctx, a.secMonitorTopic, msg); err != nil {
		log.Printf("[%s] send cargo: %v", a.ComponentID, err)
	}
}

func (a *Autopilot) logToJournal(ctx context.Context, event string, details map[string]interface{}) {
	msg := map[string]interface{}{
		"action": "proxy_publish",
		"sender": a.ComponentID,
		"payload": map[string]interface{}{
			"target": map[string]interface{}{"topic": a.journalTopic, "action": "LOG_EVENT"},
			"data":   map[string]interface{}{"event": event, "source": "autopilot", "details": details},
		},
	}
	if err := a.Bus.Publish(ctx, a.secMonitorTopic, msg); err != nil {
		log.Printf("[%s] log journal: %v", a.ComponentID, err)
	}
}
