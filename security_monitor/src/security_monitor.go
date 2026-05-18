// Package securitymonitor implements the policy-based gateway.
package securitymonitor

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

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
	cfg               *config.Config
	mu                sync.RWMutex
	policies          map[PolicyKey]struct{}
	normalPolicies    map[PolicyKey]struct{}
	policyAdmin       string
	safetyState       SafetyState
	systemName        string
	journalTopic      string
	proxyTimeoutSec   float64
	heartbeatTimeout  time.Duration
	failsafeAction    string
	lastSeenBySender  map[string]time.Time
	watchdogTick      time.Duration
}

// SecurityMonitorComponentID is the fixed trusted sender ID for security monitor.
const SecurityMonitorComponentID = "security_monitor"

// Safety modes.
const (
	ModeNormal   = "NORMAL"
	ModeIsolated = "ISOLATED"
)

// SafetyState is the canonical safety transition state owned by security_monitor.
type SafetyState struct {
	Mode             string
	Reason           string
	Source           string
	TransitionID     string
	Version          int64
	ActivatedAt      time.Time
	LastTransitionAt time.Time
}

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
	orvdTopic := strings.TrimSpace(os.Getenv("ORVD_TOPIC"))
	if orvdTopic == "" {
		orvdTopic = strings.TrimSpace(os.Getenv("ORVD_EXTERNAL_TOPIC"))
	}
	rawPolicies = strings.ReplaceAll(rawPolicies, "${TOPIC_PREFIX}", topicPrefix)
	rawPolicies = strings.ReplaceAll(rawPolicies, "${SYSTEM_NAME}", topicPrefix)
	rawPolicies = strings.ReplaceAll(rawPolicies, "$${SYSTEM_NAME}", topicPrefix)
	rawPolicies = strings.ReplaceAll(rawPolicies, "$SYSTEM_NAME", topicPrefix)
	rawPolicies = strings.ReplaceAll(rawPolicies, "${ORVD_TOPIC}", orvdTopic)
	policyAdmin := strings.TrimSpace(os.Getenv("POLICY_ADMIN_SENDER"))
	timeout := 10.0
	if t := os.Getenv("SECURITY_MONITOR_PROXY_REQUEST_TIMEOUT_S"); t != "" {
		if v, err := strconv.ParseFloat(strings.TrimSpace(t), 64); err == nil && v >= 0.1 {
			timeout = v
		}
	}
	heartbeatTimeout := 0.0
	if t := os.Getenv("SECURITY_MONITOR_HEARTBEAT_TIMEOUT_S"); t != "" {
		if v, err := strconv.ParseFloat(strings.TrimSpace(t), 64); err == nil && v > 0 {
			heartbeatTimeout = v
		}
	}
	failsafeAction := strings.ToLower(strings.TrimSpace(os.Getenv("SECURITY_MONITOR_FAILSAFE_ACTION")))
	if failsafeAction == "" {
		failsafeAction = "isolate"
	}
	initialPolicies := parsePolicies(rawPolicies)
	sm := &SecurityMonitor{
		BaseComponent:     base,
		cfg:               cfg,
		policies:          clonePolicyMap(initialPolicies),
		normalPolicies:    clonePolicyMap(initialPolicies),
		policyAdmin:       policyAdmin,
		safetyState:       newInitialSafetyState(),
		systemName:        systemName,
		journalTopic:      cfg.BrokerTopicFor("journal"),
		proxyTimeoutSec:   timeout,
		heartbeatTimeout:  time.Duration(heartbeatTimeout * float64(time.Second)),
		failsafeAction:    failsafeAction,
		lastSeenBySender:  make(map[string]time.Time),
		watchdogTick:      time.Second,
	}
	sm.registerHandlers()
	return sm
}

func newInitialSafetyState() SafetyState {
	now := time.Now().UTC()
	return SafetyState{
		Mode:             ModeNormal,
		Reason:           "startup",
		Source:           SecurityMonitorComponentID,
		TransitionID:     fmt.Sprintf("safety-%d", now.UnixNano()),
		Version:          1,
		ActivatedAt:      now,
		LastTransitionAt: now,
	}
}

func (sm *SecurityMonitor) registerHandlers() {
	sm.RegisterHandler("proxy_request", sm.handleProxyRequest)
	sm.RegisterHandler("proxy_publish", sm.handleProxyPublish)
	sm.RegisterHandler("set_policy", sm.handleSetPolicy)
	sm.RegisterHandler("remove_policy", sm.handleRemovePolicy)
	sm.RegisterHandler("clear_policies", sm.handleClearPolicies)
	sm.RegisterHandler("list_policies", sm.handleListPolicies)
	sm.RegisterHandler("ISOLATION_START", sm.handleIsolationStart)
	sm.RegisterHandler("ISOLATION_END", sm.handleIsolationEnd)
	sm.RegisterHandler("isolation_status", sm.handleIsolationStatus)
	sm.RegisterHandler("safety_heartbeat", sm.handleSafetyHeartbeat)
}

// Start subscribes monitor handlers and launches optional watchdog supervision.
func (sm *SecurityMonitor) Start(ctx context.Context) error {
	if err := sm.BaseComponent.Start(ctx); err != nil {
		return err
	}
	if sm.heartbeatTimeout > 0 {
		go sm.watchdogLoop(ctx)
	}
	return nil
}

func (sm *SecurityMonitor) watchdogLoop(ctx context.Context) {
	ticker := time.NewTicker(sm.watchdogTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sm.checkHeartbeatTimeout(ctx)
		}
	}
}

func (sm *SecurityMonitor) checkHeartbeatTimeout(ctx context.Context) {
	if sm.heartbeatTimeout <= 0 {
		return
	}
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.safetyState.Mode == ModeIsolated || sm.failsafeAction != "isolate" {
		return
	}
	now := time.Now()
	for _, sender := range []string{"limiter", "emergency"} {
		last, ok := sm.lastSeenBySender[sender]
		if !ok || now.Sub(last) > sm.heartbeatTimeout {
			if sm.enterIsolatedLocked("heartbeat_timeout:"+sender, SecurityMonitorComponentID) {
				sm.logModeTransitionLocked(ctx)
			}
			return
		}
	}
}

func (sm *SecurityMonitor) recordSenderSeen(sender string) {
	sender = strings.TrimSpace(sender)
	if sender == "" {
		return
	}
	sm.mu.Lock()
	sm.lastSeenBySender[sender] = time.Now()
	sm.mu.Unlock()
}
