package certification

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/IBM/sarama"
)

// RequestFirmwareCert publishes a firmware certification request and waits for the result.
func RequestFirmwareCert(ctx context.Context, opts Options) (*FirmwareCertificate, error) {
	if opts.Timeout <= 0 {
		opts.Timeout = 10 * time.Minute
	}
	if opts.KafkaBootstrap == "" {
		opts.KafkaBootstrap = "localhost:9092"
	}
	if opts.DeveloperID == "" {
		opts.DeveloperID = "AMCP-Drones"
	}
	if opts.DroneType == "" {
		opts.DroneType = "DeliveryDrone-X2"
	}
	if opts.RepositoryURL == "" {
		return nil, fmt.Errorf("repository URL is required")
	}
	if opts.CommitHash == "" {
		return nil, fmt.Errorf("commit hash is required")
	}

	requestID := fmt.Sprintf("req-%d", time.Now().UnixNano())
	req := FirmwareCertRequest{
		RequestID:   requestID,
		Timestamp:   time.Now().UTC(),
		DeveloperID: opts.DeveloperID,
		DroneType:   opts.DroneType,
		Firmware: map[string]interface{}{
			"repository_url": opts.RepositoryURL,
			"commit_hash":    opts.CommitHash,
			"version":        opts.Version,
		},
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	cfg := sarama.NewConfig()
	cfg.Consumer.Return.Errors = true
	cfg.Consumer.Offsets.Initial = sarama.OffsetNewest
	cfg.Producer.Return.Successes = true
	cfg.ClientID = "regulator_cert"
	if opts.KafkaUser != "" {
		cfg.Net.SASL.Enable = true
		cfg.Net.SASL.Mechanism = sarama.SASLTypePlaintext
		cfg.Net.SASL.User = opts.KafkaUser
		cfg.Net.SASL.Password = opts.KafkaPassword
	}

	groupID := fmt.Sprintf("regulator_cert_%d", time.Now().UnixNano())
	consumer, err := sarama.NewConsumerGroup(strings.Split(opts.KafkaBootstrap, ","), groupID, cfg)
	if err != nil {
		return nil, fmt.Errorf("consumer group: %w", err)
	}
	defer consumer.Close()

	resultCh := make(chan *FirmwareCertResult, 1)
	errCh := make(chan error, 1)
	handler := &resultHandler{requestID: requestID, ch: resultCh}

	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			if err := consumer.Consume(ctx, []string{TopicFirmwareResult}, handler); err != nil {
				if ctx.Err() != nil {
					return
				}
				errCh <- err
				return
			}
			if ctx.Err() != nil {
				return
			}
		}
	}()

	// Allow consumer to join group before publish.
	time.Sleep(2 * time.Second)

	producer, err := sarama.NewSyncProducer(strings.Split(opts.KafkaBootstrap, ","), cfg)
	if err != nil {
		cancel()
		wg.Wait()
		return nil, fmt.Errorf("producer: %w", err)
	}
	defer producer.Close()

	_, _, err = producer.SendMessage(&sarama.ProducerMessage{
		Topic: TopicFirmwareRequest,
		Value: sarama.ByteEncoder(payload),
	})
	if err != nil {
		cancel()
		wg.Wait()
		return nil, fmt.Errorf("publish request: %w", err)
	}

	select {
	case res := <-resultCh:
		return parseFirmwareCertificate(res)
	case err := <-errCh:
		return nil, err
	case <-ctx.Done():
		wg.Wait()
		return nil, fmt.Errorf("firmware certification timed out after %s", opts.Timeout)
	}
}

type resultHandler struct {
	requestID string
	ch        chan *FirmwareCertResult
	once      sync.Once
}

func (h *resultHandler) Setup(sarama.ConsumerGroupSession) error   { return nil }
func (h *resultHandler) Cleanup(sarama.ConsumerGroupSession) error { return nil }

func (h *resultHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for msg := range claim.Messages() {
		var res FirmwareCertResult
		if err := json.Unmarshal(msg.Value, &res); err != nil {
			session.MarkMessage(msg, "")
			continue
		}
		if res.RequestID != h.requestID {
			session.MarkMessage(msg, "")
			continue
		}
		h.once.Do(func() { h.ch <- &res })
		session.MarkMessage(msg, "")
		return nil
	}
	return nil
}

func parseFirmwareCertificate(res *FirmwareCertResult) (*FirmwareCertificate, error) {
	if res == nil {
		return nil, fmt.Errorf("empty regulator response")
	}
	status := strings.ToUpper(strings.TrimSpace(res.Status))
	if status != "CERTIFIED" {
		return nil, fmt.Errorf("firmware certification %s (request_id=%s)", status, res.RequestID)
	}
	if res.Certificate == nil {
		return nil, fmt.Errorf("certified response missing certificate")
	}
	certID, _ := res.Certificate["certificate_id"].(string)
	if certID == "" {
		return nil, fmt.Errorf("certificate missing certificate_id")
	}
	out := &FirmwareCertificate{
		CertificateID: certID,
		Status:        status,
		Raw:           res.Certificate,
	}
	if sig, ok := res.Certificate["digital_signature"].(string); ok {
		out.DigitalSignature = sig
	}
	goals := extractStringList(res.Certificate["requirements_checked"])
	if len(goals) == 0 {
		goals = extractStringList(res.Certificate["security_goals_checked"])
	}
	if len(goals) == 0 {
		goals = extractStringList(res.Certificate["security_goals"])
	}
	out.SecurityGoalsChecked = goals
	return out, nil
}

func extractStringList(v interface{}) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []interface{}:
		var out []string
		for _, item := range t {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}
