// Package testutil provides in-memory bus.Bus for tests (no Kafka/MQTT).
package testutil

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

var memBusSeq int64

// MemoryBus is a synchronous in-process implementation of bus.Bus.
// Publish invokes the subscribed handler for that topic on the caller goroutine.
// Request registers a pending reply keyed by correlation_id and delivers responses
// published to the bus's private reply topic.
type MemoryBus struct {
	mu         sync.Mutex
	handlers   map[string]func(map[string]interface{})
	pending    map[string]chan map[string]interface{}
	replyTopic string
	started    bool
}

// NewMemoryBus creates a bus with a unique internal reply topic.
func NewMemoryBus() *MemoryBus {
	id := atomic.AddInt64(&memBusSeq, 1)
	return &MemoryBus{
		handlers:   make(map[string]func(map[string]interface{})),
		pending:    make(map[string]chan map[string]interface{}),
		replyTopic: fmt.Sprintf("mem-replies.%d", id),
	}
}

// ReplyTopic exposes the internal reply topic (for advanced tests).
func (b *MemoryBus) ReplyTopic() string { return b.replyTopic }

// Start marks the bus ready (no background goroutines).
func (b *MemoryBus) Start(_ context.Context) error {
	b.mu.Lock()
	b.started = true
	b.mu.Unlock()
	return nil
}

// Stop clears state.
func (b *MemoryBus) Stop(_ context.Context) error {
	b.mu.Lock()
	b.started = false
	b.handlers = make(map[string]func(map[string]interface{}))
	for _, ch := range b.pending {
		close(ch)
	}
	b.pending = make(map[string]chan map[string]interface{})
	b.mu.Unlock()
	return nil
}

// Subscribe registers the handler for topic (one handler per topic, like Kafka adapter).
func (b *MemoryBus) Subscribe(_ context.Context, topic string, handler func(map[string]interface{})) error {
	b.mu.Lock()
	b.handlers[topic] = handler
	b.mu.Unlock()
	return nil
}

// Unsubscribe removes the handler for topic.
func (b *MemoryBus) Unsubscribe(_ context.Context, topic string) error {
	b.mu.Lock()
	delete(b.handlers, topic)
	b.mu.Unlock()
	return nil
}

// Publish delivers to a reply waiter or invokes the topic handler.
func (b *MemoryBus) Publish(_ context.Context, topic string, message map[string]interface{}) error {
	b.mu.Lock()
	if topic == b.replyTopic {
		cid, _ := message["correlation_id"].(string)
		if cid != "" {
			if ch, ok := b.pending[cid]; ok {
				b.mu.Unlock()
				select {
				case ch <- cloneMap(message):
				default:
				}
				return nil
			}
		}
		b.mu.Unlock()
		return nil
	}
	h := b.handlers[topic]
	b.mu.Unlock()
	if h != nil {
		h(cloneMap(message))
	}
	return nil
}

// Request sends a message with correlation_id and reply_to, then waits for a response map on the reply topic.
func (b *MemoryBus) Request(ctx context.Context, topic string, message map[string]interface{}, timeoutSec float64) (map[string]interface{}, error) {
	_ = b.Start(ctx)
	cid := fmt.Sprintf("cid-%d", atomic.AddInt64(&memBusSeq, 1))
	ch := make(chan map[string]interface{}, 1)
	b.mu.Lock()
	b.pending[cid] = ch
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.pending, cid)
		b.mu.Unlock()
	}()
	msg := cloneMap(message)
	msg["correlation_id"] = cid
	msg["reply_to"] = b.replyTopic
	if err := b.Publish(ctx, topic, msg); err != nil {
		return nil, err
	}
	timeout := time.Duration(timeoutSec * float64(time.Second))
	select {
	case resp := <-ch:
		return resp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(timeout):
		return nil, fmt.Errorf("request timeout after %.1fs", timeoutSec)
	}
}

func cloneMap(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
