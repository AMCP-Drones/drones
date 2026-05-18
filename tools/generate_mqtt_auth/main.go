// Command generate_mqtt_auth prints Mosquitto passwd/acl instructions from env (for local ops).
//
// Usage:
//
//	source docker/.env
//	go run ./tools/generate_mqtt_auth
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/AMCP-Drones/drones/systems/deliverydron/bus/auth"
	"github.com/AMCP-Drones/drones/systems/deliverydron/config/src"
)

func main() {
	cfg := config.FromEnv()
	spec := auth.MQTTACLSpec{
		TopicPrefix: cfg.TopicPrefix(),
		InstanceID:  cfg.InstanceID,
		Components:  config.InternalComponents,
		UsernameForComponent: func(instanceID, component string) string {
			c := *cfg
			c.ComponentID = component
			u, _ := c.BrokerCredentials()
			return u
		},
	}
	spec.External = parseExternal(os.Getenv("MQTT_EXTERNAL_USERS"))

	fmt.Print(spec.GenerateACL())
	fmt.Fprintln(os.Stderr, "--- users (mosquitto_passwd -b passwd <user> <pass>) ---")
	for _, comp := range spec.Components {
		c := *cfg
		c.ComponentID = comp
		u, p := c.BrokerCredentials()
		fmt.Fprintf(os.Stderr, "%s (%s) password=%q\n", u, comp, p)
	}
}

func parseExternal(raw string) []auth.ExternalPublisher {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var out []auth.ExternalPublisher
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, ":", 2)
		if len(kv) != 2 {
			continue
		}
		out = append(out, auth.ExternalPublisher{
			Username:  strings.TrimSpace(kv[0]),
			Component: strings.TrimSpace(kv[1]),
		})
	}
	return out
}
