package docs

import (
	_ "embed"
	"fmt"
	"sort"
	"strings"

	"m31labs.dev/gosx"
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/scene/capability"
	"m31labs.dev/gosx/scene/schema"
)

//go:embed probe.js
var capabilityProbeScript string

const capabilitySourceBase = "https://github.com/odvcencio/gosx/tree/main/"

var capabilityBackendOrder = []capability.Backend{
	capability.BackendWebGPU,
	capability.BackendWebGL,
	capability.BackendCanvas2D,
}

var capabilityFeatureLabels = map[capability.Feature]string{
	capability.FeatureSkinning:                  "Skinning",
	capability.FeatureIBL:                       "Image-based lighting",
	capability.FeatureEnvironmentMap:            "Environment map",
	capability.FeatureGPUPicking:                "GPU picking",
	capability.FeatureLineDashed:                "Dashed lines",
	capability.FeatureComputeParts:              "Compute particles",
	capability.FeatureGPUCull:                   "GPU culling",
	capability.FeatureWaterSim:                  "Water simulation",
	capability.FeatureWaterObjectTexturePass:    "Water object texture pass",
	capability.FeatureWaterObjectMeshShadowPass: "Water object mesh shadow pass",
	capability.FeatureRectAreaLight:             "Rect-area light shape",
	capability.FeatureRectAreaSpecular:          "Rect-area specular lobe",
	capability.FeatureLightProbeSH:              "Light probe spherical harmonics",
	capability.FeatureSkyEnvironment:            "Environment sky",
	capability.FeatureSkyGradient:               "Gradient sky",
	capability.FeatureSkyPhysical:               "Physical sky",
}

// These reasons summarize the implementation recorded in capability.Matrix
// and its renderer corroboration. Keep one reason for every backend cell.
var capabilityCellReasons = map[capability.Feature]map[capability.Backend]string{
	capability.FeatureSkinning: {
		capability.BackendWebGPU:   "The compute pass skins positions, normals, and tangents.",
		capability.BackendWebGL:    "The vertex shader skins positions, normals, and tangents.",
		capability.BackendCanvas2D: "Canvas2D draws line and point data, not triangle meshes.",
	},
	capability.FeatureIBL: {
		capability.BackendWebGPU:   "The renderer binds irradiance, radiance, and BRDF lookup textures.",
		capability.BackendWebGL:    "The renderer samples the IBL products within its core texture budget.",
		capability.BackendCanvas2D: "Canvas2D draws no lit mesh surfaces to receive image-based lighting.",
	},
	capability.FeatureEnvironmentMap: {
		capability.BackendWebGPU:   "The shader samples the authored equirectangular map when IBL is inactive.",
		capability.BackendWebGL:    "The shader samples the authored environment map after the IBL branch.",
		capability.BackendCanvas2D: "Canvas2D draws no lit mesh surfaces to receive an environment map.",
	},
	capability.FeatureGPUPicking: {
		capability.BackendWebGPU:   "An integer ID pass identifies the picked mesh.",
		capability.BackendWebGL:    "The shared raycast helpers return the same mesh hit contract.",
		capability.BackendCanvas2D: "Canvas2D has no GPU ID pass or Scene3D mesh picker.",
	},
	capability.FeatureLineDashed: {
		capability.BackendWebGPU:   "Dashed lines are refused instead of being drawn as solid lines.",
		capability.BackendWebGL:    "The WebGL2 line shader has no dash pattern path.",
		capability.BackendCanvas2D: "Canvas2D applies the authored dash and gap with setLineDash.",
	},
	capability.FeatureComputeParts: {
		capability.BackendWebGPU:   "The authored particle kernel runs in a GPU compute pass.",
		capability.BackendWebGL:    "A bounded CPU mirror moves particles but cannot run authored WGSL.",
		capability.BackendCanvas2D: "Canvas2D has no compute stage for Scene3D particle systems.",
	},
	capability.FeatureGPUCull: {
		capability.BackendWebGPU:   "Compute culling builds the visible instance list before drawing.",
		capability.BackendWebGL:    "The renderer has no GPU compute culling path.",
		capability.BackendCanvas2D: "Canvas2D does not draw instanced triangle meshes.",
	},
	capability.FeatureWaterSim: {
		capability.BackendWebGPU:   "Compute passes advance the authored water height field.",
		capability.BackendWebGL:    "Ping-pong fragment passes advance the authored water height field.",
		capability.BackendCanvas2D: "Canvas2D does not simulate the Scene3D water height field.",
	},
	capability.FeatureWaterObjectTexturePass: {
		capability.BackendWebGPU:   "The renderer updates object textures used by water shading.",
		capability.BackendWebGL:    "The renderer updates object textures used by water shading.",
		capability.BackendCanvas2D: "Canvas2D has no water object texture pass.",
	},
	capability.FeatureWaterObjectMeshShadowPass: {
		capability.BackendWebGPU:   "The pass rasterizes the caster mesh geometry.",
		capability.BackendWebGL:    "The pass uses analytic shapes and does not rasterize caster meshes.",
		capability.BackendCanvas2D: "Canvas2D does not render Scene3D water shadows.",
	},
	capability.FeatureRectAreaLight: {
		capability.BackendWebGPU:   "The shader evaluates the rectangle's analytic light shape.",
		capability.BackendWebGL:    "The light is lowered as a point and loses its rectangle dimensions.",
		capability.BackendCanvas2D: "Canvas2D does not shade light sources on meshes.",
	},
	capability.FeatureRectAreaSpecular: {
		capability.BackendWebGPU:   "A representative-point lobe does not match the fitted specular shape.",
		capability.BackendWebGL:    "WebGL2 has no rectangle-light shading path.",
		capability.BackendCanvas2D: "Canvas2D does not shade specular lobes on meshes.",
	},
	capability.FeatureLightProbeSH: {
		capability.BackendWebGPU:   "Probe coefficients are lowered but the shader does not evaluate them.",
		capability.BackendWebGL:    "Probe coefficients are lowered but the shader does not evaluate them.",
		capability.BackendCanvas2D: "Canvas2D does not shade spherical-harmonic probes.",
	},
	capability.FeatureSkyEnvironment: {
		capability.BackendWebGPU:   "The renderer draws the authored environment as a sky.",
		capability.BackendWebGL:    "The renderer draws the authored environment as a sky.",
		capability.BackendCanvas2D: "Canvas2D keeps a flat clear color instead of an environment sky.",
	},
	capability.FeatureSkyGradient: {
		capability.BackendWebGPU:   "The renderer draws the authored gradient as a sky.",
		capability.BackendWebGL:    "The renderer draws the authored gradient as a sky.",
		capability.BackendCanvas2D: "Canvas2D keeps a flat clear color instead of a gradient sky.",
	},
	capability.FeatureSkyPhysical: {
		capability.BackendWebGPU:   "The renderer draws analytic Rayleigh and Mie scattering with a sun disk.",
		capability.BackendWebGL:    "The renderer draws analytic Rayleigh and Mie scattering with a sun disk.",
		capability.BackendCanvas2D: "Canvas2D keeps a flat clear color; the server fills matching gradient stops.",
	},
}

type capabilityPageCell struct {
	Backend      string
	BackendLabel string
	Supported    bool
	Status       string
	Reason       string
	SourceURL    string
}

type capabilityPageRow struct {
	Feature    string
	Label      string
	LightKinds string
	WebGPU     capabilityPageCell
	WebGL2     capabilityPageCell
	Canvas2D   capabilityPageCell
}

func init() {
	docsapp.RegisterStaticDocsPage(
		"Scene3D capabilities",
		"A source-linked record of Scene3D feature support and browser backend selection.",
		route.FileModuleOptions{
			Load: func(_ *route.RouteContext, _ route.FilePage) (any, error) {
				return map[string]any{
					"rows":        capabilityPageRows(),
					"probeScript": gosx.RawHTML("<script>" + capabilityProbeScript + "</script>"),
				}, nil
			},
		},
	)
}

func capabilityPageRows() []capabilityPageRow {
	features := make([]capability.Feature, 0, len(capability.Matrix))
	for feature := range capability.Matrix {
		features = append(features, feature)
	}
	sort.Slice(features, func(i, j int) bool { return features[i] < features[j] })

	lightKinds := make(map[capability.Feature][]string)
	for _, kind := range schema.LightKinds() {
		for _, feature := range capability.LightKindFeatures(kind) {
			lightKinds[feature] = append(lightKinds[feature], kind)
		}
	}

	rows := make([]capabilityPageRow, 0, len(features))
	for _, feature := range features {
		label := capabilityFeatureLabels[feature]
		if label == "" {
			label = strings.ReplaceAll(string(feature), "-", " ")
			label = strings.ToUpper(label[:1]) + label[1:]
		}
		kinds := lightKinds[feature]
		kindLabel := ""
		if len(kinds) > 0 {
			for i := range kinds {
				kinds[i] = strings.ReplaceAll(kinds[i], "-", " ")
				kinds[i] = strings.ToUpper(kinds[i][:1]) + kinds[i][1:]
			}
			kindLabel = "Used by " + strings.Join(kinds, ", ")
		}

		row := capabilityPageRow{
			Feature:    string(feature),
			Label:      label,
			LightKinds: kindLabel,
		}
		for _, backend := range capabilityBackendOrder {
			supported := capability.Supports(backend, feature)
			reason := capabilityCellReasons[feature][backend]
			if reason == "" {
				panic(fmt.Sprintf("missing capability reason for %s on %s", feature, backend))
			}
			status := "Feature missing"
			if supported {
				status = "Supported"
			}
			cell := capabilityPageCell{
				Backend:      string(backend),
				BackendLabel: capabilityBackendLabel(backend),
				Supported:    supported,
				Status:       status,
				Reason:       reason,
				SourceURL:    capabilitySourceBase + capabilityRendererSource(backend),
			}
			switch backend {
			case capability.BackendWebGPU:
				row.WebGPU = cell
			case capability.BackendWebGL:
				row.WebGL2 = cell
			case capability.BackendCanvas2D:
				row.Canvas2D = cell
			}
		}
		rows = append(rows, row)
	}
	return rows
}

func capabilityBackendLabel(backend capability.Backend) string {
	switch backend {
	case capability.BackendWebGPU:
		return "WebGPU"
	case capability.BackendWebGL:
		return "WebGL2"
	case capability.BackendCanvas2D:
		return "Canvas2D"
	default:
		return string(backend)
	}
}

func capabilityRendererSource(backend capability.Backend) string {
	switch backend {
	case capability.BackendWebGPU, capability.BackendWebGL:
		return "client/runtime/scene3d/"
	case capability.BackendCanvas2D:
		return "client/js/bootstrap-src/"
	default:
		return ""
	}
}
