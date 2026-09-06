package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"time"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/internal/modelschema"
	"m31labs.dev/gosx/ir"
)

// Saved state is server-owned and immutable after publication. No disk access
// occurs when checking whether a changed GSX buffer can reuse this snapshot.
type savedSchema struct {
	refs    string
	dir     string
	pkg     string
	schemas *ir.SchemaSet
	err     error
}

func schemaRefs(source []byte) (modelschema.Refs, string, bool) {
	tree, lang, err := gosx.Parse(source)
	if err != nil || tree.RootNode().HasError() {
		return modelschema.Refs{}, "", false
	}
	refs := modelschema.SourceRefs(tree.RootNode(), lang, source)
	data, _ := json.Marshal(refs)
	return refs, string(data), true
}

func canonicalSchemaDir(path string) string {
	dir, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return filepath.Clean(filepath.Dir(path))
	}
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		return real
	}
	return dir
}

func (s *server) refreshSavedSchema(uri string) {
	if s.schemas == nil {
		s.schemas = make(map[string]savedSchema)
	}
	delete(s.schemas, uri)
	source, open := s.docs[uri]
	path := URIToPath(uri)
	if !open || filepath.Ext(path) != ".gsx" {
		return
	}
	refs, key, valid := schemaRefs([]byte(source))
	if !valid {
		return
	}
	state := savedSchema{refs: key, dir: canonicalSchemaDir(path), pkg: refs.Package}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	snapshot, err := modelschema.Resolve(ctx, state.dir, refs, modelschema.Selection{GoTool: true, Env: os.Environ()})
	if err != nil {
		state.err = fmt.Errorf("saved Go model schema: %w", err)
	} else {
		state.schemas = snapshot.Schemas()
	}
	s.schemas[uri] = state
}

func (s *server) savedSourceIndex(uri string, source []byte) (sourceIndex, []Diagnostic) {
	_, key, valid := schemaRefs(source)
	state, exists := s.schemas[uri]
	if !valid || !exists || state.refs != key {
		return indexSource(URIToPath(uri), source)
	}
	idx, diags := indexSourceWithSchemas(URIToPath(uri), source, state.schemas)
	if state.err != nil {
		diags = append(diags, diagnosticForSourceError(source, state.err))
	}
	return idx, diags
}

func (s *server) saveGoModels(uri string) error {
	path := URIToPath(uri)
	dir := canonicalSchemaDir(path)
	pkg := ""
	if file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.PackageClauseOnly); err == nil {
		pkg = file.Name.Name
	}
	for _, owner := range sortedStringKeys(s.docs) {
		if filepath.Ext(URIToPath(owner)) != ".gsx" || canonicalSchemaDir(URIToPath(owner)) != dir {
			continue
		}
		refs, _, valid := schemaRefs([]byte(s.docs[owner]))
		if valid && pkg != "" && refs.Package != pkg {
			continue
		}
		s.refreshSavedSchema(owner)
		if err := s.publishDiagnostics(owner); err != nil {
			return err
		}
	}
	return nil
}
