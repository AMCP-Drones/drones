package sdk

import (
	"encoding/json"
	"testing"
)

func TestNewMessage_Defaults(t *testing.T) {
	m := NewMessage("ping", nil, "s1", "", "", "")
	if m.Action != "ping" || m.Timestamp == "" {
		t.Fatalf("%+v", m)
	}
	out := m.ToMap()
	if out["correlation_id"] != nil {
		t.Fatalf("expected no correlation_id")
	}
}

func TestNewMessage_WithOptionalFields(t *testing.T) {
	m := NewMessage("x", map[string]interface{}{"a": 1}, "s", "cid", "reply", "ts")
	out := m.ToMap()
	if out["correlation_id"] != "cid" || out["reply_to"] != "reply" {
		t.Fatalf("%#v", out)
	}
}

func TestParseMessage_RoundTrip(t *testing.T) {
	raw, _ := json.Marshal(Message{Action: "a", Payload: map[string]interface{}{"k": "v"}})
	m, err := ParseMessage(raw)
	if err != nil || m.Action != "a" {
		t.Fatalf("err=%v m=%+v", err, m)
	}
}
