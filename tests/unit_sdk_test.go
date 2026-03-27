package tests

import (
	"testing"

	"github.com/AMCP-Drones/drones/src/sdk"
)

func TestUnit_SDK_CreateResponseShape(t *testing.T) {
	t.Parallel()
	m := sdk.CreateResponse("corr-1", map[string]interface{}{"x": 1}, "srv", true, "")
	if m["action"] != "response" || m["correlation_id"] != "corr-1" || m["success"] != true {
		t.Fatalf("%#v", m)
	}
	pl, _ := m["payload"].(map[string]interface{})
	if pl["x"].(int) != 1 {
		t.Fatalf("payload %#v", pl)
	}
}
