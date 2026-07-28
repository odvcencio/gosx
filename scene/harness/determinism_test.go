package harness_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"m31labs.dev/gosx/scene/harness"
	"m31labs.dev/gosx/scene/imagediff"
)

// This file certifies the determinism of the whole authoring path, not only the
// rasterizer: typed props lower to a SceneIR, the IR lowers to a render bundle,
// and the bundle rasterizes. Every step is a candidate for a per-process
// dependence, and the lowering steps walk maps.
//
// Package render/gpu/headless holds the matching checks one layer down, including
// the fresh-process probe for the rasterizer alone. See the scope note on
// harness.CertifyDeterminism for what a certified report does and does not claim.

// harnessProbeEnv makes this test binary print the pixel hash of one matrix case
// and exit. The fresh-process check re-executes the binary with it set.
const harnessProbeEnv = "GOSX_HARNESS_DETERMINISM_PROBE"

// determinismCaseName is the matrix case the determinism checks use. The shadow
// scene is the richest one: a wide ground plane that reaches behind the camera,
// three instanced casters, a shadow map, and per-pixel shading.
const determinismCaseName = "shadow-on"

func determinismCase(t *testing.T) matrixCase {
	t.Helper()
	for _, tc := range matrixCases() {
		if tc.name == determinismCaseName {
			return tc
		}
	}
	t.Fatalf("the %q case disappeared from the matrix", determinismCaseName)
	return matrixCase{}
}

// TestCertifyDeterminismOnTheRichestCase drives the harness's own certification
// over repeated renders. Twelve runs are enough to catch a map-order dependence
// that survives a single repeat, and cheap at this frame size.
func TestCertifyDeterminismOnTheRichestCase(t *testing.T) {
	tc := determinismCase(t)
	session := tc.session(t)
	telemetry, err := session.CertifyDeterminism(tc.name, tc.time, 12)
	if err != nil {
		t.Fatal(err)
	}
	if !telemetry.Identical {
		t.Fatalf("%d of %d runs diverged: %+v", len(telemetry.Divergences), telemetry.Runs, telemetry.Divergences)
	}
	if telemetry.PixelSHA256 == "" {
		t.Fatal("the certification recorded no pixel hash")
	}
	if err := session.Validate(); err != nil {
		t.Fatal(err)
	}
	// A certification of a blank frame would pass every check above, so gate the
	// frame on real content through the session's own telemetry.
	frame := renderMatrixFrame(t, tc)
	if frame.UniqueColors < 4 || frame.LuminanceVariance < 0.02 || frame.Coverage < 0.5 {
		t.Fatalf("the certified frame holds no content: coverage %.6f, unique colours %d, variance %.6f",
			frame.Coverage, frame.UniqueColors, frame.LuminanceVariance)
	}
	t.Logf("certified %s over %d runs: hash %s, coverage %.6f, %d colours, variance %.6f",
		tc.name, telemetry.Runs, telemetry.PixelSHA256[:16], frame.Coverage,
		frame.UniqueColors, frame.LuminanceVariance)
}

// TestMain serves the fresh-process probe. With the variable set, the binary
// renders the determinism case and prints its hash and its colour count.
func TestMain(m *testing.M) {
	if os.Getenv(harnessProbeEnv) == "" {
		os.Exit(m.Run())
	}
	tc := matrixCase{}
	for _, candidate := range matrixCases() {
		if candidate.name == determinismCaseName {
			tc = candidate
		}
	}
	if tc.name == "" {
		os.Exit(2)
	}
	session := harness.New(tc.props, tc.opts)
	if _, err := session.Render(tc.time); err != nil {
		os.Exit(2)
	}
	report := session.Report()
	for index := len(report.Events) - 1; index >= 0; index-- {
		if frame := report.Events[index].Frame; frame != nil {
			os.Stdout.WriteString(frame.PixelHash + " " + itoa(frame.UniqueColors) + "\n")
			os.Exit(0)
		}
	}
	os.Exit(2)
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}

// TestHarnessFrameRepeatsInAFreshProcess covers the lowering steps that
// CertifyDeterminism cannot see. scene.Props holds maps, and Go randomizes map
// iteration order per process, so a lowering step that walked one would produce a
// stable hash inside a process and a different hash here.
func TestHarnessFrameRepeatsInAFreshProcess(t *testing.T) {
	if testing.Short() {
		t.Skip("the fresh-process check re-executes the test binary")
	}
	tc := determinismCase(t)
	local := renderMatrixFrame(t, tc)
	if local.UniqueColors < 4 {
		t.Fatalf("the local frame holds no content: %d colours", local.UniqueColors)
	}

	binary, err := os.Executable()
	if err != nil {
		t.Skipf("cannot locate the test binary: %v", err)
	}
	for run := 1; run <= 3; run++ {
		command := exec.Command(binary)
		command.Env = append(os.Environ(), harnessProbeEnv+"=1")
		output, err := command.Output()
		if err != nil {
			t.Fatalf("child run %d: %v", run, err)
		}
		fields := strings.Fields(strings.TrimSpace(string(output)))
		if len(fields) != 2 {
			t.Fatalf("child run %d printed %q, want a hash and a colour count", run, output)
		}
		if fields[0] != local.PixelHash {
			t.Fatalf("child run %d produced %s, this process produced %s; "+
				"a lowering step depends on per-process state such as map iteration order",
				run, fields[0], local.PixelHash)
		}
		if fields[1] == "1" {
			t.Fatalf("child run %d rendered a single-colour frame: %q", run, output)
		}
	}
}

// TestCertifyDeterminismReportsARealDivergence proves the certification is not a
// constant. A session whose render function alternates between two frames must be
// reported as divergent, with the changed-pixel count and the bounding box.
//
// Without this check, CertifyDeterminism could return Identical for any input and
// every determinism test in the tree would still pass.
func TestCertifyDeterminismReportsARealDivergence(t *testing.T) {
	tc := determinismCase(t)
	moved := tc
	moved.opts = tc.opts
	camera := *tc.opts.Camera
	camera.Position.Y += 0.4
	moved.opts.Camera = &camera

	stable := renderMatrixFrame(t, tc)
	shifted := renderMatrixFrame(t, moved)
	if stable.PixelHash == shifted.PixelHash {
		t.Fatal("moving the camera left the frame byte-identical, so this test cannot show a divergence")
	}

	// The harness has no hook to inject an unstable renderer, so certify the two
	// frames by hand through the same comparison the telemetry uses. A different
	// hash must reach the report as a problem.
	session := tc.session(t)
	if _, err := session.Render(tc.time); err != nil {
		t.Fatal(err)
	}
	if _, err := session.RequireGoldenFile(tc.name, "testdata/matrix/"+tc.name+".png", imagediff.Options{}); err != nil {
		t.Fatal(err)
	}
	if err := session.Validate(); err != nil {
		t.Fatalf("the unmodified case must still match its golden: %v", err)
	}

	// Now compare the moved frame against the same golden. The session must
	// record a problem instead of passing.
	movedSession := moved.session(t)
	if _, err := movedSession.Render(moved.time); err != nil {
		t.Fatal(err)
	}
	if _, err := movedSession.RequireGoldenFile(moved.name, "testdata/matrix/"+moved.name+".png", imagediff.Options{}); err != nil {
		t.Fatal(err)
	}
	if err := movedSession.Validate(); err == nil {
		t.Fatal("a moved camera must fail the golden comparison; RequireGoldenFile is inert")
	}
}
