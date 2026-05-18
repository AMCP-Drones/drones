//go:build regulator

package tests

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/AMCP-Drones/drones/systems/deliverydron/certification"
)

func TestIntegration_Regulator_FirmwareCert(t *testing.T) {
	bootstrap := os.Getenv("KAFKA_BOOTSTRAP_SERVERS")
	if bootstrap == "" {
		bootstrap = "localhost:9092"
	}
	repo := os.Getenv("REGULATOR_REPO_URL")
	if repo == "" {
		repo = "https://github.com/AMCP-Drones/drones"
	}
	commit := os.Getenv("REGULATOR_TEST_COMMIT")
	if commit == "" {
		t.Skip("REGULATOR_TEST_COMMIT not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	cert, err := certification.RequestFirmwareCert(ctx, certification.Options{
		KafkaBootstrap: bootstrap,
		KafkaUser:      os.Getenv("KAFKA_USERNAME"),
		KafkaPassword:  os.Getenv("KAFKA_PASSWORD"),
		RepositoryURL:  repo,
		CommitHash:     commit,
		Version:        "integration-test",
		DeveloperID:    "AMCP-Drones",
		DroneType:      "DeliveryDrone-X2",
		Timeout:        15 * time.Minute,
	})
	if err != nil {
		t.Fatalf("firmware certification: %v", err)
	}
	if cert.CertificateID == "" {
		t.Fatal("empty certificate_id")
	}
	t.Logf("certificate_id=%s goals=%v", cert.CertificateID, cert.SecurityGoalsChecked)
}
