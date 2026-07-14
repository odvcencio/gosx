package harness_test

import (
	"bytes"
	"math"
	"strings"
	"testing"

	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/scene/harness"
	"m31labs.dev/gosx/scene/preview"
)

func TestSessionRendersTracesAndValidatesSelenaTransport(t *testing.T) {
	material, _, err := scene.CompileSelenaMaterial([]byte(`
material HarnessMaterial {
  param tint : color = rgb(0.2, 0.8, 0.6)
  surface(geo) -> color { return vec4f(tint.rgb, 1.0) }
}`), scene.SelenaMaterialOptions{Material: "HarnessMaterial", Standard: scene.StandardMaterial{Color: "#33cc99", Roughness: 0.3}})
	if err != nil {
		t.Fatal(err)
	}
	props := scene.Props{
		Background: "#030507",
		Camera:     scene.PerspectiveCamera{Position: scene.Vec3(0, 2, 5), FOV: 48, Near: 0.1, Far: 30},
		Graph: scene.NewGraph(scene.InstancedMesh{
			ID: "tokens", Count: 2, Geometry: scene.SphereGeometry{Radius: 0.5, Segments: 12}, Material: material,
			Positions: []scene.Vector3{scene.Vec3(0, 0, 0), scene.Vec3(2, 0, 0)},
		}),
	}
	session := harness.New(props, preview.Options{Width: 96, Height: 64, DisableShadows: true, DisablePostFX: true})
	if _, err := session.Render(0); err != nil {
		t.Fatal(err)
	}
	trace := session.Trace("center token", scene.Ray{Origin: scene.Vec3(0, 3, 0), Direction: scene.Vec3(0, -1, 0)})
	if trace.Closest == nil || trace.Closest.InstanceIndex == nil || *trace.Closest.InstanceIndex != 0 {
		t.Fatalf("native trace missed center token: %#v", trace)
	}
	pointerTrace := session.TracePointer("center token pointer", 48, 32, 96, 64, scene.OrbitCameraForTarget(scene.PerspectiveCamera{
		Position: scene.Vec3(0, 2, 5), FOV: 48,
	}, scene.Vec3(0, 0, 0)))
	if pointerTrace.Closest == nil {
		t.Fatal("native pointer trace missed the center token")
	}
	drag := session.OrbitDrag("reference grab", scene.OrbitState{}, scene.OrbitDragInput{
		DeltaX: 12, DeltaY: 4, RotateMode: scene.ControlRotateModePixelDegrees,
		RotateDirection: scene.ControlRotateDirectionGrab,
	})
	if drag.DeltaYaw >= 0 || drag.DeltaPitch <= 0 {
		t.Fatalf("native interaction certificate detected inverted drag: %#v", drag)
	}
	objectDrag := session.ObjectDrag("selected sphere", scene.ObjectDragState{
		Position:    scene.Vec3(-0.4, -0.75, 0.2),
		PreviousHit: scene.Vec3(-0.4, -0.5, 0.2),
		PlaneNormal: scene.Vec3(0, 0, 1),
	}, scene.ObjectDragInput{
		Ray: scene.Ray{Origin: scene.Vec3(-0.2, -0.4, -2), Direction: scene.Vec3(0, 0, 1)},
		Bounds: scene.ObjectDragBounds{
			Width: 1, Height: 1, Length: 1,
			XLimitRadius: 0.25, ZLimitRadius: 0.25, FloorClearance: 0.25,
		},
	})
	position := objectDrag.After.Position
	if !objectDrag.Applied || math.Abs(position.X+0.2) > 1e-12 || math.Abs(position.Y+0.65) > 1e-12 || math.Abs(position.Z-0.2) > 1e-12 {
		t.Fatalf("native object manipulation certificate = %#v", objectDrag)
	}
	if err := session.Validate(); err != nil {
		t.Fatal(err)
	}
	report := session.Report()
	if !report.Valid || len(report.Materials) != 1 || !report.Materials[0].Valid {
		t.Fatalf("Selena evidence = %#v", report.Materials)
	}
	if len(report.Events) != 5 || report.Events[0].Frame.PNGHash == "" || report.Events[0].Frame.Coverage <= 0 {
		t.Fatalf("interactive telemetry = %#v", report.Events)
	}
	var encoded bytes.Buffer
	if err := session.WriteJSON(&encoded); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(encoded.String(), `"schema": "gosx.scene3d.harness/v1"`) ||
		!strings.Contains(encoded.String(), `"instanceIndex": 0`) ||
		!strings.Contains(encoded.String(), `"kind": "orbit-drag"`) ||
		!strings.Contains(encoded.String(), `"kind": "object-drag"`) ||
		!strings.Contains(encoded.String(), `"applied": true`) {
		t.Fatalf("agent report missing contracts:\n%s", encoded.String())
	}
}

func TestSessionCertifiesWaterSemanticsWithoutBrowser(t *testing.T) {
	const shader = "compiled Selena artifact"
	props := scene.Props{
		Background: "#02070b", Controls: scene.ControlOrbit, ControlTarget: scene.Vec3(0, -0.5, 0),
		Camera: scene.PerspectiveCamera{Position: scene.Vec3(1.2695827068526726, 1.1904730469627978, 3.395653196065958), FOV: 45, Near: 0.01, Far: 100},
		Graph: scene.NewGraph(scene.WaterSystem{
			ID: "water-main", Resolution: 256, SurfaceResolution: 201,
			PoolWidth: 1, PoolHeight: 1, PoolLength: 1,
			AboveWaterColor:    scene.Vec3(0.25, 1, 1.25),
			CausticsResolution: 1024, ObjectShadowResolution: 1024,
			ObjectTextureResolutionMode: "viewport", ObjectTexturePixelBudget: 786432,
			Caustics: true, Reflection: true, Refraction: true, LightDirection: scene.Vec3(2, 2, -1),
			SeedSelenaWGSL: shader, DropSelenaWGSL: shader, DisplacementSelenaWGSL: shader,
			SimulationSelenaWGSL: shader, NormalSelenaWGSL: shader, PoolSelenaWGSL: shader,
			SurfaceSelenaWGSL: shader, SurfaceBelowSelenaWGSL: shader, CausticsSelenaWGSL: shader,
			ObjectShadowSelenaWGSL: shader, CompoundShadowSelenaWGSL: shader, ObjectMeshShadowSelenaWGSL: shader,
		})}
	session := harness.New(props, preview.Options{Width: 160, Height: 100, DisableShadows: true, DisablePostFX: true})
	if _, err := session.Render(0); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Render(0.75); err != nil {
		t.Fatal(err)
	}
	session.Trace("water center", scene.Ray{Origin: scene.Vec3(0, 2, 0), Direction: scene.Vec3(0, -1, 0)})
	session.OrbitDrag("water grab", scene.OrbitState{}, scene.OrbitDragInput{
		DeltaX: 10, DeltaY: 5, RotateMode: scene.ControlRotateModePixelDegrees, RotateDirection: scene.ControlRotateDirectionGrab,
	})
	session.ObjectDrag("water sphere", scene.ObjectDragState{
		Position: scene.Vec3(-0.4, -0.75, 0.2), PreviousHit: scene.Vec3(-0.4, -0.5, 0.2), PlaneNormal: scene.Vec3(0, 0, 1),
	}, scene.ObjectDragInput{
		Ray:    scene.Ray{Origin: scene.Vec3(-0.2, -0.4, -2), Direction: scene.Vec3(0, 0, 1)},
		Bounds: scene.ObjectDragBounds{Width: 1, Height: 1, Length: 1, XLimitRadius: 0.25, ZLimitRadius: 0.25, FloorClearance: 0.25},
	})
	evidence := session.CertifyWater("reference parity", harness.WaterContract{
		SystemID: "water-main", SimulationResolution: 256, SurfaceResolution: 201,
		CausticsResolution: 1024, ObjectShadowResolution: 1024,
		RequireCaustics: true, RequireReflection: true, RequireRefraction: true,
		RequireSelenaArtifacts: true, RequireRayTrace: true, RequireOrbitDrag: true, RequireObjectDrag: true,
		ExpectedLightDirection: scene.Vec3(2, 2, -1), ExpectedAboveWaterColor: scene.Vec3(0.25, 1, 1.25),
		MinCoverage: 0.04, MinUniqueColors: 12, MinTemporalPixels: 40,
		MinLuminanceVariance: 0.0001, MinEdgeEnergy: 0.0001,
	})
	if !evidence.Valid || evidence.SurfaceVertices != 240000 {
		t.Fatalf("water certificate = %#v", evidence)
	}
	if !evidence.Interaction.Valid || evidence.Interaction.AppliedObjectDrags != 1 {
		t.Fatalf("water interaction evidence = %#v", evidence.Interaction)
	}
	if evidence.CellSizeX != 0.0078125 || evidence.CellSizeZ != 0.0078125 {
		t.Fatalf("physical cell spacing = (%v,%v)", evidence.CellSizeX, evidence.CellSizeZ)
	}
	if err := session.Validate(); err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := session.WriteJSON(&encoded); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"kind": "water-certification"`, `"surfaceVertices": 240000`, `"cellSizeX": 0.0078125`, `"name": "normal"`, `"appliedObjectDrags": 1`} {
		if !strings.Contains(encoded.String(), want) {
			t.Fatalf("water agent report missing %s:\n%s", want, encoded.String())
		}
	}
}
