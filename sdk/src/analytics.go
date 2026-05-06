package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// TelemetryLog matches DroneAnalytics /log/telemetry schema.
type TelemetryLog struct {
	APIVersion string   `json:"apiVersion"`
	Timestamp  int64    `json:"timestamp"`
	Drone      string   `json:"drone"`
	DroneID    int64    `json:"drone_id"`
	Battery    *int     `json:"battery,omitempty"`
	Pitch      *float64 `json:"pitch,omitempty"`
	Roll       *float64 `json:"roll,omitempty"`
	Course     *float64 `json:"course,omitempty"`
	Latitude   float64  `json:"latitude"`
	Longitude  float64  `json:"longitude"`
	Height     *float64 `json:"height,omitempty"`
}

// EventLog matches DroneAnalytics /log/event schema.
type EventLog struct {
	APIVersion string `json:"apiVersion"`
	Timestamp  int64  `json:"timestamp"`
	EventType  string `json:"event_type,omitempty"`
	Service    string `json:"service"`
	ServiceID  int64  `json:"service_id"`
	Severity   string `json:"severity,omitempty"`
	Message    string `json:"message"`
}

// AnalyticsClient is a tiny HTTP client for DroneAnalytics ingestion endpoints.
type AnalyticsClient struct {
	enabled    bool
	baseURL    string
	apiKey     string
	httpClient *http.Client

	apiVersion string
	drone      string
	droneID    int64
	service    string
	serviceID  int64
}

// NewAnalyticsClientFromEnv configures analytics from environment variables.
func NewAnalyticsClientFromEnv() *AnalyticsClient {
	enabled := parseBool(os.Getenv("ANALYTICS_ENABLED"))
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("ANALYTICS_BASE_URL")), "/")
	apiKey := strings.TrimSpace(os.Getenv("ANALYTICS_API_KEY"))
	timeoutSec := parseFloat(os.Getenv("ANALYTICS_TIMEOUT_S"), 2.0)
	apiVersion := strings.TrimSpace(os.Getenv("ANALYTICS_API_VERSION"))
	if apiVersion == "" {
		apiVersion = "1.1.0"
	}
	drone := strings.TrimSpace(os.Getenv("ANALYTICS_DRONE"))
	if drone == "" {
		drone = "delivery"
	}
	droneID := parseInt64(os.Getenv("ANALYTICS_DRONE_ID"), 1)
	service := strings.TrimSpace(os.Getenv("ANALYTICS_SERVICE"))
	if service == "" {
		service = "delivery"
	}
	serviceID := parseInt64(os.Getenv("ANALYTICS_SERVICE_ID"), 1)

	if !enabled || baseURL == "" || apiKey == "" {
		enabled = false
	}
	return &AnalyticsClient{
		enabled: enabled,
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: time.Duration(timeoutSec * float64(time.Second)),
		},
		apiVersion: apiVersion,
		drone:      drone,
		droneID:    droneID,
		service:    service,
		serviceID:  serviceID,
	}
}

func (c *AnalyticsClient) Enabled() bool { return c != nil && c.enabled }
func (c *AnalyticsClient) APIVersion() string {
	if c == nil {
		return "1.1.0"
	}
	return c.apiVersion
}
func (c *AnalyticsClient) Drone() string {
	if c == nil {
		return "delivery"
	}
	return c.drone
}
func (c *AnalyticsClient) DroneID() int64 {
	if c == nil {
		return 1
	}
	return c.droneID
}
func (c *AnalyticsClient) Service() string {
	if c == nil {
		return "delivery"
	}
	return c.service
}
func (c *AnalyticsClient) ServiceID() int64 {
	if c == nil {
		return 1
	}
	return c.serviceID
}

func (c *AnalyticsClient) PostTelemetry(ctx context.Context, logs []TelemetryLog) error {
	if !c.Enabled() || len(logs) == 0 {
		return nil
	}
	return c.postJSON(ctx, "/log/telemetry", logs)
}

func (c *AnalyticsClient) PostEvent(ctx context.Context, logs []EventLog) error {
	if !c.Enabled() || len(logs) == 0 {
		return nil
	}
	return c.postJSON(ctx, "/log/event", logs)
}

func (c *AnalyticsClient) postJSON(ctx context.Context, path string, payload interface{}) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", c.apiKey)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusMultiStatus {
		return nil
	}
	return fmt.Errorf("analytics status %d", resp.StatusCode)
}

func parseBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func parseFloat(s string, fallback float64) float64 {
	if v, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil && v > 0 {
		return v
	}
	return fallback
}

func parseInt64(s string, fallback int64) int64 {
	if v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64); err == nil && v > 0 {
		return v
	}
	return fallback
}
