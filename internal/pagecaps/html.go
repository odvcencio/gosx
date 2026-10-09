package pagecaps

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
	"m31labs.dev/gosx/hydrate"
)

// FromHTML reads capabilities from active markup and hydration contracts.
// A dormant bundle reference alone never establishes a WASM requirement.
func FromHTML(data []byte) (Capabilities, error) {
	if len(data) > 16<<20 || !utf8.Valid(data) {
		return Capabilities{}, errors.New("invalid capability HTML")
	}
	root, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		return Capabilities{}, errors.New("invalid capability HTML")
	}
	executable, err := executableMarkup(root)
	if err != nil {
		return Capabilities{}, err
	}
	c := Capabilities{BootstrapMode: "none", Runtime: "none", decoded: true}
	modes := map[string]bool{}
	bootstrapModes := map[string]bool{}
	var manifest *hydrate.Manifest
	var visit func(*html.Node) error
	visit = func(node *html.Node) error {
		if node.Type == html.ElementNode {
			if node.Data == "template" {
				return nil
			}
			attrs := map[string]string{}
			for _, attr := range node.Attr {
				key := strings.ToLower(attr.Key)
				attrs[key] = attr.Val
			}
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
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if err := visit(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit(root); err != nil {
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
			case "shared":
				modes["shared"] = true
				c.WASM = true
			case "go-wasm":
				if strings.TrimSpace(entry.ProgramRef) == "" {
					return Capabilities{}, errors.New("Go-WASM requires a program reference")
				}
				modes["go-wasm"] = true
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

// executableMarkup applies one detector to active HTML, SVG and inline child
// documents. Template content stays inert; srcdoc is parsed as HTML rather
// than searched as a string, so data blocks and nested templates stay inert.
func executableMarkup(root *html.Node) (bool, error) {
	type pending struct {
		node  *html.Node
		depth int
	}
	queue := []pending{{node: root}}
	for len(queue) > 0 {
		item := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		node := item.node
		if node.Type == html.ElementNode {
			if node.Namespace == "" && node.Data == "template" {
				continue
			}
			attrs := map[string]string{}
			for _, attr := range node.Attr {
				key := strings.ToLower(attr.Key)
				attrs[key] = attr.Val
				if strings.HasPrefix(key, "on") && len(key) > 2 || javascriptURL(attr.Val) {
					return true, nil
				}
			}
			if node.Data == "script" && ExecutableScriptType(attrs["type"]) {
				return true, nil
			}
			if node.Data == "meta" && strings.EqualFold(strings.TrimSpace(attrs["http-equiv"]), "refresh") && refreshJavascriptURL(attrs["content"]) {
				return true, nil
			}
			if node.Namespace == "" && node.Data == "iframe" {
				if content, ok := attrs["srcdoc"]; ok {
					// Bound nested parsing independently of the outer byte limit.
					if item.depth >= 32 {
						return false, errors.New("invalid capability HTML")
					}
					child, err := html.Parse(strings.NewReader(content))
					if err != nil {
						return false, errors.New("invalid capability HTML")
					}
					queue = append(queue, pending{child, item.depth + 1})
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			queue = append(queue, pending{child, item.depth})
		}
	}
	return false, nil
}

func javascriptURL(value string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(value)), "javascript:")
}

func refreshJavascriptURL(value string) bool {
	if separator := strings.IndexAny(value, ";,"); separator >= 0 {
		value = value[separator+1:]
	}
	value = strings.TrimSpace(value)
	if len(value) >= 3 && strings.EqualFold(value[:3], "url") {
		value = strings.TrimSpace(value[3:])
		if !strings.HasPrefix(value, "=") {
			return false
		}
		value = strings.TrimSpace(value[1:])
	}
	return javascriptURL(strings.TrimLeft(value, "'\""))
}
