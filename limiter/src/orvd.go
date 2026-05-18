package limiter

import (
	"context"
	"fmt"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/AMCP-Drones/drones/systems/deliverydron/component/src"
)

// ORVD authorization status values exposed via get_state.
const (
	ORVDStatusDisabled    = "DISABLED"
	ORVDStatusPending     = "PENDING"
	ORVDStatusAuthorized  = "AUTHORIZED"
	ORVDStatusDenied      = "DENIED"
	ORVDStatusOutOfBounds = "OUT_OF_BOUNDS"
)

const (
	defaultORVDDroneID        = "drone_001"
	defaultORVDAction         = "validate_mission"
	defaultMaxMissionAltM     = 5000.0
	orvdActionTakeoff         = "request_takeoff"
	orvdStatusMissionAuth     = "mission_authorized"
	orvdStatusTakeoffAuth     = "takeoff_authorized"
)

var orvdDroneIDPattern = regexp.MustCompile(`^drone_[0-9]{3,4}$`)

type orvdConfig struct {
	topic           string
	droneID         string
	mockSuccess     bool
	action          string
	requestTimeout  float64
	maxMissionAltM  float64
	orvdProxy       *component.ProxyClient
}

func loadORVDConfig(cfgComponentID string, instanceID string, requestTimeout float64, proxy *component.ProxyClient) orvdConfig {
	topic := strings.TrimSpace(os.Getenv("ORVD_TOPIC"))
	if topic == "" {
		topic = strings.TrimSpace(os.Getenv("ORVD_EXTERNAL_TOPIC"))
	}
	droneID := strings.TrimSpace(os.Getenv("ORVD_DRONE_ID"))
	if droneID == "" {
		droneID = strings.TrimSpace(instanceID)
	}
	if !orvdDroneIDPattern.MatchString(droneID) {
		droneID = defaultORVDDroneID
	}
	action := strings.TrimSpace(os.Getenv("LIMITER_ORVD_ACTION"))
	if action == "" {
		action = defaultORVDAction
	}
	orvdTimeout := requestTimeout
	if s := os.Getenv("LIMITER_ORVD_REQUEST_TIMEOUT_S"); s != "" {
		if v, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil && v > 0 {
			orvdTimeout = v
		}
	}
	maxMissionAlt := defaultMaxMissionAltM
	if s := os.Getenv("LIMITER_MAX_MISSION_ALT_M"); s != "" {
		if v, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil && v > 0 {
			maxMissionAlt = v
		}
	}
	mock := parseBoolEnv(os.Getenv("LIMITER_ORVD_MOCK_SUCCESS"))
	orvdProxy := &component.ProxyClient{
		Bus:                  proxy.Bus,
		SenderID:             cfgComponentID,
		SecurityMonitorTopic: proxy.SecurityMonitorTopic,
		TimeoutSec:           orvdTimeout,
	}
	return orvdConfig{
		topic:          topic,
		droneID:        droneID,
		mockSuccess:    mock,
		action:         action,
		requestTimeout: orvdTimeout,
		maxMissionAltM: maxMissionAlt,
		orvdProxy:      orvdProxy,
	}
}

func parseBoolEnv(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func stepFloat(step map[string]interface{}, key string) (float64, bool) {
	v, ok := step[key]
	if !ok {
		return 0, false
	}
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	default:
		return 0, false
	}
}

func validateMissionBounds(mission map[string]interface{}, maxMissionAltM float64) (bool, string) {
	steps, _ := mission["steps"].([]interface{})
	if len(steps) == 0 {
		return false, "empty_steps"
	}
	for i, s := range steps {
		step, ok := s.(map[string]interface{})
		if !ok {
			return false, fmt.Sprintf("invalid_step_%d", i)
		}
		lat, okLat := stepFloat(step, "lat")
		lon, okLon := stepFloat(step, "lon")
		alt, okAlt := stepFloat(step, "alt_m")
		if !okLat || !okLon || !okAlt {
			return false, fmt.Sprintf("invalid_coords_step_%d", i)
		}
		if math.Abs(lat) > 90 || math.Abs(lon) > 180 {
			return false, fmt.Sprintf("coords_out_of_range_step_%d", i)
		}
		if alt < 0 || alt > maxMissionAltM {
			return false, fmt.Sprintf("alt_out_of_range_step_%d", i)
		}
	}
	return true, ""
}

func orvdResponseAuthorized(resp map[string]interface{}) bool {
	if resp == nil {
		return false
	}
	status, _ := resp["status"].(string)
	status = strings.TrimSpace(strings.ToLower(status))
	return status == orvdStatusMissionAuth || status == orvdStatusTakeoffAuth
}

func applyORVDConstraints(l *Limiter, resp map[string]interface{}) {
	constraints, _ := resp["constraints"].(map[string]interface{})
	if constraints == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if v, ok := constraints["max_distance_from_path_m"].(float64); ok && v > 0 {
		l.maxDistanceFromPathM = v
	}
	if v, ok := constraints["max_alt_deviation_m"].(float64); ok && v > 0 {
		l.maxAltDeviationM = v
	}
}

func (l *Limiter) requestORVDValidation(ctx context.Context, mission map[string]interface{}) (status string, errMsg string) {
	mid, _ := mission["mission_id"].(string)
	if l.orvd.topic == "" {
		return ORVDStatusAuthorized, ""
	}
	if l.orvd.mockSuccess {
		l.logToJournal(ctx, "ORVD_MISSION_AUTHORIZED", map[string]interface{}{
			"mission_id": mid,
			"stub":       true,
			"reason":     "LIMITER_ORVD_MOCK_SUCCESS",
		})
		return ORVDStatusAuthorized, ""
	}
	payload := map[string]interface{}{
		"drone_id":    l.orvd.droneID,
		"mission_id":  mid,
		"mission":     mission,
		"time":        time.Now().UTC().Format(time.RFC3339),
	}
	resp, err := l.orvd.orvdProxy.ProxyRequest(ctx, l.orvd.topic, l.orvd.action, payload)
	if err != nil {
		l.logToJournal(ctx, "ORVD_MISSION_DENIED", map[string]interface{}{
			"mission_id": mid,
			"error":      err.Error(),
		})
		return ORVDStatusDenied, "orvd_denied"
	}
	if orvdResponseAuthorized(resp) {
		applyORVDConstraints(l, resp)
		l.logToJournal(ctx, "ORVD_MISSION_AUTHORIZED", map[string]interface{}{"mission_id": mid})
		return ORVDStatusAuthorized, ""
	}
	l.logToJournal(ctx, "ORVD_MISSION_DENIED", map[string]interface{}{
		"mission_id": mid,
		"response":   resp,
	})
	return ORVDStatusDenied, "orvd_denied"
}
