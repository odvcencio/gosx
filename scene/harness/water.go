package harness

import (
	"fmt"
	"math"
	"strings"

	"m31labs.dev/gosx/scene"
)

// WaterContract describes observable engine semantics, not a particular demo.
// Zero expected values mean "report but do not compare".
type WaterContract struct {
	SystemID                string
	SimulationResolution    int
	SurfaceResolution       int
	CausticsResolution      int
	ObjectShadowResolution  int
	RequireCaustics         bool
	RequireReflection       bool
	RequireRefraction       bool
	RequireSelenaArtifacts  bool
	RequireRayTrace         bool
	RequireOrbitDrag        bool
	RequireObjectDrag       bool
	ExpectedLightDirection  scene.Vector3
	ExpectedAboveWaterColor scene.Vector3
	MinCoverage             float64
	MinUniqueColors         int
	MinTemporalPixels       int
	MinLuminanceVariance    float64
	MinEdgeEnergy           float64
}

type WaterTelemetry struct {
	Label                    string                   `json:"label,omitempty"`
	SystemID                 string                   `json:"systemID"`
	SimulationResolution     int                      `json:"simulationResolution"`
	SurfaceResolution        int                      `json:"surfaceResolution"`
	SurfaceVertices          int                      `json:"surfaceVertices"`
	CellSizeX                float64                  `json:"cellSizeX"`
	CellSizeZ                float64                  `json:"cellSizeZ"`
	CausticsResolution       int                      `json:"causticsResolution"`
	ObjectShadowResolution   int                      `json:"objectShadowResolution"`
	ObjectTextureMode        string                   `json:"objectTextureMode,omitempty"`
	ObjectTexturePixelBudget int                      `json:"objectTexturePixelBudget,omitempty"`
	LightDirection           WaterVector              `json:"lightDirection"`
	AboveWaterColor          WaterVector              `json:"aboveWaterColor"`
	Optics                   WaterOptics              `json:"optics"`
	Artifacts                []WaterArtifactEvidence  `json:"artifacts"`
	Visual                   *WaterVisualEvidence     `json:"visual,omitempty"`
	Interaction              WaterInteractionEvidence `json:"interaction"`
	Valid                    bool                     `json:"valid"`
	Problems                 []string                 `json:"problems,omitempty"`
}

type WaterVector struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

type WaterOptics struct {
	Caustics   bool `json:"caustics"`
	Reflection bool `json:"reflection"`
	Refraction bool `json:"refraction"`
}

type WaterArtifactEvidence struct {
	Name    string `json:"name"`
	Present bool   `json:"present"`
}

type WaterVisualEvidence struct {
	Frames                int     `json:"frames"`
	Coverage              float64 `json:"coverage"`
	UniqueColors          int     `json:"uniqueColors"`
	TemporalChangedPixels int     `json:"temporalChangedPixels"`
	LuminanceVariance     float64 `json:"luminanceVariance"`
	EdgeEnergy            float64 `json:"edgeEnergy"`
}

type WaterInteractionEvidence struct {
	RayTraces          int  `json:"rayTraces"`
	OrbitDrags         int  `json:"orbitDrags"`
	ObjectDrags        int  `json:"objectDrags"`
	AppliedObjectDrags int  `json:"appliedObjectDrags"`
	Valid              bool `json:"valid"`
}

// CertifyWater records a browser-free water-system certificate from the same
// typed SceneIR consumed by native and browser renderers. It covers physical
// normal spacing, independent simulation/render topology, optics, resource
// budgets, normalized lighting, and Selena pass completeness.
func (s *Session) CertifyWater(label string, contract WaterContract) WaterTelemetry {
	ir := s.props.SceneIR()
	var water *scene.WaterSystemIR
	for i := range ir.WaterSystems {
		if contract.SystemID == "" || ir.WaterSystems[i].ID == contract.SystemID {
			water = &ir.WaterSystems[i]
			break
		}
	}
	evidence := WaterTelemetry{Label: label, SystemID: contract.SystemID, Valid: true, Artifacts: []WaterArtifactEvidence{}}
	if water == nil {
		evidence.Valid = false
		evidence.Problems = append(evidence.Problems, "water system not found")
		s.recordWater(evidence)
		return evidence
	}
	evidence.SystemID = water.ID
	evidence.SimulationResolution = water.Resolution
	evidence.SurfaceResolution = water.SurfaceResolution
	evidence.SurfaceVertices = max(0, (water.SurfaceResolution-1)*(water.SurfaceResolution-1)*6)
	if water.Resolution > 0 {
		evidence.CellSizeX = 2 * water.PoolWidth / float64(water.Resolution)
		evidence.CellSizeZ = 2 * water.PoolLength / float64(water.Resolution)
	}
	evidence.CausticsResolution = water.CausticsResolution
	evidence.ObjectShadowResolution = water.ObjectShadowResolution
	evidence.ObjectTextureMode = water.ObjectTextureResolutionMode
	evidence.ObjectTexturePixelBudget = water.ObjectTexturePixelBudget
	evidence.LightDirection = normalizedWaterVector(water.LightDirectionX, water.LightDirectionY, water.LightDirectionZ)
	evidence.AboveWaterColor = WaterVector{X: water.AboveWaterColorR, Y: water.AboveWaterColorG, Z: water.AboveWaterColorB}
	evidence.Optics = WaterOptics{Caustics: water.Caustics, Reflection: water.Reflection, Refraction: water.Refraction}

	compareWaterInt(&evidence, "simulation resolution", contract.SimulationResolution, water.Resolution)
	compareWaterInt(&evidence, "surface resolution", contract.SurfaceResolution, water.SurfaceResolution)
	compareWaterInt(&evidence, "caustics resolution", contract.CausticsResolution, water.CausticsResolution)
	compareWaterInt(&evidence, "object shadow resolution", contract.ObjectShadowResolution, water.ObjectShadowResolution)
	if contract.RequireCaustics && !water.Caustics {
		evidence.Problems = append(evidence.Problems, "caustics disabled")
	}
	if contract.RequireReflection && !water.Reflection {
		evidence.Problems = append(evidence.Problems, "reflection disabled")
	}
	if contract.RequireRefraction && !water.Refraction {
		evidence.Problems = append(evidence.Problems, "refraction disabled")
	}
	expectedLight := normalizedWaterVector(contract.ExpectedLightDirection.X, contract.ExpectedLightDirection.Y, contract.ExpectedLightDirection.Z)
	if expectedLight != (WaterVector{}) && waterVectorDistance(evidence.LightDirection, expectedLight) > 1e-9 {
		evidence.Problems = append(evidence.Problems, fmt.Sprintf("light direction = (%g,%g,%g), want (%g,%g,%g)", evidence.LightDirection.X, evidence.LightDirection.Y, evidence.LightDirection.Z, expectedLight.X, expectedLight.Y, expectedLight.Z))
	}
	expectedColor := WaterVector{X: contract.ExpectedAboveWaterColor.X, Y: contract.ExpectedAboveWaterColor.Y, Z: contract.ExpectedAboveWaterColor.Z}
	if expectedColor != (WaterVector{}) && waterVectorDistance(evidence.AboveWaterColor, expectedColor) > 1e-9 {
		evidence.Problems = append(evidence.Problems, fmt.Sprintf("above-water color = (%g,%g,%g), want (%g,%g,%g)", evidence.AboveWaterColor.X, evidence.AboveWaterColor.Y, evidence.AboveWaterColor.Z, expectedColor.X, expectedColor.Y, expectedColor.Z))
	}
	artifacts := []struct{ name, source string }{
		{"seed", water.SeedSelenaWGSL}, {"drop", water.DropSelenaWGSL}, {"displacement", water.DisplacementSelenaWGSL},
		{"simulation", water.SimulationSelenaWGSL}, {"normal", water.NormalSelenaWGSL}, {"pool", water.PoolSelenaWGSL},
		{"surface", water.SurfaceSelenaWGSL}, {"surface-below", water.SurfaceBelowSelenaWGSL}, {"caustics", water.CausticsSelenaWGSL},
		{"object-shadow", water.ObjectShadowSelenaWGSL}, {"compound-shadow", water.CompoundShadowSelenaWGSL}, {"object-mesh-shadow", water.ObjectMeshShadowSelenaWGSL},
	}
	for _, artifact := range artifacts {
		present := strings.TrimSpace(artifact.source) != ""
		evidence.Artifacts = append(evidence.Artifacts, WaterArtifactEvidence{Name: artifact.name, Present: present})
		if contract.RequireSelenaArtifacts && !present {
			evidence.Problems = append(evidence.Problems, "missing Selena "+artifact.name+" artifact")
		}
	}
	visual := s.waterVisualEvidence()
	if visual.Frames > 0 {
		evidence.Visual = &visual
	}
	if visual.Coverage < contract.MinCoverage {
		evidence.Problems = append(evidence.Problems, fmt.Sprintf("native coverage = %g, want at least %g", visual.Coverage, contract.MinCoverage))
	}
	if visual.UniqueColors < contract.MinUniqueColors {
		evidence.Problems = append(evidence.Problems, fmt.Sprintf("native unique colors = %d, want at least %d", visual.UniqueColors, contract.MinUniqueColors))
	}
	if visual.TemporalChangedPixels < contract.MinTemporalPixels {
		evidence.Problems = append(evidence.Problems, fmt.Sprintf("native temporal pixels = %d, want at least %d", visual.TemporalChangedPixels, contract.MinTemporalPixels))
	}
	if visual.LuminanceVariance < contract.MinLuminanceVariance {
		evidence.Problems = append(evidence.Problems, fmt.Sprintf("native luminance variance = %g, want at least %g", visual.LuminanceVariance, contract.MinLuminanceVariance))
	}
	if visual.EdgeEnergy < contract.MinEdgeEnergy {
		evidence.Problems = append(evidence.Problems, fmt.Sprintf("native edge energy = %g, want at least %g", visual.EdgeEnergy, contract.MinEdgeEnergy))
	}
	evidence.Interaction = s.waterInteractionEvidence()
	if contract.RequireRayTrace && evidence.Interaction.RayTraces == 0 {
		evidence.Problems = append(evidence.Problems, "missing native ray trace evidence")
	}
	if contract.RequireOrbitDrag && evidence.Interaction.OrbitDrags == 0 {
		evidence.Problems = append(evidence.Problems, "missing native orbit drag evidence")
	}
	if contract.RequireObjectDrag && evidence.Interaction.AppliedObjectDrags == 0 {
		evidence.Problems = append(evidence.Problems, "missing applied native object drag evidence")
	}
	if water.Resolution <= 0 || water.SurfaceResolution < 2 || evidence.CellSizeX <= 0 || evidence.CellSizeZ <= 0 {
		evidence.Problems = append(evidence.Problems, "invalid physical water topology")
	}
	evidence.Valid = len(evidence.Problems) == 0
	s.recordWater(evidence)
	return evidence
}

func (s *Session) waterInteractionEvidence() WaterInteractionEvidence {
	var out WaterInteractionEvidence
	for _, event := range s.report.Events {
		if event.Trace != nil {
			out.RayTraces++
		}
		if event.Interaction == nil {
			continue
		}
		switch event.Interaction.Kind {
		case "orbit-drag":
			out.OrbitDrags++
		case "object-drag":
			out.ObjectDrags++
			if event.Interaction.ObjectDrag != nil && event.Interaction.ObjectDrag.Applied {
				out.AppliedObjectDrags++
			}
		}
	}
	out.Valid = out.RayTraces > 0 && out.OrbitDrags > 0 && out.AppliedObjectDrags > 0
	return out
}

func (s *Session) waterVisualEvidence() WaterVisualEvidence {
	var out WaterVisualEvidence
	for _, event := range s.report.Events {
		if event.Frame == nil {
			continue
		}
		out.Frames++
		out.Coverage = math.Max(out.Coverage, event.Frame.Coverage)
		out.UniqueColors = max(out.UniqueColors, event.Frame.UniqueColors)
		out.TemporalChangedPixels = max(out.TemporalChangedPixels, event.Frame.TemporalChangedPixels)
		out.LuminanceVariance = math.Max(out.LuminanceVariance, event.Frame.LuminanceVariance)
		out.EdgeEnergy = math.Max(out.EdgeEnergy, event.Frame.EdgeEnergy)
	}
	return out
}

func (s *Session) recordWater(evidence WaterTelemetry) {
	s.report.Events = append(s.report.Events, Event{Sequence: len(s.report.Events) + 1, Kind: "water-certification", Water: &evidence})
	if !evidence.Valid {
		s.problem("water certification: " + strings.Join(evidence.Problems, "; "))
	}
}

func compareWaterInt(e *WaterTelemetry, name string, want, got int) {
	if want > 0 && want != got {
		e.Problems = append(e.Problems, fmt.Sprintf("%s = %d, want %d", name, got, want))
	}
}

func normalizedWaterVector(x, y, z float64) WaterVector {
	length := math.Sqrt(x*x + y*y + z*z)
	if length == 0 {
		return WaterVector{}
	}
	return WaterVector{X: x / length, Y: y / length, Z: z / length}
}

func waterVectorDistance(a, b WaterVector) float64 {
	dx, dy, dz := a.X-b.X, a.Y-b.Y, a.Z-b.Z
	return math.Sqrt(dx*dx + dy*dy + dz*dz)
}
