// Package pagecaps decodes rendered page requirements without executing scripts.
package pagecaps

// Capabilities records requirements found in HTML and the hydration manifest.
type Capabilities struct {
	Navigation     bool   `json:"navigation"`
	Bootstrap      bool   `json:"bootstrap"`
	WASM           bool   `json:"wasm"`
	Scene3D        bool   `json:"scene3d"`
	Video          bool   `json:"video"`
	Motion         bool   `json:"motion"`
	BootstrapMode  string `json:"bootstrapMode"`
	Islands        int    `json:"islands"`
	ComputeIslands int    `json:"computeIslands"`
	Engines        int    `json:"engines"`
	Hubs           int    `json:"hubs"`
	Controllers    int    `json:"controllers"`
	Runtime        string `json:"runtime"`

	// Per-engine evidence keeps mixed runtimes distinct after HTML decoding.
	engineTypes    uint16
	engineRuntimes uint8
	decoded        bool
}
