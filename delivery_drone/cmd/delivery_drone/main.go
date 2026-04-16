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

	"github.com/AMCP-Drones/drones/systems/deliverydron/bus/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/config/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/delivery/src"
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
		if _, err := w.Write([]byte("ok")); err != nil {
			log.Printf("health write: %v", err)
		}
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
	if err := drone.Stop(context.Background()); err != nil {
		log.Printf("drone stop: %v", err)
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("health server shutdown: %v", err)
	}
}
