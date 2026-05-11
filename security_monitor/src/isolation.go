package securitymonitor

import (
	"context"
	"fmt"
	"log"
	"time"
)

func (sm *SecurityMonitor) emergencyPolicies() map[PolicyKey]struct{} {
	cfg := sm.cfg
	return map[PolicyKey]struct{}{
		{Sender: "emergency", Topic: cfg.BrokerTopicFor("navigation"), Action: "get_state"}:              {},
		{Sender: "emergency", Topic: cfg.BrokerTopicFor("motors"), Action: "LAND"}:                       {},
		{Sender: "emergency", Topic: cfg.BrokerTopicFor("cargo"), Action: "CLOSE"}:                       {},
		{Sender: "emergency", Topic: cfg.BrokerTopicFor("journal"), Action: "LOG_EVENT"}:                 {},
		{Sender: "emergency", Topic: cfg.BrokerTopicFor("security_monitor"), Action: "isolation_status"}: {},
		{Sender: "emergency", Topic: cfg.BrokerTopicFor("security_monitor"), Action: "ISOLATION_END"}:    {},
	}
}

func (sm *SecurityMonitor) enterIsolatedLocked(reason, source string) bool {
	if sm.safetyState.Mode == ModeIsolated {
		return false
	}
	now := time.Now().UTC()
	sm.policies = sm.emergencyPolicies()
	sm.safetyState.Mode = ModeIsolated
	sm.safetyState.Reason = reason
	sm.safetyState.Source = source
	sm.safetyState.Version++
	sm.safetyState.TransitionID = fmt.Sprintf("safety-%d", now.UnixNano())
	sm.safetyState.ActivatedAt = now
	sm.safetyState.LastTransitionAt = now
	return true
}

func (sm *SecurityMonitor) enterNormalLocked(reason, source string) bool {
	if sm.safetyState.Mode == ModeNormal {
		return false
	}
	now := time.Now().UTC()
	sm.policies = clonePolicyMap(sm.normalPolicies)
	sm.safetyState.Mode = ModeNormal
	sm.safetyState.Reason = reason
	sm.safetyState.Source = source
	sm.safetyState.Version++
	sm.safetyState.TransitionID = fmt.Sprintf("safety-%d", now.UnixNano())
	sm.safetyState.LastTransitionAt = now
	return true
}

func (sm *SecurityMonitor) logModeTransitionLocked(ctx context.Context) {
	mode := sm.safetyState.Mode
	reason := sm.safetyState.Reason
	source := sm.safetyState.Source
	version := sm.safetyState.Version
	transitionID := sm.safetyState.TransitionID
	transitionAt := sm.safetyState.LastTransitionAt.Format(time.RFC3339Nano)
	msg := map[string]interface{}{
		"action": "LOG_EVENT",
		"sender": sm.ComponentID,
		"payload": map[string]interface{}{
			"event":   "SECURITY_MONITOR_MODE_TRANSITION",
			"source":  "security_monitor",
			"details": map[string]interface{}{
				"mode":           mode,
				"reason":         reason,
				"transition_id":  transitionID,
				"transition_at":  transitionAt,
				"state_version":  version,
				"trigger_source": source,
			},
		},
	}
	if err := sm.Bus.Publish(ctx, sm.journalTopic, msg); err != nil {
		log.Printf("[%s] failed to log isolation: %v", sm.ComponentID, err)
	}
}

func (sm *SecurityMonitor) handleIsolationStart(ctx context.Context, message map[string]interface{}) (map[string]interface{}, error) {
	sender, _ := message["sender"].(string)
	if sender == "" {
		return map[string]interface{}{"activated": false, "error": "forbidden"}, nil
	}
	if sender != "emergency" && !sm.canManagePolicies(sender) {
		return map[string]interface{}{"activated": false, "error": "forbidden"}, nil
	}
	reason := "manual_isolation"
	payload, _ := message["payload"].(map[string]interface{})
	if payload != nil {
		if r, ok := payload["reason"].(string); ok && r != "" {
			reason = r
		}
	}
	sm.mu.Lock()
	changed := sm.enterIsolatedLocked(reason, sender)
	state := sm.safetyState
	if changed {
		sm.logModeTransitionLocked(ctx)
	}
	sm.mu.Unlock()
	return map[string]interface{}{
		"activated":      true,
		"already_active": !changed,
		"mode":           state.Mode,
		"reason":         state.Reason,
		"transition_id":  state.TransitionID,
		"state_version":  state.Version,
	}, nil
}

func (sm *SecurityMonitor) handleIsolationEnd(ctx context.Context, message map[string]interface{}) (map[string]interface{}, error) {
	sender, _ := message["sender"].(string)
	if sender == "" {
		return map[string]interface{}{"deactivated": false, "error": "forbidden"}, nil
	}
	if sender != "emergency" && !sm.canManagePolicies(sender) {
		return map[string]interface{}{"deactivated": false, "error": "forbidden"}, nil
	}
	reason := "manual_recovery"
	payload, _ := message["payload"].(map[string]interface{})
	if payload != nil {
		if r, ok := payload["reason"].(string); ok && r != "" {
			reason = r
		}
	}
	sm.mu.Lock()
	changed := sm.enterNormalLocked(reason, sender)
	state := sm.safetyState
	restoredPolicies := len(sm.normalPolicies)
	if changed {
		sm.logModeTransitionLocked(ctx)
	}
	sm.mu.Unlock()
	return map[string]interface{}{
		"deactivated":      true,
		"already_normal":   !changed,
		"mode":             state.Mode,
		"reason":           state.Reason,
		"transition_id":    state.TransitionID,
		"state_version":    state.Version,
		"restored_policies": restoredPolicies,
	}, nil
}

func (sm *SecurityMonitor) handleSafetyHeartbeat(_ context.Context, message map[string]interface{}) (map[string]interface{}, error) {
	sender, _ := message["sender"].(string)
	if sender != "limiter" && sender != "emergency" {
		return map[string]interface{}{"ok": false, "error": "forbidden"}, nil
	}
	sm.recordSenderSeen(sender)
	return map[string]interface{}{"ok": true, "sender": sender}, nil
}

func (sm *SecurityMonitor) handleIsolationStatus(_ context.Context, _ map[string]interface{}) (map[string]interface{}, error) {
	sm.mu.RLock()
	state := sm.safetyState
	sm.mu.RUnlock()
	return map[string]interface{}{
		"mode":              state.Mode,
		"reason":            state.Reason,
		"source":            state.Source,
		"transition_id":     state.TransitionID,
		"state_version":     state.Version,
		"activated_at":      state.ActivatedAt.Format(time.RFC3339Nano),
		"last_transition_at": state.LastTransitionAt.Format(time.RFC3339Nano),
	}, nil
}
