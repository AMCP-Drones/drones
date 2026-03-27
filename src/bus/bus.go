// Package bus provides the broker abstraction (publish, subscribe, request/response).
package bus

import (
	"context"
	"errors"

	"github.com/AMCP-Drones/drones/src/sdk"
)

var errNoReplyTo = errors.New("cannot respond: no reply_to in message")

// Bus is the abstract interface for message broker (Kafka or MQTT).
// Same semantics as Python SystemBus: publish, subscribe, request with correlation_id/reply_to.
type Bus interface {
	// Publish sends a message to the given topic.
	Publish(ctx context.Context, topic string, message map[string]interface{}) error
	// Subscribe registers a handler for the topic; call before Start.
	Subscribe(ctx context.Context, topic string, handler func(message map[string]interface{})) error
	// Unsubscribe removes the handler for the topic.
	Unsubscribe(ctx context.Context, topic string) error
	// Request sends a message with correlation_id and reply_to, waits for response or timeout.
	Request(ctx context.Context, topic string, message map[string]interface{}, timeoutSec float64) (map[string]interface{}, error)
	// Start starts the bus (connects, starts consumer).
	Start(ctx context.Context) error
	// Stop stops the bus and releases resources.
	Stop(ctx context.Context) error
}

// Respond sends a response to the reply_to topic with the given payload and correlation_id.
// Original message must contain reply_to and correlation_id. sender is the component ID responding.
func Respond(ctx context.Context, b Bus, original map[string]interface{}, responsePayload map[string]interface{}, sender string, success bool, errMsg string) error {
	replyTo, _ := original["reply_to"].(string)
	correlationID, _ := original["correlation_id"].(string)
	if replyTo == "" {
		return errNoReplyTo
	}
	resp := sdk.CreateResponse(correlationID, responsePayload, sender, success, errMsg)
	return b.Publish(ctx, replyTo, resp)
}
