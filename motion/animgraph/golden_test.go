package animgraph

import (
	"encoding/hex"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var updateGolden = flag.Bool("update", false, "rewrite testdata golden files")

// goldenFrame encodes a fixed scripted frame. The TypeScript decoder test
// (client/js/scene3d-animation-graph.test.mjs) decodes the same bytes, so
// the Go encoder and the browser decoder cannot drift apart.
func goldenFrame(t *testing.T) []byte {
	t.Helper()
	g := locomotion()
	g.Layers[0].Machine.Entry = "Move"
	ik, look := Humanoid(false)
	g.IK, g.LookAt = ik, look
	g.Masks = []Mask{{Name: "upper", Include: []string{"spine"}}}
	g.Layers = append(g.Layers, Layer{Name: "aim", Mask: "upper", Additive: true, Machine: StateMachine{States: []State{{Name: "Aim", Clip: "Idle"}}}})
	p := mustProgram(t, g)
	in := p.New("hero")
	in.SetFloat("speed", 2.7)
	for i := 0; i < 15; i++ {
		in.Step(1.0 / 60)
	}
	in.SetIKWeight(p.IKID("leftFoot"), 1)
	in.SetGround(p.IKID("leftFoot"), 0.25, [3]float64{0, 1, 0})
	in.SetIKWeight(p.IKID("rightHand"), 0.5)
	in.SetIKTarget(p.IKID("rightHand"), [3]float64{0.5, 1.25, 0.75})
	in.SetIKPole(p.IKID("rightHand"), [3]float64{0.5, 1, -1})
	in.SetLookAt(p.LookAtID("head"), [3]float64{0, 1.5, 4}, 0.5)
	data, err := EncodeFrame(in)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestGoldenFrame(t *testing.T) {
	data := goldenFrame(t)
	path := filepath.Join("testdata", "frame.golden.hex")
	if *updateGolden {
		if err := os.WriteFile(path, []byte(hex.EncodeToString(data)+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test -run TestGoldenFrame -update)", err)
	}
	if got := hex.EncodeToString(data); got != strings.TrimSpace(string(want)) {
		t.Fatalf("encoder output drifted from %s:\n got %s\nwant %s", path, got, strings.TrimSpace(string(want)))
	}
}
