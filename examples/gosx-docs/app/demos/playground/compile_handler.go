package playground

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/action"
	"github.com/odvcencio/gosx/ir"
	"github.com/odvcencio/gosx/island/program"
)

// Diagnostic is a user-facing parse/validate error for the playground editor.
type Diagnostic struct {
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Message string `json:"message"`
}

// CompileResult is the pure output of the playground pipeline.
type CompileResult struct {
	// HTML is the SSR placeholder the client runtime hydrates. In this task
	// it is a static hydration target; future tasks may enrich it.
	HTML string `json:"html"`

	// Program is the binary-encoded island VM program. Callers are expected
	// to base64 the bytes when they travel over JSON.
	Program []byte `json:"-"`

	// Diagnostics are non-fatal user-facing errors (parse or validation
	// failures). A non-empty Diagnostics slice with a zero Program means the
	// input had a problem the user needs to see.
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// ErrEmptySource is returned when CompileSource receives empty input.
var ErrEmptySource = errors.New("playground: source is empty")

// ErrNoComponents is returned when the source compiles but yields no components.
var ErrNoComponents = errors.New("playground: no components in source")

// ErrFirstComponentNotIsland is returned when Components[0] is not an island.
// The playground expects the first component to be the island under test
// (matching the preset shape).
var ErrFirstComponentNotIsland = errors.New("playground: first component is not an island (missing //gosx:island directive?)")

// CompileSource parses gsx source, lowers the first component to an island
// program, and encodes it. Fatal pipeline errors are returned as err. User-
// facing problems (parse/validation failures) are returned in the
// CompileResult.Diagnostics slice with a nil err.
func CompileSource(source []byte) (CompileResult, error) {
	if len(source) == 0 {
		return CompileResult{}, ErrEmptySource
	}

	prog, err := gosx.Compile(source)
	if err != nil {
		// Parse or validation error — return as a diagnostic, not a fatal err.
		return CompileResult{
			Diagnostics: []Diagnostic{{Message: err.Error()}},
		}, nil
	}
	if len(prog.Components) == 0 {
		return CompileResult{}, ErrNoComponents
	}
	if !prog.Components[0].IsIsland {
		return CompileResult{}, ErrFirstComponentNotIsland
	}

	island, err := ir.LowerIsland(prog, 0)
	if err != nil {
		return CompileResult{
			Diagnostics: []Diagnostic{{Message: err.Error()}},
		}, nil
	}

	bin, err := program.EncodeBinary(island)
	if err != nil {
		return CompileResult{}, fmt.Errorf("encode island program: %w", err)
	}

	return CompileResult{
		HTML:    renderPlaygroundSSR(prog.Components[0].Name),
		Program: bin,
	}, nil
}

// renderPlaygroundSSR emits the minimal hydration target element. The client
// replaces its children when the new program is hydrated. We keep the element
// slot stable across recompiles so the hydrator can find it.
func renderPlaygroundSSR(componentName string) string {
	return `<div data-gosx-island="playground-preview" data-component="` + componentName + `"></div>`
}

// CompileAction is the action.Context adapter used by the page's
// RegisterDocsPage Actions map. Wire it in as the "compile" action in
// page.server.go (later task). The request body is JSON {"source": "..."}.
// On success it returns ctx.Success("", data) where data encodes HTML, the
// base64 program, and diagnostics.
func CompileAction(ctx *action.Context) error {
	var req struct {
		Source string `json:"source"`
	}
	if err := json.Unmarshal(ctx.Payload, &req); err != nil {
		return ctx.Success("", map[string]any{
			"html":        "",
			"program":     "",
			"diagnostics": []Diagnostic{{Message: "invalid request body"}},
		})
	}
	result, err := CompileSource([]byte(req.Source))
	if err != nil {
		// Fatal — expose as a single diagnostic so the client renders something.
		return ctx.Success("", map[string]any{
			"html":        "",
			"program":     "",
			"diagnostics": []Diagnostic{{Message: err.Error()}},
		})
	}
	return ctx.Success("", map[string]any{
		"html":        result.HTML,
		"program":     base64.StdEncoding.EncodeToString(result.Program),
		"diagnostics": result.Diagnostics,
	})
}
