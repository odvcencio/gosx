package scene

import _ "embed"

// SelenaCloudSource is the shared cloud-scattering surface used by sky and
// ocean reflections. The build emits backend code; browsers never compile .sel.
//
//go:embed clouds.sel
var SelenaCloudSource []byte
