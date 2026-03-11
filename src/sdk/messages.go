// Package sdk provides message protocol and types aligned with sdk/messages.py.
package sdk

import (
	"encoding/json"
	"time"
)

// Message is the base message format for SystemBus (action, payload, sender, correlation_id, reply_to, timestamp).
type Message struct {
	Action        string                 `json:"action"`
	Payload       map[string]interface{} `json:"payload,omitempty"`
	Sender        string                 `json:"sender,omitempty"`
	CorrelationID string                 `json:"correlation_id,omitempty"`
	ReplyTo       string                 `json:"reply_to,omitempty"`
	Timestamp     string                 `json:"timestamp,omitempty"`
}

// Response is the response message format (action "response", success, error).
type Response struct {
	Action        string                 `json:"action"`
	Payload       map[string]interface{} `json:"payload,omitempty"`
	Sender        string                 `json:"sender,omitempty"`
	CorrelationID string                 `json:"correlation_id,omitempty"`
	Success       bool                   `json:"success"`
	Error         string                 `json:"error,omitempty"`
	Timestamp     string                 `json:"timestamp,omitempty"`
}

// NewMessage creates a message with optional fields; timestamp is set to UTC now if empty.
func NewMessage(action string, payload map[string]interface{}, sender, correlationID, replyTo, timestamp string) Message {
	if timestamp == "" {
		timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if payload == nil {
		payload = make(map[string]interface{})
	}
	return Message{
		Action:        action,
		Payload:       payload,
		Sender:        sender,
		CorrelationID: correlationID,
		ReplyTo:       replyTo,
		Timestamp:     timestamp,
	}
}

// CreateResponse builds a response message for request/response (action "response", success, error).
func CreateResponse(correlationID string, payload map[string]interface{}, sender string, success bool, errMsg string) map[string]interface{} {
	out := map[string]interface{}{
		"action":         "response",
		"payload":        payload,
		"sender":         sender,
		"correlation_id": correlationID,
		"success":        success,
		"timestamp":      time.Now().UTC().Format(time.RFC3339Nano),
	}
	if errMsg != "" {
		out["error"] = errMsg
	}
	return out
}

// ParseMessage unmarshals a generic map into Message fields (for incoming JSON).
func ParseMessage(data []byte) (Message, error) {
	var m Message
	err := json.Unmarshal(data, &m)
	return m, err
}

// ToMap serializes a Message to a map for publishing.
func (m Message) ToMap() map[string]interface{} {
	out := map[string]interface{}{
		"action":    m.Action,
		"payload":  m.Payload,
		"sender":    m.Sender,
		"timestamp": m.Timestamp,
	}
	if m.CorrelationID != "" {
		out["correlation_id"] = m.CorrelationID
	}
	if m.ReplyTo != "" {
		out["reply_to"] = m.ReplyTo
	}
	return out
}
