package config

import (
	"os"
	"testing"
)

func TestBrokerCredentials_ComponentEnv(t *testing.T) {
	os.Clearenv()
	_ = os.Setenv("COMPONENT_ID", "autopilot")
	_ = os.Setenv("INSTANCE_ID", "Delivery001")
	_ = os.Setenv("AUTOPILOT_BROKER_USER", "ap_user")
	_ = os.Setenv("AUTOPILOT_BROKER_PASSWORD", "ap_pass")
	cfg := FromEnv()
	if cfg.BrokerUser != "ap_user" || cfg.BrokerPassword != "ap_pass" {
		t.Fatalf("got %q / %q", cfg.BrokerUser, cfg.BrokerPassword)
	}
}

func TestBrokerCredentials_DefaultUsername(t *testing.T) {
	os.Clearenv()
	_ = os.Setenv("COMPONENT_ID", "journal")
	_ = os.Setenv("INSTANCE_ID", "Delivery002")
	_ = os.Setenv("JOURNAL_BROKER_PASSWORD", "secret")
	cfg := FromEnv()
	if cfg.BrokerUser != "dd_Delivery002_journal" {
		t.Fatalf("user=%q", cfg.BrokerUser)
	}
	if cfg.BrokerPassword != "secret" {
		t.Fatalf("pass=%q", cfg.BrokerPassword)
	}
}

func TestReplyBrokerTopic(t *testing.T) {
	os.Clearenv()
	_ = os.Setenv("COMPONENT_ID", "limiter")
	cfg := FromEnv()
	want := "v1.deliverydron.Delivery001.replies.limiter"
	if cfg.ReplyBrokerTopic() != want {
		t.Fatalf("got %q want %q", cfg.ReplyBrokerTopic(), want)
	}
}
