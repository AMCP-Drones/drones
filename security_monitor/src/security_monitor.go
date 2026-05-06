// Package securitymonitor implements the policy-based gateway.
package securitymonitor

import (
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
