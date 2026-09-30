package docs

import (
	"os"
	"strings"
	"testing"

	"golang.org/x/net/html"
	"m31labs.dev/gosx"
)

func TestHomeHeroKeepsSceneScriptsInsideInertTemplate(t *testing.T) {
	doc, err := html.Parse(strings.NewReader(gosx.RenderHTML(HomeHero())))
	if err != nil {
		t.Fatal(err)
	}
	var eager, deferred, mounts, templates, stills int
	var walk func(*html.Node, bool)
	walk = func(n *html.Node, inert bool) {
		inert = inert || n.Type == html.ElementNode && n.Data == "template"
		if n.Type == html.ElementNode {
			for _, attr := range n.Attr {
				if attr.Key == "class" && attr.Val == "hero__still" {
					stills++
				}
				if n.Data == "template" && attr.Key == "data-home-hero" {
					templates++
				}
				if n.Data == "script" && attr.Key == "src" {
					if inert {
						deferred++
					} else {
						eager++
					}
				}
				if attr.Key == "data-gosx-scene3d" {
					if !inert {
						t.Fatal("live scene escaped the template")
					}
					mounts++
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child, inert)
		}
	}
	walk(doc, false)
	if eager != 0 || deferred == 0 || mounts != 1 || templates != 1 || stills != 1 {
		t.Fatalf("script/mount contract: eager=%d deferred=%d mounts=%d templates=%d stills=%d", eager, deferred, mounts, templates, stills)
	}
}

func TestHomeHeroGPUHonestyGateMatchesRuntimeSource(t *testing.T) {
	data, err := os.ReadFile("../../../client/runtime/scene3d/mount-backend.ts")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, name := range []string{"sceneWebGLRendererLooksMasked", "sceneReadWebGLRendererMetadata", "sceneWebGLRendererLooksSoftware"} {
		start := strings.Index(source, "  function "+name+"(")
		if start < 0 {
			t.Fatalf("missing runtime function %s", name)
		}
		opening := start + strings.Index(source[start:], "{")
		depth, end := 1, opening+1
		for depth > 0 && end < len(source) {
			switch source[end] {
			case '{':
				depth++
			case '}':
				depth--
			}
			end++
		}
		if !strings.Contains(heroGPUProbe, source[start:end]) {
			t.Fatalf("%s changed; run go generate ./examples/gosx-docs/app", name)
		}
	}
}
