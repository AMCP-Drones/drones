// Binary for the deliverydron limiter component (geofence, emergency + journal).
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/AMCP-Drones/drones/components/bus/src"
	"github.com/AMCP-Drones/drones/components/config/src"
	"github.com/AMCP-Drones/drones/components/limiter/src"
)

func main() {
	cfg := config.FromEnv()
	log.Printf("[%s] limiter broker_type=%s topic=%s", cfg.ComponentID, cfg.BrokerType, cfg.ComponentTopic)

	b, err := bus.New(cfg)
	if err != nil {
		log.Fatalf("bus: %v", err)
	}

	comp := limiter.New(cfg, b)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := comp.Start(ctx); err != nil {
		log.Fatalf("start: %v", err)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Printf("[%s] shutting down", cfg.ComponentID)
	cancel()
	if err := comp.Stop(context.Background()); err != nil {
		log.Printf("stop: %v", err)
	}
	os.Exit(0)
}
