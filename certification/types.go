package certification

import "time"

const (
	TopicFirmwareRequest = "v1.firmware.certification.request"
	TopicFirmwareResult  = "v1.firmware.certificate.result"
)

// FirmwareCertRequest is sent to the regulator on certification request topic.
type FirmwareCertRequest struct {
	RequestID   string                 `json:"request_id"`
	Timestamp   time.Time              `json:"timestamp"`
	DeveloperID string                 `json:"developer_id"`
	DroneType   string                 `json:"drone_type"`
	Firmware    map[string]interface{} `json:"firmware"`
}

// FirmwareCertResult is received from the regulator result topic.
type FirmwareCertResult struct {
	RequestID   string                 `json:"request_id"`
	Timestamp   *time.Time             `json:"timestamp,omitempty"`
	Status      string                 `json:"status"`
	Certificate map[string]interface{} `json:"certificate,omitempty"`
	Errors      []interface{}          `json:"errors,omitempty"`
}

// FirmwareCertificate holds fields needed for ORVD and audit.
type FirmwareCertificate struct {
	CertificateID        string
	Status               string
	SecurityGoalsChecked []string
	DigitalSignature     string
	Raw                  map[string]interface{}
}

// Options configures RequestFirmwareCert.
type Options struct {
	KafkaBootstrap string
	KafkaUser      string
	KafkaPassword  string
	DeveloperID    string
	DroneType      string
	RepositoryURL  string
	CommitHash     string
	Version        string
	Timeout        time.Duration
}
