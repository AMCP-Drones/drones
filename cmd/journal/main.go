// Binary for the deliverydron journal component (append-only event log).
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/AMCP-Drones/drones/src/bus"
	"github.com/AMCP-Drones/drones/src/config"
	"github.com/AMCP-Drones/drones/src/journal"
)

func main() {
	cfg := config.FromEnv()
	log.Printf("[%s] journal broker_type=%s topic=%s", cfg.ComponentID, cfg.BrokerType, cfg.ComponentTopic)

	b, err := bus.New(cfg)
	if err != nil {
		log.Fatalf("bus: %v", err)
	}

	j := journal.New(cfg, b)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := j.Start(ctx); err != nil {
		log.Fatalf("start: %v", err)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Printf("[%s] shutting down", cfg.ComponentID)
	cancel()
	if err := j.Stop(context.Background()); err != nil {
		log.Printf("stop: %v", err)
	}
	os.Exit(0)
}
