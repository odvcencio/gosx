package buildsurface

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"

	"m31labs.dev/gosx/ir"
)

// eventMapping maps a canonical DOM event name to the runtime struct field
// name and the surface.Wrap* function to use.
type eventMapping struct {
	Field    string
	WrapFunc string
}

// eventMappings is the complete table of supported events.
var eventMappings = map[string]eventMapping{
	"mount":         {Field: "OnMount", WrapFunc: "surface.WrapMount"},
	"click":         {Field: "OnClick", WrapFunc: "surface.WrapPointer"},
	"dblclick":      {Field: "OnDblClick", WrapFunc: "surface.WrapPointer"},
	"pointerdown":   {Field: "OnPointerDown", WrapFunc: "surface.WrapPointer"},
	"pointermove":   {Field: "OnPointerMove", WrapFunc: "surface.WrapPointer"},
	"pointerup":     {Field: "OnPointerUp", WrapFunc: "surface.WrapPointer"},
	"pointercancel": {Field: "OnPointerCancel", WrapFunc: "surface.WrapPointer"},
	"wheel":         {Field: "OnWheel", WrapFunc: "surface.WrapWheel"},
	"keydown":       {Field: "OnKeyDown", WrapFunc: "surface.WrapKey"},
	"keyup":         {Field: "OnKeyUp", WrapFunc: "surface.WrapKey"},
	"resize":        {Field: "OnResize", WrapFunc: "surface.WrapResize"},
	"dispose":       {Field: "OnDispose", WrapFunc: "surface.WrapDispose"},
}

var mainTemplate = template.Must(template.New("surface_main").Parse(`//go:build js && wasm

package main

import (
	"m31labs.dev/gosx/engine/surface"
	"m31labs.dev/gosx/engine/surface/runtime"
	user "{{.UserImportPath}}"
)

func main() {
	runtime.Register({{printf "%q" .Name}}, runtime.Surface{
{{- range .Handlers}}
		{{.Field}}: {{.WrapFunc}}(user.{{.FunctionName}}),
{{- end}}
	})
	select {}
}
`))

// handlerEntry holds the template data for one handler binding.
type handlerEntry struct {
	Field        string
	WrapFunc     string
	FunctionName string
}

// templateData is passed to mainTemplate.
type templateData struct {
	Name           string
	UserImportPath string
	Handlers       []handlerEntry
}

// GenerateMain returns the Go source bytes for the WASM entry point generated
// from sp. userPackageImportPath is the full Go import path of the user's
// package inside the scratch module (e.g. "gosx_surface_Graph_abc12345/user").
func GenerateMain(sp *ir.SurfaceProgram, userPackageImportPath string) ([]byte, error) {
	if sp == nil {
		return nil, fmt.Errorf("GenerateMain: nil SurfaceProgram")
	}
	if strings.TrimSpace(userPackageImportPath) == "" {
		return nil, fmt.Errorf("GenerateMain: empty userPackageImportPath")
	}

	entries := make([]handlerEntry, 0, len(sp.Handlers))
	for _, h := range sp.Handlers {
		m, ok := eventMappings[h.EventName]
		if !ok {
			// Unknown event — skip rather than produce uncompilable code.
			continue
		}
		entries = append(entries, handlerEntry{
			Field:        m.Field,
			WrapFunc:     m.WrapFunc,
			FunctionName: h.FunctionName,
		})
	}

	data := templateData{
		Name:           sp.Name,
		UserImportPath: userPackageImportPath,
		Handlers:       entries,
	}

	var buf bytes.Buffer
	if err := mainTemplate.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("GenerateMain: execute template: %w", err)
	}
	return buf.Bytes(), nil
}
