package scene3d

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"m31labs.dev/gosx/scene"
)

// clientMaterialPatch describes object material updates consumed by
// normalizeSceneObject and sceneObjectMaterialValue in the browser. A named
// material table (scene.IRMaterial) has a different shape. Record identity,
// geometry, HTML, and arbitrary replacement props are not material fields.
type clientMaterialPatch struct {
	Material            clientMaterialInput              `json:"material"`
	MaterialKind        string                           `json:"materialKind"`
	Color               string                           `json:"color"`
	Texture             string                           `json:"texture"`
	Unlit               bool                             `json:"unlit"`
	Opacity             clientMaterialNumber             `json:"opacity"`
	Emissive            clientMaterialNumber             `json:"emissive"`
	EmissiveColor       *[3]float64                      `json:"emissiveColor"`
	NormalScale         float64                          `json:"normalScale"`
	NormalUVScale       *[2]float64                      `json:"normalUVScale"`
	OcclusionStrength   float64                          `json:"occlusionStrength"`
	Roughness           clientMaterialNumber             `json:"roughness"`
	Metalness           clientMaterialNumber             `json:"metalness"`
	IOR                 float64                          `json:"ior"`
	SpecularIntensity   *float64                         `json:"specularIntensity"`
	SpecularColor       *[3]float64                      `json:"specularColor"`
	Clearcoat           clientMaterialNumber             `json:"clearcoat"`
	Sheen               clientMaterialNumber             `json:"sheen"`
	Transmission        clientMaterialNumber             `json:"transmission"`
	Iridescence         clientMaterialNumber             `json:"iridescence"`
	Anisotropy          clientMaterialNumber             `json:"anisotropy"`
	Thickness           float64                          `json:"thickness"`
	AttenuationDistance float64                          `json:"attenuationDistance"`
	AttenuationColor    *[3]float64                      `json:"attenuationColor"`
	AlphaCutoff         scene.AlphaCutoff                `json:"alphaCutoff"`
	Detail              *scene.Detail                    `json:"detail"`
	NormalMap           string                           `json:"normalMap"`
	RoughnessMap        string                           `json:"roughnessMap"`
	MetalnessMap        string                           `json:"metalnessMap"`
	OcclusionMap        string                           `json:"occlusionMap"`
	EmissiveMap         string                           `json:"emissiveMap"`
	TextureDescriptors  scene.MaterialTextureDescriptors `json:"textureDescriptors"`
	BlendMode           string                           `json:"blendMode"`
	Blend               string                           `json:"blend"`
	RenderPass          string                           `json:"renderPass"`
	Wireframe           bool                             `json:"wireframe"`
	DepthWrite          bool                             `json:"depthWrite"`
	LineDash            bool                             `json:"lineDash"`
	DashSize            float64                          `json:"dashSize"`
	GapSize             float64                          `json:"gapSize"`
	CustomVertex        string                           `json:"customVertex"`
	CustomFragment      string                           `json:"customFragment"`
	CustomVertexWGSL    string                           `json:"customVertexWGSL"`
	CustomFragmentWGSL  string                           `json:"customFragmentWGSL"`
	CustomUniforms      map[string]any                   `json:"customUniforms"`
	ShaderBackend       string                           `json:"shaderBackend"`
	ShaderLayout        map[string]any                   `json:"shaderLayout"`
	ShaderSource        string                           `json:"shaderSource"`
	ShaderSourceFiles   map[string]string                `json:"shaderSourceFiles"`
}

// These types validate the browser's union inputs without changing the raw
// patch bytes that are retained in the document and emitted to watchers.
type clientMaterialNumber struct{}

var materialCSSVar = regexp.MustCompile(`^var\(\s*--[-_a-zA-Z0-9]+\s*(?:,|\))`)

func (*clientMaterialNumber) UnmarshalJSON(data []byte) error {
	var number float64
	if err := json.Unmarshal(data, &number); err == nil {
		return nil
	}
	var reference string
	if err := json.Unmarshal(data, &reference); err != nil || !materialCSSVar.MatchString(strings.TrimSpace(reference)) {
		return fmt.Errorf("expected a material number or CSS variable")
	}
	return nil
}

type clientMaterialInput struct{}

func (*clientMaterialInput) UnmarshalJSON(data []byte) error {
	var name string
	if err := json.Unmarshal(data, &name); err == nil {
		return nil
	}
	// Nested material records use kind; top-level object patches use
	// materialKind. Decode only the supported material fields in either form.
	var material struct {
		clientMaterialPatch
		Kind     string          `json:"kind"`
		Material json.RawMessage `json:"material"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&material); err != nil {
		return err
	}
	if len(material.Material) != 0 {
		return fmt.Errorf("nested material records cannot contain another material")
	}
	return nil
}
