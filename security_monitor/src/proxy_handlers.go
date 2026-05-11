package securitymonitor

import (
	"context"
	"log"
	"strings"
)

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
	sm.recordSenderSeen(sender)
	payload, _ := message["payload"].(map[string]interface{})
	if payload == nil {
		return map[string]interface{}{"error": "invalid_payload"}, nil
	}
	targetTopic, targetAction, targetPayload := extractTarget(payload)
	if targetTopic == "" || targetAction == "" {
		return map[string]interface{}{"error": "invalid_target"}, nil
	}
	if !sm.allowed(sender, targetTopic, targetAction) {
		return map[string]interface{}{"error": "forbidden"}, nil
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
	sm.recordSenderSeen(sender)
	payload, _ := message["payload"].(map[string]interface{})
	if payload == nil {
		return map[string]interface{}{"published": false, "error": "invalid_payload"}, nil
	}
	targetTopic, targetAction, targetPayload := extractTarget(payload)
	if targetTopic == "" || targetAction == "" {
		return map[string]interface{}{"published": false, "error": "invalid_target"}, nil
	}
	if !sm.allowed(sender, targetTopic, targetAction) {
		return map[string]interface{}{"published": false, "error": "forbidden"}, nil
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
