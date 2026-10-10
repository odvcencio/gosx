//go:build js && wasm

package browserdom

import (
	"syscall/js"
	"testing"
)

func TestInstantiateTemplateValidatesBeforeImport(t *testing.T) {
	if (Document{}).InstantiateTemplate("missing").Valid() {
		t.Fatal("zero document returned an element")
	}
	for _, tc := range []struct {
		name, tag, namespace string
		missing, noContent   bool
		roots                int
	}{
		{name: "missing", missing: true},
		{name: "ordinary element", tag: "div"},
		{name: "svg template", tag: "template", namespace: "http://www.w3.org/2000/svg"},
		{name: "missing content", tag: "template", noContent: true},
		{name: "empty", tag: "template", roots: 0},
		{name: "multiple roots", tag: "template", roots: 2},
		{name: "one root", tag: "template", roots: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, template, content, root, clone := domObject(), domObject(), domObject(), domObject(), domObject()
			template.Set("localName", tc.tag)
			namespace := tc.namespace
			if namespace == "" {
				namespace = "http://www.w3.org/1999/xhtml"
			}
			template.Set("namespaceURI", namespace)
			if !tc.noContent {
				template.Set("content", content)
			}
			content.Set("childElementCount", tc.roots)
			content.Set("firstElementChild", root)
			domMethod(t, doc, "getElementById", func(args []js.Value) any {
				if len(args) != 1 || args[0].String() != "row-template" {
					t.Error("template lookup changed ID")
				}
				if tc.missing {
					return nil
				}
				return template
			})
			imports := 0
			domMethod(t, doc, "importNode", func(args []js.Value) any {
				imports++
				if len(args) != 2 || !args[0].Equal(root) || !args[1].Bool() {
					t.Error("import must deeply copy the element root")
				}
				return clone
			})
			got := (Document{doc}).InstantiateTemplate("row-template")
			valid := tc.name == "one root"
			if got.Valid() != valid {
				t.Fatalf("valid=%v, want %v", got.Valid(), valid)
			}
			if valid {
				if imports != 1 || !got.Equal(FromJS(clone)) {
					t.Fatal("did not return exactly one imported clone")
				}
			} else if imports != 0 {
				t.Fatal("invalid template reached importNode")
			}
		})
	}
}

// The normal Node wasm runner has no DOM. Run this test in a browser wasm host
// to exercise native template/importNode semantics in addition to bridge tests.
func TestInstantiateTemplateInBrowser(t *testing.T) {
	globalDocument := js.Global().Get("document")
	if !globalDocument.Truthy() {
		t.Skip("requires a browser DOM")
	}
	// An independent document also verifies that lookup and clone ownership use
	// the receiver rather than a global document or the template content's inert
	// owner document.
	nativeDocument := globalDocument.Get("implementation").Call("createHTMLDocument", "templates")
	doc := Document{nativeDocument}
	template := doc.Create("template")
	template.SetAttr("id", "row-template")
	content := Value(template).Get("content")
	content.Call("appendChild", nativeDocument.Call("createTextNode", "\n  leading text ignored\n"))
	content.Call("appendChild", nativeDocument.Call("createComment", "outside root"))
	root, label, nested := doc.Create("li"), doc.Create("span"), doc.Create("strong")
	root.SetAttr("class", "row")
	label.SetAttr("data-label", "")
	nested.SetText("original")
	label.Append(nested)
	root.Append(doc.CreateText("prefix "), label)
	Value(root).Call("appendChild", nativeDocument.Call("createComment", "inside root"))
	content.Call("appendChild", Value(root))
	content.Call("appendChild", nativeDocument.Call("createTextNode", " trailing text ignored "))
	doc.Body().Append(template)

	first, second := doc.InstantiateTemplate("row-template"), doc.InstantiateTemplate("row-template")
	if !first.Valid() || !second.Valid() || first.Equal(second) || first.Equal(root) {
		t.Fatal("instances must have independent element identity")
	}
	if first.Parent().Valid() || second.Parent().Valid() || first.Connected() || second.Connected() || doc.Body().ChildElementCount() != 1 {
		t.Fatal("instantiation mutated the document")
	}
	if !Value(first).Get("ownerDocument").Equal(nativeDocument) || !Value(first.Query("strong")).Get("ownerDocument").Equal(nativeDocument) {
		t.Fatal("clone or descendant belongs to the template's inert document")
	}
	if first.Text() != "prefix original" || Value(first).Get("lastChild").Get("nodeType").Int() != 8 {
		t.Fatal("descendant text/comment was lost or outer text was imported")
	}
	retained := first.Query("[data-label]").Query("strong")
	retained.SetText("updated")
	first.SetAttr("data-key", "first")
	if first.Text() != "prefix updated" || second.Text() != "prefix original" || root.Text() != "prefix original" || second.HasAttr("data-key") || root.HasAttr("data-key") {
		t.Fatal("updating one instance changed the template or another instance")
	}
	doc.Body().Append(first)
	if !first.Parent().Equal(doc.Body()) || !retained.Equal(first.Query("strong")) || doc.Body().ChildElementCount() != 2 || second.Parent().Valid() {
		t.Fatal("append failed to preserve retained child identity")
	}

	for _, id := range []string{"missing", "ordinary", "empty", "many", "svg-template"} {
		if id != "missing" {
			tag := "template"
			if id == "ordinary" {
				tag = "div"
			}
			node := doc.Create(tag)
			if id == "svg-template" {
				node = FromJS(nativeDocument.Call("createElementNS", "http://www.w3.org/2000/svg", "template"))
			}
			node.SetAttr("id", id)
			if id == "many" {
				Value(node).Get("content").Call("appendChild", Value(doc.Create("p")))
				Value(node).Get("content").Call("appendChild", Value(doc.Create("p")))
			}
			doc.Body().Append(node)
		}
		if doc.InstantiateTemplate(id).Valid() {
			t.Fatalf("invalid template %q returned an element", id)
		}
	}

	// Even a script root is only cloned, never evaluated by instantiation.
	scriptTemplate, script := doc.Create("template"), doc.Create("script")
	scriptTemplate.SetAttr("id", "script-template")
	script.SetText("globalThis.__gosxTemplateScriptRan = true")
	Value(scriptTemplate).Get("content").Call("appendChild", Value(script))
	doc.Body().Append(scriptTemplate)
	if !doc.InstantiateTemplate("script-template").Valid() || js.Global().Get("__gosxTemplateScriptRan").Truthy() {
		t.Fatal("instantiation evaluated authored script content")
	}
}
