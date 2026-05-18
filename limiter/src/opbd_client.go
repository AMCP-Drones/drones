package limiter

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
)

// OpBD action names (gateway external API).
const (
	opbdActionRegisterDrone    = "register_drone"
	opbdActionRegisterMission  = "register_mission"
	opbdActionAuthorizeMission = "authorize_mission"
	opbdActionRequestTakeoff   = "request_takeoff"
	opbdActionSendTelemetry    = "send_telemetry"
	opbdActionCompleteMission  = "complete_mission"
	opbdActionReportIncident   = "report_incident"
	opbdActionGetMissionStatus = "get_mission_status"
)

// OpBD response status values.
const (
	opbdStatusRegistered        = "registered"
	opbdStatusMissionRegistered = "mission_registered"
	opbdStatusAuthorized        = "authorized"
	opbdStatusTakeoffAuthorized = "takeoff_authorized"
	opbdStatusMissionCompleted  = "mission_completed"
	opbdStatusTelemetryReceived = "telemetry_received"
	opbdStatusRejected          = "rejected"
	opbdStatusTakeoffDenied     = "takeoff_denied"
	opbdStatusEmergency         = "emergency"
	opbdStatusError             = "error"
)

// ORVD phase exposed via get_state.
const (
	ORVDPhaseDisabled          = "DISABLED"
	ORVDPhasePending           = "PENDING"
	ORVDPhaseDroneRegistered   = "DRONE_REGISTERED"
	ORVDPhaseMissionRegistered = "MISSION_REGISTERED"
	ORVDPhaseAuthorized        = "AUTHORIZED"
	ORVDPhaseTakeoffAuthorized = "TAKEOFF_AUTHORIZED"
	ORVDPhaseCompleted         = "COMPLETED"
	ORVDPhaseDenied            = "DENIED"
)

func opbdStatus(resp map[string]interface{}) string {
	if resp == nil {
		return ""
	}
	s, _ := resp["status"].(string)
	return strings.TrimSpace(strings.ToLower(s))
}

func missionStepsToRoute(mission map[string]interface{}) []map[string]interface{} {
	steps, _ := mission["steps"].([]interface{})
	route := make([]map[string]interface{}, 0, len(steps))
	for _, s := range steps {
		step, ok := s.(map[string]interface{})
		if !ok {
			continue
		}
		lat, okLat := stepFloat(step, "lat")
		lon, okLon := stepFloat(step, "lon")
		if !okLat || !okLon {
			continue
		}
		route = append(route, map[string]interface{}{"lat": lat, "lon": lon})
	}
	return route
}

func (l *Limiter) callORVD(ctx context.Context, action string, payload map[string]interface{}) (map[string]interface{}, error) {
	if l.orvd.topic == "" {
		return nil, fmt.Errorf("orvd_disabled")
	}
	return l.orvd.orvdProxy.ProxyRequest(ctx, l.orvd.topic, action, payload)
}

func (l *Limiter) setORVDPhase(phase string) {
	l.orvdPhase = phase
}

func (l *Limiter) runORVDMissionLoad(ctx context.Context, mission map[string]interface{}) (status string, errMsg string) {
	mid, _ := mission["mission_id"].(string)
	if l.orvd.topic == "" {
		l.mu.Lock()
		l.setORVDPhase(ORVDPhaseAuthorized)
		l.orvdTakeoffAuthorized = false
		l.mu.Unlock()
		return ORVDStatusAuthorized, ""
	}
	if l.orvd.mockSuccess {
		l.mu.Lock()
		l.setORVDPhase(ORVDPhaseAuthorized)
		l.orvdTakeoffAuthorized = false
		l.orvdDroneRegistered = true
		l.mu.Unlock()
		l.logToJournal(ctx, "ORVD_MISSION_AUTHORIZED", map[string]interface{}{
			"mission_id": mid,
			"stub":       true,
			"reason":     "LIMITER_ORVD_MOCK_SUCCESS",
		})
		return ORVDStatusAuthorized, ""
	}

	l.mu.Lock()
	l.orvdTakeoffAuthorized = false
	l.mu.Unlock()

	if !l.orvdDroneRegistered {
		regPayload := map[string]interface{}{
			"drone_id": l.orvd.droneID,
		}
		if l.orvd.droneModel != "" {
			regPayload["model"] = l.orvd.droneModel
		}
		if l.orvd.operator != "" {
			regPayload["operator"] = l.orvd.operator
		}
		if l.orvd.certificateID != "" {
			regPayload["certificate_id"] = l.orvd.certificateID
		}
		resp, err := l.callORVD(ctx, opbdActionRegisterDrone, regPayload)
		if err != nil {
			l.logToJournal(ctx, "ORVD_MISSION_DENIED", map[string]interface{}{
				"mission_id": mid,
				"step":       opbdActionRegisterDrone,
				"error":      err.Error(),
			})
			l.mu.Lock()
			l.setORVDPhase(ORVDPhaseDenied)
			l.mu.Unlock()
			return ORVDStatusDenied, "orvd_denied"
		}
		if opbdStatus(resp) != opbdStatusRegistered {
			l.logToJournal(ctx, "ORVD_MISSION_DENIED", map[string]interface{}{
				"mission_id": mid,
				"step":       opbdActionRegisterDrone,
				"response":   resp,
			})
			l.mu.Lock()
			l.setORVDPhase(ORVDPhaseDenied)
			l.mu.Unlock()
			return ORVDStatusDenied, "orvd_denied"
		}
		l.mu.Lock()
		l.orvdDroneRegistered = true
		l.setORVDPhase(ORVDPhaseDroneRegistered)
		l.mu.Unlock()
		l.logToJournal(ctx, "ORVD_DRONE_REGISTERED", map[string]interface{}{"drone_id": l.orvd.droneID})
	}

	route := missionStepsToRoute(mission)
	regMissionPayload := map[string]interface{}{
		"drone_id":   l.orvd.droneID,
		"mission_id": mid,
		"route":      route,
		"time":       time.Now().UTC().Format(time.RFC3339),
	}
	if v, ok := mission["velocity"].(float64); ok && v > 0 {
		regMissionPayload["velocity"] = v
	}
	resp, err := l.callORVD(ctx, opbdActionRegisterMission, regMissionPayload)
	if err != nil {
		l.logToJournal(ctx, "ORVD_MISSION_DENIED", map[string]interface{}{
			"mission_id": mid,
			"step":       opbdActionRegisterMission,
			"error":      err.Error(),
		})
		l.mu.Lock()
		l.setORVDPhase(ORVDPhaseDenied)
		l.mu.Unlock()
		return ORVDStatusDenied, "orvd_denied"
	}
	st := opbdStatus(resp)
	if st == opbdStatusRejected || st == opbdStatusError {
		reason, _ := resp["reason"].(string)
		if reason == "" {
			reason, _ = resp["message"].(string)
		}
		l.logToJournal(ctx, "ORVD_MISSION_DENIED", map[string]interface{}{
			"mission_id": mid,
			"step":       opbdActionRegisterMission,
			"reason":     reason,
			"response":   resp,
		})
		l.mu.Lock()
		l.setORVDPhase(ORVDPhaseDenied)
		l.mu.Unlock()
		return ORVDStatusDenied, "orvd_denied"
	}
	if st != opbdStatusMissionRegistered {
		l.logToJournal(ctx, "ORVD_MISSION_DENIED", map[string]interface{}{
			"mission_id": mid,
			"step":       opbdActionRegisterMission,
			"response":   resp,
		})
		l.mu.Lock()
		l.setORVDPhase(ORVDPhaseDenied)
		l.mu.Unlock()
		return ORVDStatusDenied, "orvd_denied"
	}
	l.mu.Lock()
	l.setORVDPhase(ORVDPhaseMissionRegistered)
	l.mu.Unlock()
	l.logToJournal(ctx, "ORVD_MISSION_REGISTERED", map[string]interface{}{"mission_id": mid})

	authResp, err := l.callORVD(ctx, opbdActionAuthorizeMission, map[string]interface{}{
		"mission_id": mid,
	})
	if err != nil {
		l.logToJournal(ctx, "ORVD_MISSION_DENIED", map[string]interface{}{
			"mission_id": mid,
			"step":       opbdActionAuthorizeMission,
			"error":      err.Error(),
		})
		l.mu.Lock()
		l.setORVDPhase(ORVDPhaseDenied)
		l.mu.Unlock()
		return ORVDStatusDenied, "orvd_denied"
	}
	if opbdStatus(authResp) != opbdStatusAuthorized {
		l.logToJournal(ctx, "ORVD_MISSION_DENIED", map[string]interface{}{
			"mission_id": mid,
			"step":       opbdActionAuthorizeMission,
			"response":   authResp,
		})
		l.mu.Lock()
		l.setORVDPhase(ORVDPhaseDenied)
		l.mu.Unlock()
		return ORVDStatusDenied, "orvd_denied"
	}
	l.mu.Lock()
	l.setORVDPhase(ORVDPhaseAuthorized)
	l.mu.Unlock()
	l.logToJournal(ctx, "ORVD_MISSION_AUTHORIZED", map[string]interface{}{"mission_id": mid})
	return ORVDStatusAuthorized, ""
}

func (l *Limiter) runORVDRequestTakeoff(ctx context.Context, missionID string) (ok bool, pending bool, errMsg string) {
	if l.orvd.topic == "" {
		return true, false, ""
	}
	if l.orvd.mockSuccess {
		l.mu.Lock()
		l.orvdTakeoffAuthorized = true
		l.setORVDPhase(ORVDPhaseTakeoffAuthorized)
		nav := l.lastNav
		l.mu.Unlock()
		l.logToJournal(ctx, "ORVD_TAKEOFF_AUTHORIZED", map[string]interface{}{
			"mission_id": missionID,
			"stub":       true,
		})
		if nav != nil {
			l.runORVDSendTelemetry(ctx, nav)
		}
		return true, false, ""
	}
	l.mu.Lock()
	if l.orvdTakeoffAuthorized && l.orvdMissionID == missionID {
		l.mu.Unlock()
		return true, false, ""
	}
	l.mu.Unlock()

	resp, err := l.callORVD(ctx, opbdActionRequestTakeoff, map[string]interface{}{
		"drone_id":   l.orvd.droneID,
		"mission_id": missionID,
		"time":       time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		l.logToJournal(ctx, "ORVD_TAKEOFF_DENIED", map[string]interface{}{
			"mission_id": missionID,
			"error":      err.Error(),
		})
		return false, false, "orvd_takeoff_denied"
	}
	st := opbdStatus(resp)
	if st == opbdStatusTakeoffAuthorized {
		l.mu.Lock()
		l.orvdTakeoffAuthorized = true
		l.setORVDPhase(ORVDPhaseTakeoffAuthorized)
		nav := l.lastNav
		l.mu.Unlock()
		l.logToJournal(ctx, "ORVD_TAKEOFF_AUTHORIZED", map[string]interface{}{"mission_id": missionID})
		if nav != nil {
			l.runORVDSendTelemetry(ctx, nav)
		}
		return true, false, ""
	}
	if st == opbdStatusTakeoffDenied || st == opbdStatusError {
		reason, _ := resp["reason"].(string)
		l.logToJournal(ctx, "ORVD_TAKEOFF_DENIED", map[string]interface{}{
			"mission_id": missionID,
			"reason":     reason,
			"response":   resp,
		})
		return false, false, "orvd_takeoff_denied"
	}
	return false, false, "orvd_takeoff_denied"
}

func (l *Limiter) runORVDCompleteMission(ctx context.Context, missionID string, result string) {
	if l.orvd.topic == "" || l.orvd.mockSuccess {
		l.mu.Lock()
		l.setORVDPhase(ORVDPhaseCompleted)
		l.orvdTakeoffAuthorized = false
		l.mu.Unlock()
		return
	}
	payload := map[string]interface{}{
		"mission_id": missionID,
		"drone_id":   l.orvd.droneID,
		"result":     result,
	}
	resp, err := l.callORVD(ctx, opbdActionCompleteMission, payload)
	if err != nil {
		l.logToJournal(ctx, "ORVD_COMPLETE_FAILED", map[string]interface{}{
			"mission_id": missionID,
			"error":      err.Error(),
		})
		return
	}
	if opbdStatus(resp) == opbdStatusMissionCompleted {
		l.mu.Lock()
		l.setORVDPhase(ORVDPhaseCompleted)
		l.orvdTakeoffAuthorized = false
		l.mu.Unlock()
		l.logToJournal(ctx, "ORVD_MISSION_COMPLETED", map[string]interface{}{"mission_id": missionID})
	}
}

func (l *Limiter) runORVDSendTelemetry(ctx context.Context, nav map[string]interface{}) {
	if l.orvd.topic == "" || l.orvd.mockSuccess {
		return
	}
	l.mu.RLock()
	if !l.orvdTakeoffAuthorized || l.mission == nil {
		l.mu.RUnlock()
		return
	}
	mid := l.orvdMissionID
	l.mu.RUnlock()

	lat := getFloat(nav, "lat")
	lon := getFloat(nav, "lon")
	alt := getFloat(nav, "alt_m")
	payload := map[string]interface{}{
		"drone_id": l.orvd.droneID,
		"coords":   map[string]interface{}{"lat": lat, "lon": lon},
		"altitude": alt,
	}
	resp, err := l.callORVD(ctx, opbdActionSendTelemetry, payload)
	if err != nil {
		return
	}
	st := opbdStatus(resp)
	if st == opbdStatusEmergency {
		cmd, _ := resp["command"].(string)
		if strings.EqualFold(cmd, "LAND") {
			reason, _ := resp["reason"].(string)
			l.logToJournal(ctx, "ORVD_ZONE_VIOLATION", map[string]interface{}{
				"mission_id": mid,
				"reason":     reason,
			})
			l.reportORVDIncident(ctx, mid, "zone_violation", "critical", lat, lon)
			l.publishOREmergencyFromORVD(ctx, reason)
		}
	}
}

func (l *Limiter) reportORVDIncident(ctx context.Context, missionID, incidentType, severity string, lat, lon float64) {
	if l.orvd.topic == "" || l.orvd.mockSuccess {
		return
	}
	_, _ = l.callORVD(ctx, opbdActionReportIncident, map[string]interface{}{
		"drone_id":      l.orvd.droneID,
		"mission_id":    missionID,
		"incident_type": incidentType,
		"severity":      severity,
		"coords":        map[string]interface{}{"lat": lat, "lon": lon},
	})
}

func (l *Limiter) publishOREmergencyFromORVD(ctx context.Context, reason string) {
	details := map[string]interface{}{"source": "orvd", "reason": reason}
	l.logToJournal(ctx, "ORVD_EMERGENCY_LAND_REQUIRED", details)
	eventPayload := map[string]interface{}{
		"event":   "EMERGENCY_LAND_REQUIRED",
		"details": details,
	}
	if err := l.proxy.ProxyPublishAsync(ctx, l.emergencyTopic, "limiter_event", eventPayload); err != nil {
		log.Printf("[%s] publish orvd emergency: %v", l.ComponentID, err)
	}
}
