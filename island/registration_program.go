package island

import (
	"crypto/sha256"
	"fmt"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/island/program"
)

// RenderRegisteredProgram requires the exact pre-registered build asset. It
// never invents a fetch URL from a component name.
func (r *Renderer) RenderRegisteredProgram(prog *program.Program, props any, slots ServerSlots) (gosx.Node, error) {
	if r == nil || prog == nil {
		return gosx.Node{}, fmt.Errorf("island entry: missing program")
	}
	asset, ok := r.programAssets[prog.Name]
	if !ok || asset.path == "" {
		return gosx.Node{}, fmt.Errorf("island entry: missing program asset for %s", prog.Name)
	}
	var data []byte
	var err error
	switch asset.format {
	case "json":
		data, err = program.EncodeJSON(prog)
	case "bin":
		data, err = program.EncodeBinary(prog)
	default:
		return gosx.Node{}, fmt.Errorf("island entry: unsupported program format %q", asset.format)
	}
	if err != nil {
		return gosx.Node{}, err
	}
	sum := sha256.Sum256(data)
	if asset.hash != fmt.Sprintf("%x", sum[:8]) {
		return gosx.Node{}, fmt.Errorf("island entry: stale program asset for %s", prog.Name)
	}
	if prog.HasServerSlots() {
		return r.RenderWithServerSlots(prog, props, slots)
	}
	if len(slots) != 0 {
		return gosx.Node{}, fmt.Errorf("island entry: program has no ServerSlots")
	}
	encoded, err := SerializeProps(props)
	if err != nil {
		return gosx.Node{}, err
	}
	return r.RenderIslandFromProgram(prog, encoded), nil
}
