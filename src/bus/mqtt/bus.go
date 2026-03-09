package mqtt

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// Bus implements bus.Bus using MQTT. Topic dots are converted to slashes for MQTT.
type Bus struct {
	broker     string
	port       int
	clientID   string
	qos        byte
	username   string
	password   string
	client     mqtt.Client
	handlers   map[string]func(map[string]interface{})
	handlersMu sync.RWMutex
	pending    map[string]chan map[string]interface{}
	pendingMu  sync.Mutex
	replyTopic string
	running    bool
}

// New creates an MQTT bus.
func New(broker string, port int, clientID string, qos int, username, password string) *Bus {
	if port <= 0 {
		port = 1883
	}
	q := byte(1)
	if qos >= 0 && qos <= 2 {
		q = byte(qos)
	}
	replyTopic := "replies." + clientID + "." + fmt.Sprintf("%d", time.Now().UnixNano()%100000)
	return &Bus{
		broker:     broker,
		port:       port,
		clientID:   clientID + "_" + fmt.Sprintf("%d", time.Now().UnixNano()%10000),
		qos:        q,
		username:   username,
		password:   password,
		handlers:   make(map[string]func(map[string]interface{})),
		pending:    make(map[string]chan map[string]interface{}),
		replyTopic: replyTopic,
	}
}

func (b *Bus) topicToMQTT(topic string) string {
	return strings.ReplaceAll(topic, ".", "/")
}

func (b *Bus) mqttToTopic(mqttTopic string) string {
	return strings.ReplaceAll(mqttTopic, "/", ".")
}

func (b *Bus) getClient(ctx context.Context) (mqtt.Client, error) {
	if b.client != nil && b.client.IsConnected() {
		return b.client, nil
	}
	opts := mqtt.NewClientOptions()
	opts.AddBroker(fmt.Sprintf("tcp://%s:%d", b.broker, b.port))
	opts.SetClientID(b.clientID)
	if b.username != "" && b.password != "" {
		opts.SetUsername(b.username)
		opts.SetPassword(b.password)
	}
	opts.SetAutoReconnect(true)
	opts.SetConnectRetry(true)
	opts.SetDefaultPublishHandler(func(client mqtt.Client, msg mqtt.Message) {
		topic := b.mqttToTopic(msg.Topic())
		var m map[string]interface{}
		if err := json.Unmarshal(msg.Payload(), &m); err != nil {
			return
		}
		if corr, ok := m["correlation_id"].(string); ok && corr != "" {
			b.pendingMu.Lock()
			if ch, ok := b.pending[corr]; ok {
				delete(b.pending, corr)
				b.pendingMu.Unlock()
				select {
				case ch <- m:
				default:
				}
				return
			}
			b.pendingMu.Unlock()
		}
		b.handlersMu.RLock()
		h := b.handlers[topic]
		b.handlersMu.RUnlock()
		if h != nil {
			h(m)
		}
	})
	client := mqtt.NewClient(opts)
	token := client.Connect()
	if !token.WaitTimeout(10 * time.Second) {
		return nil, fmt.Errorf("mqtt connect timeout")
	}
	if err := token.Error(); err != nil {
		return nil, err
	}
	b.client = client
	return client, nil
}

func (b *Bus) Start(ctx context.Context) error {
	if b.running {
		return nil
	}
	_, err := b.getClient(ctx)
	if err != nil {
		return err
	}
	b.running = true
	// Subscribe to reply topic and all handler topics
	b.handlersMu.RLock()
	topics := make([]string, 0, len(b.handlers)+1)
	topics = append(topics, b.replyTopic)
	for t := range b.handlers {
		topics = append(topics, t)
	}
	b.handlersMu.RUnlock()
	for _, t := range topics {
		mqttTopic := b.topicToMQTT(t)
		if b.client != nil {
			b.client.Subscribe(mqttTopic, b.qos, nil)
		}
	}
	return nil
}

func (b *Bus) Stop(ctx context.Context) error {
	if !b.running {
		return nil
	}
	b.running = false
	if b.client != nil {
		b.client.Disconnect(250)
		b.client = nil
	}
	return nil
}

func (b *Bus) Publish(ctx context.Context, topic string, message map[string]interface{}) error {
	cli, err := b.getClient(ctx)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(message)
	if err != nil {
		return err
	}
	mqttTopic := b.topicToMQTT(topic)
	token := cli.Publish(mqttTopic, b.qos, false, payload)
	if !token.WaitTimeout(5 * time.Second) {
		return fmt.Errorf("publish timeout")
	}
	return token.Error()
}

func (b *Bus) Subscribe(ctx context.Context, topic string, handler func(map[string]interface{})) error {
	b.handlersMu.Lock()
	b.handlers[topic] = handler
	b.handlersMu.Unlock()
	if b.client != nil && b.client.IsConnected() {
		mqttTopic := b.topicToMQTT(topic)
		b.client.Subscribe(mqttTopic, b.qos, nil)
	}
	return nil
}

func (b *Bus) Unsubscribe(ctx context.Context, topic string) error {
	b.handlersMu.Lock()
	delete(b.handlers, topic)
	b.handlersMu.Unlock()
	if b.client != nil && b.client.IsConnected() {
		mqttTopic := b.topicToMQTT(topic)
		b.client.Unsubscribe(mqttTopic)
	}
	return nil
}

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
