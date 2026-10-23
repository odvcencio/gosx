package scene

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestDeferLowersIntoProps(t *testing.T) {
	p := Props{Defer: &Defer{Until: "load", TimeoutMS: 2500, DelayMS: 300, Public: true}}
	raw := p.RawPropsJSON()
	var got struct {
		Defer *Defer `json:"defer"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Defer == nil || got.Defer.Until != "load" || got.Defer.TimeoutMS != 2500 || got.Defer.DelayMS != 300 || !got.Defer.Public {
		t.Fatalf("defer did not lower: %s", raw)
	}
	if bytes.Contains(Props{}.RawPropsJSON(), []byte(`"defer"`)) {
		t.Fatal("a scene without Defer must not carry a defer key")
	}
	if _, ok := p.GoSXSpreadProps()["defer"]; !ok {
		t.Fatal("the GSX spread path must carry defer too")
	}
}

func TestEnvironmentGatesPassThroughRequiredCapabilities(t *testing.T) {
	p := Props{RequiredCapabilities: []string{"hardware-gpu", "full-motion", "full-data"}}
	cfg := p.EngineConfig()
	if len(cfg.RequiredCapabilities) != 3 || string(cfg.RequiredCapabilities[0]) != "hardware-gpu" {
		t.Fatalf("required capabilities = %v", cfg.RequiredCapabilities)
	}
}
