//go:build sitl
// +build sitl

package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/AMCP-Drones/drones/systems/deliverydron/bus/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/config/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/motors/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/navigation/src"
	securitymonitor "github.com/AMCP-Drones/drones/systems/deliverydron/security_monitor/src"
)

const sitlTestSender = "delivery_drone_sitl_contract_test"

type sitlBrokerTest struct {
	ctx      context.Context
	probeBus bus.Bus
	prefix   string
}

func newSITLBrokerTest(t *testing.T, name string) *sitlBrokerTest {
	t.Helper()
	prefix := "tests.sitl." + strings.ToLower(name) + fmt.Sprintf(".%d", time.Now().UnixNano())
	probe := newMQTTBus(t, "probe_"+name)
	ctx := context.Background()
	if err := probe.Start(ctx); err != nil {
		t.Fatalf("start probe mqtt bus: %v", err)
	}
	t.Cleanup(func() { _ = probe.Stop(ctx) })
	return &sitlBrokerTest{ctx: ctx, probeBus: probe, prefix: prefix}
}

func newMQTTBus(t *testing.T, id string) bus.Bus {
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

func componentConfig(id, topic string) *config.Config {
	return &config.Config{
		BrokerType:     "mqtt",
		ComponentID:    id,
		ComponentTopic: topic,
		SystemName:     "sitl_contract_tests",
		TopicVersion:   "v1",
		InstanceID:     "T001",
		MQTTBroker:     "localhost",
		MQTTPort:       1883,
		MQTTQoS:        1,
		BrokerUser:     "admin",
		BrokerPassword: "admin_secret_123",
	}
}

func (s *sitlBrokerTest) topic(component string) string {
	return s.prefix + "." + component
}

func (s *sitlBrokerTest) startMotors(t *testing.T, sitlCommandsTopic string) string {
	t.Helper()
	t.Setenv("SITL_COMMANDS_TOPIC", sitlCommandsTopic)
	componentTopic := s.topic("motors")
	componentBus := newMQTTBus(t, "motors_"+fmt.Sprint(time.Now().UnixNano()))
	m := motors.New(componentConfig("motors", componentTopic), componentBus)
	if err := m.Start(s.ctx); err != nil {
		t.Fatalf("start motors: %v", err)
	}
	t.Cleanup(func() { _ = m.Stop(s.ctx) })
	time.Sleep(150 * time.Millisecond)
	return componentTopic
}

func (s *sitlBrokerTest) startNavigation(t *testing.T) string {
	t.Helper()
	componentTopic := s.topic("navigation")
	componentBus := newMQTTBus(t, "navigation_"+fmt.Sprint(time.Now().UnixNano()))
	nav := navigation.New(componentConfig("navigation", componentTopic), componentBus)
	if err := nav.Start(s.ctx); err != nil {
		t.Fatalf("start navigation: %v", err)
	}
	t.Cleanup(func() { _ = nav.Stop(s.ctx) })
	time.Sleep(150 * time.Millisecond)
	return componentTopic
}

func (s *sitlBrokerTest) startSecurityMonitor(t *testing.T, policies []map[string]string) string {
	t.Helper()
	raw, err := json.Marshal(policies)
	if err != nil {
		t.Fatalf("marshal policies: %v", err)
	}
	t.Setenv("SECURITY_POLICIES", string(raw))
	componentTopic := s.topic("security_monitor")
	componentBus := newMQTTBus(t, "security_monitor_"+fmt.Sprint(time.Now().UnixNano()))
	sm := securitymonitor.New(componentConfig("security_monitor", componentTopic), componentBus)
	if err := sm.Start(s.ctx); err != nil {
		t.Fatalf("start security_monitor: %v", err)
	}
	t.Cleanup(func() { _ = sm.Stop(s.ctx) })
	time.Sleep(150 * time.Millisecond)
	return componentTopic
}

func (s *sitlBrokerTest) subscribe(t *testing.T, topic string) <-chan map[string]interface{} {
	t.Helper()
	ch := make(chan map[string]interface{}, 8)
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

func (s *sitlBrokerTest) requestPayload(t *testing.T, topic string, action string, sender string, payload map[string]interface{}) map[string]interface{} {
	t.Helper()
	resp, err := s.probeBus.Request(s.ctx, topic, map[string]interface{}{
		"action":  action,
		"sender":  sender,
		"payload": payload,
	}, 3.0)
	if err != nil {
		t.Fatalf("request %s %s failed: %v", topic, action, err)
	}
	if success, _ := resp["success"].(bool); !success {
		t.Fatalf("request %s %s returned unsuccessful response: %#v", topic, action, resp)
	}
	pl, _ := resp["payload"].(map[string]interface{})
	if pl == nil {
		t.Fatalf("response payload missing: %#v", resp)
	}
	return pl
}

func waitBrokerMessage(t *testing.T, ch <-chan map[string]interface{}, timeout time.Duration) map[string]interface{} {
	t.Helper()
	select {
	case msg := <-ch:
		return msg
	case <-time.After(timeout):
		t.Fatalf("expected broker message, got none")
		return nil
	}
}

func assertNoBrokerMessage(t *testing.T, ch <-chan map[string]interface{}) {
	t.Helper()
	select {
	case msg := <-ch:
		t.Fatalf("unexpected broker message: %#v", msg)
	case <-time.After(350 * time.Millisecond):
	}
}

func assertSITLCommandSchema(t *testing.T, msg map[string]interface{}, want map[string]float64) {
	t.Helper()
	var problems []string
	if _, hasCommand := msg["command"]; hasCommand {
		problems = append(problems, "has nested command object")
	}
	if _, hasSource := msg["source"]; hasSource {
		problems = append(problems, "has extra source field")
	}
	if droneID, ok := msg["drone_id"].(string); !ok || droneID == "" {
		problems = append(problems, "missing drone_id")
	}
	for field, expected := range want {
		actual, ok := asFloat(msg[field])
		if !ok {
			problems = append(problems, "missing numeric "+field)
			continue
		}
		if math.Abs(actual-expected) > 1e-9 {
			problems = append(problems, fmt.Sprintf("%s=%v want %v", field, actual, expected))
		}
	}
	if len(problems) > 0 {
		t.Fatalf("message does not match SITL command schema: %v; message=%#v", problems, msg)
	}
}

func asFloat(v interface{}) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

func TestSITL_001_MotorsSetTargetPublishesValidSITLCommand(t *testing.T) {
	s := newSITLBrokerTest(t, "SITL001")
	sitlTopic := s.topic("sitl.commands")
	sitlOut := s.subscribe(t, sitlTopic)
	motorsTopic := s.startMotors(t, sitlTopic)

	pl := s.requestPayload(t, motorsTopic, "SET_TARGET", "security_monitor", map[string]interface{}{
		"drone_id": "drone_001",
		"vx":       5.0, "vy": 3.0, "vz": 1.0,
		"mag_heading": 45.0,
		"heading_deg": 45.0,
	})
	if pl["ok"] != true {
		t.Fatalf("motors rejected valid SET_TARGET: %#v", pl)
	}
	assertSITLCommandSchema(t, waitBrokerMessage(t, sitlOut, 2*time.Second), map[string]float64{
		"vx": 5.0, "vy": 3.0, "vz": 1.0, "mag_heading": 45.0,
	})
}

func TestSITL_002_MotorsLandPublishesValidSITLCommand(t *testing.T) {
	s := newSITLBrokerTest(t, "SITL002")
	sitlTopic := s.topic("sitl.commands")
	sitlOut := s.subscribe(t, sitlTopic)
	motorsTopic := s.startMotors(t, sitlTopic)

	pl := s.requestPayload(t, motorsTopic, "LAND", "security_monitor", map[string]interface{}{"drone_id": "drone_001"})
	if pl["ok"] != true {
		t.Fatalf("motors rejected LAND: %#v", pl)
	}
	assertSITLCommandSchema(t, waitBrokerMessage(t, sitlOut, 2*time.Second), map[string]float64{
		"vx": 0.0, "vy": 0.0, "vz": -0.5, "mag_heading": 0.0,
	})
}

func TestSITL_003_InvalidMotorsCommandIsRejectedAndNotSentToSITL(t *testing.T) {
	s := newSITLBrokerTest(t, "SITL003")
	sitlTopic := s.topic("sitl.commands")
	sitlOut := s.subscribe(t, sitlTopic)
	motorsTopic := s.startMotors(t, sitlTopic)

	pl := s.requestPayload(t, motorsTopic, "SET_TARGET", "security_monitor", map[string]interface{}{
		"vx": "bad", "vy": 0.0, "vz": 0.0,
	})
	if pl["ok"] != false || pl["error"] == "" {
		t.Fatalf("invalid command should be rejected with error, got %#v", pl)
	}
	assertNoBrokerMessage(t, sitlOut)
}

func TestSITL_004_UntrustedSenderCannotSendMotorsCommandToSITL(t *testing.T) {
	s := newSITLBrokerTest(t, "SITL004")
	sitlTopic := s.topic("sitl.commands")
	sitlOut := s.subscribe(t, sitlTopic)
	motorsTopic := s.startMotors(t, sitlTopic)

	if err := s.probeBus.Publish(s.ctx, motorsTopic, map[string]interface{}{
		"action": "SET_TARGET",
		"sender": "unknown_sender",
		"payload": map[string]interface{}{
			"vx": 5.0, "vy": 0.0, "vz": 0.0,
		},
	}); err != nil {
		t.Fatalf("publish untrusted command: %v", err)
	}
	assertNoBrokerMessage(t, sitlOut)
}

func TestSITL_016_SITLNavStateUpdatesNavigationThroughSecurityMonitor(t *testing.T) {
	s := newSITLBrokerTest(t, "SITL016")
	navTopic := s.startNavigation(t)
	secTopic := s.startSecurityMonitor(t, []map[string]string{
		{"sender": "sitl_adapter", "topic": navTopic, "action": "nav_state"},
		{"sender": "limiter", "topic": navTopic, "action": "get_state"},
	})

	publishPl := s.requestPayload(t, secTopic, "proxy_publish", "sitl_adapter", map[string]interface{}{
		"target": map[string]interface{}{"topic": navTopic, "action": "nav_state"},
		"data": map[string]interface{}{
			"lat": 55.7558, "lon": 37.6173, "alt_m": 120.0,
			"heading_deg": 90.0, "ground_speed_mps": 8.0,
		},
	})
	if publishPl["published"] != true {
		t.Fatalf("security_monitor did not publish nav_state: %#v", publishPl)
	}

	state := s.requestPayload(t, navTopic, "get_state", "security_monitor", map[string]interface{}{})
	if lat, _ := asFloat(state["lat"]); lat != 55.7558 {
		t.Fatalf("navigation was not updated from SITL nav_state: %#v", state)
	}
	if alt, _ := asFloat(state["alt_m"]); alt != 120.0 {
		t.Fatalf("navigation alt_m was not updated from SITL nav_state: %#v", state)
	}
}

func TestSITL_017_SecurityMonitorBlocksUnauthorizedSITLNavState(t *testing.T) {
	s := newSITLBrokerTest(t, "SITL017")
	navTopic := s.startNavigation(t)
	secTopic := s.startSecurityMonitor(t, []map[string]string{
		{"sender": "sitl_adapter", "topic": navTopic, "action": "nav_state"},
	})

	pl := s.requestPayload(t, secTopic, "proxy_publish", "unknown_sitl_sender", map[string]interface{}{
		"target": map[string]interface{}{"topic": navTopic, "action": "nav_state"},
		"data":   map[string]interface{}{"lat": 1.0, "lon": 2.0, "alt_m": 3.0},
	})
	if pl["error"] != "forbidden" || pl["published"] == true {
		t.Fatalf("unauthorized SITL nav_state should be forbidden, got %#v", pl)
	}
}

func TestSITL_015_AutopilotCommandThroughSecurityMonitorReachesSITLBoundary(t *testing.T) {
	s := newSITLBrokerTest(t, "SITL015")
	sitlTopic := s.topic("sitl.commands")
	sitlOut := s.subscribe(t, sitlTopic)
	motorsTopic := s.startMotors(t, sitlTopic)
	secTopic := s.startSecurityMonitor(t, []map[string]string{
		{"sender": "autopilot", "topic": motorsTopic, "action": "SET_TARGET"},
	})

	pl := s.requestPayload(t, secTopic, "proxy_publish", "autopilot", map[string]interface{}{
		"target": map[string]interface{}{"topic": motorsTopic, "action": "SET_TARGET"},
		"data": map[string]interface{}{
			"drone_id": "drone_001",
			"vx":       2.0, "vy": 0.0, "vz": 0.0,
			"mag_heading": 10.0,
			"heading_deg": 10.0,
		},
	})
	if pl["published"] != true {
		t.Fatalf("security_monitor did not route autopilot command to motors: %#v", pl)
	}
	assertSITLCommandSchema(t, waitBrokerMessage(t, sitlOut, 2*time.Second), map[string]float64{
		"vx": 2.0, "vy": 0.0, "vz": 0.0, "mag_heading": 10.0,
	})
}
