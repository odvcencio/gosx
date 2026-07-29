package scene

import "strings"

// TextureRole describes how texture samples participate in shading. Roles are
// explicit so a backend never has to infer transfer functions from a filename
// or material slot.
type TextureRole string

const (
	TextureRoleBaseColor             TextureRole = "base-color"
	TextureRoleEmissive              TextureRole = "emissive"
	TextureRoleNormal                TextureRole = "normal"
	TextureRoleRoughness             TextureRole = "roughness"
	TextureRoleMetalness             TextureRole = "metalness"
	TextureRoleAmbientOcclusion      TextureRole = "ambient-occlusion"
	TextureRoleData                  TextureRole = "data"
	TextureRoleEnvironmentRadiance   TextureRole = "environment-radiance"
	TextureRoleEnvironmentIrradiance TextureRole = "environment-irradiance"
	TextureRoleBRDFLUT               TextureRole = "brdf-lut"
)

// TextureColorSpace names the transfer function applied to stored texels.
type TextureColorSpace string

const (
	TextureColorSpaceSRGB   TextureColorSpace = "srgb"
	TextureColorSpaceLinear TextureColorSpace = "linear"
)

// TextureDescriptor is the backend-consumable contract for one texture.
// Existing string URI fields remain authoritative for older runtimes; this
// descriptor removes ambiguity for color-correct upload and sampling.
type TextureDescriptor struct {
	URI        string            `json:"uri"`
	Role       TextureRole       `json:"role"`
	ColorSpace TextureColorSpace `json:"colorSpace"`
	Channels   string            `json:"channels,omitempty"`
	View       string            `json:"view,omitempty"`
	Format     string            `json:"format,omitempty"`
	MipLevels  int               `json:"mipLevels,omitempty"`
	Width      int               `json:"width,omitempty"`
	Height     int               `json:"height,omitempty"`
	Faces      int               `json:"faces,omitempty"`
}

// IsZero supports json:",omitzero" on descriptor fields.
func (d TextureDescriptor) IsZero() bool {
	return strings.TrimSpace(d.URI) == ""
}

// MaterialTextureDescriptors names every standard material slot. A packed
// metallic-roughness image intentionally appears twice with different channel
// selectors because the two semantic reads are distinct.
type MaterialTextureDescriptors struct {
	BaseColor TextureDescriptor            `json:"baseColor,omitzero"`
	Normal    TextureDescriptor            `json:"normal,omitzero"`
	Roughness TextureDescriptor            `json:"roughness,omitzero"`
	Metalness TextureDescriptor            `json:"metalness,omitzero"`
	Occlusion TextureDescriptor            `json:"occlusion,omitzero"`
	Emissive  TextureDescriptor            `json:"emissive,omitzero"`
	Data      map[string]TextureDescriptor `json:"data,omitempty"`
}

// IsZero supports json:",omitzero" on material descriptor sets.
func (d MaterialTextureDescriptors) IsZero() bool {
	return d.BaseColor.IsZero() &&
		d.Normal.IsZero() &&
		d.Roughness.IsZero() &&
		d.Metalness.IsZero() &&
		d.Occlusion.IsZero() &&
		d.Emissive.IsZero() &&
		len(d.Data) == 0
}

// EnvironmentIBL describes the products of an HDR prefilter build. It is a
// transport contract only: carrying it does not claim that a renderer consumes
// the products or that the IBL capability is available.
type EnvironmentIBL struct {
	SchemaVersion      int               `json:"schemaVersion,omitempty"`
	Source             string            `json:"source,omitempty"`
	Radiance           TextureDescriptor `json:"radiance,omitzero"`
	Irradiance         TextureDescriptor `json:"irradiance,omitzero"`
	BRDFLUT            TextureDescriptor `json:"brdfLUT,omitzero"`
	BRDFModel          string            `json:"brdfModel,omitempty"`
	RoughnessPerLevel  []float64         `json:"roughnessPerLevel,omitempty"`
	SphericalHarmonics [][3]float64      `json:"sphericalHarmonics,omitempty"`
}

// IsZero supports json:",omitzero" on environment records.
func (d EnvironmentIBL) IsZero() bool {
	return strings.TrimSpace(d.Source) == "" &&
		d.SchemaVersion == 0 &&
		d.Radiance.IsZero() &&
		d.Irradiance.IsZero() &&
		d.BRDFLUT.IsZero() &&
		strings.TrimSpace(d.BRDFModel) == "" &&
		len(d.RoughnessPerLevel) == 0 &&
		len(d.SphericalHarmonics) == 0
}

func textureDescriptor(uri string, role TextureRole, colorSpace TextureColorSpace, channels string) TextureDescriptor {
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return TextureDescriptor{}
	}
	return TextureDescriptor{
		URI:        uri,
		Role:       role,
		ColorSpace: colorSpace,
		Channels:   channels,
		View:       "2d",
	}
}

func materialTextureDescriptors(texture, normal, roughness, metalness, occlusion, emissive string) MaterialTextureDescriptors {
	return MaterialTextureDescriptors{
		BaseColor: textureDescriptor(texture, TextureRoleBaseColor, TextureColorSpaceSRGB, "rgba"),
		Normal:    textureDescriptor(normal, TextureRoleNormal, TextureColorSpaceLinear, "rgb"),
		Roughness: textureDescriptor(roughness, TextureRoleRoughness, TextureColorSpaceLinear, "g"),
		Metalness: textureDescriptor(metalness, TextureRoleMetalness, TextureColorSpaceLinear, "b"),
		Occlusion: textureDescriptor(occlusion, TextureRoleAmbientOcclusion, TextureColorSpaceLinear, "r"),
		Emissive:  textureDescriptor(emissive, TextureRoleEmissive, TextureColorSpaceSRGB, "rgb"),
	}
}

func populateSceneTextureDescriptors(ir *SceneIR) {
	if ir == nil {
		return
	}
	for i := range ir.Objects {
		object := &ir.Objects[i]
		object.TextureDescriptors = materialTextureDescriptors(
			object.Texture,
			object.NormalMap,
			object.RoughnessMap,
			object.MetalnessMap,
			object.OcclusionMap,
			object.EmissiveMap,
		)
	}
	for i := range ir.Models {
		model := &ir.Models[i].ObjectIR
		model.TextureDescriptors = materialTextureDescriptors(
			model.Texture,
			model.NormalMap,
			model.RoughnessMap,
			model.MetalnessMap,
			model.OcclusionMap,
			model.EmissiveMap,
		)
	}
	for i := range ir.InstancedMeshes {
		mesh := &ir.InstancedMeshes[i]
		mesh.TextureDescriptors = materialTextureDescriptors(
			mesh.Texture,
			mesh.NormalMap,
			mesh.RoughnessMap,
			mesh.MetalnessMap,
			mesh.OcclusionMap,
			mesh.EmissiveMap,
		)
	}
}

func legacyTextureDescriptors(value MaterialTextureDescriptors) map[string]any {
	if value.IsZero() {
		return nil
	}
	out := map[string]any{}
	add := func(name string, descriptor TextureDescriptor) {
		if !descriptor.IsZero() {
			out[name] = descriptor
		}
	}
	add("baseColor", value.BaseColor)
	add("normal", value.Normal)
	add("roughness", value.Roughness)
	add("metalness", value.Metalness)
	add("occlusion", value.Occlusion)
	add("emissive", value.Emissive)
	if len(value.Data) > 0 {
		out["data"] = value.Data
	}
	return out
}

func normalizeEnvironmentIBL(value EnvironmentIBL) EnvironmentIBL {
	value.Source = strings.TrimSpace(value.Source)
	value.BRDFModel = strings.TrimSpace(value.BRDFModel)
	value.Radiance = normalizeEnvironmentProduct(value.Radiance, TextureRoleEnvironmentRadiance, "cube", "rgba")
	value.Irradiance = normalizeEnvironmentProduct(value.Irradiance, TextureRoleEnvironmentIrradiance, "cube", "rgba")
	value.BRDFLUT = normalizeEnvironmentProduct(value.BRDFLUT, TextureRoleBRDFLUT, "2d", "rg")
	return value
}

func normalizeEnvironmentProduct(value TextureDescriptor, role TextureRole, view, channels string) TextureDescriptor {
	value.URI = strings.TrimSpace(value.URI)
	if value.URI == "" {
		return TextureDescriptor{}
	}
	value.Role = role
	value.ColorSpace = TextureColorSpaceLinear
	if strings.TrimSpace(value.View) == "" {
		value.View = view
	}
	if strings.TrimSpace(value.Channels) == "" {
		value.Channels = channels
	}
	return value
}
