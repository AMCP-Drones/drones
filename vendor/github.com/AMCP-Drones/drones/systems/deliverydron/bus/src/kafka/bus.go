// Package kafka implements the bus.Bus interface using Apache Kafka.
package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/IBM/sarama"
)

// Bus implements bus.Bus using Kafka.
type Bus struct {
	broker          string
	clientID        string
	groupID         string
	username        string
	password        string
	producer        sarama.SyncProducer
	consumer        sarama.ConsumerGroup
	replyTopic      string
	handlers        map[string]func(map[string]interface{})
	handlersMu      sync.RWMutex
	pending         map[string]chan map[string]interface{}
	pendingMu       sync.Mutex
	consumerGroupID string
	running         bool
	consumeCancel   context.CancelFunc
	consumeCtx      context.Context
	consumeTopicsMu sync.Mutex
}

// New creates a Kafka bus. groupID can be empty to use clientID_group.
func New(broker, clientID, groupID, username, password string) *Bus {
	if groupID == "" {
		groupID = clientID + "_group"
	}
	replyTopic := "replies." + clientID + "." + fmt.Sprintf("%d", time.Now().UnixNano()%100000)
	return &Bus{
		broker:          broker,
		clientID:        clientID,
		groupID:         groupID,
		username:        username,
		password:        password,
		replyTopic:      replyTopic,
		handlers:        make(map[string]func(map[string]interface{})),
		pending:         make(map[string]chan map[string]interface{}),
		consumerGroupID: groupID + "_cg",
	}
}

func (b *Bus) getProducer() (sarama.SyncProducer, error) {
	if b.producer != nil {
		return b.producer, nil
	}
	cfg := sarama.NewConfig()
	cfg.Producer.RequiredAcks = sarama.WaitForAll
	cfg.Producer.Return.Successes = true
	cfg.ClientID = b.clientID
	if b.username != "" && b.password != "" {
		cfg.Net.SASL.Enable = true
		cfg.Net.SASL.Mechanism = sarama.SASLTypePlaintext
		cfg.Net.SASL.User = b.username
		cfg.Net.SASL.Password = b.password
	}
	p, err := sarama.NewSyncProducer([]string{b.broker}, cfg)
	if err != nil {
		return nil, err
	}
	b.producer = p
	return p, nil
}

func (b *Bus) publishJSON(topic string, msg map[string]interface{}) error {
	p, err := b.getProducer()
	if err != nil {
		return err
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, _, err = p.SendMessage(&sarama.ProducerMessage{
		Topic: topic,
		Value: sarama.ByteEncoder(payload),
	})
	return err
}

func (b *Bus) getTopics() []string {
	b.handlersMu.RLock()
	defer b.handlersMu.RUnlock()
	topics := make([]string, 0, len(b.handlers)+1)
	topics = append(topics, b.replyTopic)
	for t := range b.handlers {
		topics = append(topics, t)
	}
	return topics
}

func (b *Bus) startConsumer(ctx context.Context) {
	cfg := sarama.NewConfig()
	cfg.Consumer.Return.Errors = true
	cfg.Consumer.Offsets.Initial = sarama.OffsetOldest
	cfg.ClientID = b.clientID + "_consumer"
	if b.username != "" && b.password != "" {
		cfg.Net.SASL.Enable = true
		cfg.Net.SASL.Mechanism = sarama.SASLTypePlaintext
		cfg.Net.SASL.User = b.username
		cfg.Net.SASL.Password = b.password
	}
	group, err := sarama.NewConsumerGroup([]string{b.broker}, b.consumerGroupID, cfg)
	if err != nil {
		return
	}
	b.consumeTopicsMu.Lock()
	b.consumer = group
	b.consumeTopicsMu.Unlock()
	topics := b.getTopics()
	handler := &consumerHandler{bus: b}
	if err := group.Consume(ctx, topics, handler); err != nil && ctx.Err() == nil {
		log.Printf("kafka consume: %v", err)
	}
	if err := group.Close(); err != nil {
		log.Printf("kafka consumer group close: %v", err)
	}
}

type consumerHandler struct {
	bus *Bus
}

func (h *consumerHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for msg := range claim.Messages() {
		var m map[string]interface{}
		if err := json.Unmarshal(msg.Value, &m); err != nil {
			session.MarkMessage(msg, "")
			continue
		}
		topic := msg.Topic
		if corr, ok := m["correlation_id"].(string); ok && corr != "" {
			h.bus.pendingMu.Lock()
			if ch, ok := h.bus.pending[corr]; ok {
				delete(h.bus.pending, corr)
				h.bus.pendingMu.Unlock()
				select {
				case ch <- m:
				default:
				}
				session.MarkMessage(msg, "")
				continue
			}
			h.bus.pendingMu.Unlock()
		}
		h.bus.handlersMu.RLock()
		handler := h.bus.handlers[topic]
		h.bus.handlersMu.RUnlock()
		if handler != nil {
			handler(m)
		}
		session.MarkMessage(msg, "")
	}
	return nil
}

func (h *consumerHandler) Setup(sarama.ConsumerGroupSession) error   { return nil }
func (h *consumerHandler) Cleanup(sarama.ConsumerGroupSession) error { return nil }

// Start starts the bus: creates producer and consumer group, subscribes to handler and reply topics.
func (b *Bus) Start(ctx context.Context) error {
	if b.running {
		return nil
	}
	if _, err := b.getProducer(); err != nil {
		return err
	}
	if err := b.publishJSON(b.replyTopic, map[string]interface{}{"_init": true}); err != nil {
		log.Printf("kafka reply topic init publish: %v", err)
	}
	b.running = true
	b.consumeCtx, b.consumeCancel = context.WithCancel(ctx)
	go b.startConsumer(b.consumeCtx)
	time.Sleep(1 * time.Second)
	return nil
}

// Stop stops the consumer and producer and releases resources.
func (b *Bus) Stop(_ context.Context) error {
	if !b.running {
		return nil
	}
	b.running = false
	if b.consumeCancel != nil {
		b.consumeCancel()
	}
	b.consumeTopicsMu.Lock()
	cg := b.consumer
	b.consumeTopicsMu.Unlock()
	if cg != nil {
		if err := cg.Close(); err != nil {
			log.Printf("kafka consumer close: %v", err)
		}
	}
	if b.producer != nil {
		if err := b.producer.Close(); err != nil {
			log.Printf("kafka producer close: %v", err)
		}
		b.producer = nil
	}
	return nil
}

// Publish sends a JSON message to the given topic.
func (b *Bus) Publish(_ context.Context, topic string, message map[string]interface{}) error {
	return b.publishJSON(topic, message)
}

// Subscribe registers a handler for the topic. Call Subscribe for all topics before Start().
func (b *Bus) Subscribe(_ context.Context, topic string, handler func(map[string]interface{})) error {
	b.handlersMu.Lock()
	b.handlers[topic] = handler
	b.handlersMu.Unlock()
	return nil
}

// Unsubscribe removes the handler for the topic.
func (b *Bus) Unsubscribe(_ context.Context, topic string) error {
	b.handlersMu.Lock()
	delete(b.handlers, topic)
	b.handlersMu.Unlock()
	return nil
}

// Request publishes a message with correlation_id and reply_to, then waits for a response or timeout.
func (b *Bus) Request(ctx context.Context, topic string, message map[string]interface{}, timeoutSec float64) (map[string]interface{}, error) {
	if !b.running {
		if err := b.Start(ctx); err != nil {
			return nil, err
		}
	}
	correlationID := fmt.Sprintf("%d", time.Now().UnixNano())
	ch := make(chan map[string]interface{}, 1)
	b.pendingMu.Lock()
	b.pending[correlationID] = ch
	b.pendingMu.Unlock()
	defer func() {
		b.pendingMu.Lock()
		delete(b.pending, correlationID)
		b.pendingMu.Unlock()
	}()
	msg := make(map[string]interface{})
	for k, v := range message {
		msg[k] = v
	}
	msg["correlation_id"] = correlationID
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
