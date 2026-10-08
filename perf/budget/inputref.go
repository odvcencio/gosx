// Package budget loads the versioned inputs for performance allocations.
package budget

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
)

const maxInputBytes = 2 << 20

var pathPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+(/[A-Za-z0-9_.-]+)*$`)
var shaPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// LoadOptions sets the private project root for every file reference.
type LoadOptions struct{ RootDir string }

// Ref binds a root-relative input to its exact file bytes.
type Ref struct {
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
}

//go:embed schema/v2.schema.json
var inputSchema []byte

var inputDefinitions = func() map[string]any {
	var s struct {
		Defs map[string]any `json:"$defs"`
	}
	if err := json.Unmarshal(inputSchema, &s); err != nil {
		panic(err)
	}
	return s.Defs
}()

func inputRoot(path string, opts LoadOptions) (string, error) {
	root := opts.RootDir
	if root == "" {
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", errors.New("invalid input path")
		}
		root = filepath.Dir(abs)
		for {
			if info, err := os.Stat(filepath.Join(root, "go.mod")); err == nil && info.Mode().IsRegular() {
				break
			}
			parent := filepath.Dir(root)
			if parent == root {
				return "", errors.New("input requires a project root")
			}
			root = parent
		}
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "", errors.New("invalid project root")
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", errors.New("invalid project root")
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return "", errors.New("invalid project root")
	}
	return root, nil
}

func safePath(path string) bool {
	if len(path) > 240 || !pathPattern.MatchString(path) {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if part == "." || part == ".." {
			return false
		}
	}
	return true
}

func readWithin(root, path string, limit int64) ([]byte, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, errors.New("invalid input path")
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, errors.New("cannot resolve input path")
	}
	// Root confines symlink resolution as well as the final file open.
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, errors.New("cannot open project root")
	}
	defer r.Close()
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return nil, errors.New("input escapes project root")
	}
	f, err := r.Open(rel)
	if err != nil {
		return nil, errors.New("cannot open confined input")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("input must be a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errors.New("input exceeds read limit")
	}
	return data, nil
}

func readReference(root string, ref Ref, limit int64) ([]byte, error) {
	if !safePath(ref.File) || !shaPattern.MatchString(ref.SHA256) {
		return nil, errors.New("invalid input reference")
	}
	data, err := readWithin(root, filepath.Join(root, filepath.FromSlash(ref.File)), limit)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != ref.SHA256 {
		return nil, errors.New("input reference hash mismatch")
	}
	return data, nil
}

func loadInput(path string, opts LoadOptions, definition string, out any) (string, error) {
	root, err := inputRoot(path, opts)
	if err != nil {
		return "", err
	}
	data, err := readWithin(root, path, maxInputBytes)
	if err != nil {
		return "", err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var value any
	if err := d.Decode(&value); err != nil {
		return "", errors.New("invalid input JSON")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return "", errors.New("input must contain one JSON value")
	}
	if err := validateInput(value, inputDefinitions[definition]); err != nil {
		return "", err
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return "", errors.New("input does not match typed contract")
	}
	return root, nil
}

// validateInput evaluates the schema vocabulary used by the input contracts.
// Typed decoding follows this check so required nulls and absent fields differ.
func validateInput(value, raw any) error {
	s := raw.(map[string]any)
	fail := func() error { return errors.New("input does not match schema") }
	if ref, ok := s["$ref"].(string); ok {
		return validateInput(value, inputDefinitions[strings.TrimPrefix(ref, "#/$defs/")])
	}
	if c, ok := s["const"]; ok && !equalJSON(value, c) {
		return fail()
	}
	if choices, ok := s["enum"].([]any); ok {
		found := false
		for _, c := range choices {
			found = found || equalJSON(value, c)
		}
		if !found {
			return fail()
		}
	}
	kind := "null"
	switch value.(type) {
	case string:
		kind = "string"
	case bool:
		kind = "boolean"
	case json.Number:
		kind = "integer"
	case []any:
		kind = "array"
	case map[string]any:
		kind = "object"
	}
	if t, ok := s["type"]; ok {
		match := t == kind
		if ts, ok := t.([]any); ok {
			for _, k := range ts {
				match = match || k == kind
			}
		}
		if !match {
			return fail()
		}
	}
	switch v := value.(type) {
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			return fail()
		}
		if low, ok := s["minimum"].(float64); ok && n < int64(low) {
			return fail()
		}
		if high, ok := s["maximum"].(float64); ok && n > int64(high) {
			return fail()
		}
	case string:
		if max, ok := s["maxLength"].(float64); ok && len(v) > int(max) {
			return fail()
		}
		if pattern, ok := s["pattern"].(string); ok && !regexp.MustCompile(pattern).MatchString(v) {
			return fail()
		}
	case []any:
		if min, ok := s["minItems"].(float64); ok && len(v) < int(min) {
			return fail()
		}
		if max, ok := s["maxItems"].(float64); ok && len(v) > int(max) {
			return fail()
		}
		for _, item := range v {
			if err := validateInput(item, s["items"]); err != nil {
				return err
			}
		}
	case map[string]any:
		if min, ok := s["minProperties"].(float64); ok && len(v) < int(min) {
			return fail()
		}
		props, _ := s["properties"].(map[string]any)
		if required, ok := s["required"].([]any); ok {
			for _, k := range required {
				if _, ok := v[k.(string)]; !ok {
					return fail()
				}
			}
		}
		for k, child := range v {
			p, ok := props[k]
			if !ok {
				return fail()
			}
			if err := validateInput(child, p); err != nil {
				return err
			}
		}
	}
	return nil
}

func equalJSON(a, b any) bool {
	if n, ok := a.(json.Number); ok {
		f, err := n.Float64()
		return err == nil && reflect.DeepEqual(f, b)
	}
	return reflect.DeepEqual(a, b)
}
