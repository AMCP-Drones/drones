package securitymonitor

import (
	"context"
	"log"
	"strings"
)

func (sm *SecurityMonitor) loadEmergencyPolicies() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	cfg := sm.cfg
	sm.policies = map[PolicyKey]struct{}{
		{Sender: "emergency", Topic: cfg.BrokerTopicFor("navigation"), Action: "get_state"}:              {},
		{Sender: "emergency", Topic: cfg.BrokerTopicFor("motors"), Action: "LAND"}:                       {},
		{Sender: "emergency", Topic: cfg.BrokerTopicFor("cargo"), Action: "CLOSE"}:                       {},
		{Sender: "emergency", Topic: cfg.BrokerTopicFor("journal"), Action: "LOG_EVENT"}:                 {},
		{Sender: "emergency", Topic: cfg.BrokerTopicFor("security_monitor"), Action: "isolation_status"}: {},
	}
	sm.mode = "ISOLATED"
}

func (sm *SecurityMonitor) handleIsolationStart(ctx context.Context, message map[string]interface{}) (map[string]interface{}, error) {
	sender, _ := message["sender"].(string)
	if sender == "" {
		return map[string]interface{}{"activated": false, "error": "forbidden"}, nil
	}
	if !strings.HasPrefix(sender, "emergency") && !sm.canManagePolicies(sender) {
		return map[string]interface{}{"activated": false, "error": "forbidden"}, nil
	}
	sm.loadEmergencyPolicies()
	sm.logIsolationActivated(ctx)
	return map[string]interface{}{"activated": true, "mode": sm.mode}, nil
}

func (sm *SecurityMonitor) logIsolationActivated(ctx context.Context) {
	msg := map[string]interface{}{
		"action": "LOG_EVENT",
		"sender": sm.ComponentID,
		"payload": map[string]interface{}{
			"event":   "SECURITY_MONITOR_ISOLATION_ACTIVATED",
			"source":  "security_monitor",
			"details": map[string]interface{}{"mode": sm.mode},
		},
	}
	if err := sm.Bus.Publish(ctx, sm.journalTopic, msg); err != nil {
		log.Printf("[%s] failed to log isolation: %v", sm.ComponentID, err)
	}
}

func (sm *SecurityMonitor) handleIsolationStatus(_ context.Context, _ map[string]interface{}) (map[string]interface{}, error) {
	sm.mu.RLock()
	mode := sm.mode
	sm.mu.RUnlock()
	return map[string]interface{}{"mode": mode}, nil
}
