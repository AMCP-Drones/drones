package component

import (
	"context"
	"fmt"

	"github.com/AMCP-Drones/drones/systems/deliverydron/bus/src"
)

// ProxyClient encapsulates proxy_request/proxy_publish calls via security_monitor.
type ProxyClient struct {
	Bus                  bus.Bus
	SenderID             string
	SecurityMonitorTopic string
	TimeoutSec           float64
}

// ProxyRequest executes a security_monitor proxy_request and returns target payload.
func (p *ProxyClient) ProxyRequest(ctx context.Context, targetTopic, targetAction string, data map[string]interface{}) (map[string]interface{}, error) {
	if data == nil {
		data = map[string]interface{}{}
	}
	msg := map[string]interface{}{
		"action": "proxy_request",
		"sender": p.SenderID,
		"payload": map[string]interface{}{
			"target": map[string]interface{}{"topic": targetTopic, "action": targetAction},
			"data":   data,
		},
	}
	resp, err := p.Bus.Request(ctx, p.SecurityMonitorTopic, msg, p.TimeoutSec)
	if err != nil {
		return nil, err
	}
	payload, _ := resp["payload"].(map[string]interface{})
	if payload == nil {
		return nil, fmt.Errorf("proxy response payload missing")
	}
	if errText, _ := payload["error"].(string); errText != "" {
		return nil, fmt.Errorf(errText)
	}
	targetResp, _ := payload["target_response"].(map[string]interface{})
	if targetResp == nil {
		return nil, fmt.Errorf("proxy target_response missing")
	}
	targetPayload, _ := targetResp["payload"].(map[string]interface{})
	return targetPayload, nil
}

// ProxyPublishAsync sends proxy_publish without waiting for acknowledgement.
func (p *ProxyClient) ProxyPublishAsync(ctx context.Context, targetTopic, targetAction string, data map[string]interface{}) error {
	if data == nil {
		data = map[string]interface{}{}
	}
	msg := map[string]interface{}{
		"action": "proxy_publish",
		"sender": p.SenderID,
		"payload": map[string]interface{}{
			"target": map[string]interface{}{"topic": targetTopic, "action": targetAction},
			"data":   data,
		},
	}
	return p.Bus.Publish(ctx, p.SecurityMonitorTopic, msg)
}
