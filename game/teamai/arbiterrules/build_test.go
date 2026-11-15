//go:build !js || !wasm

package arbiterrules

import (
	"bytes"
	"flag"
	"os"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata/*.bundle from testdata/*.arb")

// TestBundlesAreCurrent rebuilds the test bundles from source. The other tests,
// which also run as js/wasm, load the bundles because js/wasm builds do not
// link the .arb compiler.
func TestBundlesAreCurrent(t *testing.T) {
	for _, name := range []string{"reads", "bad"} {
		src, err := os.ReadFile("testdata/" + name + ".arb")
		if err != nil {
			t.Fatal(err)
		}
		bundle, err := Bundle(src)
		if err != nil {
			t.Fatalf("%s.arb: %v", name, err)
		}
		path := "testdata/" + name + ".bundle"
		if *update {
			if err := os.WriteFile(path, bundle, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		have, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(have, bundle) {
			t.Errorf("%s is stale; run go test -run TestBundlesAreCurrent -update", path)
		}
	}
}

func TestCompileMatchesLoad(t *testing.T) {
	src, err := os.ReadFile("testdata/reads.arb")
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(src)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(readsBundle)
	if err != nil {
		t.Fatal(err)
	}
	if compiled.Digest() != loaded.Digest() {
		t.Fatal("Compile and Load of the same source give different programs")
	}
	if got := strings.Join(compiled.Names(), ","); got != "LaneOpen,Crowded,Late,PickDeep,PickShort" {
		t.Fatalf("rule names %q, want them kept", got)
	}
}

func TestGovernanceIsRefused(t *testing.T) {
	_, err := Compile([]byte(`
rule Slow {
    rollout 50
    when { self.speed > 1 }
    then Read { branch: "x" }
}`))
	if err == nil {
		t.Fatal("a rule with a rollout compiled for the game runtime")
	}
	t.Logf("refused: %v", err)
}
