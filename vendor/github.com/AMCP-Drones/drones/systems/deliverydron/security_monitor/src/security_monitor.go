// Package securitymonitor implements the policy-based gateway: proxy_request, proxy_publish, and isolation.
package securitymonitor

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/AMCP-Drones/drones/systems/deliverydron/bus/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/component/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/config/src"
)

// PolicyKey identifies an allowed (sender, topic, action) triple.
type PolicyKey struct {
	Sender string
	Topic  string
	Action string
}

// SecurityMonitor implements the policy gateway and isolation mode.
type SecurityMonitor struct {
	*component.BaseComponent
	cfg             *config.Config
	mu              sync.RWMutex
	policies        map[PolicyKey]struct{}
	policyAdmin     string
	mode            string // NORMAL | ISOLATED
	systemName      string
	journalTopic    string
	proxyTimeoutSec float64
}

// SecurityMonitorComponentID is the fixed trusted sender ID for security monitor.
const SecurityMonitorComponentID = "security_monitor"

// New creates a SecurityMonitor. Call RegisterHandler for actions, then Start.
func New(cfg *config.Config, b bus.Bus) *SecurityMonitor {
	systemName := cfg.SystemName
	if systemName == "" {
		systemName = "deliverydron"
	}
	topic := cfg.ComponentTopic
	if topic == "" {
		topic = cfg.BrokerTopicFor("security_monitor")
	}
	base := component.NewBaseComponent(SecurityMonitorComponentID, "security_monitor", topic, b)
	topicPrefix := cfg.TopicPrefix()
	rawPolicies := os.Getenv("SECURITY_POLICIES")
	rawPolicies = strings.ReplaceAll(rawPolicies, "${TOPIC_PREFIX}", topicPrefix)
	rawPolicies = strings.ReplaceAll(rawPolicies, "${SYSTEM_NAME}", topicPrefix)
	rawPolicies = strings.ReplaceAll(rawPolicies, "$${SYSTEM_NAME}", topicPrefix)
	rawPolicies = strings.ReplaceAll(rawPolicies, "$SYSTEM_NAME", topicPrefix)
	policyAdmin := strings.TrimSpace(os.Getenv("POLICY_ADMIN_SENDER"))
	timeout := 10.0
	if t := os.Getenv("SECURITY_MONITOR_PROXY_REQUEST_TIMEOUT_S"); t != "" {
		if v, err := strconv.ParseFloat(strings.TrimSpace(t), 64); err == nil && v >= 0.1 {
			timeout = v
		}
	}
	sm := &SecurityMonitor{
		BaseComponent:   base,
		cfg:             cfg,
		policies:        parsePolicies(rawPolicies),
		policyAdmin:     policyAdmin,
		mode:            "NORMAL",
		systemName:      systemName,
		journalTopic:    cfg.BrokerTopicFor("journal"),
		proxyTimeoutSec: timeout,
	}
	sm.registerHandlers()
	return sm
}

func parsePolicies(raw string) map[PolicyKey]struct{} {
	out := make(map[PolicyKey]struct{})
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return out
	}
	// Try JSON array first
	var list []interface{}
	if err := json.Unmarshal([]byte(raw), &list); err == nil {
		for _, item := range list {
			switch v := item.(type) {
			case map[string]interface{}:
				s := strings.TrimSpace(getStr(v, "sender"))
				t := strings.TrimSpace(getStr(v, "topic"))
				a := strings.TrimSpace(getStr(v, "action"))
				if s != "" && t != "" && a != "" {
					out[PolicyKey{Sender: s, Topic: t, Action: a}] = struct{}{}
				}
			case []interface{}:
				if len(v) >= 3 {
					s := strings.TrimSpace(str(v[0]))
					t := strings.TrimSpace(str(v[1]))
					a := strings.TrimSpace(str(v[2]))
					if s != "" && t != "" && a != "" {
						out[PolicyKey{Sender: s, Topic: t, Action: a}] = struct{}{}
					}
				}
			}
		}
		return out
	}
	// Semicolon-separated "sender,topic,action"
	for _, chunk := range strings.Split(raw, ";") {
		parts := strings.Split(chunk, ",")
		if len(parts) != 3 {
			continue
		}
		s := strings.TrimSpace(parts[0])
		t := strings.TrimSpace(parts[1])
		a := strings.TrimSpace(parts[2])
		if s != "" && t != "" && a != "" {
			out[PolicyKey{Sender: s, Topic: t, Action: a}] = struct{}{}
		}
	}
	return out
}

func getStr(m map[string]interface{}, k string) string {
	if v, ok := m[k]; ok {
		return str(v)
	}
	return ""
}

func str(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func (sm *SecurityMonitor) registerHandlers() {
	sm.RegisterHandler("proxy_request", sm.handleProxyRequest)
	sm.RegisterHandler("proxy_publish", sm.handleProxyPublish)
	sm.RegisterHandler("set_policy", sm.handleSetPolicy)
	sm.RegisterHandler("remove_policy", sm.handleRemovePolicy)
	sm.RegisterHandler("clear_policies", sm.handleClearPolicies)
	sm.RegisterHandler("list_policies", sm.handleListPolicies)
	sm.RegisterHandler("ISOLATION_START", sm.handleIsolationStart)
	sm.RegisterHandler("isolation_status", sm.handleIsolationStatus)
}

func (sm *SecurityMonitor) allowed(sender, targetTopic, targetAction string) bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	_, ok := sm.policies[PolicyKey{Sender: sender, Topic: targetTopic, Action: targetAction}]
	return ok
}

func (sm *SecurityMonitor) canManagePolicies(sender string) bool {
	return sm.policyAdmin != "" && sender == sm.policyAdmin
}

func extractTarget(payload map[string]interface{}) (topic, action string, data map[string]interface{}) {
	target, _ := payload["target"].(map[string]interface{})
	if target == nil {
		return "", "", nil
	}
	topic = strings.TrimSpace(getStr(target, "topic"))
	action = strings.TrimSpace(getStr(target, "action"))
	data, _ = payload["data"].(map[string]interface{})
	if data == nil {
		data = make(map[string]interface{})
	}
	return topic, action, data
}

func (sm *SecurityMonitor) handleProxyRequest(ctx context.Context, message map[string]interface{}) (map[string]interface{}, error) {
	sender, _ := message["sender"].(string)
	if sender == "" {
		sender = "unknown"
	}
	payload, _ := message["payload"].(map[string]interface{})
	if payload == nil {
		return nil, nil
	}
	targetTopic, targetAction, targetPayload := extractTarget(payload)
	if targetTopic == "" || targetAction == "" {
		return nil, nil
	}
	if !sm.allowed(sender, targetTopic, targetAction) {
		return nil, nil
	}
	reqMsg := map[string]interface{}{
		"action":  targetAction,
		"sender":  sm.ComponentID,
		"payload": targetPayload,
	}
	resp, err := sm.Bus.Request(ctx, targetTopic, reqMsg, sm.proxyTimeoutSec)
	if err != nil {
		log.Printf("[%s] proxy_request %s %s: %v", sm.ComponentID, targetTopic, targetAction, err)
		return map[string]interface{}{"error": err.Error()}, nil
	}
	return map[string]interface{}{
		"target_topic":    targetTopic,
		"target_action":   targetAction,
		"target_response": resp,
	}, nil
}

func (sm *SecurityMonitor) handleProxyPublish(ctx context.Context, message map[string]interface{}) (map[string]interface{}, error) {
	sender, _ := message["sender"].(string)
	if sender == "" {
		sender = "unknown"
	}
	payload, _ := message["payload"].(map[string]interface{})
	if payload == nil {
		return nil, nil
	}
	targetTopic, targetAction, targetPayload := extractTarget(payload)
	if targetTopic == "" || targetAction == "" {
		return nil, nil
	}
	if !sm.allowed(sender, targetTopic, targetAction) {
		return nil, nil
	}
	msg := map[string]interface{}{
		"action":  targetAction,
		"sender":  sm.ComponentID,
		"payload": targetPayload,
	}
	if err := sm.Bus.Publish(ctx, targetTopic, msg); err != nil {
		log.Printf("[%s] proxy_publish %s %s: %v", sm.ComponentID, targetTopic, targetAction, err)
		return map[string]interface{}{"published": false, "error": err.Error()}, nil
	}
	return map[string]interface{}{"published": true}, nil
}

func (sm *SecurityMonitor) handleSetPolicy(_ context.Context, message map[string]interface{}) (map[string]interface{}, error) {
	sender, _ := message["sender"].(string)
	if !sm.canManagePolicies(sender) {
		return map[string]interface{}{"updated": false, "error": "forbidden"}, nil
	}
	payload, _ := message["payload"].(map[string]interface{})
	if payload == nil {
		return map[string]interface{}{"updated": false, "error": "invalid_policy"}, nil
	}
	s := strings.TrimSpace(getStr(payload, "sender"))
	t := strings.TrimSpace(getStr(payload, "topic"))
	a := strings.TrimSpace(getStr(payload, "action"))
	if s == "" || t == "" || a == "" {
		return map[string]interface{}{"updated": false, "error": "invalid_policy"}, nil
	}
	k := PolicyKey{Sender: s, Topic: t, Action: a}
	sm.mu.Lock()
	sm.policies[k] = struct{}{}
	sm.mu.Unlock()
	return map[string]interface{}{"updated": true, "policy": map[string]string{"sender": s, "topic": t, "action": a}}, nil
}

func (sm *SecurityMonitor) handleRemovePolicy(_ context.Context, message map[string]interface{}) (map[string]interface{}, error) {
	sender, _ := message["sender"].(string)
	if !sm.canManagePolicies(sender) {
		return map[string]interface{}{"removed": false, "error": "forbidden"}, nil
	}
	payload, _ := message["payload"].(map[string]interface{})
	if payload == nil {
		return map[string]interface{}{"removed": false, "error": "invalid_policy"}, nil
	}
	s := strings.TrimSpace(getStr(payload, "sender"))
	t := strings.TrimSpace(getStr(payload, "topic"))
	a := strings.TrimSpace(getStr(payload, "action"))
	if s == "" || t == "" || a == "" {
		return map[string]interface{}{"removed": false, "error": "invalid_policy"}, nil
	}
	k := PolicyKey{Sender: s, Topic: t, Action: a}
	sm.mu.Lock()
	_, existed := sm.policies[k]
	delete(sm.policies, k)
	sm.mu.Unlock()
	return map[string]interface{}{"removed": existed, "policy": map[string]string{"sender": s, "topic": t, "action": a}}, nil
}

func (sm *SecurityMonitor) handleClearPolicies(_ context.Context, message map[string]interface{}) (map[string]interface{}, error) {
	sender, _ := message["sender"].(string)
	if !sm.canManagePolicies(sender) {
		return map[string]interface{}{"cleared": false, "error": "forbidden"}, nil
	}
	sm.mu.Lock()
	n := len(sm.policies)
	sm.policies = make(map[PolicyKey]struct{})
	sm.mu.Unlock()
	return map[string]interface{}{"cleared": true, "removed_count": n}, nil
}

func (sm *SecurityMonitor) handleListPolicies(_ context.Context, _ map[string]interface{}) (map[string]interface{}, error) {
	sm.mu.RLock()
	list := make([]map[string]string, 0, len(sm.policies))
	for k := range sm.policies {
		list = append(list, map[string]string{"sender": k.Sender, "topic": k.Topic, "action": k.Action})
	}
	sm.mu.RUnlock()
	return map[string]interface{}{
		"policy_admin_sender": sm.policyAdmin,
		"count":               len(list),
		"policies":            list,
	}, nil
}

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
