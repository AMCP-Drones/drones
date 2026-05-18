// Command regulator_cert requests firmware certification from the regulator and updates docker/.env.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/AMCP-Drones/drones/systems/deliverydron/certification"
)

func main() {
	kafka := flag.String("kafka", envOr("KAFKA_BOOTSTRAP_SERVERS", "localhost:9092"), "Kafka bootstrap servers")
	user := flag.String("kafka-user", envOr("KAFKA_USERNAME", ""), "Kafka SASL username")
	password := flag.String("kafka-password", envOr("KAFKA_PASSWORD", ""), "Kafka SASL password")
	repo := flag.String("repo", envOr("REGULATOR_REPO_URL", "https://github.com/AMCP-Drones/drones"), "Repository URL to certify")
	commit := flag.String("commit", envOr("GITHUB_SHA", ""), "Commit hash to certify")
	version := flag.String("version", envOr("GITHUB_REF_NAME", ""), "Firmware version label")
	developer := flag.String("developer", envOr("REGULATOR_DEVELOPER_ID", "AMCP-Drones"), "Developer ID")
	droneType := flag.String("drone-type", envOr("REGULATOR_DRONE_TYPE", "DeliveryDrone-X2"), "Drone type")
	envFile := flag.String("env-file", "docker/.env", "Path to docker env file for ORVD_CERTIFICATE_ID")
	certOut := flag.String("cert-out", "", "Optional path to write full certificate JSON")
	timeout := flag.Duration("timeout", 15*time.Minute, "Certification wait timeout")
	flag.Parse()

	if *commit == "" {
		log.Fatal("--commit or GITHUB_SHA is required")
	}

	ctx := context.Background()
	cert, err := certification.RequestFirmwareCert(ctx, certification.Options{
		KafkaBootstrap: *kafka,
		KafkaUser:      *user,
		KafkaPassword:  *password,
		DeveloperID:    *developer,
		DroneType:      *droneType,
		RepositoryURL:  *repo,
		CommitHash:     *commit,
		Version:        *version,
		Timeout:        *timeout,
	})
	if err != nil {
		log.Fatalf("certification failed: %v", err)
	}

	if err := certification.UpdateEnvFile(*envFile, cert.CertificateID); err != nil {
		log.Fatalf("update env file: %v", err)
	}

	if *certOut != "" {
		data, err := json.MarshalIndent(cert, "", "  ")
		if err != nil {
			log.Fatalf("marshal certificate: %v", err)
		}
		if err := os.WriteFile(*certOut, data, 0o644); err != nil {
			log.Fatalf("write cert-out: %v", err)
		}
	}

	fmt.Printf("CERTIFIED certificate_id=%s\n", cert.CertificateID)
	fmt.Printf("Updated %s with ORVD_CERTIFICATE_ID\n", *envFile)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
