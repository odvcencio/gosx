package schema

import (
	"math"
	"testing"

	"m31labs.dev/gosx/scene"
)

func TestSchemaRejectsUnsupportedSpotShadowProjection(t *testing.T) {
	tests := []struct {
		name  string
		light scene.LightIR
		code  string
		path  string
	}{
		{
			name:  "wide cone",
			light: scene.LightIR{ID: "wide", Kind: "spot", CastShadow: true, Angle: math.Pi / 2},
			code:  "scene.shadow.spot_angle_unsupported",
			path:  "lights[0].angle",
		},
		{
			name:  "cascades",
			light: scene.LightIR{ID: "cascade", Kind: "spot", CastShadow: true, Angle: 0.5, ShadowCascades: 2},
			code:  "scene.shadow.spot_cascades_unsupported",
			path:  "lights[0].shadowCascades",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var report Report
			validateDocument(&report, Document{Schema: scene.SceneIRSchema, Lights: []scene.LightIR{tc.light}}, Options{})
			for _, diagnostic := range report.Diagnostics {
				if diagnostic.Code == tc.code && diagnostic.Path == tc.path && diagnostic.Severity == Error {
					return
				}
			}
			t.Fatalf("missing %s at %s: %+v", tc.code, tc.path, report.Diagnostics)
		})
	}
}

func TestSchemaAcceptsSpotShadowDefaultsAndOneMapLimit(t *testing.T) {
	for _, light := range []scene.LightIR{
		{ID: "default", Kind: "spot", CastShadow: true},
		{ID: "one", Kind: "spot", CastShadow: true, Angle: 0.5, ShadowCascades: 1},
		{ID: "wide-unshadowed", Kind: "spot", Angle: math.Pi},
	} {
		var report Report
		validateDocument(&report, Document{Schema: scene.SceneIRSchema, Lights: []scene.LightIR{light}}, Options{})
		for _, diagnostic := range report.Diagnostics {
			if diagnostic.Severity == Error {
				t.Fatalf("valid spot %#v rejected: %+v", light, report.Diagnostics)
			}
		}
	}
}
