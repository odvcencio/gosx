package docs

import (
	"math"
	"testing"

	"m31labs.dev/gosx/scene"
)

// TestFibonacciSpherePoints_Count verifies the helper returns exactly n
// points, or nil/empty for non-positive n.
func TestFibonacciSpherePoints_Count(t *testing.T) {
	cases := []int{0, -3, 1, 2, 5, 100}
	for _, n := range cases {
		points := fibonacciSpherePoints(n, 4.5)
		want := n
		if n <= 0 {
			want = 0
		}
		if len(points) != want {
			t.Errorf("fibonacciSpherePoints(%d, 4.5) len = %d, want %d", n, len(points), want)
		}
	}
}

// TestFibonacciSpherePoints_OnSphereSurface verifies every returned point
// lies (within floating point tolerance) on the surface of the requested
// sphere radius — i.e. the distribution is a shell, not a solid ball.
func TestFibonacciSpherePoints_OnSphereSurface(t *testing.T) {
	const radius = 4.5
	const n = 100
	points := fibonacciSpherePoints(n, radius)
	if len(points) != n {
		t.Fatalf("len(points) = %d, want %d", len(points), n)
	}
	const tolerance = 1e-6
	for i, p := range points {
		dist := math.Sqrt(p.X*p.X + p.Y*p.Y + p.Z*p.Z)
		if math.Abs(dist-radius) > tolerance {
			t.Errorf("point %d distance from origin = %v, want %v (+/- %v)", i, dist, radius, tolerance)
		}
	}
}

// TestFibonacciSpherePoints_Spread verifies the points are not degenerate
// (all identical or collinear) — a cheap smoke test that the golden-angle
// spiral is actually spreading points around the sphere rather than
// clustering them.
func TestFibonacciSpherePoints_Spread(t *testing.T) {
	points := fibonacciSpherePoints(100, 4.5)
	seen := map[[3]float64]bool{}
	for _, p := range points {
		key := [3]float64{
			math.Round(p.X*1e6) / 1e6,
			math.Round(p.Y*1e6) / 1e6,
			math.Round(p.Z*1e6) / 1e6,
		}
		if seen[key] {
			t.Fatalf("duplicate point found at %v; distribution is degenerate", key)
		}
		seen[key] = true
	}

	minY, maxY := math.Inf(1), math.Inf(-1)
	for _, p := range points {
		if p.Y < minY {
			minY = p.Y
		}
		if p.Y > maxY {
			maxY = p.Y
		}
	}
	// The lattice should span close to the full [-radius, radius] range on Y.
	const radius = 4.5
	if maxY-minY < radius*1.5 {
		t.Errorf("Y spread = %v, want at least %v (points look clustered)", maxY-minY, radius*1.5)
	}
}

// TestFibonacciSpherePoints_SinglePoint verifies the n=1 edge case returns
// a single point on the +Z pole rather than dividing by zero.
func TestFibonacciSpherePoints_SinglePoint(t *testing.T) {
	points := fibonacciSpherePoints(1, 4.5)
	if len(points) != 1 {
		t.Fatalf("len(points) = %d, want 1", len(points))
	}
	if points[0].X != 0 || points[0].Y != 0 || points[0].Z != 4.5 {
		t.Errorf("single point = %+v, want {0 0 4.5}", points[0])
	}
}

// TestBenchScene_Dispatch verifies every known workload name reaches its
// dedicated scene builder (rather than silently falling back to "mixed"),
// and that unknown names do fall back to "mixed" as documented.
func TestBenchScene_Dispatch(t *testing.T) {
	cases := []struct {
		workload  string
		wantNodes int
	}{
		{"static", len(BenchStaticScene().Graph.Nodes)},
		{"pbr-heavy", len(BenchPBRHeavyScene().Graph.Nodes)},
		{"thick-lines", len(BenchThickLinesScene().Graph.Nodes)},
		{"particles", len(BenchParticlesScene().Graph.Nodes)},
		{"mesh-swarm", len(BenchMeshSwarmScene().Graph.Nodes)},
		{"particles-storm", len(BenchParticlesStormScene().Graph.Nodes)},
		{"mixed", len(BenchMixedScene().Graph.Nodes)},
		{"", len(BenchMixedScene().Graph.Nodes)},
		{"not-a-real-workload", len(BenchMixedScene().Graph.Nodes)},
	}
	for _, tc := range cases {
		got := BenchScene(tc.workload)
		if len(got.Graph.Nodes) != tc.wantNodes {
			t.Errorf("BenchScene(%q) node count = %d, want %d", tc.workload, len(got.Graph.Nodes), tc.wantNodes)
		}
	}
}

// TestBenchMeshSwarmScene_HeavierThanPBRHeavy is a guardrail that the swarm
// workload actually stays meaningfully heavier (by node count) than the
// existing pbr-heavy baseline, since that separation is the entire point
// of the workload.
func TestBenchMeshSwarmScene_HeavierThanPBRHeavy(t *testing.T) {
	heavy := len(BenchPBRHeavyScene().Graph.Nodes)
	swarm := len(BenchMeshSwarmScene().Graph.Nodes)
	if swarm <= heavy {
		t.Errorf("BenchMeshSwarmScene node count (%d) should exceed BenchPBRHeavyScene (%d)", swarm, heavy)
	}
}

// TestBenchParticlesStormScene_HeavierThanParticles is a guardrail that the
// storm workload actually requests more particles than the baseline
// particles workload.
func TestBenchParticlesStormScene_HeavierThanParticles(t *testing.T) {
	base := BenchParticlesScene()
	storm := BenchParticlesStormScene()
	baseCount := computeParticlesCount(t, base)
	stormCount := computeParticlesCount(t, storm)
	if stormCount <= baseCount {
		t.Errorf("particles-storm Count (%d) should exceed particles Count (%d)", stormCount, baseCount)
	}
}

// computeParticlesCount extracts the ComputeParticles.Count from the first
// matching node in a scene graph, failing the test if none is found.
func computeParticlesCount(t *testing.T, props scene.Props) int {
	t.Helper()
	for _, node := range props.Graph.Nodes {
		if cp, ok := node.(scene.ComputeParticles); ok {
			return cp.Count
		}
	}
	t.Fatal("no scene.ComputeParticles node found in graph")
	return 0
}
