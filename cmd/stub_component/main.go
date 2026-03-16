// Stub component: runs a minimal broker subscriber with ping/get_status only.
// Used for deliverydron system placeholders (security_monitor, journal, navigation, etc.).
// Set COMPONENT_ID and optionally COMPONENT_TOPIC, SYSTEM_NAME, BROKER_TYPE, etc.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/AMCP-Drones/drones/src/bus"
	"github.com/AMCP-Drones/drones/src/component"
	"github.com/AMCP-Drones/drones/src/config"
)

func main() {
	cfg := config.FromEnv()
	log.Printf("[%s] stub broker_type=%s topic=%s", cfg.ComponentID, cfg.BrokerType, cfg.ComponentTopic)

	b, err := bus.New(cfg)
	if err != nil {
		log.Fatalf("bus: %v", err)
	}

	compType := os.Getenv("COMPONENT_TYPE")
	if compType == "" {
		compType = cfg.ComponentID
	}
	c := component.NewBaseComponent(cfg.ComponentID, compType, cfg.ComponentTopic, b)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := c.Start(ctx); err != nil {
		log.Fatalf("start: %v", err)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Printf("[%s] shutting down", cfg.ComponentID)
	cancel()
	if err := c.Stop(context.Background()); err != nil {
		log.Printf("stop: %v", err)
	}
}
