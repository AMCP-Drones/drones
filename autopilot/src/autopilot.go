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

	"github.com/AMCP-Drones/drones/systems/deliverydron/bus/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/component/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/config/src"
)

// Autopilot state constants.
const (
	StateIDLE          = "IDLE"
	StateMissionLoaded = "MISSION_LOADED"
	StatePreFlight     = "PRE_FLIGHT"
	StateExecuting     = "EXECUTING"
	StatePaused        = "PAUSED"
	StateCompleted     = "COMPLETED"
	StateAborted       = "ABORTED"
	StateEmergencyStop = "EMERGENCY_STOP"
)

type commandTransition struct {
	RequireMission bool
	AllowedFrom    map[string]struct{}
	NextState      string
	ClearMission   bool
	DisableKover   bool
	ActivateKover  bool
}

var commandTransitions = map[string]commandTransition{
	"START": {RequireMission: true, AllowedFrom: map[string]struct{}{StateMissionLoaded: {}, StateIDLE: {}}, NextState: StatePreFlight},
	"PAUSE": {AllowedFrom: map[string]struct{}{StateExecuting: {}}, NextState: StatePaused},
	"RESUME": {AllowedFrom: map[string]struct{}{StatePaused: {}}, NextState: StateExecuting},
	"ABORT": {NextState: StateAborted, DisableKover: true},
	"RESET": {NextState: StateIDLE, ClearMission: true, DisableKover: true},
	"EMERGENCY_STOP": {NextState: StateEmergencyStop, DisableKover: true},
	"KOVER": {ActivateKover: true},
}

// Autopilot runs the mission control loop and commands motors and cargo.
type Autopilot struct {
	*component.BaseComponent
	systemName         string
	secMonitorTopic    string
	journalTopic       string
	navigationTopic    string
	motorsTopic        string
	cargoTopic         string
	limiterTopic       string
	emergencyTopic     string
	controlIntervalSec    float64
	navPollIntervalSec    float64
	requestTimeoutSec     float64
	preflightTimeoutSec   float64
	preFlightStartedAt    time.Time
	proxy              *component.ProxyClient
	audit              *component.AuditLogger
	mu                 sync.RWMutex
	mission            map[string]interface{}
	state              string
	currentStepIndex   int
	lastNavState       map[string]interface{}
	cargoState         string
	koverActive        bool
	lastNavPollTs      float64
	lastError          string
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
	limiterTopic := strings.TrimSpace(os.Getenv("LIMITER_TOPIC"))
	if limiterTopic == "" {
		limiterTopic = cfg.BrokerTopicFor("limiter")
	}
	emergencyTopic := strings.TrimSpace(os.Getenv("EMERGENCY_TOPIC"))
	if emergencyTopic == "" {
		emergencyTopic = cfg.BrokerTopicFor("emergency")
	}
	controlInterval := 0.2
	navPollInterval := 0.1
	requestTimeout := 5.0
	preflightTimeout := 60.0
	for _, pair := range []struct {
		env string
		v   *float64
	}{
		{"AUTOPILOT_CONTROL_INTERVAL_S", &controlInterval},
		{"AUTOPILOT_NAV_POLL_INTERVAL_S", &navPollInterval},
		{"AUTOPILOT_REQUEST_TIMEOUT_S", &requestTimeout},
		{"AUTOPILOT_PREFLIGHT_TIMEOUT_S", &preflightTimeout},
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
		limiterTopic:       limiterTopic,
		emergencyTopic:     emergencyTopic,
		controlIntervalSec:  controlInterval,
		navPollIntervalSec:  navPollInterval,
		requestTimeoutSec:   requestTimeout,
		preflightTimeoutSec: preflightTimeout,
		proxy: &component.ProxyClient{
			Bus:                  b,
			SenderID:             cfg.ComponentID,
			SecurityMonitorTopic: secTopic,
			TimeoutSec:           requestTimeout,
		},
		state:              StateIDLE,
		currentStepIndex:   0,
		lastNavState:       nil,
		cargoState:         "CLOSED",
		koverActive:        false,
		lastNavPollTs:      0,
	}
	a.audit = &component.AuditLogger{
		Proxy:        a.proxy,
		JournalTopic: journalTopic,
		Source:       "autopilot",
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
	component.RunControlLoop(ctx, a.Running, a.controlIntervalSec, func(ctx context.Context) {
		a.pollNavigationIfDue(ctx)
		a.stepControl(ctx)
	})
}

func (a *Autopilot) pollNavigationIfDue(ctx context.Context) {
	now := float64(time.Now().UnixNano()) / 1e9
	if !component.ShouldRunInterval(now, &a.lastNavPollTs, a.navPollIntervalSec) {
		return
	}
	navPl, err := a.proxy.ProxyRequest(ctx, a.navigationTopic, "get_state", map[string]interface{}{})
	if err != nil {
		return
	}
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

func (a *Autopilot) handleCmd(ctx context.Context, message map[string]interface{}) (map[string]interface{}, error) {
	if !component.IsTrustedSender(message, "security_monitor") {
		return nil, nil
	}
	payload, _ := message["payload"].(map[string]interface{})
	if payload == nil {
		return map[string]interface{}{"ok": false, "error": "invalid_payload"}, nil
	}
	cmd, _ := payload["command"].(string)
	cmd = strings.TrimSpace(strings.ToUpper(cmd))
	transition, ok := commandTransitions[cmd]
	if !ok {
		return map[string]interface{}{"ok": false, "error": "unknown_command"}, nil
	}

	a.mu.Lock()
	oldState := a.state
	if transition.RequireMission && a.mission == nil {
		a.mu.Unlock()
		return map[string]interface{}{"ok": false, "error": "no_mission"}, nil
	}
	if cmd == "START" {
		a.lastError = ""
		a.preFlightStartedAt = time.Now()
	}
	if transition.AllowedFrom != nil {
		if _, allowed := transition.AllowedFrom[a.state]; !allowed {
			a.mu.Unlock()
			return map[string]interface{}{"ok": true, "state": a.state}, nil
		}
	}
	if transition.ClearMission {
		a.mission = nil
		a.currentStepIndex = 0
	}
	if transition.DisableKover {
		a.koverActive = false
	}
	if transition.ActivateKover {
		a.koverActive = true
		if a.state != StateExecuting && a.state != StatePaused {
			a.state = StateExecuting
		}
	}
	if transition.NextState != "" {
		a.state = transition.NextState
	}
	newState := a.state
	needsSafeStop := cmd == "ABORT" || cmd == "EMERGENCY_STOP"
	a.mu.Unlock()

	if cmd == "KOVER" {
		a.logToJournal(context.Background(), "AUTOPILOT_KOVER_ACTIVE", map[string]interface{}{})
	}
	if needsSafeStop {
		a.safeActuatorStop(context.Background())
		if cmd == "ABORT" {
			a.logToJournal(context.Background(), "AUTOPILOT_ABORTED", map[string]interface{}{"old_state": oldState})
		}
		if cmd == "EMERGENCY_STOP" {
			a.logToJournal(context.Background(), "AUTOPILOT_EMERGENCY_STOP", map[string]interface{}{"old_state": oldState})
		}
	}
	if oldState != newState {
		a.logToJournal(context.Background(), "AUTOPILOT_STATE_CHANGE", map[string]interface{}{"old_state": oldState, "new_state": newState, "command": cmd})
	}
	if cmd == "START" && newState == StatePreFlight {
		mid := ""
		if a.mission != nil {
			mid, _ = a.mission["mission_id"].(string)
		}
		a.logToJournal(context.Background(), "AUTOPILOT_START_ACCEPTED", map[string]interface{}{"mission_id": mid, "state": newState})
		a.stepPreFlight(ctx)
		a.mu.Lock()
		newState = a.state
		a.mu.Unlock()
	}
	return map[string]interface{}{"ok": true, "state": newState}, nil
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
		"last_error":         a.lastError,
	}, nil
}

const preflightPending = "pending"

// checkLimiterAuthorization verifies limiter ORVD clearance for the loaded mission.
// Returns "" when cleared, preflightPending while ORVD is still pending, or an error key.
func (a *Autopilot) checkLimiterAuthorization(ctx context.Context) string {
	a.mu.RLock()
	mission := a.mission
	a.mu.RUnlock()
	if mission == nil {
		return "no_mission"
	}
	mid, _ := mission["mission_id"].(string)
	pl, err := a.proxy.ProxyRequest(ctx, a.limiterTopic, "get_state", map[string]interface{}{})
	if err != nil {
		return "limiter_not_cleared"
	}
	status, _ := pl["orvd_status"].(string)
	switch status {
	case "AUTHORIZED":
		// cleared
	case "PENDING":
		return preflightPending
	default:
		return "limiter_not_cleared"
	}
	orvdMid, _ := pl["orvd_mission_id"].(string)
	if orvdMid != "" && mid != "" && orvdMid != mid {
		return "mission_id_mismatch"
	}
	return ""
}

// requestORVDTakeoff asks limiter to call OpBD request_takeoff. Returns "" when cleared,
// preflightPending while waiting, or an error key.
func (a *Autopilot) requestORVDTakeoff(ctx context.Context) string {
	a.mu.RLock()
	mission := a.mission
	a.mu.RUnlock()
	if mission == nil {
		return "no_mission"
	}
	mid, _ := mission["mission_id"].(string)
	pl, err := a.proxy.ProxyRequest(ctx, a.limiterTopic, "get_state", map[string]interface{}{})
	if err == nil {
		if takeoff, _ := pl["orvd_takeoff_authorized"].(bool); takeoff {
			return ""
		}
	}
	resp, err := a.proxy.ProxyRequest(ctx, a.limiterTopic, "orvd_takeoff", map[string]interface{}{
		"mission_id": mid,
	})
	if err != nil {
		return "orvd_takeoff_denied"
	}
	if resp == nil || resp["ok"] != true {
		return "orvd_takeoff_denied"
	}
	return ""
}

func (a *Autopilot) notifyORVDComplete(ctx context.Context, missionID string) {
	if missionID == "" {
		return
	}
	_, _ = a.proxy.ProxyRequest(ctx, a.limiterTopic, "orvd_complete", map[string]interface{}{
		"mission_id": missionID,
		"result":     "success",
	})
}

func (a *Autopilot) requestDroneportTakeoff(ctx context.Context) string {
	a.mu.RLock()
	mission := a.mission
	a.mu.RUnlock()
	if mission == nil {
		return "no_mission"
	}
	mid, _ := mission["mission_id"].(string)
	st, err := a.proxy.ProxyRequest(ctx, a.emergencyTopic, "get_state", map[string]interface{}{})
	if err == nil && st != nil {
		if phase, _ := st["droneport_phase"].(string); phase == "DEPARTED" {
			return ""
		}
	}
	pl, err := a.proxy.ProxyRequest(ctx, a.emergencyTopic, "droneport_takeoff", map[string]interface{}{
		"mission_id": mid,
	})
	if err != nil {
		return "droneport_denied"
	}
	if pl != nil && pl["pending"] == true {
		return preflightPending
	}
	if pl == nil || pl["ok"] != true {
		return "droneport_denied"
	}
	return ""
}

func (a *Autopilot) notifyDroneportLand(ctx context.Context, missionID string) {
	if missionID == "" {
		return
	}
	payload := map[string]interface{}{"mission_id": missionID}
	a.mu.RLock()
	nav := a.lastNavState
	a.mu.RUnlock()
	if nav != nil {
		if v, ok := nav["battery_pct"]; ok {
			payload["battery_pct"] = v
		} else if v, ok := nav["battery"]; ok {
			payload["battery"] = v
		}
	}
	_, _ = a.proxy.ProxyRequest(ctx, a.emergencyTopic, "droneport_land", payload)
}

func (a *Autopilot) stepPreFlight(ctx context.Context) {
	a.mu.RLock()
	state := a.state
	started := a.preFlightStartedAt
	timeout := a.preflightTimeoutSec
	a.mu.RUnlock()
	if state != StatePreFlight {
		return
	}
	if timeout > 0 && !started.IsZero() && time.Since(started).Seconds() > timeout {
		a.mu.Lock()
		a.state = StateAborted
		a.lastError = "preflight_timeout"
		mid := ""
		if a.mission != nil {
			mid, _ = a.mission["mission_id"].(string)
		}
		a.mu.Unlock()
		a.logToJournal(ctx, "AUTOPILOT_PREFLIGHT_TIMEOUT", map[string]interface{}{"mission_id": mid})
		return
	}
	errKey := a.checkLimiterAuthorization(ctx)
	if errKey == preflightPending {
		return
	}
	if errKey != "" {
		a.mu.Lock()
		if a.state == StatePreFlight {
			a.state = StateAborted
			a.lastError = errKey
		}
		a.mu.Unlock()
		return
	}
	takeoffErr := a.requestORVDTakeoff(ctx)
	if takeoffErr == preflightPending {
		return
	}
	if takeoffErr != "" {
		a.mu.Lock()
		if a.state == StatePreFlight {
			a.state = StateAborted
			a.lastError = takeoffErr
		}
		a.mu.Unlock()
		return
	}
	dpErr := a.requestDroneportTakeoff(ctx)
	if dpErr == preflightPending {
		return
	}
	if dpErr != "" {
		a.mu.Lock()
		if a.state == StatePreFlight {
			a.state = StateAborted
			a.lastError = dpErr
		}
		a.mu.Unlock()
		return
	}
	a.mu.Lock()
	if a.state != StatePreFlight {
		a.mu.Unlock()
		return
	}
	a.state = StateExecuting
	a.lastError = ""
	mid := ""
	if a.mission != nil {
		mid, _ = a.mission["mission_id"].(string)
	}
	a.mu.Unlock()
	a.logToJournal(ctx, "AUTOPILOT_PREFLIGHT_PASSED", map[string]interface{}{"mission_id": mid})
}

func (a *Autopilot) stepControl(ctx context.Context) {
	a.mu.RLock()
	nav := a.lastNavState
	mission := a.mission
	state := a.state
	stepIndex := a.currentStepIndex
	kover := a.koverActive
	a.mu.RUnlock()
	if state == StatePreFlight {
		a.stepPreFlight(ctx)
		return
	}
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
			a.mu.Unlock()
			a.logToJournal(ctx, "AUTOPILOT_MISSION_COMPLETED", map[string]interface{}{"mission_id": mid})
			a.notifyDroneportLand(ctx, mid)
			a.notifyORVDComplete(ctx, mid)
			return
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
			a.notifyDroneportLand(ctx, mid)
			a.notifyORVDComplete(ctx, mid)
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

func (a *Autopilot) safeActuatorStop(ctx context.Context) {
	if a.lastNavState != nil {
		lat := getFloat(a.lastNavState, "lat")
		lon := getFloat(a.lastNavState, "lon")
		alt := getFloat(a.lastNavState, "alt_m")
		heading := getFloat(a.lastNavState, "heading_deg")
		a.sendMotorsTarget(ctx, 0, 0, 0, alt, lat, lon, heading, false)
	}
	a.sendCargo(ctx, false)
}

func (a *Autopilot) sendMotorsTarget(ctx context.Context, vx, vy, vz, altM, lat, lon, headingDeg float64, drop bool) {
	if err := a.proxy.ProxyPublishAsync(ctx, a.motorsTopic, "SET_TARGET", map[string]interface{}{
		"vx": vx, "vy": vy, "vz": vz,
		"alt_m": altM, "lat": lat, "lon": lon,
		"heading_deg": headingDeg, "drop": drop,
	}); err != nil {
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
	if err := a.proxy.ProxyPublishAsync(ctx, a.cargoTopic, action, map[string]interface{}{}); err != nil {
		log.Printf("[%s] send cargo: %v", a.ComponentID, err)
	}
}

func (a *Autopilot) logToJournal(ctx context.Context, event string, details map[string]interface{}) {
	if err := a.audit.LogEvent(ctx, event, details); err != nil {
		log.Printf("[%s] log journal: %v", a.ComponentID, err)
	}
}
