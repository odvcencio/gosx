package capability

// corroborationEntry names the test file that must corroborate a Matrix row
// against renderer source, and the Go identifier that names the row in this
// package's exported constants.
type corroborationEntry struct {
	// File is the test file's base name, relative to this package directory.
	File string
	// Identifier is the exported Feature constant's Go name, for example
	// "FeatureIBL". Searching for the Go identifier rather than the raw
	// string value (for example "ibl") is deliberate: the corroborating
	// tests call evidenceFor/needs/refutedBy with the constant, and a file
	// that only re-derives the raw string proves nothing about which row it
	// reads.
	Identifier string
}

// corroborationIndex names, for every row in Matrix, the test file that reads
// renderer source before asserting the cell. TestEveryMatrixRowHasCorroboration
// enforces that the file exists and mentions the identifier.
//
// The failure mode this guards against: the FeatureIBL row comment kept
// claiming the IBL samplers did not exist for some time after they landed.
// TestDriftGuard could not catch it because the Matrix cell and the WebGPU
// manifest still agreed with each other. A corroboration test that reads the
// renderer FIRST and asserts the cell SECOND catches a wrong cell regardless
// of what the manifest says. A Matrix row with no entry here — or with an
// entry naming a file that does not corroborate it — is exactly as exposed
// to the same silent drift, so add an entry (and the test it names) in the
// same commit as a new row.
var corroborationIndex = map[Feature]corroborationEntry{
	FeatureSkinning:                  {"skinning_test.go", "FeatureSkinning"},
	FeatureIBL:                       {"ibl_test.go", "FeatureIBL"},
	FeatureEnvironmentMap:            {"environmentmap_test.go", "FeatureEnvironmentMap"},
	FeatureGPUPicking:                {"gpupicking_test.go", "FeatureGPUPicking"},
	FeatureLineDashed:                {"linedashed_test.go", "FeatureLineDashed"},
	FeatureComputeParts:              {"computeparticles_test.go", "FeatureComputeParts"},
	FeatureGPUCull:                   {"gpucull_test.go", "FeatureGPUCull"},
	FeatureWaterSim:                  {"watersim_test.go", "FeatureWaterSim"},
	FeatureWaterObjectTexturePass:    {"water_shadow_test.go", "FeatureWaterObjectTexturePass"},
	FeatureWaterObjectMeshShadowPass: {"water_shadow_test.go", "FeatureWaterObjectMeshShadowPass"},
	FeatureRectAreaLight:             {"lights_test.go", "FeatureRectAreaLight"},
	FeatureRectAreaSpecular:          {"lights_test.go", "FeatureRectAreaSpecular"},
	FeatureLightProbeSH:              {"lights_test.go", "FeatureLightProbeSH"},
	FeatureSkyEnvironment:            {"sky_test.go", "FeatureSkyEnvironment"},
}
