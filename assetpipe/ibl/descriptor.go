package ibl

// DescriptorSchemaVersion versions the backend-consumable IBL metadata block.
const DescriptorSchemaVersion = 1

// ProductRole identifies the physical quantity represented by an IBL product.
type ProductRole string

const (
	ProductRoleRadiance   ProductRole = "environment-radiance"
	ProductRoleIrradiance ProductRole = "environment-irradiance"
	ProductRoleBRDFLUT    ProductRole = "brdf-lut"
)

// ProductDescriptor contains everything a backend needs to classify and
// allocate one generated IBL texture before it parses the KTX2 payload.
type ProductDescriptor struct {
	URI        string      `json:"uri"`
	Role       ProductRole `json:"role"`
	ColorSpace string      `json:"colorSpace"`
	Channels   string      `json:"channels"`
	View       string      `json:"view"`
	Format     string      `json:"format"`
	MipLevels  int         `json:"mipLevels"`
	Width      int         `json:"width"`
	Height     int         `json:"height"`
	Faces      int         `json:"faces"`
}

// EnvironmentDescriptor is the handoff from assetpipe to SceneIR and renderer
// work. Carrying this record does not mean a shipped backend consumes it.
type EnvironmentDescriptor struct {
	SchemaVersion      int               `json:"schemaVersion"`
	Source             string            `json:"source"`
	Radiance           ProductDescriptor `json:"radiance"`
	Irradiance         ProductDescriptor `json:"irradiance"`
	BRDFLUT            ProductDescriptor `json:"brdfLUT"`
	BRDFModel          string            `json:"brdfModel"`
	RoughnessPerLevel  []float64         `json:"roughnessPerLevel"`
	SphericalHarmonics [][3]float64      `json:"sphericalHarmonics"`
}
