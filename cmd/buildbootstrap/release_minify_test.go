package main

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Exercise constructs used by the runtime: receiver-sensitive optional calls,
// nullish defaults, closures, async cleanup, typed arrays and shader templates.
const releaseBehaviorFixture = `
globalThis.result = (async function () {
  const events = [];
  const target = {
    value: 3,
    advance({ step = 1 } = {}) { this.value += step; return this.value; },
    get current() { events.push("read"); return this.value; },
  };
  const changed = target?.advance?.({ step: 2 });
  const zero = 0 ?? target.advance();
  const missing = null;
  const fallback = missing?.advance?.() ?? 7;
  const closures = [];
  for (const value of new Uint32Array([2, 4])) closures.push(() => value);
  try { await Promise.resolve(); events.push("awaited"); }
  finally { events.push("cleanup"); }
  const shader = ` + "`fn shade() {\n  return ${changed}.0;\n}`" + `;
  return {
    changed, zero, fallback, current: target.current,
    closures: closures.map(read => read()), events,
    shader: shader.replace("return 5.0", "return 6.0"),
    regexp: "a/b".replace(/\//g, "-"),
    negativeZeroEqualsPositive: Object.is(-0, 0),
    negativeZeroReciprocal: String(1 / -0), bigint: String(2n ** 4n),
  };
})();
//# sourceMappingURL=obsolete.js.map
`

func TestReleaseSecondPassSelectionAndDebugMaps(t *testing.T) {
	f := newFixture(t)
	rel := f.writeSource("runtime.js", releaseBehaviorFixture)
	for _, tc := range []struct {
		name   string
		second bool
	}{
		{"bootstrap.js", true},
		{"bootstrap-feature-scene3d.js", true},
		{"bootstrap-feature-scene3d-webgl.js", true},
		{"bootstrap-feature-scene3d-webgpu.js", true},
		{"bootstrap-feature-scene3d-animation.js", true},
		{"bootstrap-feature-scene3d-gltf.js", false},
		{"bootstrap-runtime.js", false},
		{"bootstrap-lite.js", false},
		{"patch.js", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entry := chunk(tc.name, rel)
			compacted, err := buildCompactedBundle(f.dir, entry)
			if err != nil {
				t.Fatal(err)
			}
			first, err := minifyESBuild(entry, compacted)
			if err != nil {
				t.Fatal(err)
			}
			second, err := minifyTdewolff(first.code)
			if err != nil {
				t.Fatal(err)
			}
			if normalizeGeneratedCode(first.code, "", false) == normalizeGeneratedCode(second, "", false) {
				t.Fatal("fixture cannot distinguish the two minification paths")
			}
			release, err := buildBundle(f.dir, entry, "esbuild", false)
			if err != nil {
				t.Fatal(err)
			}
			want := first.code
			if tc.second {
				want = second
			}
			if release.code != normalizeGeneratedCode(want, entry.name+".map", false) {
				t.Fatal("release selected the wrong minification path")
			}
			if strings.Contains(release.code, "sourceMappingURL=") {
				t.Fatal("release advertised a source map")
			}
			debug, err := buildBundle(f.dir, entry, "esbuild", true)
			if err != nil {
				t.Fatal(err)
			}
			if debug.code != normalizeGeneratedCode(first.code, entry.name+".map", true) || debug.m != first.m {
				t.Fatal("debug code and composed map must remain the original esbuild pair")
			}
			if strings.Count(debug.code, "sourceMappingURL=") != 1 || release.m != first.m {
				t.Fatal("map identity or debug trailer changed")
			}
		})
	}
}

func TestReleaseSecondPassPreservesModernJavaScriptBehavior(t *testing.T) {
	// Node is already provided by test-js CI; the Go bundle builder itself
	// retains its pure-Go dependency contract when Node is unavailable.
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required to execute the emitted JavaScript")
	}
	f := newFixture(t)
	rel := f.writeSource("runtime.js", releaseBehaviorFixture)
	sources := map[string]string{"original": releaseBehaviorFixture}
	for _, name := range []string{"bootstrap.js", "bootstrap-feature-scene3d.js", "bootstrap-feature-scene3d-webgl.js", "bootstrap-feature-scene3d-webgpu.js", "bootstrap-feature-scene3d-animation.js"} {
		for _, debug := range []bool{false, true} {
			built, err := buildBundle(f.dir, chunk(name, rel), "esbuild", debug)
			if err != nil {
				t.Fatal(err)
			}
			label := name + "/release"
			if debug {
				label = name + "/debug"
			}
			sources[label] = built.code
		}
	}
	input, err := json.Marshal(sources)
	if err != nil {
		t.Fatal(err)
	}
	const runner = `
const vm = require("node:vm");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const sources = JSON.parse(fs.readFileSync(0, "utf8"));
(async () => {
  const expected = { changed: 5, zero: 0, fallback: 7, current: 5,
    closures: [2, 4], events: ["awaited", "cleanup", "read"],
    shader: "fn shade() {\n  return 6.0;\n}", regexp: "a-b",
    negativeZeroEqualsPositive: false, negativeZeroReciprocal: "-Infinity",
    bigint: "16" };
  for (const [name, source] of Object.entries(sources)) {
    const context = vm.createContext({});
    vm.runInContext(source, context, { timeout: 1000 });
    assert.deepEqual(JSON.parse(JSON.stringify(await context.result)), expected, name);
  }
})().catch(error => { console.error(error); process.exitCode = 1; });
`
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, "-e", runner)
	cmd.Stdin = strings.NewReader(string(input))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("emitted JavaScript changed behavior: %v\n%s", err, output)
	}
}
