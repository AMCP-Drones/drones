//go:build security
// +build security

package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AMCP-Drones/drones/systems/deliverydron/bus/src"
	cargo "github.com/AMCP-Drones/drones/systems/deliverydron/cargo/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/config/src"
	journal "github.com/AMCP-Drones/drones/systems/deliverydron/journal/src"
	motors "github.com/AMCP-Drones/drones/systems/deliverydron/motors/src"
	navigation "github.com/AMCP-Drones/drones/systems/deliverydron/navigation/src"
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

func (s *secBrokerTest) startNavigation(t *testing.T) string {
	t.Helper()
	componentBus := secNewMQTTBus(t, "sec_navigation_"+fmt.Sprint(time.Now().UnixNano()))
	n := navigation.New(s.cfg("navigation"), componentBus)
	if err := n.Start(s.ctx); err != nil {
		t.Fatalf("start navigation: %v", err)
	}
	t.Cleanup(func() { _ = n.Stop(s.ctx) })
	time.Sleep(150 * time.Millisecond)
	return s.topic("navigation")
}

func (s *secBrokerTest) startCargo(t *testing.T) string {
	t.Helper()
	componentBus := secNewMQTTBus(t, "sec_cargo_"+fmt.Sprint(time.Now().UnixNano()))
	c := cargo.New(s.cfg("cargo"), componentBus)
	if err := c.Start(s.ctx); err != nil {
		t.Fatalf("start cargo: %v", err)
	}
	t.Cleanup(func() { _ = c.Stop(s.ctx) })
	time.Sleep(150 * time.Millisecond)
	return s.topic("cargo")
}

func (s *secBrokerTest) startJournal(t *testing.T, path string) string {
	t.Helper()
	t.Setenv("JOURNAL_FILE_PATH", path)
	componentBus := secNewMQTTBus(t, "sec_journal_"+fmt.Sprint(time.Now().UnixNano()))
	j := journal.New(s.cfg("journal"), componentBus)
	if err := j.Start(s.ctx); err != nil {
		t.Fatalf("start journal: %v", err)
	}
	t.Cleanup(func() { _ = j.Stop(s.ctx) })
	time.Sleep(150 * time.Millisecond)
	return s.topic("journal")
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

func secReadJSONLines(t *testing.T, path string) []map[string]interface{} {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read journal: %v", err)
	}
	var out []map[string]interface{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var item map[string]interface{}
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			t.Fatalf("invalid journal line %q: %v", line, err)
		}
		out = append(out, item)
	}
	return out
}

func secWaitJournalEvent(t *testing.T, path, event string, timeout time.Duration) map[string]interface{} {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, item := range secReadJSONLines(t, path) {
			if item["event"] == event {
				return item
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("expected journal event %s, got %#v", event, secReadJSONLines(t, path))
	return nil
}

func secAssertForbidden(t *testing.T, payload map[string]interface{}) {
	t.Helper()
	if payload["published"] == true || payload["error"] != "forbidden" {
		t.Fatalf("expected forbidden response, got %#v", payload)
	}
}

func TestSEC_001_DefaultDenyBlocksPolicylessCommand(t *testing.T) {
	s := newSecBrokerTest(t, "SEC001")
	sitlTopic := s.topic("sitl_commands")
	sitlOut := s.subscribe(t, sitlTopic)
	motorsTopic := s.startMotors(t, sitlTopic)
	secTopic := s.startSecurityMonitor(t, nil)

	pl := s.requestPayload(t, secTopic, "proxy_publish", "autopilot", map[string]interface{}{
		"target": map[string]interface{}{"topic": motorsTopic, "action": "SET_TARGET"},
		"data":   map[string]interface{}{"vx": 1.0, "vy": 0.0, "vz": 0.0},
	})
	secAssertForbidden(t, pl)
	secAssertNoBrokerMessage(t, sitlOut)
}

func TestSEC_002_AllowedPolicyForwardsThroughSecurityMonitor(t *testing.T) {
	s := newSecBrokerTest(t, "SEC002")
	sitlTopic := s.topic("sitl_commands")
	sitlOut := s.subscribe(t, sitlTopic)
	motorsTopic := s.startMotors(t, sitlTopic)
	secTopic := s.startSecurityMonitor(t, []map[string]string{
		{"sender": "autopilot", "topic": motorsTopic, "action": "SET_TARGET"},
	})

	pl := s.requestPayload(t, secTopic, "proxy_publish", "autopilot", map[string]interface{}{
		"target": map[string]interface{}{"topic": motorsTopic, "action": "SET_TARGET"},
		"data":   map[string]interface{}{"vx": 2.0, "vy": 0.0, "vz": 0.0, "heading_deg": 45.0},
	})
	if pl["published"] != true {
		t.Fatalf("allowed policy should publish command, got %#v", pl)
	}
	secWaitBrokerMessage(t, sitlOut, 2*time.Second)
}

func TestSEC_003_WrongActionDeniedForKnownSender(t *testing.T) {
	s := newSecBrokerTest(t, "SEC003")
	sitlTopic := s.topic("sitl_commands")
	sitlOut := s.subscribe(t, sitlTopic)
	motorsTopic := s.startMotors(t, sitlTopic)
	secTopic := s.startSecurityMonitor(t, []map[string]string{
		{"sender": "autopilot", "topic": motorsTopic, "action": "SET_TARGET"},
	})

	pl := s.requestPayload(t, secTopic, "proxy_publish", "autopilot", map[string]interface{}{
		"target": map[string]interface{}{"topic": motorsTopic, "action": "LAND"},
		"data":   map[string]interface{}{"reason": "wrong_action"},
	})
	secAssertForbidden(t, pl)
	secAssertNoBrokerMessage(t, sitlOut)
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
	secAssertForbidden(t, pl)
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
		"data":   map[string]interface{}{"vx": 5.0, "vy": 0.0, "vz": 0.0, "heading_deg": 10.0},
	})
	secAssertForbidden(t, blocked)
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

func TestSEC_006_CriticalComponentsRejectDirectCommands(t *testing.T) {
	s := newSecBrokerTest(t, "SEC006")
	sitlTopic := s.topic("sitl_commands")
	sitlOut := s.subscribe(t, sitlTopic)
	motorsTopic := s.startMotors(t, sitlTopic)
	navTopic := s.startNavigation(t)
	cargoTopic := s.startCargo(t)

	if err := s.probeBus.Publish(s.ctx, motorsTopic, map[string]interface{}{
		"action": "SET_TARGET", "sender": "intruder", "payload": map[string]interface{}{"vx": 9.0},
	}); err != nil {
		t.Fatalf("publish direct motors command: %v", err)
	}
	secAssertNoBrokerMessage(t, sitlOut)
	motorsState := s.requestPayload(t, motorsTopic, "get_state", "security_monitor", map[string]interface{}{})
	if motorsState["mode"] != motors.ModeIDLE {
		t.Fatalf("direct motors command changed state: %#v", motorsState)
	}

	if err := s.probeBus.Publish(s.ctx, navTopic, map[string]interface{}{
		"action": "nav_state", "sender": "sitl_adapter", "payload": map[string]interface{}{"lat": 1.0, "lon": 2.0, "alt_m": 3.0},
	}); err != nil {
		t.Fatalf("publish direct navigation update: %v", err)
	}
	navState := s.requestPayload(t, navTopic, "get_state", "security_monitor", map[string]interface{}{})
	if navState["lat"] == 1.0 || navState["lon"] == 2.0 {
		t.Fatalf("direct navigation update changed state: %#v", navState)
	}

	if err := s.probeBus.Publish(s.ctx, cargoTopic, map[string]interface{}{
		"action": "OPEN", "sender": "intruder", "payload": map[string]interface{}{},
	}); err != nil {
		t.Fatalf("publish direct cargo command: %v", err)
	}
	cargoState := s.requestPayload(t, cargoTopic, "get_state", "security_monitor", map[string]interface{}{})
	if cargoState["state"] != cargo.StateClosed {
		t.Fatalf("direct cargo command changed state: %#v", cargoState)
	}
}

func TestSEC_007_IsolationTransitionIsLoggedToJournal(t *testing.T) {
	s := newSecBrokerTest(t, "SEC007")
	journalPath := filepath.Join(t.TempDir(), "journal.ndjson")
	s.startJournal(t, journalPath)
	secTopic := s.startSecurityMonitor(t, nil)

	pl := s.requestPayload(t, secTopic, "ISOLATION_START", "emergency", map[string]interface{}{"reason": "security_test"})
	if pl["activated"] != true || pl["mode"] != "ISOLATED" {
		t.Fatalf("emergency must activate isolated mode, got %#v", pl)
	}
	event := secWaitJournalEvent(t, journalPath, "SECURITY_MONITOR_MODE_TRANSITION", 3*time.Second)
	payload, _ := event["payload"].(map[string]interface{})
	details, _ := payload["details"].(map[string]interface{})
	if details["mode"] != "ISOLATED" {
		t.Fatalf("journal event should record isolated mode, got %#v", event)
	}
}

func TestSEC_008_AccessDecisionsAreLoggedToJournal(t *testing.T) {
	s := newSecBrokerTest(t, "SEC008")
	journalPath := filepath.Join(t.TempDir(), "journal.ndjson")
	s.startJournal(t, journalPath)
	sitlTopic := s.topic("sitl_commands")
	motorsTopic := s.startMotors(t, sitlTopic)
	secTopic := s.startSecurityMonitor(t, []map[string]string{
		{"sender": "autopilot", "topic": motorsTopic, "action": "SET_TARGET"},
	})

	_ = s.requestPayload(t, secTopic, "proxy_publish", "autopilot", map[string]interface{}{
		"target": map[string]interface{}{"topic": motorsTopic, "action": "SET_TARGET"},
		"data":   map[string]interface{}{"vx": 1.0, "vy": 0.0, "vz": 0.0},
	})
	_ = s.requestPayload(t, secTopic, "proxy_publish", "intruder", map[string]interface{}{
		"target": map[string]interface{}{"topic": motorsTopic, "action": "LAND"},
		"data":   map[string]interface{}{},
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var allowSeen, denySeen bool
		for _, item := range secReadJSONLines(t, journalPath) {
			event, _ := item["event"].(string)
			payload, _ := item["payload"].(map[string]interface{})
			decision, _ := payload["decision"].(string)
			if strings.Contains(event, "ACCESS") && decision == "allow" {
				allowSeen = true
			}
			if strings.Contains(event, "ACCESS") && decision == "deny" {
				denySeen = true
			}
		}
		if allowSeen && denySeen {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("security_monitor must log allow and deny decisions, journal=%#v", secReadJSONLines(t, journalPath))
}
