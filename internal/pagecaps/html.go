package pagecaps

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode"

	"golang.org/x/net/html"
	"m31labs.dev/gosx/hydrate"
)

// FromHTML reads capabilities from active markup and hydration contracts.
// A dormant bundle reference alone never establishes a WASM requirement.
func FromHTML(data []byte) (Capabilities, error) {
	return InspectHTML(data, nil)
}

// InspectHTML reads capabilities and emits execution evidence during the same
// traversal of active markup, including entity-decoded srcdoc documents.
func InspectHTML(data []byte, observe func(ExecutableSource)) (Capabilities, error) {
	tree, err := ParseDocumentTree(data, "", nil)
	if err != nil {
		return Capabilities{}, err
	}
	return InspectDocumentTree(tree, func(_ *Document, source ExecutableSource) {
		if observe != nil {
			observe(source)
		}
	})
}

// InspectDocumentTree classifies execution from the same explicit tree used
// for planning. Hydration metadata belongs to the root document; execution
// evidence includes every permitted document, deduplicated by document key.
func InspectDocumentTree(tree *DocumentTree, observe func(*Document, ExecutableSource)) (Capabilities, error) {
	if tree == nil || tree.Root == nil {
		return Capabilities{}, errors.New("invalid capability HTML")
	}
	executable := false
	c := Capabilities{BootstrapMode: "none", Runtime: "none", decoded: true}
	modes := map[string]bool{}
	bootstrapModes := map[string]bool{}
	var manifest *hydrate.Manifest
	visit := func(node *html.Node, attrs map[string]string, depth int) error {
		// Hydration contracts belong to this document. Embedded documents
		// contribute execution evidence, not another root hydration manifest.
		if depth != 0 {
			return nil
		}
		if node.Type == html.ElementNode {
			if _, ok := attrs["data-gosx-navigation"]; ok {
				c.Navigation = true
			}
			if _, ok := attrs["data-gosx-motion"]; ok {
				c.Motion = true
			}
			if strings.EqualFold(attrs["data-gosx-enhance"], "motion") {
				c.Motion = true
			}
			if strings.EqualFold(attrs["data-gosx-engine"], "GoSXScene3D") {
				c.Scene3D = true
			}
			if _, ok := attrs["data-gosx-scene3d"]; ok {
				c.Scene3D = true
			}
			if strings.EqualFold(attrs["data-gosx-engine-kind"], "video") {
				c.Video = true
			}
			if node.Data == "script" {
				switch attrs["data-gosx-script"] {
				case "bootstrap":
					c.Bootstrap = true
					c.BootstrapMode = strings.TrimSpace(attrs["data-gosx-bootstrap-mode"])
					if c.BootstrapMode == "" {
						c.BootstrapMode = "full"
					}
					bootstrapModes[c.BootstrapMode] = true
					if len(bootstrapModes) > 1 {
						return errors.New("conflicting bootstrap modes")
					}
				case "feature-scene3d":
					c.Scene3D = true
				}
				if attrs["id"] == "gosx-manifest" {
					if manifest != nil {
						return errors.New("duplicate hydration manifest")
					}
					manifest = &hydrate.Manifest{}
					var text strings.Builder
					for child := node.FirstChild; child != nil; child = child.NextSibling {
						if child.Type == html.TextNode {
							text.WriteString(child.Data)
						}
					}
					raw := strings.TrimSpace(text.String())
					if !strings.HasPrefix(raw, "{") {
						return errors.New("invalid hydration manifest")
					}
					if err := json.Unmarshal([]byte(raw), manifest); err != nil {
						return errors.New("invalid hydration manifest")
					}
				}
			}
		}
		return nil
	}
	if err := walkActiveDocuments(tree, visit, func(doc *Document, source ExecutableSource) {
		executable = true
		if observe != nil {
			observe(doc, source)
		}
	}); err != nil {
		return Capabilities{}, err
	}
	// Preview and island contracts require the shared VM even if a compatibility
	// configuration omitted its URL. Missing references remain reachability errors.
	if c.BootstrapMode == "preview" {
		c.WASM = true
		modes["shared"] = true
	}
	if manifest != nil {
		c.Islands = len(manifest.Islands)
		c.ComputeIslands = len(manifest.ComputeIslands)
		c.Engines = len(manifest.Engines)
		c.Hubs = len(manifest.Hubs)
		c.Controllers = len(manifest.Controllers)
		if c.Islands+c.ComputeIslands+c.Engines+c.Hubs+c.Controllers > 0 {
			c.Bootstrap = true
		}
		if strings.TrimSpace(manifest.Runtime.Path) != "" || c.Islands+c.ComputeIslands > 0 {
			c.WASM = true
			modes["shared"] = true
		}
		for _, entry := range manifest.Engines {
			switch entry.Runtime {
			case "":
				modes["js"] = true
				c.engineRuntimes |= runtimeJS
			case "shared":
				modes["shared"] = true
				c.engineRuntimes |= runtimeShared
				c.WASM = true
			case "go-wasm":
				if strings.TrimSpace(entry.ProgramRef) == "" {
					return Capabilities{}, errors.New("Go-WASM requires a program reference")
				}
				modes["go-wasm"] = true
				c.engineRuntimes |= runtimeGoWASM
				c.WASM = true
			default:
				return Capabilities{}, errors.New("invalid engine runtime")
			}
			isScene := strings.EqualFold(strings.TrimSpace(entry.Component), "GoSXScene3D")
			isVideo := strings.EqualFold(strings.TrimSpace(entry.Kind), "video")
			c.Scene3D = c.Scene3D || isScene
			c.Video = c.Video || isVideo
			if entry.Runtime == "go-wasm" {
				c.engineTypes |= standardGo
			}
			switch {
			case isVideo:
				c.engineTypes |= videoEngine
			case isScene:
				if entry.Runtime == "shared" {
					c.engineTypes |= sceneShared
				} else {
					c.engineTypes |= sceneJS
				}
			case entry.Runtime == "shared":
				c.engineTypes |= engineShared
			case entry.Runtime != "go-wasm":
				c.engineTypes |= engineJS
			}
		}
	}
	if len(modes) == 0 && (executable || c.Bootstrap || c.Scene3D || c.Video) {
		modes["js"] = true
	}
	if len(modes) > 1 {
		c.Runtime = "mixed"
	} else {
		for mode := range modes {
			c.Runtime = mode
		}
	}
	if _, err := Classify(c, false); err != nil {
		return Capabilities{}, err
	}
	return c, nil
}

func javascriptURL(value string) bool {
	_, executable := javascriptURLCode(value)
	return executable
}

func javascriptURLCode(value string) (string, bool) {
	value = strings.TrimLeftFunc(value, unicode.IsSpace)
	const scheme = "javascript:"
	if len(value) < len(scheme) || !strings.EqualFold(value[:len(scheme)], scheme) {
		return "", false
	}
	return value[len(scheme):], true
}

func refreshJavascriptURL(value string) bool {
	_, executable := refreshJavascriptURLCode(value)
	return executable
}

func refreshJavascriptURLCode(value string) (string, bool) {
	if separator := strings.IndexAny(value, ";,"); separator >= 0 {
		value = value[separator+1:]
	}
	value = strings.TrimSpace(value)
	if len(value) >= 3 && strings.EqualFold(value[:3], "url") {
		value = strings.TrimSpace(value[3:])
		if !strings.HasPrefix(value, "=") {
			return "", false
		}
		value = strings.TrimSpace(value[1:])
	}
	if len(value) > 0 && (value[0] == '\'' || value[0] == '"') {
		quote := value[0]
		value = strings.TrimLeft(value, "'\"")
		if len(value) > 0 && value[len(value)-1] == quote {
			value = value[:len(value)-1]
		}
	}
	return javascriptURLCode(value)
}
