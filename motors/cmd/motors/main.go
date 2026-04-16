// Binary for the deliverydron motors component (SET_TARGET, LAND, get_state).
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/AMCP-Drones/drones/systems/deliverydron/bus/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/config/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/motors/src"
)

func main() {
	cfg := config.FromEnv()
	log.Printf("[%s] motors broker_type=%s topic=%s", cfg.ComponentID, cfg.BrokerType, cfg.ComponentTopic)

	b, err := bus.New(cfg)
	if err != nil {
		log.Fatalf("bus: %v", err)
	}

	comp := motors.New(cfg, b)
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
