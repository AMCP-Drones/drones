package tests

import (
	"testing"

	"github.com/AMCP-Drones/drones/src/component"
)

func TestUnit_IsTrustedSender_SecurityMonitorPrefix(t *testing.T) {
	t.Parallel()
	cases := []struct {
		sender string
		want   bool
	}{
		{"security_monitor", true},
		{"security_monitor_extra", true},
		// Prefix match is on the start of sender (component id), not the hierarchical topic suffix.
		{"v1.sys.I.security_monitor", false},
		{"other", false},
		{"", false},
	}
	for _, tc := range cases {
		msg := map[string]interface{}{"sender": tc.sender}
		if got := component.IsTrustedSender(msg, "security_monitor"); got != tc.want {
			t.Errorf("sender %q: got %v want %v", tc.sender, got, tc.want)
		}
	}
}
