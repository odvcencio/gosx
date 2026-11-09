package scene

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestAutoExposureLowersOnlyWhenSet(t *testing.T) {
	ir := NewGraph().SceneIR()
	if ir.Environment.AutoExposure != nil {
		t.Fatal("auto exposure must stay off by default")
	}
	env := Environment{AutoExposure: &AutoExposure{MinExposure: 4, MaxExposure: 0.5, Key: 0.12, SpeedUp: 2, Compensation: math.Inf(1)}}
	lowered := env.sceneIR()
	a := lowered.AutoExposure
	if a == nil || a.MinExposure != 0.5 || a.MaxExposure != 4 || a.Key != 0.12 || a.SpeedUp != 2 || a.Compensation != 0 {
		t.Fatalf("normalized auto exposure: %+v", a)
	}
	record := lowered.legacyProps()
	data, _ := json.Marshal(record)
	if !strings.Contains(string(data), `"autoExposure":{"minExposure":0.5,"maxExposure":4,"key":0.12,"speedUp":2}`) {
		t.Fatalf("browser record lost autoExposure: %s", data)
	}
}

func TestAutoExposureClampsCompensation(t *testing.T) {
	a := normalizeAutoExposure(&AutoExposure{Compensation: 20, SpeedDown: -1})
	if a.Compensation != 8 || a.SpeedDown != 0 {
		t.Fatalf("compensation and negative speeds: %+v", a)
	}
}
