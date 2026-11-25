package capability

// Fidelity labels: how faithfully each renderer draws each material lobe.
//
// The Matrix above answers "does this backend draw the feature at all". It
// cannot say "draws something with the lobe's name that is not the lobe". A
// scene author who compares a jersey against another engine needs that second
// answer, so every lobe carries one of three labels per backend:
//
//   - Exact: the shader evaluates the published model for that lobe from every
//     input the scene API carries for it.
//   - Approximate: the shader draws a simpler model, or drops an input the
//     author supplied. Differences from a reference renderer are expected, not
//     bugs. Why names the simplification.
//   - Unsupported: the input is ignored, or the backend has no path for it.
//
// A label is a claim about shader source, so fidelity_test.go decides each one by
// reading that source and fails when a label is raised or lowered without the
// renderer changing. Canvas2D shades no material and carries no entry; see
// FidelityOf.

// Level is one of the three labels.
type Level string

const (
	Exact       Level = "exact"
	Approximate Level = "approximate"
	Unsupported Level = "unsupported"
)

// Lobe names one material lobe or effect that carries a fidelity label.
type Lobe string

const (
	LobeClearcoat        Lobe = "clearcoat"
	LobeSheen            Lobe = "sheen"
	LobeSpecular         Lobe = "specular"
	LobeIridescence      Lobe = "iridescence"
	LobeAnisotropy       Lobe = "anisotropy"
	LobeTransmission     Lobe = "transmission"
	LobeDispersion       Lobe = "dispersion"
	LobeDiffuseTransmit  Lobe = "diffuse-transmission"
	LobeSubsurface       Lobe = "subsurface"
	LobeObjectMotionBlur Lobe = "object-motion-blur"
)

// FidelityNote is one label with a one-sentence reason an author can act on.
type FidelityNote struct {
	Level Level
	Why   string
}

// Fidelity holds the label for every lobe on each GPU backend.
var Fidelity = map[Lobe]map[Backend]FidelityNote{
	LobeClearcoat: {
		BackendWebGPU: {Approximate, "a real GGX coat lobe, but its roughness is fixed at 0.04 and there is no coat normal; glTF clearcoatRoughness is dropped on import"},
		BackendWebGL:  {Approximate, "a real GGX coat lobe, but its roughness is fixed at 0.04 and there is no coat normal; glTF clearcoatRoughness is dropped on import"},
	},
	LobeSheen: {
		BackendWebGPU: {Approximate, "a view-angle rim, not a cloth lobe: no sheen colour or roughness reaches the shader, and glTF sheen colour is reduced to its peak"},
		BackendWebGL:  {Approximate, "a view-angle rim, not a cloth lobe: no sheen colour or roughness reaches the shader, and glTF sheen colour is reduced to its peak"},
	},
	LobeSpecular: {
		BackendWebGPU: {Exact, "KHR specular factor, colour and both textures set the dielectric F0 and F90 that direct and image light share"},
		BackendWebGL:  {Exact, "KHR specular factor, colour and both textures set the dielectric F0 and F90 that direct and image light share"},
	},
	LobeIridescence: {
		BackendWebGPU: {Approximate, "a fixed cosine colour palette on the view angle, not a thin film: film IOR and thickness do not exist"},
		BackendWebGL:  {Approximate, "a fixed cosine colour palette on the view angle, not a thin film: film IOR and thickness do not exist"},
	},
	LobeAnisotropy: {
		BackendWebGPU: {Approximate, "lowers roughness by up to 28 percent with no direction: the highlight does not stretch along a tangent"},
		BackendWebGL:  {Approximate, "lowers roughness by up to 28 percent with no direction: the highlight does not stretch along a tangent"},
	},
	LobeTransmission: {
		BackendWebGPU: {Approximate, "refracts a screen-space copy of the opaque scene at one exit point; thickness ignores object scale and glass does not refract other glass"},
		BackendWebGL:  {Approximate, "refracts a screen-space copy of the opaque scene at one exit point; thickness ignores object scale and glass does not refract other glass"},
	},
	LobeDispersion: {
		BackendWebGPU: {Unsupported, "no scene input and no shader path: glass bends every colour by the same angle"},
		BackendWebGL:  {Unsupported, "no scene input and no shader path: glass bends every colour by the same angle"},
	},
	LobeDiffuseTransmit: {
		BackendWebGPU: {Unsupported, "no scene input and no shader path: thin leaves and paper do not glow when lit from behind"},
		BackendWebGL:  {Unsupported, "no scene input and no shader path: thin leaves and paper do not glow when lit from behind"},
	},
	LobeSubsurface: {
		BackendWebGPU: {Unsupported, "no scene input and no shader path: skin and wax shade as opaque surfaces"},
		BackendWebGL:  {Unsupported, "no scene input and no shader path: skin and wax shade as opaque surfaces"},
	},
	LobeObjectMotionBlur: {
		BackendWebGPU: {Approximate, "retained and skinned meshes blur along their own motion; instanced batches and crowds blur with the camera only"},
		BackendWebGL:  {Unsupported, "only camera motion blurs: an object that moves in front of a still camera stays sharp"},
	},
}

// AllLobes lists every lobe the table must cover, in display order.
var AllLobes = []Lobe{
	LobeClearcoat, LobeSheen, LobeSpecular, LobeIridescence, LobeAnisotropy,
	LobeTransmission, LobeDispersion, LobeDiffuseTransmit, LobeSubsurface,
	LobeObjectMotionBlur,
}

// FidelityOf returns the label for one lobe on one backend. Canvas2D rasterizes
// no triangle (see the blanket rule above), so it is unsupported for every lobe.
// A lobe with no entry also reads unsupported, and TestFidelityTableIsComplete
// fails if a GPU backend lacks one.
func FidelityOf(l Lobe, b Backend) FidelityNote {
	if b == BackendCanvas2D {
		return FidelityNote{Unsupported, "Canvas2D draws only lines and point sprites, so it shades no material"}
	}
	if note, ok := Fidelity[l][b]; ok {
		return note
	}
	return FidelityNote{Unsupported, "no label has been recorded for this lobe on this backend"}
}
