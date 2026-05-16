//go:build security
// +build security

package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/AMCP-Drones/drones/systems/deliverydron/bus/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/config/src"
	motors "github.com/AMCP-Drones/drones/systems/deliverydron/motors/src"
	securitymonitor "github.com/AMCP-Drones/drones/systems/deliverydron/security_monitor/src"
)

type secBrokerTest struct {
	ctx        context.Context
	probeBus   bus.Bus
	systemName string
	instanceID string
}

func newSecBrokerTest(t *testing.T, name string) *secBrokerTest {
	t.Helper()
	ctx := context.Background()
	probe := secNewMQTTBus(t, "sec_probe_"+name)
	if err := probe.Start(ctx); err != nil {
		t.Fatalf("start security probe mqtt bus: %v", err)
	}
	t.Cleanup(func() { _ = probe.Stop(ctx) })

	cleanName := strings.NewReplacer("-", "_", ".", "_").Replace(strings.ToLower(name))
	return &secBrokerTest{
		ctx:        ctx,
		probeBus:   probe,
		systemName: fmt.Sprintf("security_%s_%d", cleanName, time.Now().UnixNano()),
		instanceID: "T001",
	}
}

func secNewMQTTBus(t *testing.T, id string) bus.Bus {
	t.Helper()
	b, err := bus.New(&config.Config{
		BrokerType:     "mqtt",
		ComponentID:    id,
		MQTTBroker:     "localhost",
		MQTTPort:       1883,
		MQTTQoS:        1,
		BrokerUser:     "admin",
		BrokerPassword: "admin_secret_123",
	})
	if err != nil {
		t.Fatalf("create mqtt bus: %v", err)
	}
	return b
}

func (s *secBrokerTest) cfg(componentID string) *config.Config {
	return &config.Config{
		BrokerType:     "mqtt",
		ComponentID:    componentID,
		SystemName:     s.systemName,
		TopicVersion:   "v1",
		InstanceID:     s.instanceID,
		MQTTBroker:     "localhost",
		MQTTPort:       1883,
		MQTTQoS:        1,
		BrokerUser:     "admin",
		BrokerPassword: "admin_secret_123",
	}
}

func (s *secBrokerTest) topic(componentID string) string {
	return s.cfg(componentID).BrokerTopicFor(componentID)
}

func (s *secBrokerTest) subscribe(t *testing.T, topic string) <-chan map[string]interface{} {
	t.Helper()
	ch := make(chan map[string]interface{}, 64)
	if err := s.probeBus.Subscribe(s.ctx, topic, func(msg map[string]interface{}) {
		select {
		case ch <- msg:
		default:
		}
	}); err != nil {
		t.Fatalf("subscribe %s: %v", topic, err)
	}
	t.Cleanup(func() { _ = s.probeBus.Unsubscribe(s.ctx, topic) })
	time.Sleep(100 * time.Millisecond)
	return ch
}

func (s *secBrokerTest) requestPayload(t *testing.T, topic, action, sender string, payload map[string]interface{}) map[string]interface{} {
	t.Helper()
	resp, err := s.probeBus.Request(s.ctx, topic, map[string]interface{}{
		"action":  action,
		"sender":  sender,
		"payload": payload,
	}, 5.0)
	if err != nil {
		t.Fatalf("request %s %s failed: %v", topic, action, err)
	}
	if success, _ := resp["success"].(bool); !success {
		t.Fatalf("request %s %s returned unsuccessful response: %#v", topic, action, resp)
	}
	pl, _ := resp["payload"].(map[string]interface{})
	if pl == nil {
		return map[string]interface{}{}
	}
	return pl
}

func (s *secBrokerTest) startMotors(t *testing.T, sitlCommandsTopic string) string {
	t.Helper()
	t.Setenv("SITL_COMMANDS_TOPIC", sitlCommandsTopic)
	componentBus := secNewMQTTBus(t, "sec_motors_"+fmt.Sprint(time.Now().UnixNano()))
	m := motors.New(s.cfg("motors"), componentBus)
	if err := m.Start(s.ctx); err != nil {
		t.Fatalf("start motors: %v", err)
	}
	t.Cleanup(func() { _ = m.Stop(s.ctx) })
	time.Sleep(150 * time.Millisecond)
	return s.topic("motors")
}

func (s *secBrokerTest) startSecurityMonitor(t *testing.T, policies []map[string]string) string {
	t.Helper()
	raw, err := json.Marshal(policies)
	if err != nil {
		t.Fatalf("marshal policies: %v", err)
	}
	t.Setenv("SECURITY_POLICIES", string(raw))
	componentBus := secNewMQTTBus(t, "sec_security_monitor_"+fmt.Sprint(time.Now().UnixNano()))
	sm := securitymonitor.New(s.cfg("security_monitor"), componentBus)
	if err := sm.Start(s.ctx); err != nil {
		t.Fatalf("start security_monitor: %v", err)
	}
	t.Cleanup(func() { _ = sm.Stop(s.ctx) })
	time.Sleep(150 * time.Millisecond)
	return s.topic("security_monitor")
}

func secDrain(ch <-chan map[string]interface{}) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

func secWaitBrokerMessage(t *testing.T, ch <-chan map[string]interface{}, timeout time.Duration) map[string]interface{} {
	t.Helper()
	select {
	case msg := <-ch:
		return msg
	case <-time.After(timeout):
		t.Fatalf("expected broker message, got none")
		return nil
	}
}

func secAssertNoBrokerMessage(t *testing.T, ch <-chan map[string]interface{}) {
	t.Helper()
	select {
	case msg := <-ch:
		t.Fatalf("unexpected broker message: %#v", msg)
	case <-time.After(350 * time.Millisecond):
	}
}

func TestSEC_004_ORVDCannotLandDroneWithoutSecurityPolicy(t *testing.T) {
	s := newSecBrokerTest(t, "SEC004")
	sitlTopic := s.topic("sitl_commands")
	sitlOut := s.subscribe(t, sitlTopic)
	motorsTopic := s.startMotors(t, sitlTopic)
	secTopic := s.startSecurityMonitor(t, []map[string]string{
		{"sender": "autopilot", "topic": motorsTopic, "action": "SET_TARGET"},
	})

	pl := s.requestPayload(t, secTopic, "proxy_publish", "orvd", map[string]interface{}{
		"target": map[string]interface{}{"topic": motorsTopic, "action": "LAND"},
		"data":   map[string]interface{}{"reason": "orvd_takeover_attempt"},
	})
	if pl["published"] == true || pl["error"] != "forbidden" {
		t.Fatalf("ORVD LAND without policy must be forbidden, got %#v", pl)
	}
	secAssertNoBrokerMessage(t, sitlOut)
}

func TestSEC_005_EmergencyIsolationBlocksExternalCommandsExceptEmergency(t *testing.T) {
	s := newSecBrokerTest(t, "SEC005")
	sitlTopic := s.topic("sitl_commands")
	sitlOut := s.subscribe(t, sitlTopic)
	motorsTopic := s.startMotors(t, sitlTopic)
	secTopic := s.startSecurityMonitor(t, []map[string]string{
		{"sender": "autopilot", "topic": motorsTopic, "action": "SET_TARGET"},
		{"sender": "orvd", "topic": motorsTopic, "action": "LAND"},
	})

	pl := s.requestPayload(t, secTopic, "ISOLATION_START", "emergency", map[string]interface{}{
		"reason": "security_test",
	})
	if pl["activated"] != true || pl["mode"] != "ISOLATED" {
		t.Fatalf("emergency must activate isolated mode, got %#v", pl)
	}
	secDrain(sitlOut)

	blocked := s.requestPayload(t, secTopic, "proxy_publish", "autopilot", map[string]interface{}{
		"target": map[string]interface{}{"topic": motorsTopic, "action": "SET_TARGET"},
		"data": map[string]interface{}{
			"vx": 5.0, "vy": 0.0, "vz": 0.0, "heading_deg": 10.0,
		},
	})
	if blocked["published"] == true || blocked["error"] != "forbidden" {
		t.Fatalf("isolated mode must block autopilot command, got %#v", blocked)
	}
	secAssertNoBrokerMessage(t, sitlOut)

	allowed := s.requestPayload(t, secTopic, "proxy_publish", "emergency", map[string]interface{}{
		"target": map[string]interface{}{"topic": motorsTopic, "action": "LAND"},
		"data":   map[string]interface{}{"reason": "emergency"},
	})
	if allowed["published"] != true {
		t.Fatalf("isolated mode must allow emergency LAND, got %#v", allowed)
	}
	secWaitBrokerMessage(t, sitlOut, 2*time.Second)
}
