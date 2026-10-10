package wire

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// Keep this corpus independent of the implementation's policy. It covers DOM
// creation/mutation, reflected HTML resource attributes, browser loading APIs,
// navigation and code construction, including accesses that escape call models.
func TestReferencesJavaScriptLoaderCapabilityCorpus(t *testing.T) {
	capabilities := strings.Fields(`
		createElement createElementNS appendChild append prepend insertBefore
		replaceChild replaceChildren replaceWith after before moveBefore
		insertAdjacentElement insertAdjacentHTML innerHTML outerHTML write writeln
		cloneNode importNode adoptNode createContextualFragment DOMParser parseFromString
		setHTML setHTMLUnsafe parseHTML parseHTMLUnsafe attachShadow insertNode surroundContents
		cloneContents extractContents createDocument createHTMLDocument createProcessingInstruction
		src href srcset data poster action formaction formAction srcdoc srcObject imageSrcset
		ping cite itemId itemType useMap background longDesc manifest profile
		archive codeBase classId code object lowsrc dynsrc dataSrc
		setAttribute setAttributeNS setAttributeNode setAttributeNodeNS
		attributes setNamedItem setNamedItemNS getAttributeNode getAttributeNodeNS nodeValue textContent
		appendData insertData deleteData replaceData splitText replaceWholeText
		style cssText setProperty insertRule addRule replaceSync
		Image Audio Video Option Request FontFace XMLHttpRequest EventSource WebSocket
		location assign replace open navigation navigate reload submit requestSubmit click load play send
		sendBeacon importScripts serviceWorker register eval Function
		Reflect getOwnPropertyDescriptor getOwnPropertyDescriptors constructor __proto__
		getPrototypeOf setPrototypeOf defineProperty defineProperties
		customElements define upgrade
	`)
	patterns := []string{
		`receiver.NAME("/hidden.js");`,
		`new receiver.NAME("/hidden.js");`,
		`receiver.NAME = "/hidden.js";`,
		`const alias = receiver.NAME; alias("/hidden.js");`,
		`const {NAME: alias} = receiver; alias("/hidden.js");`,
		`const {"NAME": alias} = receiver; alias("/hidden.js");`,
		`consume(receiver.NAME);`,
		`const options = {NAME: "/hidden.js"}; consume(options);`,
		`const options = {"NAME": "/hidden.js"}; consume(options);`,
		`const alias = NAME; consume(alias);`,
	}
	count := 0
	for _, capability := range capabilities {
		for i, pattern := range patterns {
			body := strings.ReplaceAll(pattern, "NAME", capability) + `import("/visible.js");`
			for _, kind := range []string{KindScript, KindDocument, "event-handler", "javascript-url"} {
				source := body
				if kind == KindDocument {
					source = "<script>" + source + "</script>"
				}
				set, err := scanExecutableAttributeCorpusSource(source, kind)
				if err != nil || set.Complete || !reflect.DeepEqual(set.Resources, []Reference{{"/visible.js", KindScript, false}}) {
					t.Errorf("unmodelled loader capability %s/%d/%s: source=%q set=%+v err=%v", capability, i, kind, source, set, err)
				}
				count++
			}
		}
	}
	// Modelled loaders must still reject aliases and nonliteral arguments.
	for _, loader := range []string{"fetch", "Worker", "SharedWorker", "URL", "setTimeout", "setInterval"} {
		for _, source := range []string{
			loader + `(target);`,
			`const alias = window.` + loader + `; alias("/hidden.js");`,
			`const {"` + loader + `": alias} = self; consume(alias);`,
		} {
			for _, kind := range []string{KindScript, KindDocument, "event-handler", "javascript-url"} {
				body := source
				if kind == KindDocument {
					body = "<script>" + body + "</script>"
				}
				set, err := scanExecutableAttributeCorpusSource(body, kind)
				if err != nil || set.Complete {
					t.Errorf("loader escaped its call model: source=%q set=%+v err=%v", body, set, err)
				}
				count++
			}
		}
	}
	for _, global := range []string{"window", "globalThis", "self", "document"} {
		for _, property := range []string{`"Image"`, `"create" + "Element"`, `selected`} {
			for _, kind := range []string{KindScript, KindDocument, "event-handler", "javascript-url"} {
				body := fmt.Sprintf("const alias = %s[%s]; consume(alias);", global, property)
				if kind == KindDocument {
					body = "<script>" + body + "</script>"
				}
				set, err := scanExecutableAttributeCorpusSource(body, kind)
				if err != nil || set.Complete {
					t.Errorf("computed global access claimed complete coverage: %q %+v %v", body, set, err)
				}
				count++
			}
		}
	}
	t.Logf("loader capability corpus: %d snippets", count)
	if count != 5200 {
		t.Fatalf("loader capability corpus size changed: %d", count)
	}
}

func scanExecutableAttributeCorpusSource(source, kind string) (ReferenceSet, error) {
	switch kind {
	case "event-handler":
		return ScanReferences([]byte(executableAttributeDocument("handler", source)), KindDocument)
	case "javascript-url":
		return ScanReferences([]byte(executableAttributeDocument("javascript-url", source)), KindDocument)
	default:
		return ScanReferences([]byte(source), kind)
	}
}

func TestReferencesDOMResourceCreationIsIncomplete(t *testing.T) {
	for _, source := range []string{
		`const script = document.createElement("script"); script.src = "/hidden.js"; document.head.appendChild(script);`,
		`const image = new Image(); image.src = "/hidden.png";`,
		`const make = document.createElement; const script = make("script"); script.src = "/hidden.js";`,
		`const {Image: Picture} = window; new Picture().src = "/hidden.png";`,
		`setTimeout("fetch('/hidden.bin')", 10);`,
		`setInterval("fetch('/hidden.bin')", 10);`,
		`import(selected);`,
	} {
		for _, kind := range []string{KindScript, KindDocument} {
			body := source
			if kind == KindDocument {
				body = "<script>" + body + "</script>"
			}
			set, err := ScanReferences([]byte(body), kind)
			if err != nil || set.Complete {
				t.Errorf("DOM/code loader claimed complete coverage: %q %+v %v", body, set, err)
			}
		}
	}
}

func TestReferencesJavaScriptLoaderModelsRemainExact(t *testing.T) {
	for _, tc := range []struct {
		source string
		want   []Reference
	}{
		{`import("/entry.js");`, []Reference{{"/entry.js", KindScript, false}}},
		{`fetch("/body.bin");`, []Reference{{"/body.bin", KindOther, false}}},
		{`new Worker("/worker.js");`, []Reference{{"/worker.js", KindScript, false}}},
		{`new SharedWorker(new URL("/worker.js", import.meta.url));`, []Reference{{"/worker.js", KindScript, false}}},
		{`new URL("/image.png", import.meta.url);`, []Reference{{"/image.png", KindImage, false}}},
		{`setTimeout(() => fetch("/body.bin"), 10);`, []Reference{{"/body.bin", KindOther, false}}},
		{`setInterval(function() { fetch("/body.bin"); }, 10);`, []Reference{{"/body.bin", KindOther, false}}},
		{`const text = "Image src createElement fetch"; /* document.write("/hidden.js") */`, []Reference{}},
	} {
		for _, kind := range []string{KindScript, KindDocument} {
			body := tc.source
			if kind == KindDocument {
				body = "<script>" + body + "</script>"
			}
			set, err := ScanReferences([]byte(body), kind)
			if err != nil || !set.Complete || !reflect.DeepEqual(set.Resources, tc.want) {
				t.Errorf("modelled call lost exact extraction: %q %+v %v want=%+v", body, set, err, tc.want)
			}
		}
	}
}

func TestReferencesCurrentDocumentFetchIsUnresolved(t *testing.T) {
	for _, target := range []string{"", "#x", "  #x  ", " \t "} {
		for _, receiver := range []string{"", "window.", "globalThis.", "self."} {
			for _, kind := range []string{KindScript, KindDocument} {
				body := receiver + "fetch(" + fmt.Sprintf("%q", target) + ");"
				if kind == KindDocument {
					body = "<script>" + body + "</script>"
				}
				set, err := ScanReferences([]byte(body), kind)
				if err != nil || set.Complete {
					t.Errorf("document fetch target was discarded: %q %+v %v", body, set, err)
				}
			}
		}
	}
	for _, source := range []string{`.a{filter:url('#x')}`, `.a{filter:url("#x")}`, `.a{background:url("")}`} {
		set, err := ScanReferences([]byte(source), KindStyle)
		if err != nil || !set.Complete || len(set.Resources) != 0 {
			t.Errorf("inert CSS reference changed: %q %+v %v", source, set, err)
		}
	}
}
