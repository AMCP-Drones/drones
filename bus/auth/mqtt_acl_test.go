package auth

import (
	"strings"
	"testing"
)

func TestGenerateACL_SecurityMonitorAndComponentIsolation(t *testing.T) {
	spec := MQTTACLSpec{
		TopicPrefix: "v1.deliverydron.Delivery001",
		InstanceID:  "Delivery001",
		Components:  []string{"security_monitor", "autopilot", "journal"},
	}
	acl := spec.GenerateACL()

	smUser := DefaultUsername("Delivery001", "security_monitor")
	apUser := DefaultUsername("Delivery001", "autopilot")

	if !strings.Contains(acl, "user "+smUser) {
		t.Fatal("missing security_monitor user")
	}
	if !strings.Contains(acl, "topic write v1.deliverydron.Delivery001.autopilot") {
		t.Fatal("SM must write to autopilot topic")
	}

	block := between(acl, "user "+apUser, "\n\n")
	if strings.Contains(block, "topic write v1.deliverydron.Delivery001.autopilot") {
		t.Fatal("autopilot must not write to own topic")
	}
	if !strings.Contains(block, "topic write v1.deliverydron.Delivery001.security_monitor") {
		t.Fatal("autopilot must write only to security_monitor")
	}
}

func TestGenerateACL_ExternalPublisher(t *testing.T) {
	spec := MQTTACLSpec{
		TopicPrefix: "v1.deliverydron.Delivery001",
		InstanceID:  "Delivery001",
		Components:  []string{"security_monitor", "mission_handler"},
		External: []ExternalPublisher{
			{Username: "nus_ext", Component: "mission_handler"},
		},
	}
	acl := spec.GenerateACL()
	if !strings.Contains(acl, "user nus_ext") {
		t.Fatal("missing external user")
	}
	if !strings.Contains(acl, "topic write v1.deliverydron.Delivery001.mission_handler") {
		t.Fatal("external must publish to mission_handler only")
	}
}

func between(s, start, end string) string {
	i := strings.Index(s, start)
	if i < 0 {
		return ""
	}
	s = s[i+len(start):]
	j := strings.Index(s, end)
	if j < 0 {
		return s
	}
	return s[:j]
}
