package emergency

import (
	"context"
	"fmt"
	"time"
)

// DronePort external API actions (drone_manager + orchestrator).
const (
	droneportActionRequestLanding     = "request_landing"
	droneportActionRequestTakeoff     = "request_takeoff"
	droneportActionGetAvailableDrones = "get_available_drones"
)

// DronePort session phases exposed via get_state.
const (
	DroneportPhaseDisabled      = "DISABLED"
	DroneportPhaseNotRegistered = "NOT_REGISTERED"
	DroneportPhaseCharging      = "CHARGING"
	DroneportPhaseReady         = "READY"
	DroneportPhaseDeparted      = "DEPARTED"
)

type preflightResult int

const (
	preflightOK preflightResult = iota
	preflightPending
	preflightDenied
)

func (e *Emergency) setDroneportPhase(phase string) {
	e.dpMu.Lock()
	e.droneportPhase = phase
	e.dpMu.Unlock()
}

func (e *Emergency) setDroneportError(err string) {
	e.dpMu.Lock()
	e.droneportLastError = err
	e.dpMu.Unlock()
}

func (e *Emergency) callDroneport(ctx context.Context, topic, action string, payload map[string]interface{}) (map[string]interface{}, error) {
	if topic == "" {
		return nil, fmt.Errorf("droneport_disabled")
	}
	return e.droneport.proxy.ProxyRequest(ctx, topic, action, payload)
}

func droneportApproved(body map[string]interface{}) bool {
	if body == nil || body["error"] != nil {
		return false
	}
	if approved, ok := body["approved"].(bool); ok && approved {
		return true
	}
	return false
}

func droneportLandingOK(resp map[string]interface{}) bool {
	body := unwrapDroneportBody(resp)
	if body == nil || body["error"] != nil {
		return false
	}
	if droneportApproved(body) {
		return true
	}
	_, hasPort := body["port_id"]
	return hasPort
}

func droneportTakeoffOK(resp map[string]interface{}) bool {
	body := unwrapDroneportBody(resp)
	if body == nil || body["error"] != nil {
		return false
	}
	if droneportApproved(body) {
		_, hasBatt := body["battery"]
		return hasBatt
	}
	_, hasPort := body["port_id"]
	_, hasBatt := body["battery"]
	return hasPort && hasBatt
}

func parseDroneportBattery(v interface{}) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case string:
		if x == "" || x == "unknown" {
			return 0, false
		}
		var f float64
		_, err := fmt.Sscanf(x, "%f", &f)
		return f, err == nil
	default:
		return 0, false
	}
}

func (e *Emergency) isDroneReady(ctx context.Context) (bool, float64, error) {
	if e.droneport.orchestratorTopic == "" {
		return false, 0, fmt.Errorf("orchestrator_topic_unset")
	}
	resp, err := e.callDroneport(ctx, e.droneport.orchestratorTopic, droneportActionGetAvailableDrones, map[string]interface{}{})
	if err != nil {
		return false, 0, err
	}
	body := unwrapDroneportBody(resp)
	if body == nil || body["error"] != nil {
		return false, 0, nil
	}
	drones, _ := body["drones"].([]interface{})
	for _, d := range drones {
		dm, ok := d.(map[string]interface{})
		if !ok {
			continue
		}
		id, _ := dm["drone_id"].(string)
		if id != e.droneport.droneID {
			continue
		}
		batt, ok := parseDroneportBattery(dm["battery"])
		if ok && batt > e.droneport.minBatteryTakeoff {
			return true, batt, nil
		}
	}
	return false, 0, nil
}

func (e *Emergency) runLanding(ctx context.Context, battery float64) (bool, string, map[string]interface{}) {
	if e.droneport.topic == "" {
		return true, "", nil
	}
	payload := map[string]interface{}{
		"drone_id": e.droneport.droneID,
		"model":    e.droneport.droneModel,
		"battery":  battery,
	}
	resp, err := e.callDroneport(ctx, e.droneport.topic, droneportActionRequestLanding, payload)
	if err != nil {
		return false, err.Error(), map[string]interface{}{"error": err.Error()}
	}
	if !droneportLandingOK(resp) {
		body := unwrapDroneportBody(resp)
		errMsg := "landing_denied"
		if body != nil {
			if em, ok := body["error"].(string); ok && em != "" {
				errMsg = em
			}
		}
		return false, errMsg, resp
	}
	body := unwrapDroneportBody(resp)
	portID := ""
	if body != nil {
		portID, _ = body["port_id"].(string)
	}
	e.dpMu.Lock()
	e.droneportPortID = portID
	e.droneportPhase = DroneportPhaseCharging
	e.dpMu.Unlock()
	return true, "", resp
}

// checkChargeReady polls orchestrator once; returns pending if still charging within timeout.
func (e *Emergency) checkChargeReady(ctx context.Context) (ready bool, timedOut bool, err error) {
	e.dpMu.Lock()
	started := e.droneportChargeWaitAt
	e.dpMu.Unlock()
	if started.IsZero() {
		e.dpMu.Lock()
		e.droneportChargeWaitAt = time.Now()
		e.dpMu.Unlock()
		started = time.Now()
	}
	deadline := started.Add(time.Duration(e.droneport.chargeTimeoutSec) * time.Second)
	ready, _, err = e.isDroneReady(ctx)
	if err != nil {
		return false, false, err
	}
	if ready {
		e.dpMu.Lock()
		e.droneportPhase = DroneportPhaseReady
		e.droneportChargeWaitAt = time.Time{}
		e.dpMu.Unlock()
		return true, false, nil
	}
	if time.Now().After(deadline) {
		e.dpMu.Lock()
		e.droneportChargeWaitAt = time.Time{}
		e.dpMu.Unlock()
		return false, true, nil
	}
	return false, false, nil
}

func (e *Emergency) runTakeoff(ctx context.Context, missionID string) (bool, string, map[string]interface{}) {
	if e.droneport.topic == "" {
		return true, "", nil
	}
	payload := map[string]interface{}{
		"drone_id": e.droneport.droneID,
	}
	if missionID != "" {
		payload["mission_id"] = missionID
	}
	resp, err := e.callDroneport(ctx, e.droneport.topic, droneportActionRequestTakeoff, payload)
	if err != nil {
		return false, err.Error(), map[string]interface{}{"error": err.Error()}
	}
	if !droneportTakeoffOK(resp) {
		body := unwrapDroneportBody(resp)
		errMsg := "takeoff_denied"
		if body != nil {
			if em, ok := body["error"].(string); ok && em != "" {
				errMsg = em
			}
		}
		return false, errMsg, resp
	}
	body := unwrapDroneportBody(resp)
	e.dpMu.Lock()
	if body != nil {
		if pid, ok := body["port_id"].(string); ok && pid != "" {
			e.droneportPortID = pid
		}
	}
	e.droneportPhase = DroneportPhaseDeparted
	e.droneportLastError = ""
	e.dpMu.Unlock()
	return true, "", resp
}

func (e *Emergency) runPreflight(ctx context.Context, missionID string, landingBattery *float64) (preflightResult, map[string]interface{}) {
	if e.droneport.topic == "" && e.droneport.orchestratorTopic == "" {
		e.setDroneportPhase(DroneportPhaseDisabled)
		return preflightOK, nil
	}
	if e.droneport.mockSuccess {
		e.logToJournal(ctx, "DRONEPORT_TAKEOFF_APPROVED", map[string]interface{}{
			"mission_id": missionID,
			"stub":       true,
			"reason":     "EMERGENCY_DRONEPORT_MOCK_SUCCESS",
		})
		e.setDroneportPhase(DroneportPhaseDeparted)
		return preflightOK, nil
	}

	e.dpMu.Lock()
	if e.droneportPhase == DroneportPhaseDeparted && e.droneportLastMissionID == missionID {
		e.dpMu.Unlock()
		return preflightOK, map[string]interface{}{"already_departed": true}
	}
	if e.droneportPhase == DroneportPhaseDeparted {
		e.dpMu.Unlock()
		e.setDroneportError("already_departed")
		return preflightDenied, map[string]interface{}{"error": "already_departed"}
	}
	e.droneportLastMissionID = missionID
	phase := e.droneportPhase
	e.dpMu.Unlock()

	ready, _, err := e.isDroneReady(ctx)
	if err != nil {
		e.setDroneportError(err.Error())
		e.logToJournal(ctx, "DRONEPORT_TAKEOFF_DENIED", map[string]interface{}{
			"mission_id": missionID,
			"error":      err.Error(),
		})
		return preflightDenied, map[string]interface{}{"error": err.Error()}
	}

	if !ready && phase != DroneportPhaseCharging {
		batt := e.droneport.landingBattery
		if landingBattery != nil {
			batt = *landingBattery
		}
		e.setDroneportPhase(DroneportPhaseNotRegistered)
		ok, errMsg, resp := e.runLanding(ctx, batt)
		if !ok {
			e.setDroneportError(errMsg)
			e.logToJournal(ctx, "DRONEPORT_LANDING_DENIED", map[string]interface{}{
				"mission_id": missionID,
				"error":      errMsg,
				"response":   resp,
			})
			return preflightDenied, map[string]interface{}{"error": errMsg, "response": resp}
		}
		e.logToJournal(ctx, "DRONEPORT_LANDING_APPROVED", map[string]interface{}{
			"mission_id": missionID,
			"battery":    batt,
		})
		e.setDroneportPhase(DroneportPhaseCharging)
		e.logToJournal(ctx, "DRONEPORT_CHARGING_WAIT", map[string]interface{}{"mission_id": missionID})
		ready = false
	}

	if !ready {
		readyNow, timedOut, waitErr := e.checkChargeReady(ctx)
		if waitErr != nil {
			e.setDroneportError(waitErr.Error())
			e.logToJournal(ctx, "DRONEPORT_TAKEOFF_DENIED", map[string]interface{}{
				"mission_id": missionID,
				"error":      waitErr.Error(),
			})
			return preflightDenied, map[string]interface{}{"error": waitErr.Error()}
		}
		if timedOut {
			e.setDroneportError("charge_timeout")
			e.logToJournal(ctx, "DRONEPORT_CHARGE_TIMEOUT", map[string]interface{}{"mission_id": missionID})
			return preflightDenied, map[string]interface{}{"error": "charge_timeout"}
		}
		if !readyNow {
			return preflightPending, map[string]interface{}{"pending": true}
		}
	} else {
		e.setDroneportPhase(DroneportPhaseReady)
	}

	ok, errMsg, resp := e.runTakeoff(ctx, missionID)
	if !ok {
		e.setDroneportError(errMsg)
		e.logToJournal(ctx, "DRONEPORT_TAKEOFF_DENIED", map[string]interface{}{
			"mission_id": missionID,
			"error":      errMsg,
			"response":   resp,
		})
		return preflightDenied, map[string]interface{}{"error": errMsg, "response": resp}
	}
	e.logToJournal(ctx, "DRONEPORT_TAKEOFF_APPROVED", map[string]interface{}{"mission_id": missionID})
	return preflightOK, resp
}

func (e *Emergency) runPostMissionLand(ctx context.Context, missionID string, landingBattery *float64) (bool, map[string]interface{}) {
	if e.droneport.topic == "" {
		e.setDroneportPhase(DroneportPhaseDisabled)
		return true, nil
	}
	if e.droneport.mockSuccess {
		e.dpMu.Lock()
		e.droneportPhase = DroneportPhaseCharging
		e.droneportChargeWaitAt = time.Time{}
		e.dpMu.Unlock()
		e.logToJournal(ctx, "DRONEPORT_LAND_COMPLETE", map[string]interface{}{
			"mission_id": missionID,
			"stub":       true,
		})
		return true, nil
	}
	batt := e.droneport.landingBattery
	if landingBattery != nil {
		batt = *landingBattery
	}
	ok, errMsg, resp := e.runLanding(ctx, batt)
	if !ok {
		e.setDroneportError(errMsg)
		e.logToJournal(ctx, "DRONEPORT_LANDING_DENIED", map[string]interface{}{
			"mission_id": missionID,
			"error":      errMsg,
			"response":   resp,
		})
		return false, map[string]interface{}{"error": errMsg, "response": resp}
	}
	e.dpMu.Lock()
	e.droneportChargeWaitAt = time.Time{}
	e.dpMu.Unlock()
	e.logToJournal(ctx, "DRONEPORT_LAND_COMPLETE", map[string]interface{}{
		"mission_id": missionID,
		"battery":    batt,
	})
	return true, resp
}

func (e *Emergency) droneportStateSnapshot() map[string]interface{} {
	e.dpMu.Lock()
	defer e.dpMu.Unlock()
	return map[string]interface{}{
		"droneport_phase":      e.droneportPhase,
		"droneport_port_id":    e.droneportPortID,
		"droneport_last_error": e.droneportLastError,
	}
}
