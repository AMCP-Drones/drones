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
	Publish(ctx context.Context, topic string, message map[string]interface{}) error
	Subscribe(ctx context.Context, topic string, handler func(message map[string]interface{})) error
	Unsubscribe(ctx context.Context, topic string) error
	Request(ctx context.Context, topic string, message map[string]interface{}, timeoutSec float64) (map[string]interface{}, error)
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// Respond sends a response to the reply_to topic with the given payload and correlation_id.
// Original message must contain reply_to and correlation_id. sender is the component ID responding.
func Respond(b Bus, ctx context.Context, original map[string]interface{}, responsePayload map[string]interface{}, sender string, success bool, errMsg string) error {
	replyTo, _ := original["reply_to"].(string)
	correlationID, _ := original["correlation_id"].(string)
	if replyTo == "" {
		return errNoReplyTo
	}
	resp := sdk.CreateResponse(correlationID, responsePayload, sender, success, errMsg)
	return b.Publish(ctx, replyTo, resp)
}
