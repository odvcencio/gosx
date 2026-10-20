package evalparity

import (
	"encoding/json"
	"fmt"

	"m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/island"
)

// runVM renders c through ir.LowerIsland + the client VM
// (client/vm.ResolveInitialTree, island.RenderResolvedHTML) — the exact
// pair island/island.go's own server-side initial render uses (see
// renderProgramHTMLWithProps). This is the same Go code the browser
// runs under WASM (client/vm has no browser dependency), so running it
// natively here evaluates the real client bytecode, not a stand-in.
//
// LowerIsland is a second, stricter lowering pass than gosx.Compile's
// base IR validation — a case whose props/expression the shared IR
// gate accepts can still fail here, and that failure is this backend's
// own "unsupported" signal, independent of route's.
func runVM(prog *ir.Program, compIdx int, c Case) (string, error) {
	lowered, err := ir.LowerIsland(prog, compIdx)
	if err != nil {
		return "", err
	}
	propsJSON := "{}"
	if c.PropsValue != nil {
		raw, err := json.Marshal(c.PropsValue)
		if err != nil {
			return "", fmt.Errorf("marshal props: %w", err)
		}
		propsJSON = string(raw)
	}
	resolved := vm.ResolveInitialTree(lowered, propsJSON)
	html := island.RenderResolvedHTML(lowered, resolved)
	return stripDivWrapper(html)
}
