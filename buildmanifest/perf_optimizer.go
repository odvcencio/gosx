package buildmanifest

// WASMOptimization is private producer evidence for one completed optimizer
// invocation. It is not a browser measurement or an approval record.
type WASMOptimization struct {
	Tool         string `json:"tool"`
	Version      string `json:"version"`
	Applied      bool   `json:"applied"`
	InputSHA256  string `json:"inputSHA256"`
	OutputSHA256 string `json:"outputSHA256"`
}
