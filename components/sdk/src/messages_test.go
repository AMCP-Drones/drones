package sdk

import "testing"

func TestCreateResponse(t *testing.T) {
	resp := CreateResponse("corr1", map[string]interface{}{"key": "value"}, "sender1", true, "")
	if resp["action"] != "response" {
		t.Errorf("action: got %v", resp["action"])
	}
	if resp["correlation_id"] != "corr1" {
		t.Errorf("correlation_id: got %v", resp["correlation_id"])
	}
	if resp["success"] != true {
		t.Errorf("success: got %v", resp["success"])
	}
	if resp["sender"] != "sender1" {
		t.Errorf("sender: got %v", resp["sender"])
	}
	pl, _ := resp["payload"].(map[string]interface{})
	if pl["key"] != "value" {
		t.Errorf("payload: got %v", pl)
	}
	if _, ok := resp["timestamp"]; !ok {
		t.Error("timestamp missing")
	}
}

func TestCreateResponse_WithError(t *testing.T) {
	resp := CreateResponse("c1", nil, "s1", false, "something failed")
	if resp["error"] != "something failed" {
		t.Errorf("error: got %v", resp["error"])
	}
	if resp["success"] != false {
		t.Errorf("success: got %v", resp["success"])
	}
}

func TestMessage_ToMap(t *testing.T) {
	m := NewMessage("echo", map[string]interface{}{"x": 1}, "snd", "corr", "reply", "2020-01-01T00:00:00Z")
	out := m.ToMap()
	if out["action"] != "echo" {
		t.Errorf("action: got %v", out["action"])
	}
	if out["reply_to"] != "reply" {
		t.Errorf("reply_to: got %v", out["reply_to"])
	}
}

func TestParseMessage(t *testing.T) {
	raw := []byte(`{"action":"ping","payload":{},"sender":"s1","correlation_id":"c1"}`)
	m, err := ParseMessage(raw)
	if err != nil {
		t.Fatal(err)
	}
	if m.Action != "ping" || m.Sender != "s1" || m.CorrelationID != "c1" {
		t.Errorf("parsed: %+v", m)
	}
}

func TestParseMessage_InvalidJSON(t *testing.T) {
	_, err := ParseMessage([]byte("not json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}
