package tests

import (
	"context"
	"testing"

	"github.com/AMCP-Drones/drones/systems/deliverydron/bus/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/component/src"
	navigation "github.com/AMCP-Drones/drones/systems/deliverydron/navigation/src"
	securitymonitor "github.com/AMCP-Drones/drones/systems/deliverydron/security_monitor/src"
	"github.com/AMCP-Drones/drones/systems/deliverydron/tests/testutil"
)

func TestModule_Navigation_UpdateConfig_HappyAndInvalid(t *testing.T) {
	ctx := context.Background()
	mem := testutil.NewMemoryBus()
	cfg := testutil.Config("navigation")
	n := navigation.New(cfg, mem)
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = n.Stop(ctx) })

	invalidResp, err := mem.Request(ctx, cfg.BrokerTopicFor("navigation"), map[string]interface{}{
		"action": "update_config", "sender": "security_monitor", "payload": "bad",
	}, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	invalidPayload, _ := invalidResp["payload"].(map[string]interface{})
	if invalidPayload["ok"] != false {
		t.Fatalf("expected invalid payload rejection, got %#v", invalidPayload)
	}

	okResp, err := mem.Request(ctx, cfg.BrokerTopicFor("navigation"), map[string]interface{}{
		"action": "update_config",
		"sender": "security_monitor",
		"payload": map[string]interface{}{
			"hdop": 1.2,
			"fix":  2.0,
		},
	}, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	okPayload, _ := okResp["payload"].(map[string]interface{})
	if okPayload["ok"] != true {
		t.Fatalf("expected successful update, got %#v", okPayload)
	}
}

func TestUnit_Component_Request_SuccessAndRemoteError(t *testing.T) {
	ctx := context.Background()
	mem := testutil.NewMemoryBus()
	c := component.NewBaseComponent("client", "base", "client.topic", mem)
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Stop(ctx) })

	_ = mem.Subscribe(ctx, "remote.topic", func(msg map[string]interface{}) {
		act, _ := msg["action"].(string)
		if act == "ok_action" {
			_ = bus.Respond(ctx, mem, msg, map[string]interface{}{"result": "ok"}, "remote", true, "")
			return
		}
		_ = bus.Respond(ctx, mem, msg, map[string]interface{}{}, "remote", false, "boom")
	})

	okPayload, err := c.Request(ctx, "remote.topic", "ok_action", map[string]interface{}{}, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	if okPayload["result"] != "ok" {
		t.Fatalf("unexpected payload: %#v", okPayload)
	}

	_, err = c.Request(ctx, "remote.topic", "bad_action", map[string]interface{}{}, 1.0)
	if err == nil {
		t.Fatal("expected remote error")
	}
}

func TestModule_SecurityMonitor_ParsePolicies_FromMultipleFormats(t *testing.T) {
	ctx := context.Background()
	mem := testutil.NewMemoryBus()
	cfg := testutil.Config("security_monitor")

	t.Setenv("SECURITY_POLICIES", `[{"sender":"a","topic":"`+cfg.BrokerTopicFor("motors")+`","action":"LAND"},["b","`+cfg.BrokerTopicFor("cargo")+`","OPEN"]]`)
	smJSON := securitymonitor.New(cfg, mem)
	if err := smJSON.Start(ctx); err != nil {
		t.Fatal(err)
	}
	respJSON, err := mem.Request(ctx, cfg.BrokerTopicFor("security_monitor"), map[string]interface{}{
		"action": "list_policies", "sender": "qa",
	}, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	pJSON, _ := respJSON["payload"].(map[string]interface{})
	if pJSON["count"] != 2 {
		t.Fatalf("expected two JSON policies, got %#v", pJSON)
	}
	_ = smJSON.Stop(ctx)

	t.Setenv("SECURITY_POLICIES", "a,"+cfg.BrokerTopicFor("motors")+",LAND;b,"+cfg.BrokerTopicFor("cargo")+",OPEN")
	smCSV := securitymonitor.New(cfg, mem)
	if err := smCSV.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = smCSV.Stop(ctx) })
	respCSV, err := mem.Request(ctx, cfg.BrokerTopicFor("security_monitor"), map[string]interface{}{
		"action": "list_policies", "sender": "qa",
	}, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	pCSV, _ := respCSV["payload"].(map[string]interface{})
	if pCSV["count"] != 2 {
		t.Fatalf("expected two CSV policies, got %#v", pCSV)
	}
}
