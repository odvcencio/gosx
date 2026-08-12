// @ts-check
// GoSX browser host: the single compatibility adapter for ambient browser names.
//
// Host modules communicate through window.__gosx.host. Legacy ambient names
// exist only for Go/WASM and pre-facade browser consumers, and every one has an
// owner plus an explicit removal checkpoint.

/**
 * @typedef {{name: string, owner: string, removeAfter: string}} GoSXHostCompatibilityEntry
 */

/** @type {GoSXHostCompatibilityEntry[]} */
var GOSX_HOST_COMPATIBILITY_ALLOWLIST = [
  { name: "__gosx_apply_patches", owner: "host/patch", removeAfter: "Stack 07 compatibility audit" },
  { name: "__gosx_apply_scene_command_scripts", owner: "host/regions", removeAfter: "Stack 07 compatibility audit" },
  { name: "__gosx_bootstrap_page", owner: "host/lifecycle", removeAfter: "Stack 07 compatibility audit" },
  { name: "__gosx_current_event", owner: "host/events", removeAfter: "Stack 07 compatibility audit" },
  { name: "__gosx_current_handler", owner: "host/events", removeAfter: "Stack 07 compatibility audit" },
  { name: "__gosx_declarative_actions", owner: "host/actions", removeAfter: "Stack 07 compatibility audit" },
  { name: "__gosx_declarative_regions", owner: "host/regions", removeAfter: "Stack 07 compatibility audit" },
  { name: "__gosx_disconnect_hub", owner: "host/hubs", removeAfter: "Stack 07 compatibility audit" },
  { name: "__gosx_dispose_compute_island", owner: "host/islands", removeAfter: "Stack 07 compatibility audit" },
  { name: "__gosx_dispose_controller", owner: "host/controllers", removeAfter: "Stack 07 compatibility audit" },
  { name: "__gosx_dispose_declarative_regions", owner: "host/regions", removeAfter: "Stack 07 compatibility audit" },
  { name: "__gosx_dispose_engine", owner: "host/engines", removeAfter: "Stack 07 compatibility audit" },
  { name: "__gosx_dispose_island", owner: "host/islands", removeAfter: "Stack 07 compatibility audit" },
  { name: "__gosx_dispose_page", owner: "host/lifecycle", removeAfter: "Stack 07 compatibility audit" },
  { name: "__gosx_dispose_runtime_content", owner: "host/dom", removeAfter: "Stack 07 compatibility audit" },
  { name: "__gosx_dispose_runtime_surfaces", owner: "host/facade", removeAfter: "Stack 07 compatibility audit" },
  { name: "__gosx_engine_frame", owner: "host/engines", removeAfter: "Stack 07 compatibility audit" },
  { name: "__gosx_loaded_scripts", owner: "host/navigation", removeAfter: "Stack 07 compatibility audit" },
  { name: "__gosx_mount_declarative_regions", owner: "host/regions", removeAfter: "Stack 07 compatibility audit" },
  { name: "__gosx_mount_runtime_content", owner: "host/dom", removeAfter: "Stack 07 compatibility audit" },
  { name: "__gosx_mount_runtime_surfaces", owner: "host/facade", removeAfter: "Stack 07 compatibility audit" },
  { name: "__gosx_mount_stream_templates", owner: "host/stream", removeAfter: "Stack 07 compatibility audit" },
  { name: "__gosx_page_cache", owner: "host/navigation", removeAfter: "Stack 07 compatibility audit" },
  { name: "__gosx_page_nav", owner: "host/navigation", removeAfter: "Stack 07 compatibility audit" },
  { name: "__gosx_reconcile_runtime_content", owner: "host/dom", removeAfter: "Stack 07 compatibility audit" },
  { name: "__gosx_register_runtime_surface", owner: "host/facade", removeAfter: "Stack 07 compatibility audit" },
  { name: "__gosx_relay_configure", owner: "host/relay", removeAfter: "Stack 07 compatibility audit" },
  { name: "__gosx_relay_enabled", owner: "host/relay", removeAfter: "Stack 07 compatibility audit" },
  { name: "__gosx_relay_flush_inbound", owner: "host/relay", removeAfter: "Stack 07 compatibility audit" },
  { name: "__gosx_relay_register_peer", owner: "host/relay", removeAfter: "Stack 07 compatibility audit" },
  { name: "__gosx_relay_send", owner: "host/relay", removeAfter: "Stack 07 compatibility audit" },
  { name: "__gosx_replace_runtime_content", owner: "host/dom", removeAfter: "Stack 07 compatibility audit" },
  { name: "__gosx_replace_runtime_fragment", owner: "host/dom", removeAfter: "Stack 07 compatibility audit" },
  { name: "__gosx_reusable_engines", owner: "host/lifecycle", removeAfter: "Stack 07 compatibility audit" },
  { name: "__gosx_runtime_dom", owner: "host/dom", removeAfter: "Stack 07 compatibility audit" },
  { name: "__gosx_runtime_surface_factories", owner: "host/facade", removeAfter: "Stack 07 compatibility audit" },
  { name: "__gosx_runtime_surfaces", owner: "host/facade", removeAfter: "Stack 07 compatibility audit" },
  { name: "__gosx_stream_templates", owner: "host/stream", removeAfter: "Stack 07 compatibility audit" },
  { name: "__gosx_stripe", owner: "host/stripe", removeAfter: "Stack 07 compatibility audit" },
  { name: "__gosx_submit_action", owner: "host/navigation", removeAfter: "Stack 07 compatibility audit" },
];

var gosxHostNamespace = window.__gosx || (window.__gosx = {});
var gosxHost = gosxHostNamespace.host || (gosxHostNamespace.host = {});
var gosxHostCompatibilityMetadata = new Map(
  GOSX_HOST_COMPATIBILITY_ALLOWLIST.map(function(entry) { return [entry.name, entry]; })
);

var gosxHostCompatibility = gosxHost.compatibility || {
  metadata: gosxHostCompatibilityMetadata,
  install(name, value) {
    if (!gosxHostCompatibilityMetadata.has(name)) {
      throw new Error("[gosx] unowned ambient compatibility name: " + name);
    }
    window[name] = value;
    return value;
  },
  read(name) {
    if (!gosxHostCompatibilityMetadata.has(name)) {
      throw new Error("[gosx] unowned ambient compatibility read: " + name);
    }
    return window[name];
  },
  clear(name) {
    if (!gosxHostCompatibilityMetadata.has(name)) {
      throw new Error("[gosx] unowned ambient compatibility clear: " + name);
    }
    try {
      delete window[name];
    } catch (_) {
      window[name] = undefined;
    }
  },
  forward(name, args) {
    const fn = this.read(name);
    if (typeof fn === "function") return fn.apply(window, args || []);
    return undefined;
  },
};
gosxHost.compatibility = gosxHostCompatibility;

// During the stacked migration, navigation.ts is already served from its
// typed authority while the generated bootstrap remains at the last release.
// These typed facade shims resolve the legacy implementation at call time, so
// either load order preserves current-main behavior without letting production
// internals call ambient names directly.
gosxHost.dom = gosxHost.dom || {
  replace: function() {
    const fn = gosxHostCompatibility.read("__gosx_replace_runtime_content");
    return typeof fn === "function" ? fn.apply(window, arguments) : false;
  },
  replaceFragment: function() {
    const fn = gosxHostCompatibility.read("__gosx_replace_runtime_fragment");
    return typeof fn === "function" ? fn.apply(window, arguments) : false;
  },
  reconcile: function() {
    const fn = gosxHostCompatibility.read("__gosx_reconcile_runtime_content");
    return typeof fn === "function" ? fn.apply(window, arguments) : false;
  },
};
gosxHost.regions = gosxHost.regions || {
  mount: function() { return gosxHostCompatibility.forward("__gosx_mount_declarative_regions", arguments); },
  dispose: function() { return gosxHostCompatibility.forward("__gosx_dispose_declarative_regions", arguments); },
};
gosxHost.surfaces = gosxHost.surfaces || {
  mount: function() { return gosxHostCompatibility.forward("__gosx_mount_runtime_surfaces", arguments); },
  dispose: function() { return gosxHostCompatibility.forward("__gosx_dispose_runtime_surfaces", arguments); },
};
gosxHost.lifecycle = gosxHost.lifecycle || {
  bootstrapPage: function() { return gosxHostCompatibility.forward("__gosx_bootstrap_page", arguments); },
  disposePage: function() { return gosxHostCompatibility.forward("__gosx_dispose_page", arguments); },
};
