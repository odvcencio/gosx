package evalparity

import (
	"fmt"
	"strings"

	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/route"
)

// stripDivWrapper removes the "<div>" ... "</div>" wrapper every case's
// generated component returns, so callers compare only the expression's
// rendered text, not markup every backend produces identically by
// construction.
func stripDivWrapper(html string) (string, error) {
	const open, close = "<div>", "</div>"
	if !strings.HasPrefix(html, open) || !strings.HasSuffix(html, close) {
		return "", fmt.Errorf("rendered HTML %q is not wrapped in %s...%s", html, open, close)
	}
	return html[len(open) : len(html)-len(close)], nil
}

// runRoute renders c through route.RenderProgramComponent — the file
// router's per-request reflect interpreter (route/fileeval.go), which is
// what router.AddDir actually serves pages with. Legacy (non-strict)
// components bind "props" through ProgramRenderEnv.Values, not the
// strict-only ProgramRenderEnv.Props path (see route/fileprogram.go).
func runRoute(prog *ir.Program, c Case) (string, error) {
	env := route.ProgramRenderEnv{}
	if c.PropsValue != nil {
		env.Values = map[string]any{"props": c.PropsValue}
	}
	html, err := route.RenderProgramComponent(prog, componentName(c), env)
	if err != nil {
		return "", err
	}
	return stripDivWrapper(html)
}
