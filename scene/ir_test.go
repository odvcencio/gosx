package scene

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

const canonicalIRGolden = `{
  "version": 1,
  "camera": {
    "kind": "perspective",
    "y": 1.5,
    "z": 6,
    "fov": 60,
    "near": 0.1,
    "far": 128
  },
  "environment": {
    "background": "#08151f",
    "ambientColor": "#f4fbff",
    "ambientIntensity": 0.2,
    "fogColor": "#050008",
    "fogDensity": 0.001
  },
  "materials": [
    {
      "name": "steel",
      "kind": "standard",
      "color": "#8de1ff",
      "roughness": 0.42,
      "metalness": 0.8
    }
  ],
  "lights": [
    {
      "id": "sun",
      "kind": "directional",
      "color": "#fff1d6",
      "intensity": 1.2,
      "directionX": 0.3,
      "directionY": -1,
      "directionZ": -0.4,
      "castShadow": true,
      "shadowSize": 1024
    }
  ],
  "nodes": [
    {
      "kind": "mesh",
      "id": "hero",
      "transform": {
        "y": 0.25,
        "scaleX": 1,
        "scaleY": 1,
        "scaleZ": 1
      },
      "mesh": {
        "kind": "box",
        "width": 1.8,
        "height": 1.2,
        "depth": 0.8,
        "castShadow": true,
        "receiveShadow": true
      }
    },
    {
      "kind": "points",
      "id": "stars",
      "transform": {
        "spinY": 0.1,
        "scaleX": 1,
        "scaleY": 1,
        "scaleZ": 1
      },
      "points": {
        "count": 2,
        "positions": [0, 0, 0, 1, 1, 1],
        "size": 0.5,
        "color": "#ffffff",
        "blendMode": "additive"
      }
    }
  ],
  "postFX": [
    {
      "kind": "bloom",
      "threshold": 0.8,
      "intensity": 1.1
    }
  ]
}`

func TestIRGoldenRoundTrip(t *testing.T) {
	var ir IR
	if err := json.Unmarshal([]byte(canonicalIRGolden), &ir); err != nil {
		t.Fatalf("unmarshal canonical IR: %v", err)
	}
	if err := ir.Validate(); err != nil {
		t.Fatalf("validate canonical IR: %v", err)
	}

	got, err := json.Marshal(ir)
	if err != nil {
		t.Fatalf("marshal canonical IR: %v", err)
	}

	var gotTree any
	if err := json.Unmarshal(got, &gotTree); err != nil {
		t.Fatalf("unmarshal marshaled IR: %v", err)
	}
	var wantTree any
	if err := json.Unmarshal([]byte(canonicalIRGolden), &wantTree); err != nil {
		t.Fatalf("unmarshal golden IR: %v", err)
	}
	if !reflect.DeepEqual(gotTree, wantTree) {
		t.Fatalf("canonical IR roundtrip mismatch\n got: %s\nwant: %s", got, canonicalIRGolden)
	}
}

func TestIRValidateRejectsBrokenNode(t *testing.T) {
	ir := IR{
		Version: IRVersion,
		Camera:  IRCamera{Near: 1, Far: 0.5},
		Nodes: []IRNode{
			{Kind: "mesh"},
			{Kind: "points", Points: &IRPointsNode{Count: -1}},
		},
	}
	err := ir.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	for _, want := range []string{
		"camera.far must be greater than camera.near",
		"nodes[0] must set exactly one payload",
		"nodes[0].mesh is required",
		"nodes[1].points.count must be non-negative",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected validation error %q in %q", want, err.Error())
		}
	}
}

func TestPropsCanonicalIRValidates(t *testing.T) {
	props := Props{
		Width:      640,
		Height:     360,
		Background: "#08151f",
		Camera: PerspectiveCamera{
			Position: Vec3(0, 0.4, 6),
			FOV:      62,
			Near:     0.15,
			Far:      64,
		},
		Environment: Environment{
			AmbientColor:     "#f4fbff",
			AmbientIntensity: 0.22,
		},
		Graph: NewGraph(
			DirectionalLight{
				ID:        "sun",
				Color:     "#fff1d6",
				Intensity: 1.2,
				Direction: Vec3(0.4, -1, -0.3),
			},
			Mesh{
				ID:       "hero",
				Geometry: BoxGeometry{Width: 1.8, Height: 1.2, Depth: 0.8},
				Material: FlatMaterial{Color: "#8de1ff"},
				Position: Vec3(0, 0.2, 0),
			},
		),
	}

	ir := props.CanonicalIR()
	if err := ir.Validate(); err != nil {
		t.Fatalf("canonical props IR did not validate: %v", err)
	}
	if ir.Version != IRVersion {
		t.Fatalf("expected IR version %d, got %d", IRVersion, ir.Version)
	}
	if len(ir.Nodes) != 1 || ir.Nodes[0].Kind != "mesh" {
		t.Fatalf("expected one mesh node, got %#v", ir.Nodes)
	}
	if len(ir.Lights) != 1 || ir.Lights[0].Kind != "directional" {
		t.Fatalf("expected one directional light, got %#v", ir.Lights)
	}
}

func TestPropsCanonicalIRValidatesEveryNodeKind(t *testing.T) {
	props := Props{
		Camera: PerspectiveCamera{Position: Vec3(0, 0, 6), FOV: 70, Near: 0.05, Far: 128},
		Graph: NewGraph(
			Mesh{
				ID:       "mesh",
				Geometry: BoxGeometry{Width: 1, Height: 1, Depth: 1},
				Material: StandardMaterial{Color: "#8de1ff", Roughness: 0.42, Metalness: 0.1},
			},
			Points{
				ID:        "points",
				Count:     2,
				Positions: []Vector3{Vec3(0, 0, 0), Vec3(1, 1, 1)},
				Color:     "#ffffff",
				Size:      0.5,
			},
			InstancedMesh{
				ID:        "instances",
				Count:     1,
				Geometry:  SphereGeometry{Radius: 0.5, Segments: 12},
				Material:  StandardMaterial{Color: "#5eead4", Roughness: 0.3},
				Positions: []Vector3{Vec3(0, 0, 0)},
				Scales:    []Vector3{Vec3(1, 1, 1)},
			},
			ComputeParticles{
				ID:    "compute",
				Count: 8,
				Emitter: ParticleEmitter{
					Kind:   "sphere",
					Radius: 1,
				},
				Material: ParticleMaterial{Color: "#ffffff", Size: 1},
			},
			Sprite{
				ID:     "sprite",
				Src:    "/sprite.png",
				Width:  32,
				Height: 32,
			},
			Label{
				ID:   "label",
				Text: "Hello",
			},
		),
	}

	ir := props.CanonicalIR()
	if err := ir.Validate(); err != nil {
		t.Fatalf("canonical all-node IR did not validate: %v", err)
	}
	if err := ValidateCapabilities(ir, []string{"webgl2"}); err != nil {
		t.Fatalf("expected webgl2 to satisfy compute node capability: %v", err)
	}
	kinds := map[string]bool{}
	for _, node := range ir.Nodes {
		kinds[node.Kind] = true
	}
	for _, kind := range []string{"mesh", "points", "instanced-mesh", "compute-particles", "sprite", "label"} {
		if !kinds[kind] {
			t.Fatalf("expected canonical IR to include %s node, got %#v", kind, ir.Nodes)
		}
	}
}

func TestScene3DElementLoweringAndCapabilityGate(t *testing.T) {
	ir := LowerScene3D(Scene3DProps{
		Camera: IRCamera{Kind: "perspective", Z: 6, FOV: 72, Near: 0.05, Far: 128},
		Materials: []IRMaterial{
			{Name: "steel", Kind: "standard", Color: "#8de1ff"},
		},
		Nodes: []IRNode{
			LowerMesh(MeshElementProps{
				ID:            "hero",
				MaterialIndex: 0,
				Kind:          "box",
				Width:         1.8,
				Height:        1.2,
				Depth:         0.8,
			}),
			LowerComputeParticles(ComputeParticlesElementProps{
				ID:    "gpu-dust",
				Count: 128,
				Emitter: IRParticleEmitter{
					Kind: "sphere",
				},
			}),
		},
	})
	if err := ir.Validate(); err != nil {
		t.Fatalf("lowered element IR did not validate: %v", err)
	}
	if err := ValidateCapabilities(ir, []string{"canvas"}); err == nil {
		t.Fatal("expected compute-particles capability gate to fail")
	}
	if err := ValidateCapabilities(ir, []string{"webgl2"}); err != nil {
		t.Fatalf("expected webgl2 capability to satisfy compute gate: %v", err)
	}

	generic := IR{
		Version: IRVersion,
		Nodes: []IRNode{
			{
				Kind:         "mesh",
				ID:           "special",
				Capabilities: []string{"raytrace"},
				Mesh:         &IRMeshNode{Kind: "box"},
			},
		},
	}
	if err := ValidateCapabilities(generic, []string{"canvas"}); err == nil {
		t.Fatal("expected generic node capability gate to fail")
	}
	if err := ValidateCapabilities(generic, []string{"raytrace"}); err != nil {
		t.Fatalf("expected explicit raytrace capability to pass: %v", err)
	}
}
