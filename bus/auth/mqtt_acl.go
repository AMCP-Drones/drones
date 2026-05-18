// Package auth generates Mosquitto ACL/password plans for per-component broker credentials.
package auth

import (
	"fmt"
	"sort"
	"strings"
)

// ExternalPublisher allows an external system to publish only to a named component topic.
type ExternalPublisher struct {
	Username  string
	Component string
}

// MQTTACLSpec describes one BAS instance on the bus.
type MQTTACLSpec struct {
	TopicPrefix string
	InstanceID  string
	Components  []string
	External    []ExternalPublisher
	// UsernameForComponent overrides default dd_<instance>_<component> usernames.
	UsernameForComponent func(instanceID, component string) string
}

// DefaultUsername matches config.DefaultBrokerUsername.
func DefaultUsername(instanceID, component string) string {
	return fmt.Sprintf("dd_%s_%s", slug(instanceID), slug(component))
}

func slug(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "default"
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
	out := b.String()
	if out == "" {
		return "default"
	}
	return out
}

func (s *MQTTACLSpec) username(component string) string {
	if s.UsernameForComponent != nil {
		return s.UsernameForComponent(s.InstanceID, component)
	}
	return DefaultUsername(s.InstanceID, component)
}

func (s *MQTTACLSpec) componentTopic(component string) string {
	return s.TopicPrefix + "." + strings.TrimSpace(component)
}

func (s *MQTTACLSpec) replyTopic(component string) string {
	return s.TopicPrefix + ".replies." + strings.TrimSpace(component)
}

// GenerateACL returns mosquitto acl_file contents.
//
// Rules:
//   - security_monitor: read own topic + all component/reply topics; write all component topics + own topic
//   - each other component: read own topic + own reply topic; write only security_monitor topic
//   - external publishers: write only their bound component topic
func (s *MQTTACLSpec) GenerateACL() string {
	comps := append([]string(nil), s.Components...)
	sort.Strings(comps)

	prefix := strings.TrimSpace(s.TopicPrefix)
	smTopic := prefix + ".security_monitor"

	var b strings.Builder
	b.WriteString("# Auto-generated Mosquitto ACL for deliverydron\n")
	b.WriteString("# Instance: " + s.InstanceID + "\n\n")

	smUser := s.username("security_monitor")
	b.WriteString("user " + smUser + "\n")
	b.WriteString("topic read " + smTopic + "\n")
	b.WriteString("topic write " + smTopic + "\n")
	for _, comp := range comps {
		if comp == "security_monitor" {
			continue
		}
		b.WriteString("topic read " + s.componentTopic(comp) + "\n")
		b.WriteString("topic write " + s.componentTopic(comp) + "\n")
		b.WriteString("topic read " + s.replyTopic(comp) + "\n")
		b.WriteString("topic write " + s.replyTopic(comp) + "\n")
	}
	b.WriteString("\n")

	for _, comp := range comps {
		if comp == "security_monitor" {
			continue
		}
		b.WriteString("user " + s.username(comp) + "\n")
		b.WriteString("topic read " + s.componentTopic(comp) + "\n")
		b.WriteString("topic read " + s.replyTopic(comp) + "\n")
		b.WriteString("topic write " + smTopic + "\n")
		b.WriteString("\n")
	}

	for _, ext := range s.External {
		u := strings.TrimSpace(ext.Username)
		c := strings.TrimSpace(ext.Component)
		if u == "" || c == "" {
			continue
		}
		b.WriteString("user " + u + "\n")
		b.WriteString("topic write " + s.componentTopic(c) + "\n")
		b.WriteString("topic read " + s.replyTopic(c) + "\n")
		b.WriteString("\n")
	}

	return b.String()
}

// BrokerUsers returns all internal component usernames for password file generation.
func (s *MQTTACLSpec) BrokerUsers() []string {
	seen := make(map[string]struct{})
	var out []string
	for _, comp := range s.Components {
		u := s.username(comp)
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}
	sort.Strings(out)
	return out
}
