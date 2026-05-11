package securitymonitor

import (
	"context"
	"encoding/json"
	"strings"
)

func parsePolicies(raw string) map[PolicyKey]struct{} {
	out := make(map[PolicyKey]struct{})
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return out
	}
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

func clonePolicyMap(src map[PolicyKey]struct{}) map[PolicyKey]struct{} {
	dst := make(map[PolicyKey]struct{}, len(src))
	for k := range src {
		dst[k] = struct{}{}
	}
	return dst
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

func (sm *SecurityMonitor) allowed(sender, targetTopic, targetAction string) bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	_, ok := sm.policies[PolicyKey{Sender: sender, Topic: targetTopic, Action: targetAction}]
	return ok
}

func (sm *SecurityMonitor) canManagePolicies(sender string) bool {
	return sm.policyAdmin != "" && sender == sm.policyAdmin
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
	sm.normalPolicies[k] = struct{}{}
	if sm.safetyState.Mode == ModeNormal {
		sm.policies[k] = struct{}{}
	}
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
	_, existed := sm.normalPolicies[k]
	delete(sm.normalPolicies, k)
	if sm.safetyState.Mode == ModeNormal {
		delete(sm.policies, k)
	}
	sm.mu.Unlock()
	return map[string]interface{}{"removed": existed, "policy": map[string]string{"sender": s, "topic": t, "action": a}}, nil
}

func (sm *SecurityMonitor) handleClearPolicies(_ context.Context, message map[string]interface{}) (map[string]interface{}, error) {
	sender, _ := message["sender"].(string)
	if !sm.canManagePolicies(sender) {
		return map[string]interface{}{"cleared": false, "error": "forbidden"}, nil
	}
	sm.mu.Lock()
	n := len(sm.normalPolicies)
	sm.normalPolicies = make(map[PolicyKey]struct{})
	if sm.safetyState.Mode == ModeNormal {
		sm.policies = make(map[PolicyKey]struct{})
	}
	sm.mu.Unlock()
	return map[string]interface{}{"cleared": true, "removed_count": n}, nil
}

func (sm *SecurityMonitor) handleListPolicies(_ context.Context, _ map[string]interface{}) (map[string]interface{}, error) {
	sm.mu.RLock()
	sourcePolicies := sm.normalPolicies
	if sm.safetyState.Mode == ModeIsolated {
		sourcePolicies = sm.policies
	}
	list := make([]map[string]string, 0, len(sourcePolicies))
	for k := range sourcePolicies {
		list = append(list, map[string]string{"sender": k.Sender, "topic": k.Topic, "action": k.Action})
	}
	mode := sm.safetyState.Mode
	sm.mu.RUnlock()
	return map[string]interface{}{
		"policy_admin_sender": sm.policyAdmin,
		"mode":                mode,
		"count":               len(list),
		"policies":            list,
	}, nil
}
