package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// currentMainHostAmbientBaseline is the compatibility surface present before
// the typed host move. Stack 03 may shrink this set, but it must not grow it.
var currentMainHostAmbientBaseline = map[string]bool{
	"__gosx_apply_patch_mailbox": true,
	"__gosx_apply_patches":       true, "__gosx_apply_scene_command_scripts": true,
	"__gosx_bootstrap_page": true, "__gosx_current_event": true,
	"__gosx_current_handler": true, "__gosx_declarative_actions": true,
	"__gosx_declarative_regions": true, "__gosx_disconnect_hub": true,
	"__gosx_dispose_compute_island": true, "__gosx_dispose_controller": true,
	"__gosx_dispose_declarative_regions": true, "__gosx_dispose_engine": true,
	"__gosx_dispose_island": true, "__gosx_dispose_page": true,
	"__gosx_dispose_runtime_content": true, "__gosx_dispose_runtime_surfaces": true,
	"__gosx_engine_frame": true, "__gosx_loaded_scripts": true,
	"__gosx_mount_declarative_regions": true, "__gosx_mount_runtime_content": true,
	"__gosx_mount_runtime_surfaces": true, "__gosx_mount_stream_templates": true,
	"__gosx_page_cache": true, "__gosx_page_nav": true,
	"__gosx_reconcile_runtime_content": true, "__gosx_register_runtime_surface": true,
	"__gosx_relay_configure": true, "__gosx_relay_enabled": true,
	"__gosx_relay_flush_inbound": true, "__gosx_relay_register_peer": true,
	"__gosx_relay_send": true, "__gosx_replace_runtime_content": true,
	"__gosx_replace_runtime_fragment": true, "__gosx_reusable_engines": true,
	"__gosx_runtime_dom": true, "__gosx_runtime_surface_factories": true,
	"__gosx_runtime_surfaces": true, "__gosx_stream_templates": true,
	"__gosx_stripe": true, "__gosx_submit_action": true,
}

func TestHostAmbientCompatibilityDoesNotIncrease(t *testing.T) {
	clientJS := shippedClientJS(t)
	hostDir := filepath.Join(clientJS, "..", "runtime", "host")
	adapterPath := filepath.Join(hostDir, "compatibility.ts")
	adapterBytes, err := os.ReadFile(adapterPath)
	if err != nil {
		t.Fatal(err)
	}
	adapter := string(adapterBytes)
	if !strings.Contains(adapter, "var GOSX_HOST_COMPATIBILITY_ALLOWLIST") ||
		!strings.Contains(adapter, "var gosxHostCompatibility") {
		t.Fatal("compatibility adapter must be safe to evaluate more than once across lazy chunks")
	}

	entryRE := regexp.MustCompile(`\{ name: "(__gosx[A-Za-z0-9_]*)", owner: "([^"]+)", removeAfter: "([^"]+)" \}`)
	allowlist := make(map[string]bool)
	for _, match := range entryRE.FindAllStringSubmatch(adapter, -1) {
		name := match[1]
		if allowlist[name] {
			t.Fatalf("duplicate compatibility name %q", name)
		}
		allowlist[name] = true
		if !currentMainHostAmbientBaseline[name] {
			t.Errorf("ambient compatibility name %q was not in the current-main baseline", name)
		}
		if strings.TrimSpace(match[2]) == "" || strings.TrimSpace(match[3]) == "" {
			t.Errorf("compatibility name %q lacks owner/removal metadata", name)
		}
	}
	if len(allowlist) == 0 {
		t.Fatal("compatibility allowlist is empty or unparsable")
	}

	directWriteRE := regexp.MustCompile(`(?:window\.__gosx[A-Za-z0-9_]*\s*=(?:[^=])|delete\s+window\.__gosx[A-Za-z0-9_]*)`)
	installRE := regexp.MustCompile(`gosxHostCompatibility\.install\("(__gosx[A-Za-z0-9_]*)"`)
	entries, err := os.ReadDir(hostDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".ts" || entry.Name() == "compatibility.ts" {
			continue
		}
		bodyBytes, err := os.ReadFile(filepath.Join(hostDir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		body := string(bodyBytes)
		if write := directWriteRE.FindString(body); write != "" {
			t.Errorf("%s writes ambient compatibility outside the adapter: %s", entry.Name(), write)
		}
		for _, match := range installRE.FindAllStringSubmatch(body, -1) {
			if !allowlist[match[1]] {
				t.Errorf("%s installs unallowlisted ambient name %q", entry.Name(), match[1])
			}
		}
	}
}

func TestBuilderUsesTypedHostAuthorities(t *testing.T) {
	wanted := map[string]bool{
		"../runtime/host/actions.ts": false, "../runtime/host/regions.ts": false,
		"../runtime/host/controllers.ts": false, "../runtime/host/dom.ts": false,
		"../runtime/host/facade.ts": false, "../runtime/host/stream.ts": false,
		"../runtime/host/events.ts": false, "../runtime/host/hubs.ts": false,
		"../runtime/host/disposal.ts": false, "../runtime/host/engine-disposal.ts": false,
		"../runtime/host/hub-disposal.ts": false, "../runtime/host/page-disposal.ts": false,
		"../runtime/host/hydration.ts": false, "../runtime/host/patch.ts": false,
		"../runtime/host/relay.ts": false, "../runtime/host/stripe-bridge.ts": false,
	}
	for _, entry := range outputs {
		for _, src := range entry.sources {
			if _, ok := wanted[src.rel]; ok {
				wanted[src.rel] = true
			}
			if strings.HasSuffix(src.rel, "06-declarative-actions.js") ||
				strings.HasSuffix(src.rel, "07-declarative-regions.js") ||
				strings.HasSuffix(src.rel, "08-controllers.js") ||
				strings.Contains(src.rel, "30a-tail-event-delegation.js") {
				t.Errorf("%s still references retired host authority %s", entry.name, src.rel)
			}
		}
	}
	for source, found := range wanted {
		if !found {
			t.Errorf("typed host authority is absent from the build graph: %s", source)
		}
	}
}

// TestEveryRuntimeTypeScriptAuthorityIsInTheBuildGraph closes the rename-only
// loophole: a .ts suffix is not evidence that source is shipped or even
// transpiled. Every runtime authority must feed a checked output. Declarations
// and the generated ABI are compile-time inputs consumed by an authority and
// are the only explicit exceptions.
func TestEveryRuntimeTypeScriptAuthorityIsInTheBuildGraph(t *testing.T) {
	clientJS := shippedClientJS(t)
	runtimeDir := filepath.Clean(filepath.Join(clientJS, "..", "runtime"))
	reachable := make(map[string]bool)
	for _, entry := range outputs {
		for _, src := range entry.sources {
			absolute := filepath.Clean(filepath.Join(clientJS, filepath.FromSlash(src.rel)))
			if strings.HasPrefix(absolute, runtimeDir+string(filepath.Separator)) {
				reachable[absolute] = true
			}
		}
	}
	exempt := map[string]string{
		filepath.Join(runtimeDir, "types.d.ts"):                  "strict-check ambient declarations",
		filepath.Join(runtimeDir, "generated", "runtime-abi.ts"): "consumed by wasm/abi.ts and checked by test-runtime-types",
		filepath.Join(runtimeDir, "host", "navigation.ts"):       "embedded by host/navigation_asset.go and exercised by the runtime test harness",
	}
	err := filepath.WalkDir(runtimeDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || (!strings.HasSuffix(path, ".ts") && !strings.HasSuffix(path, ".d.ts")) {
			return nil
		}
		if !reachable[path] && exempt[path] == "" {
			t.Errorf("runtime TypeScript authority is outside the semantic build graph: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
