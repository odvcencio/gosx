package budget

import (
	"encoding/json"
	"path"
	"path/filepath"
	"reflect"
	"strings"

	"m31labs.dev/gosx/buildmanifest"
)

// Path is also used for logical asset IDs. Classify every Path field reached
// from the input schemas, including absent optional fields, before deriving
// filenames. A new Ref needs no special case; a new Path needs an explicit role.
var producerSchemaPathFields = map[string]bool{
	"Ref.file": true, "RouteFixture.sourcePath": true,
	"RouteFixture.criticalAssetIDs": false,
	"AssetRule.id":                  false, "AssetRule.dependencies": false,
}

func producerSchemaInputPaths(definitions map[string]any, roles map[string]bool, definition string, value any) ([]string, error) {
	files := []string{}
	var walk func(map[string]any, any, string) error
	walk = func(schema map[string]any, value any, field string) error {
		if ref, ok := schema["$ref"].(string); ok {
			name := strings.TrimPrefix(ref, "#/$defs/")
			if name == "Path" {
				file, classified := roles[field]
				if !classified {
					return producerOutputFailure()
				}
				if value != nil && file {
					name, ok := value.(string)
					if !ok || !safePath(name) {
						return producerOutputFailure()
					}
					files = append(files, name)
				}
				return nil
			}
			return walk(definitions[name].(map[string]any), value, name)
		}
		if properties, ok := schema["properties"].(map[string]any); ok {
			object, _ := value.(map[string]any)
			for name, child := range properties {
				if err := walk(child.(map[string]any), object[name], field+"."+name); err != nil {
					return err
				}
			}
		}
		if items, ok := schema["items"].(map[string]any); ok {
			// Inspect the item schema even for empty or absent arrays.
			if err := walk(items, nil, field); err != nil {
				return err
			}
			values, _ := value.([]any)
			for _, item := range values {
				if err := walk(items, item, field); err != nil {
					return err
				}
			}
		}
		if child, ok := schema["additionalProperties"].(map[string]any); ok {
			if err := walk(child, nil, field); err != nil {
				return err
			}
			object, _ := value.(map[string]any)
			for _, item := range object {
				if err := walk(child, item, field); err != nil {
					return err
				}
			}
		}
		for _, keyword := range []string{"oneOf", "anyOf", "allOf"} {
			choices, _ := schema[keyword].([]any)
			for _, child := range choices {
				if err := walk(child.(map[string]any), value, field); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(definitions[definition].(map[string]any), value, definition); err != nil {
		return nil, err
	}
	return files, nil
}

// Build manifests have no JSON schema. This table classifies every string
// field (including metadata), checked recursively against the Go types even
// when a pointer, slice or map is empty. New path fields cannot disappear.
var producerBuildPathFields = map[string]string{
	"Manifest.SourceRoot": "root",
	"HashedAsset.File":    "asset", "HashedAsset.Hash": "", "HashedAsset.Integrity": "",
	"SceneAssetManifest.File": "dist",
	"IslandAsset.SourceFile":  "source", "IslandAsset.Name": "", "IslandAsset.Format": "", "IslandAsset.SourceHash": "",
	"CSSAsset.Source": "source", "CSSAsset.Component": "",
	"ImageAsset.Source": "public", "ImageVariantAsset.Format": "",
	"RuntimeVariantAsset.Variant": "", "RuntimeVariantAsset.ManifestHash": "",
	"WASMOptimization.Tool": "", "WASMOptimization.Version": "", "WASMOptimization.InputSHA256": "", "WASMOptimization.OutputSHA256": "",
	"PerfAssetUse.URL": "url", "PerfAssetUse.ID": "", "PerfAssetUse.SHA256": "", "PerfAssetUse.Owner": "",
	"PerfAssetUse.Kind": "", "PerfAssetUse.Phase": "", "PerfAssetUse.Condition": "", "PerfAssetUse.Dependencies": "",
}

func producerCheckBuildPathFields(typ reflect.Type, roles map[string]string) error {
	switch typ.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
		return producerCheckBuildPathFields(typ.Elem(), roles)
	case reflect.Struct:
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			leaf := field.Type
			for leaf.Kind() == reflect.Slice || leaf.Kind() == reflect.Array || leaf.Kind() == reflect.Pointer || leaf.Kind() == reflect.Map {
				leaf = leaf.Elem()
			}
			if leaf.Kind() == reflect.String {
				role, ok := roles[typ.Name()+"."+field.Name]
				// Physical build paths are scalar strings. A future path container
				// must gain traversal support before its table entry can pass.
				if !ok || role != "" && field.Type.Kind() != reflect.String {
					return producerOutputFailure()
				}
			} else if err := producerCheckBuildPathFields(field.Type, roles); err != nil {
				return err
			}
		}
	}
	return nil
}

func producerBoundInputPaths(opts ProducerOptions, catalog json.RawMessage) ([]string, error) {
	files := append([]string{}, opts.Inputs.inputFiles...)
	for _, input := range []struct {
		definition string
		value      any
	}{
		{"Budget", opts.Inputs.File}, {"Profile", opts.Inputs.Profile}, {"Coefficients", opts.Inputs.Coefficients},
		{"Toolchain", opts.Inputs.Toolchain}, {"FixtureCatalog", catalog},
	} {
		raw, err := json.Marshal(input.value)
		if err != nil {
			return nil, producerOutputFailure()
		}
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, producerOutputFailure()
		}
		paths, err := producerSchemaInputPaths(inputDefinitions, producerSchemaPathFields, input.definition, value)
		if err != nil {
			return nil, err
		}
		for _, file := range paths {
			files = append(files, filepath.Join(opts.Inputs.RootDir(), filepath.FromSlash(file)))
		}
	}
	if err := producerCheckBuildPathFields(reflect.TypeFor[buildmanifest.Manifest](), producerBuildPathFields); err != nil {
		return nil, err
	}
	sourceRoot := opts.Build.SourceRoot
	if sourceRoot == "" {
		sourceRoot = opts.Inputs.RootDir()
	}
	var walk func(reflect.Value, string) error
	walk = func(value reflect.Value, bucket string) error {
		switch value.Kind() {
		case reflect.Pointer:
			if !value.IsNil() {
				return walk(value.Elem(), bucket)
			}
		case reflect.Slice, reflect.Array:
			for i := 0; i < value.Len(); i++ {
				if err := walk(value.Index(i), bucket); err != nil {
					return err
				}
			}
		case reflect.Map:
			for _, key := range value.MapKeys() {
				if err := walk(value.MapIndex(key), bucket); err != nil {
					return err
				}
			}
		case reflect.Struct:
			for i := 0; i < value.NumField(); i++ {
				field, child := value.Type().Field(i), value.Field(i)
				if child.Kind() != reflect.String {
					next := bucket
					if value.Type() == reflect.TypeFor[buildmanifest.Manifest]() {
						switch field.Name {
						case "Runtime":
							next = "runtime"
						case "Islands":
							next = "islands"
						case "CSS":
							next = "css"
						case "Images":
							next = "images"
						}
					}
					if err := walk(child, next); err != nil {
						return err
					}
					continue
				}
				name := child.String()
				if name == "" {
					continue
				}
				base := opts.DistDir
				switch producerBuildPathFields[value.Type().Name()+"."+field.Name] {
				case "source":
					// Imported sources can live outside the project. Resolve them
					// before reserving their path and identity; only distribution
					// paths require the confined spelling checked below.
					file := filepath.FromSlash(name)
					if !filepath.IsAbs(file) {
						file = filepath.Join(sourceRoot, file)
					}
					file, err := filepath.Abs(file)
					if err != nil {
						return producerOutputFailure()
					}
					files = append(files, file)
					continue
				case "asset":
					if bucket == "" {
						return producerOutputFailure()
					}
					name = path.Join("assets", bucket, name)
				case "public":
					name = path.Join("public", strings.TrimPrefix(name, "/"))
				case "url":
					var err error
					name, err = fixtureFilePath(name, value.FieldByName("Kind").String())
					if err != nil {
						return producerOutputFailure()
					}
				case "dist":
				default:
					continue
				}
				if !safePath(name) {
					return producerOutputFailure()
				}
				files = append(files, filepath.Join(base, filepath.FromSlash(name)))
			}
		}
		return nil
	}
	if err := walk(reflect.ValueOf(opts.Build), ""); err != nil {
		return nil, err
	}
	return files, nil
}
