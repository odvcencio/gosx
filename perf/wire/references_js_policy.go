package wire

// Completeness models accidental regressions in our own app: code a developer
// would plausibly write, including common esbuild/Terser output, using modelled
// forms. Code deliberately hiding loads is outside the threat model. Computed
// access, code construction, enumeration/reflection, global-object aliasing
// except static alias.name, and the loader denylist always mean incomplete.
// Known literal references are retained alongside that uncertainty.
func javaScriptGlobalObjectAlias(name string) bool {
	switch name {
	case "globalThis", "window", "self", "top", "parent", "frames", "opener", "defaultView":
		return true
	}
	return false
}

// Enumeration accesses/keys can expose capabilities without naming a loader.
// The AST policy distinguishes these from ordinary local variables named keys
// or values. Existing reflection/assign tokens also stay on the loader denylist.
func unmodeledJavaScriptEnumeration(name string) bool {
	switch name {
	case "values", "entries", "keys", "getOwnPropertyNames", "getOwnPropertySymbols",
		"getOwnPropertyDescriptor", "getOwnPropertyDescriptors", "getPrototypeOf", "fromEntries", "assign":
		return true
	}
	return false
}

// unmodeledJavaScriptLoader is a capability policy, not receiver/type analysis.
// These tokens can create, activate or redirect resources, or conceal a loader.
// Their appearance in accesses, bindings and property keys prevents a complete
// claim even when a local object might use the same name for an unrelated API.
// Literal fetch/import/worker/URL calls and inline function timers have separate
// models; every other use of those capabilities is also incomplete.
func unmodeledJavaScriptLoader(name string) bool {
	switch name {
	// DOM creation, cloning and insertion can activate resource-bearing nodes:
	// https://dom.spec.whatwg.org/#interface-document
	// https://dom.spec.whatwg.org/#interface-node
	// https://dom.spec.whatwg.org/#interface-parentnode
	// https://dom.spec.whatwg.org/#interface-range
	case "createElement", "createElementNS", "appendChild", "append", "prepend", "insertBefore",
		"replaceChild", "replaceChildren", "replaceWith", "after", "before", "moveBefore",
		"insertAdjacentElement", "cloneNode", "importNode", "adoptNode", "attachShadow",
		"insertNode", "surroundContents", "cloneContents", "extractContents", "createDocument",
		"createHTMLDocument", "createProcessingInstruction":
		return true
	// Parsing and markup replacement can synthesize new HTML dependencies:
	// https://html.spec.whatwg.org/multipage/dynamic-markup-insertion.html
	case "insertAdjacentHTML", "innerHTML", "outerHTML", "write", "writeln", "DOMParser",
		"parseFromString", "createContextualFragment", "setHTML", "setHTMLUnsafe", "parseHTML", "parseHTMLUnsafe":
		return true
	// Reflected resource/navigation attributes, including legacy HTML fields:
	// https://html.spec.whatwg.org/multipage/indices.html#attributes-3
	// https://html.spec.whatwg.org/multipage/obsolete.html
	case "src", "href", "srcset", "data", "poster", "action", "formaction", "formAction", "srcdoc", "srcObject", "imageSrcset",
		"ping", "cite", "itemId", "itemType", "useMap", "background", "longDesc", "manifest", "profile",
		"archive", "codeBase", "classId", "code", "object", "lowsrc", "dynsrc", "dataSrc":
		return true
	// Attribute and character-data mutations also reach URL attributes or CSS:
	// https://dom.spec.whatwg.org/#interface-element
	// https://dom.spec.whatwg.org/#interface-attr
	// https://dom.spec.whatwg.org/#interface-characterdata
	case "setAttribute", "setAttributeNS", "setAttributeNode", "setAttributeNodeNS", "attributes",
		"setNamedItem", "setNamedItemNS", "getAttributeNode", "getAttributeNodeNS", "nodeValue", "textContent",
		"appendData", "insertData", "deleteData", "replaceData", "splitText", "replaceWholeText":
		return true
	// CSSOM setters may introduce url() or @import without a scanned stylesheet:
	// https://drafts.csswg.org/cssom/#the-cssstyledeclaration-interface
	// https://drafts.csswg.org/cssom/#the-cssstylesheet-interface
	case "style", "cssText", "setProperty", "insertRule", "addRule", "replaceSync":
		return true
	// Resource constructors and browser APIs have no literal-call model here:
	// https://html.spec.whatwg.org/multipage/embedded-content.html
	// https://html.spec.whatwg.org/multipage/comms.html
	// https://fetch.spec.whatwg.org/#request-class
	// https://drafts.csswg.org/css-font-loading/#fontface-interface
	// https://w3c.github.io/beacon/#sendbeacon-method
	// https://w3c.github.io/ServiceWorker/#service-worker-container-register
	case "Image", "Audio", "Video", "Option", "Request", "FontFace", "XMLHttpRequest", "EventSource", "WebSocket",
		"sendBeacon", "importScripts", "serviceWorker", "register", "load", "play", "send":
		return true
	// Navigation, form submission and activation can request a new document:
	// https://html.spec.whatwg.org/multipage/nav-history-apis.html
	// https://html.spec.whatwg.org/multipage/form-control-infrastructure.html
	case "location", "assign", "replace", "open", "navigation", "navigate", "reload", "submit", "requestSubmit", "click":
		return true
	// Dynamic code, custom-element activation and reflection can hide loaders:
	// https://html.spec.whatwg.org/multipage/custom-elements.html
	// https://tc39.es/ecma262/#sec-function-constructor
	case "eval", "Function", "Reflect", "getOwnPropertyDescriptor", "getOwnPropertyDescriptors",
		"constructor", "__proto__", "getPrototypeOf", "setPrototypeOf", "defineProperty", "defineProperties",
		"customElements", "define", "upgrade":
		return true
	}
	return false
}
