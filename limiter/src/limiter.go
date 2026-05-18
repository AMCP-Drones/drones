// Package limiter implements the geofence component: mission_load, update_config, get_state; polls nav and telemetry, publishes limiter_event to emergency and LOG_EVENT to journal on deviation.
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

	"github.com/AMCP-Drones/drones/systems/deliverydron/bus/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/component/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/config/src"
)

// Limiter state constants.
const (
	StateNormal    = "NORMAL"
	StateWarning   = "WARNING"
	StateEmergency = "EMERGENCY"
)

// Limiter holds mission and last nav/telemetry; compares position to mission and triggers emergency on breach.
type Limiter struct {
	*component.BaseComponent
	systemName               string
	secMonitorTopic          string
	journalTopic             string
	navigationTopic          string
	telemetryTopic           string
	emergencyTopic           string
	controlIntervalSec       float64
	navPollIntervalSec       float64
	telemetryPollIntervalSec float64
	requestTimeoutSec        float64
	proxy                    *component.ProxyClient
	audit                    *component.AuditLogger
	maxDistanceFromPathM     float64
	maxAltDeviationM         float64
	mu                       sync.RWMutex
	mission                  map[string]interface{}
	lastNav                  map[string]interface{}
	lastTelemetry            map[string]interface{}
	state                    string
	lastNavPollTs            float64
	lastTelemetryPollTs      float64
	orvd                     orvdConfig
	orvdStatus               string
	orvdMissionID            string
	orvdPhase                string
	orvdDroneRegistered      bool
	orvdTakeoffAuthorized    bool
	lastORVDTelemetryTs      float64
}

// New creates a Limiter. Call Start after creation.
func New(cfg *config.Config, b bus.Bus) *Limiter {
	systemName := cfg.SystemName
	if systemName == "" {
		systemName = "deliverydron"
	}
	topic := cfg.ComponentTopic
	if topic == "" {
		topic = cfg.BrokerTopicFor("limiter")
	}
	base := component.NewBaseComponent(cfg.ComponentID, "limiter", topic, b)
	secTopic := os.Getenv("SECURITY_MONITOR_TOPIC")
	if secTopic == "" {
		secTopic = cfg.BrokerTopicFor("security_monitor")
	}
	journalTopic := cfg.BrokerTopicFor("journal")
	navTopic := cfg.BrokerTopicFor("navigation")
	telemetryTopic := cfg.BrokerTopicFor("telemetry")
	emergencyTopic := cfg.BrokerTopicFor("emergency")
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
		emergencyTopic:           emergencyTopic,
		controlIntervalSec:       controlInterval,
		navPollIntervalSec:       navPollInterval,
		telemetryPollIntervalSec: telemetryPollInterval,
		requestTimeoutSec:        requestTimeout,
		proxy: &component.ProxyClient{
			Bus:                  b,
			SenderID:             cfg.ComponentID,
			SecurityMonitorTopic: secTopic,
			TimeoutSec:           requestTimeout,
		},
		maxDistanceFromPathM:     maxDist,
		maxAltDeviationM:         maxAlt,
		state:                    StateNormal,
		lastNavPollTs:            0,
		lastTelemetryPollTs:      0,
	}
	l.audit = &component.AuditLogger{
		Proxy:        l.proxy,
		JournalTopic: journalTopic,
		Source:       "limiter",
	}
	l.orvd = loadORVDConfig(cfg.ComponentID, cfg.InstanceID, requestTimeout, l.proxy)
	l.orvdStatus = ORVDStatusDisabled
	l.orvdPhase = ORVDPhaseDisabled
	if l.orvd.topic != "" {
		log.Printf("[%s] ORVD enabled topic=%s drone_id=%s",
			cfg.ComponentID, l.orvd.topic, l.orvd.droneID)
	}
	l.registerHandlers()
	return l
}

func (l *Limiter) registerHandlers() {
	l.RegisterHandler("mission_load", l.handleMissionLoad)
	l.RegisterHandler("update_config", l.handleUpdateConfig)
	l.RegisterHandler("get_state", l.handleGetState)
	l.RegisterHandler("orvd_takeoff", l.handleORVDTakeoff)
	l.RegisterHandler("orvd_complete", l.handleORVDComplete)
	l.RegisterHandler("revoke_takeoff", l.handleRevokeTakeoff)
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
	component.RunControlLoop(ctx, l.Running, l.controlIntervalSec, func(ctx context.Context) {
		l.pollNavigationIfDue(ctx)
		l.pollTelemetryIfDue(ctx)
		l.pollORVDTelemetryIfDue(ctx)
		l.recalculate(ctx)
	})
}

func (l *Limiter) pollORVDTelemetryIfDue(ctx context.Context) {
	if l.orvd.topic == "" {
		return
	}
	now := float64(time.Now().UnixNano()) / 1e9
	if !component.ShouldRunInterval(now, &l.lastORVDTelemetryTs, l.orvd.telemetryIntervalSec) {
		return
	}
	l.mu.RLock()
	nav := l.lastNav
	l.mu.RUnlock()
	if nav == nil {
		return
	}
	l.runORVDSendTelemetry(ctx, nav)
}

func (l *Limiter) proxyRequest(ctx context.Context, targetTopic, action string) map[string]interface{} {
	resp, err := l.proxy.ProxyRequest(ctx, targetTopic, action, map[string]interface{}{})
	if err != nil {
		return nil
	}
	return resp
}

func (l *Limiter) pollNavigationIfDue(ctx context.Context) {
	now := float64(time.Now().UnixNano()) / 1e9
	if !component.ShouldRunInterval(now, &l.lastNavPollTs, l.navPollIntervalSec) {
		return
	}
	nav := l.proxyRequest(ctx, l.navigationTopic, "get_state")
	if nav != nil {
		l.mu.Lock()
		l.lastNav = nav
		l.mu.Unlock()
	}
}

func (l *Limiter) pollTelemetryIfDue(ctx context.Context) {
	now := float64(time.Now().UnixNano()) / 1e9
	if !component.ShouldRunInterval(now, &l.lastTelemetryPollTs, l.telemetryPollIntervalSec) {
		return
	}
	telem := l.proxyRequest(ctx, l.telemetryTopic, "get_state")
	if telem != nil {
		l.mu.Lock()
		l.lastTelemetry = telem
		l.mu.Unlock()
	}
}

func (l *Limiter) handleMissionLoad(ctx context.Context, message map[string]interface{}) (map[string]interface{}, error) {
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
	mid, _ := mission["mission_id"].(string)

	if ok, boundsErr := validateMissionBounds(mission, l.orvd.maxMissionAltM); !ok {
		l.mu.Lock()
		l.orvdStatus = ORVDStatusOutOfBounds
		l.orvdMissionID = mid
		l.mu.Unlock()
		l.logToJournal(ctx, "LIMITER_MISSION_OUT_OF_BOUNDS", map[string]interface{}{
			"mission_id": mid,
			"error":      boundsErr,
		})
		return map[string]interface{}{"ok": false, "error": "mission_out_of_bounds"}, nil
	}

	l.mu.Lock()
	l.orvdStatus = ORVDStatusPending
	l.orvdMissionID = mid
	l.setORVDPhase(ORVDPhasePending)
	l.orvdTakeoffAuthorized = false
	l.mu.Unlock()

	status, errMsg := l.runORVDMissionLoad(ctx, mission)
	l.mu.Lock()
	l.orvdStatus = status
	l.orvdMissionID = mid
	if status == ORVDStatusAuthorized {
		l.mission = mission
	}
	l.mu.Unlock()

	if status != ORVDStatusAuthorized {
		if errMsg == "" {
			errMsg = "orvd_denied"
		}
		return map[string]interface{}{
			"ok":          false,
			"error":       errMsg,
			"orvd_status": status,
		}, nil
	}
	return map[string]interface{}{
		"ok":          true,
		"orvd_status": status,
		"mission_id":  mid,
	}, nil
}

func isLimiterConfigSender(message map[string]interface{}) bool {
	return component.IsTrustedSender(message, "security_monitor") ||
		component.IsTrustedSender(message, "orvd")
}

func applyConfigPayload(l *Limiter, payload map[string]interface{}) {
	if v, ok := payload["max_distance_from_path_m"].(float64); ok && v > 0 {
		l.maxDistanceFromPathM = v
	}
	if v, ok := payload["max_alt_deviation_m"].(float64); ok && v > 0 {
		l.maxAltDeviationM = v
	}
	if constraints, ok := payload["constraints"].(map[string]interface{}); ok {
		if v, ok := constraints["max_distance_from_path_m"].(float64); ok && v > 0 {
			l.maxDistanceFromPathM = v
		}
		if v, ok := constraints["max_alt_deviation_m"].(float64); ok && v > 0 {
			l.maxAltDeviationM = v
		}
	}
}

func (l *Limiter) handleUpdateConfig(ctx context.Context, message map[string]interface{}) (map[string]interface{}, error) {
	if !isLimiterConfigSender(message) {
		return nil, nil
	}
	payload, _ := message["payload"].(map[string]interface{})
	if payload == nil {
		return map[string]interface{}{"ok": false, "error": "invalid_payload"}, nil
	}
	fromORVD := component.IsTrustedSender(message, "orvd")
	l.mu.Lock()
	applyConfigPayload(l, payload)
	localMaxDistanceFromPathM := l.maxDistanceFromPathM
	localMaxAltDeviationM := l.maxAltDeviationM
	l.mu.Unlock()
	if fromORVD {
		l.logToJournal(ctx, "ORVD_PUSH_UPDATE_CONFIG", map[string]interface{}{
			"max_distance_from_path_m": localMaxDistanceFromPathM,
			"max_alt_deviation_m":      localMaxAltDeviationM,
		})
	}
	return map[string]interface{}{
		"ok":                       true,
		"max_distance_from_path_m": localMaxDistanceFromPathM,
		"max_alt_deviation_m":      localMaxAltDeviationM,
	}, nil
}

func (l *Limiter) handleORVDTakeoff(ctx context.Context, message map[string]interface{}) (map[string]interface{}, error) {
	if !component.IsTrustedSender(message, "security_monitor") {
		return nil, nil
	}
	payload, _ := message["payload"].(map[string]interface{})
	mid := ""
	if payload != nil {
		mid, _ = payload["mission_id"].(string)
	}
	if mid == "" {
		l.mu.RLock()
		mid = l.orvdMissionID
		l.mu.RUnlock()
	}
	if mid == "" {
		return map[string]interface{}{"ok": false, "error": "no_mission"}, nil
	}
	l.mu.RLock()
	if l.orvdStatus != ORVDStatusAuthorized {
		st := l.orvdStatus
		l.mu.RUnlock()
		return map[string]interface{}{"ok": false, "error": "orvd_not_authorized", "orvd_status": st}, nil
	}
	l.mu.RUnlock()

	l.pollNavigationIfDue(ctx)
	ok, _, errMsg := l.runORVDRequestTakeoff(ctx, mid)
	if !ok {
		if errMsg == "" {
			errMsg = "orvd_takeoff_denied"
		}
		return map[string]interface{}{"ok": false, "error": errMsg}, nil
	}
	return map[string]interface{}{
		"ok":                      true,
		"orvd_takeoff_authorized": true,
		"mission_id":              mid,
	}, nil
}

func (l *Limiter) handleORVDComplete(ctx context.Context, message map[string]interface{}) (map[string]interface{}, error) {
	if !component.IsTrustedSender(message, "security_monitor") {
		return nil, nil
	}
	payload, _ := message["payload"].(map[string]interface{})
	if payload == nil {
		return map[string]interface{}{"ok": false, "error": "invalid_payload"}, nil
	}
	mid, _ := payload["mission_id"].(string)
	result, _ := payload["result"].(string)
	if result == "" {
		result = "success"
	}
	if mid == "" {
		l.mu.RLock()
		mid = l.orvdMissionID
		l.mu.RUnlock()
	}
	if mid == "" {
		return map[string]interface{}{"ok": false, "error": "no_mission"}, nil
	}
	l.runORVDCompleteMission(ctx, mid, result)
	return map[string]interface{}{"ok": true, "mission_id": mid}, nil
}

func (l *Limiter) handleRevokeTakeoff(ctx context.Context, message map[string]interface{}) (map[string]interface{}, error) {
	if !component.IsTrustedSender(message, "orvd") {
		return nil, nil
	}
	payload, _ := message["payload"].(map[string]interface{})
	droneID := ""
	if payload != nil {
		droneID, _ = payload["drone_id"].(string)
	}
	l.logToJournal(ctx, "ORVD_REVOKE_TAKEOFF", map[string]interface{}{
		"drone_id": droneID,
	})
	l.mu.Lock()
	l.orvdTakeoffAuthorized = false
	l.mu.Unlock()
	l.publishOREmergencyFromORVD(ctx, "takeoff_revoked")
	return map[string]interface{}{"ok": true, "status": "landing_required"}, nil
}

func (l *Limiter) handleGetState(_ context.Context, _ map[string]interface{}) (map[string]interface{}, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return map[string]interface{}{
		"state":                    l.state,
		"max_distance_from_path_m": l.maxDistanceFromPathM,
		"max_alt_deviation_m":      l.maxAltDeviationM,
		"orvd_status":              l.orvdStatus,
		"orvd_mission_id":          l.orvdMissionID,
		"orvd_phase":               l.orvdPhase,
		"orvd_takeoff_authorized":  l.orvdTakeoffAuthorized,
		"mission_loaded":           l.mission != nil,
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

func haversineDistance(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusM = 6371000.0
	dLat := (lat2 - lat1) * math.Pi / 180.0
	dLon := (lon2 - lon1) * math.Pi / 180.0
	lat1Rad := lat1 * math.Pi / 180.0
	lat2Rad := lat2 * math.Pi / 180.0
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1Rad)*math.Cos(lat2Rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadiusM * c
}

func (l *Limiter) recalculate(ctx context.Context) {
	l.mu.RLock()
	mission := l.mission
	nav := l.lastNav
	maxDistance := l.maxDistanceFromPathM
	maxAlt := l.maxAltDeviationM
	currentState := l.state
	l.mu.RUnlock()

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
	distanceM := haversineDistance(lat, lon, tLat, tLon)
	altDev := math.Abs(alt - tAlt)

	var newState string
	if distanceM > maxDistance || altDev > maxAlt {
		newState = StateEmergency
	} else if distanceM > 0.5*maxDistance || altDev > 0.5*maxAlt {
		newState = StateWarning
	} else {
		newState = StateNormal
	}

	if newState != currentState {
		l.mu.Lock()
		l.state = newState
		l.mu.Unlock()

		if newState == StateEmergency {
			l.publishEmergency(ctx, distanceM, altDev)
		} else if newState == StateWarning {
			l.logToJournal(ctx, "LIMITER_DEVIATION_WARNING", map[string]interface{}{"distance_m": distanceM, "alt_deviation_m": altDev})
		}
	}
}

func (l *Limiter) publishEmergency(ctx context.Context, distanceM, altDev float64) {
	l.mu.RLock()
	localMaxDistanceFromPathM := l.maxDistanceFromPathM
	localMaxAltDeviationM := l.maxAltDeviationM
	mid := l.orvdMissionID
	nav := l.lastNav
	l.mu.RUnlock()
	details := map[string]interface{}{
		"distance_from_path_m":     distanceM,
		"max_distance_from_path_m": localMaxDistanceFromPathM,
		"alt_deviation_m":          altDev,
		"max_alt_deviation_m":      localMaxAltDeviationM,
	}
	l.logToJournal(ctx, "LIMITER_EMERGENCY_LAND_REQUIRED", details)
	if nav != nil && mid != "" {
		l.reportORVDIncident(ctx, mid, "geofence_violation", "critical",
			getFloat(nav, "lat"), getFloat(nav, "lon"))
	}
	eventPayload := map[string]interface{}{
		"event":   "EMERGENCY_LAND_REQUIRED",
		"details": details,
	}
	if err := l.proxy.ProxyPublishAsync(ctx, l.emergencyTopic, "limiter_event", eventPayload); err != nil {
		log.Printf("[%s] publish emergency: %v", l.ComponentID, err)
	}
}

func (l *Limiter) logToJournal(ctx context.Context, event string, details map[string]interface{}) {
	if err := l.audit.LogEvent(ctx, event, details); err != nil {
		log.Printf("[%s] log journal: %v", l.ComponentID, err)
	}
}
