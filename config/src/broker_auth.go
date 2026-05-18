package config

import (
	"fmt"
	"os"
	"strings"
)

// InternalComponents is the default set of deliverydron bus participants for ACL generation.
var InternalComponents = []string{
	"security_monitor",
	"journal",
	"navigation",
	"mission_handler",
	"autopilot",
	"limiter",
	"emergency",
	"motors",
	"cargo",
	"telemetry",
}

// ComponentEnvPrefix maps COMPONENT_ID to docker-compose style env prefix (e.g. mission_handler -> MISSION_HANDLER).
func ComponentEnvPrefix(componentID string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(componentID), "-", "_"))
}

// DefaultBrokerUsername builds a stable MQTT/Kafka username for an instance + component pair.
// Example: dd_Delivery001_autopilot
func DefaultBrokerUsername(instanceID, componentID string) string {
	inst := slugBrokerID(instanceID)
	comp := slugBrokerID(componentID)
	if inst == "" {
		inst = "default"
	}
	if comp == "" {
		comp = "component"
	}
	return fmt.Sprintf("dd_%s_%s", inst, comp)
}

func slugBrokerID(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteRune('_')
		}
	}
	return b.String()
}

// BrokerCredentials resolves BROKER_USER / BROKER_PASSWORD for the running component.
// Priority: <COMPONENT>_BROKER_* -> BROKER_* -> generated username + empty password.
func (c *Config) BrokerCredentials() (user, password string) {
	prefix := ComponentEnvPrefix(c.ComponentID)
	if prefix != "" {
		if u := strings.TrimSpace(os.Getenv(prefix + "_BROKER_USER")); u != "" {
			user = u
		}
		if p := os.Getenv(prefix + "_BROKER_PASSWORD"); p != "" {
			password = p
		}
	}
	if user == "" {
		user = strings.TrimSpace(os.Getenv("BROKER_USER"))
	}
	if password == "" {
		password = os.Getenv("BROKER_PASSWORD")
	}
	if user == "" {
		user = DefaultBrokerUsername(c.InstanceID, c.ComponentID)
	}
	return user, password
}

// ReplyBrokerTopic is the deterministic reply topic for request/response (used in MQTT ACL).
func (c *Config) ReplyBrokerTopic() string {
	comp := strings.TrimSpace(c.ComponentID)
	if comp == "" {
		comp = "component"
	}
	return c.BrokerTopicFor("replies." + comp)
}

// SecurityMonitorTopic returns the security_monitor ingress topic for this instance.
func (c *Config) SecurityMonitorTopic() string {
	return c.BrokerTopicFor("security_monitor")
}
