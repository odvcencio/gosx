package wire

import (
	"encoding/json"
	"reflect"
	"strings"

	"m31labs.dev/gosx/controller"
	"m31labs.dev/gosx/hydrate"
)

// This is an explicit inventory, not a default metadata policy. The schema
// coverage test walks the producer structs independently; new fields must be
// classified here before a scan can certify them. Loader locations explain both
// fetch selection and why metadata does not independently select a URL.
type manifestFieldClass uint8

const (
	manifestFetchedNow manifestFieldClass = iota + 1
	manifestFetchedGated
	manifestMetadata
)

type manifestFieldPolicy struct {
	class             manifestFieldClass
	modelled          bool
	loader, rationale string
}

var manifestFieldPolicies = func() map[string]manifestFieldPolicy {
	policies := map[string]manifestFieldPolicy{}
	add := func(prefix, fields string, class manifestFieldClass, modelled bool, loader, rationale string) {
		for _, field := range strings.Fields(fields) {
			path := prefix + field
			if _, duplicate := policies[path]; duplicate {
				panic("duplicate manifest policy: " + path)
			}
			policies[path] = manifestFieldPolicy{class, modelled, loader, rationale}
		}
	}
	add("", "features", manifestFetchedGated, false, "client/js/bootstrap-src/26-runtime-tail.ts:234,258-273", "Opt-in feature names select chunk URLs from the bootstrap; loader variant and chunk gates are not modelled.")
	add("", "preview", manifestFetchedGated, false, "client/js/bootstrap-src/26-runtime-tail.ts:252-255,30k-tail-init.ts:65-67", "Preview relay state conditionally selects the runtime; that environment gate remains potential and incomplete.")
	add("", "version", manifestMetadata, true, "client/js/bootstrap-src/10-runtime-scene-utils.ts:51-68", "Schema version; parsing selects no URL. The scanner separately rejects unsupported versions.")
	add("", "bundles", manifestMetadata, true, "client/runtime/host/hydration.ts:65-92", "Typed inventory with bundle-ID keys. Current hydration requires programRef; the scanner retains the earlier selected-bundle compatibility references conservatively.")
	add("bundles{}.", "path", manifestMetadata, true, "client/runtime/host/hydration.ts:65-92", "Current hydration requires programRef rather than bundle paths. Existing selected-bundle compatibility references remain counted; unselected paths stay dormant.")
	add("bundles{}.", "size hash", manifestMetadata, true, "client/runtime/host/hydration.ts:65-92", "Bundle byte/hash metadata is not consumed as a URL by hydration.")
	add("runtime.", "path", manifestFetchedGated, true, "client/js/bootstrap-src/26-runtime-tail.ts:485-496", "Runtime consumer selects path; bridge-only consumers remain potential and incomplete.")
	add("runtime.", "hash manifestHash size variant featureMask", manifestMetadata, true, "client/js/bootstrap-src/26-runtime-tail.ts:485-496", "ABI/cache/size declarations; the loader URL comes from path.")
	for _, prefix := range []string{"islands[].", "computeIslands[]."} {
		add(prefix, "id component bundleId programFormat programHash", manifestMetadata, true, "client/runtime/host/hydration.ts:53-104", "Identity, bundle selection and format/hash metadata; URLs are handled at programRef or the selected bundle path.")
		add(prefix, "programRef", manifestFetchedGated, true, "client/runtime/host/hydration.ts:65-104", "Program fetch selected by hydration and its static/capability gates.")
		add(prefix, "props", manifestFetchedGated, false, "client/runtime/host/hydration.ts:109-142,204-211", "Opaque JSON passed to application hydration; URL-bearing payload and application gates are not modelled.")
	}
	add("islands[].", "static checksum", manifestMetadata, true, "client/runtime/host/hydration.ts:9-17", "Hydration gate and cache identity; neither supplies a fetch target.")
	add("islands[].events[].", "slotId eventType targetSelector handlerName serverAction", manifestMetadata, true, "client/runtime/host/events.ts:209-249", "Listener/handler metadata; the loader reads eventType to install delegation, not a URL from these fields.")
	add("computeIslands[].", "capabilities requiredCapabilities", manifestMetadata, true, "client/runtime/host/hydration.ts:20-32", "Capability tokens gate hydration; programRef supplies its fetch target.")
	add("engines[].", "id component kind mountId runtime capabilities requiredCapabilities", manifestMetadata, true, "client/js/bootstrap-src/30b-tail-engine-mounting.ts:362-487,837-865", "Mount identity, factory and capability/runtime gates; programRef/props carry targets separately.")
	add("engines[].", "programRef", manifestFetchedGated, true, "client/js/bootstrap-src/30b-tail-engine-mounting.ts:219,837", "Engine program selected by runtime or nonempty programRef.")
	add("engines[].", "props", manifestFetchedGated, false, "client/js/bootstrap-src/30b-tail-engine-mounting.ts:2844,3735,3795", "Opaque engine/video/scene configuration carries fetch and beacon URLs with unmodelled gates.")
	add("engines[].pixelSurface.", "width height scaling clearColor vsync", manifestMetadata, true, "client/js/bootstrap-src/30b-tail-engine-mounting.ts:543-560", "Canvas dimensions, presentation and timing; no URL loader.")
	add("hubs[].", "id name", manifestMetadata, true, "client/runtime/host/hubs.ts:24-25,466-475", "Connection identity and selection gate; path supplies the socket URL.")
	add("hubs[].", "path", manifestFetchedNow, false, "client/runtime/host/hubs.ts:466-475", "Selected id/path opens a WebSocket; transport/reference accounting is not modelled.")
	add("hubs[].bindings[].", "event signal direction throttleMs debounceMs sceneMountId sceneCommands sceneInput sceneInputKind refreshDebounceMs refreshPreserveScroll", manifestMetadata, true, "client/runtime/host/hubs.ts:80-151,225-264", "Event/signal/scene routing and refresh timing; no independent URL value.")
	add("hubs[].bindings[].", "refresh", manifestFetchedGated, false, "client/runtime/host/hubs.ts:151,225-264", "Inbound-event gate revalidates the current document; repeated navigation is not modelled.")
	add("hubs[].roundTrip.", "signal pingEvent pongEvent intervalMs", manifestMetadata, true, "client/runtime/host/hubs.ts:84-145", "Heartbeat events and cadence on an existing socket; no new URL.")
	add("hubs[].input.", "mode event readyEvent trainingEvent signal trainingSignal touchRoot player local spectator slotToken sendEveryMs root username minLocalGamepads attractSignal lobbySignal vsSignal", manifestMetadata, true, "client/js/bootstrap-src/30c1-tail-hub-fight-input.ts:16-62", "Input translation, selectors, signal/event names and payload; endpoint/navigation URLs are classified separately.")
	add("hubs[].input.", "fightPath cpuEndpoint localEndpoint fightCurrentEndpoint", manifestFetchedGated, false, "client/js/bootstrap-src/30c1-tail-hub-fight-input.ts:928-986", "User/match gates select API fetches and navigation; these gates are not modelled.")
	add("clientIdentity.", "storageKey cookieName legacyCookieNames headerName globalName prefix maxAgeSeconds sameSite", manifestMetadata, true, "client/runtime/host/hubs.ts:275-373", "Browser identity storage, cookie and header configuration; no independently fetched URL.")
	add("", "textureVariants", manifestFetchedGated, false, "client/runtime/scene3d/gltf.ts:2903-2906,3012-3063", "Map keys select a source texture; device/quality selection and variant fetches are not modelled.")
	add("textureVariants{}[].", "uri", manifestFetchedGated, false, "client/runtime/scene3d/gltf.ts:2963-2982,3042-3055", "Device-qualified chosen URI replaces a fetched image; selection is not modelled.")
	add("textureVariants{}[].", "quality bytes requiredCapabilities", manifestMetadata, true, "client/runtime/scene3d/gltf.ts:2963-3010", "Ranking/eligibility metadata; URI is the fetch target.")
	add("commands[].", "id", manifestMetadata, true, "client/runtime/host/workbench.ts:92-96,135-137,147-149", "Registry lookup and command-event identity; no URL selection.")
	add("commands[].", "title", manifestMetadata, true, "client/runtime/host/workbench.ts:147", "Command-list display metadata; no URL loader.")
	add("commands[].", "keys", manifestMetadata, true, "client/runtime/host/workbench.ts:48-82,136-142,156", "Parsed keyboard chords and ARIA shortcut labels; strings are never loader URLs.")
	add("commands[].", "when", manifestMetadata, true, "client/runtime/host/workbench.ts:89-90", "Modelled condition map: keys name shared signals and JSON values are compared for equality, never passed to a URL loader.")
	add("commands[].", "allowEditable", manifestMetadata, true, "client/runtime/host/workbench.ts:108-113", "Editable-target dispatch gate; no URL selection.")
	add("commands[].", "group", manifestMetadata, true, "client/runtime/host/workbench.ts:147", "Command-list grouping metadata; no URL loader.")
	add("commands[].", "reserved", manifestMetadata, true, "client/runtime/host/workbench.ts:89-90,114-118,142-147", "Availability, chord consumption and ARIA state; no action or URL loading.")
	add("commands[].action.", "signal", manifestMetadata, true, "client/runtime/host/workbench.ts:97-98", "Shared-signal routing name; no independently selected URL.")
	add("commands[].action.", "open", manifestMetadata, true, "client/runtime/host/workbench.ts:99-103,client/runtime/host/disclosure.ts:188-216", "DOM selector passed to disclosure open for visibility and focus; never used as a loader URL.")
	add("commands[].action.", "close", manifestMetadata, true, "client/runtime/host/workbench.ts:99-103,client/runtime/host/disclosure.ts:219-236", "DOM selector passed to disclosure close for visibility and focus; never used as a loader URL.")
	add("commands[].action.", "value", manifestMetadata, true, "client/runtime/host/workbench.ts:97-98,client/js/bootstrap-src/00-textlayout.ts:205-207", "Modelled command payload: JSON is written to a shared signal, never interpreted as a URL by the workbench loader.")
	add("controllers[].", "id", manifestMetadata, true, "client/runtime/host/controllers.ts:341-359", "Mount identity gate; config supplies the controller declarations.")
	add("controllers[].", "config", manifestFetchedGated, false, "client/runtime/host/controllers.ts:341-374", "Controller configuration may declare immediate/polling/refresh fetches or forward opaque URL-bearing values; configuration is not modelled.")
	add("controllers[].config.", "name root disposeAll", manifestMetadata, true, "client/runtime/host/controllers.ts:49-57,341-395", "Controller identity, DOM scope and disposal; no URL selection.")
	add("controllers[].config.inputs[].", "name signal output immediate", manifestMetadata, true, "client/runtime/host/controllers.ts:170-190", "Shared-signal subscription and output routing; no direct URL loader.")
	add("controllers[].config.outputs[].", "name signal event", manifestMetadata, true, "client/runtime/host/controllers.ts:11-38,162-167", "Signal/DOM-event routing names; no direct URL loader.")
	add("controllers[].config.outputs[].", "initial", manifestFetchedGated, false, "client/runtime/host/controllers.ts:162-167", "Opaque value forwarded to application signals; downstream URL consumers are not modelled.")
	add("controllers[].config.events[].", "type target output preventDefault stopPropagation capture allowEditable", manifestMetadata, true, "client/runtime/host/controllers.ts:193-202", "Delegated event selector/options and output routing; no URL selection.")
	add("controllers[].config.keys[].", "key code output event scope modifiers preventDefault allowEditable", manifestMetadata, true, "client/runtime/host/controllers.ts:204-213", "Keyboard selector/options and output routing; no URL selection.")
	add("controllers[].config.keys[].", "anyModifiers", manifestMetadata, true, "client/runtime/host/controllers.ts:164-170", "Exact versus subset keyboard modifier matching; no URL loading.")
	for _, prefix := range []string{"controllers[].config.events[].project.", "controllers[].config.keys[].project.", "controllers[].config.drags[].project."} {
		add(prefix, "value when", manifestFetchedGated, false, "client/runtime/host/controller-input.ts:23-38", "Opaque constants/conditions may contain URLs and feed application signals; payload gates are not modelled.")
		add(prefix, "fields", manifestMetadata, true, "client/runtime/host/controller-input.ts:13-18,33-36", "Modelled map: keys name output fields, string values are dotted property-read selectors, never fetch URLs.")
	}
	add("controllers[].config.drags[].", "source output startOutput moveOutput cancelOutput thresholdPx preventDefault", manifestMetadata, true, "client/runtime/host/controller-input.ts:103-186", "Pointer gesture selectors, output routing and timing; no direct URL loading.")
	add("controllers[].config.drags[].targets[].", "target scene hitIds requestOutput resultSignal timeoutMs", manifestMetadata, true, "client/runtime/host/controller-input.ts:152-185", "DOM/scene hit selection and signal routing; no URL selection.")
	add("controllers[].config.focus[].", "target openSignal initialFocus returnFocus", manifestMetadata, true, "client/runtime/host/controller-input.ts:211-254", "Focus selectors and open-state subscription; no URL selection.")
	add("controllers[].config.timers[].", "name output everyMs immediate", manifestMetadata, true, "client/runtime/host/controllers.ts:215-234", "Timer scheduling and output routing; no direct URL loader.")
	add("controllers[].config.timers[].", "payload", manifestFetchedGated, false, "client/runtime/host/controllers.ts:215-234", "Opaque timer payload forwarded to signals; downstream URL consumers are not modelled.")
	add("controllers[].config.", "resources", manifestFetchedGated, false, "client/runtime/host/controllers.ts:320-337", "Every resource declaration remains unresolved, including disabled immediate, polling and refresh-signal cases.")
	add("controllers[].config.resources[].", "url", manifestFetchedGated, false, "client/runtime/host/controllers.ts:247-248,251-337", "Defaults to immediate fetch, with optional polling/refresh; URL and gates are not modelled.")
	add("controllers[].config.resources[].", "name method bodySignal output errorOutput refreshSignal pollMs immediate", manifestMetadata, true, "client/runtime/host/controllers.ts:251-337", "Request method, signal routing and loading gates; enclosing resource/URL remain unresolved.")
	add("controllers[].config.resources[].", "headers body", manifestFetchedGated, false, "client/runtime/host/controllers.ts:265-280", "Free-form request headers/body can contain URLs and application payload; request semantics are not modelled.")
	add("controllers[].config.storage.", "area namespace", manifestMetadata, true, "client/runtime/host/controller-input.ts:257-270", "Browser storage area/key namespace; no URL loading.")
	for _, prefix := range []string{"controllers[].config.storage.load[].", "controllers[].config.storage.save[]."} {
		add(prefix, "key signal output", manifestMetadata, true, "client/runtime/host/controller-input.ts:272-310", "Storage keys and signal/output routing, not network URLs.")
	}
	return policies
}()

// Every inspected value goes through drop. Modelled fetch targets were handled
// by the selected-reference pass; opaque/unsupported declarations fail closed.
func classifyManifestField(path string, value any, out *referenceScanner) {
	policy, ok := manifestFieldPolicies[path]
	if !ok || policy.class < manifestFetchedNow || policy.class > manifestMetadata {
		out.drop(dropUnresolved)
	} else if !policy.modelled && !emptyManifestValue(value) {
		out.drop(dropUnresolved)
	} else if policy.modelled && policy.class != manifestMetadata {
		out.drop(dropNestedScan)
	} else {
		out.drop(dropManifestMetadata)
	}
}

func emptyManifestValue(value any) bool {
	switch v := value.(type) {
	case nil:
		return true
	case string:
		return v == ""
	case bool:
		return !v
	case float64:
		return v == 0
	case []any:
		return len(v) == 0
	case map[string]any:
		return len(v) == 0
	default:
		return false
	}
}

func scanManifestFields(raw string, out *referenceScanner) {
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		out.drop(dropUnresolved)
		return
	}
	walkManifestFields(reflect.TypeFor[hydrate.Manifest](), "", value, out)
}

// Walk present JSON values against the Go schema. Unknown fields, new unlisted
// scalar types, and free-form payloads cannot inherit a metadata exemption.
func walkManifestFields(typ reflect.Type, path string, value any, out *referenceScanner) {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ == reflect.TypeFor[json.RawMessage]() || typ.Kind() == reflect.Interface {
		classifyManifestField(path, value, out)
		return
	}
	switch typ.Kind() {
	case reflect.Struct:
		if typ == reflect.TypeFor[controller.Config]() {
			classifyManifestField(path, value, out)
		}
		object, ok := value.(map[string]any)
		if !ok {
			if value == nil {
				out.drop(dropManifestMetadata)
			} else {
				out.drop(dropUnresolved)
			}
			return
		}
		fieldTypes := map[string]reflect.Type{}
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			name := strings.Split(f.Tag.Get("json"), ",")[0]
			if f.PkgPath != "" || name == "-" {
				continue
			}
			if name == "" {
				name = f.Name
			}
			fieldTypes[name] = f.Type
		}
		out.drop(dropNestedScan)
		for name, child := range object {
			next := name
			if path != "" {
				next = path + "." + name
			}
			if childType, known := fieldTypes[name]; known {
				walkManifestFields(childType, next, child, out)
			} else {
				out.drop(dropUnresolved)
			}
		}
	case reflect.Map:
		classifyManifestField(path, value, out)
		elem := typ.Elem()
		for elem.Kind() == reflect.Slice {
			elem = elem.Elem()
		}
		if elem.Kind() != reflect.Struct {
			// The entire opaque map was classified above.
			return
		}
		if object, ok := value.(map[string]any); ok {
			for _, child := range object {
				walkManifestFields(typ.Elem(), path+"{}", child, out)
			}
		}
	case reflect.Slice, reflect.Array:
		if typ.Elem().Kind() != reflect.Struct {
			classifyManifestField(path, value, out)
			return
		}
		if typ.Elem() == reflect.TypeFor[controller.FetchResource]() {
			classifyManifestField(path, value, out)
		}
		out.drop(dropNestedScan)
		if array, ok := value.([]any); ok {
			for _, child := range array {
				walkManifestFields(typ.Elem(), path+"[]", child, out)
			}
		}
	default:
		classifyManifestField(path, value, out)
	}
}
