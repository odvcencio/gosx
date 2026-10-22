package scene

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPropsWebGPURecoveryLowers(t *testing.T) {
	reload := true
	p := Props{WebGPURecovery: &WebGPURecovery{Strikes: 3, Reload: &reload}}
	data, err := json.Marshal(p) // goes through Props.MarshalJSON -> legacyBaseProps
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"webgpuRecovery":{"strikes":3,"reload":true}`) {
		t.Fatalf("lowering: %s", data)
	}
	plain, _ := json.Marshal(Props{})
	if strings.Contains(string(plain), "webgpuRecovery") {
		t.Fatalf("nil must be omitted: %s", plain)
	}
}
