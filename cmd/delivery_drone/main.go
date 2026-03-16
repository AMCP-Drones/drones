// Main entry point for the delivery drone service. Connects to broker (Kafka or MQTT via BROKER_TYPE), runs the delivery component, and exposes HEALTH_PORT for health checks.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/AMCP-Drones/drones/src/bus"
	"github.com/AMCP-Drones/drones/src/config"
	"github.com/AMCP-Drones/drones/src/delivery"
)

func main() {
	cfg := config.FromEnv()
	log.Printf("[%s] broker_type=%s", cfg.ComponentID, cfg.BrokerType)

	b, err := bus.New(cfg)
	if err != nil {
		log.Fatalf("bus: %v", err)
	}

	drone := delivery.New(cfg.ComponentID, cfg.ComponentID, cfg.ComponentTopic, b)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := drone.Start(ctx); err != nil {
		log.Fatalf("drone start: %v", err)
	}

	// Health HTTP server (platform expects HEALTH_PORT)
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	addr := ":" + cfg.HealthPort
	srv := &http.Server{Addr: addr, Handler: mux}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("health server: %v", err)
		}
	}()
	log.Printf("[%s] health server on %s", cfg.ComponentID, addr)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Printf("[%s] shutting down", cfg.ComponentID)
	cancel()
	_ = drone.Stop(context.Background())
	shutdownCtx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	_ = srv.Shutdown(shutdownCtx)
}
