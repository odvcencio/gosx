package scene

import (
	"math"
	"testing"
)

// benchWideGraph builds count sibling meshes spread over a cube lattice. Only a
// few of them sit on the benchmark ray, so a broadphase can reject the rest.
func benchWideGraph(count int) Graph {
	nodes := make([]Node, 0, count)
	side := int(math.Cbrt(float64(count))) + 1
	for i := 0; i < count; i++ {
		x := float64(i%side) * 3
		y := float64((i/side)%side) * 3
		z := float64(i/(side*side)) * 3
		nodes = append(nodes, Mesh{
			ID:       "node",
			Geometry: SphereGeometry{Radius: 1},
			Position: Vec3(x-float64(side)*1.5, y-float64(side)*1.5, z-float64(side)*1.5),
		})
	}
	return Graph{Nodes: nodes}
}

// benchInstancedGraph builds one InstancedMesh with count instances on a
// lattice. The benchmark ray crosses a single column of that lattice.
func benchInstancedGraph(count int) Graph {
	positions := make([]Vector3, count)
	rotations := make([]Euler, count)
	scales := make([]Vector3, count)
	side := int(math.Sqrt(float64(count))) + 1
	for i := 0; i < count; i++ {
		positions[i] = Vec3(float64(i%side)*2-float64(side), 0, float64(i/side)*2-float64(side))
		rotations[i] = Euler{Y: float64(i) * 0.01}
		scales[i] = Vec3(1, 1, 1)
	}
	return Graph{Nodes: []Node{InstancedMesh{
		ID:        "instances",
		Count:     count,
		Geometry:  SphereGeometry{Radius: 0.5},
		Positions: positions,
		Rotations: rotations,
		Scales:    scales,
	}}}
}

// benchDeepGraph nests depth groups above one leaf mesh.
func benchDeepGraph(depth int) Graph {
	var node Node = Mesh{ID: "leaf", Geometry: CubeGeometry{Size: 1}, Position: Vec3(0, 0, -3)}
	for i := 0; i < depth; i++ {
		node = Group{Position: Vec3(0.0001, 0, 0), Children: []Node{node}}
	}
	return Graph{Nodes: []Node{node}}
}

var benchRay = Ray{Origin: Vec3(0, 0, 40), Direction: Vec3(0, 0, -1)}

var benchSink RayTrace

func BenchmarkTraceGraphWideNodes1000(b *testing.B) {
	graph := benchWideGraph(1000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchSink = TraceGraph(graph, benchRay)
	}
}

func BenchmarkTraceGraphInstanced10000(b *testing.B) {
	graph := benchInstancedGraph(10000)
	ray := Ray{Origin: Vec3(0, 20, 0), Direction: Vec3(0, -1, 0)}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchSink = TraceGraph(graph, ray)
	}
}

func BenchmarkTraceGraphDeepHierarchy1000(b *testing.B) {
	graph := benchDeepGraph(1000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchSink = TraceGraph(graph, benchRay)
	}
}

// benchGeometryGraph spreads count copies of one geometry over a cube lattice,
// the same layout benchWideGraph uses. It measures whether the broadphase still
// rejects well once the narrow phase costs more per candidate.
func benchGeometryGraph(geometry Geometry, count int) Graph {
	nodes := make([]Node, 0, count)
	side := int(math.Cbrt(float64(count))) + 1
	for i := 0; i < count; i++ {
		x := float64(i%side) * 3
		y := float64((i/side)%side) * 3
		z := float64(i/(side*side)) * 3
		nodes = append(nodes, Mesh{
			ID:       "node",
			Geometry: geometry,
			Position: Vec3(x-float64(side)*1.5, y-float64(side)*1.5, z-float64(side)*1.5),
		})
	}
	return Graph{Nodes: nodes}
}

var benchTorus = TorusGeometry{Radius: 0.8, Tube: 0.25}

var benchPyramid = PyramidGeometry{Width: 1.4, Height: 1.4, Depth: 1.4}

var benchLines = benchLineRing(32)

// benchLineRing builds a closed polyline ring of count segments plus one
// diameter across the middle, so a ray down the axis crosses a stroke.
func benchLineRing(count int) LinesGeometry {
	points := make([]Vector3, count)
	segments := make([][2]int, 0, count+1)
	for i := range points {
		angle := 2 * math.Pi * float64(i) / float64(count)
		points[i] = Vec3(math.Cos(angle), math.Sin(angle)*0.4, math.Sin(angle))
		segments = append(segments, [2]int{i, (i + 1) % count})
	}
	segments = append(segments, [2]int{0, count / 2})
	return LinesGeometry{Points: points, Segments: segments}
}

var benchKnot = TorusKnotGeometry{Radius: 0.6, Tube: 0.15}

// benchBufferGrid builds a side x side grid of quads as authored triangles.
func benchBufferGrid(side int) BufferGeometry {
	span := 2.0
	step := span / float64(side)
	positions := make([]float64, 0, side*side*18)
	for x := 0; x < side; x++ {
		for z := 0; z < side; z++ {
			x0 := -span/2 + float64(x)*step
			z0 := -span/2 + float64(z)*step
			x1, z1 := x0+step, z0+step
			height := func(x, z float64) float64 { return 0.2 * math.Sin(4*x) * math.Cos(4*z) }
			positions = append(positions,
				x0, height(x0, z0), z0,
				x1, height(x1, z0), z0,
				x1, height(x1, z1), z1,
				x0, height(x0, z0), z0,
				x1, height(x1, z1), z1,
				x0, height(x0, z1), z1,
			)
		}
	}
	return BufferGeometry{Positions: positions}
}

var benchBuffer = benchBufferGrid(32)

func BenchmarkTraceGraphTorusNodes1000(b *testing.B) {
	graph := benchGeometryGraph(benchTorus, 1000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchSink = TraceGraph(graph, benchRay)
	}
}

func BenchmarkSceneAcceleratorTorusNodes1000(b *testing.B) {
	accel := NewSceneAccelerator(benchGeometryGraph(benchTorus, 1000))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchSink = accel.Trace(benchRay)
	}
}

func BenchmarkTraceGraphPyramidNodes1000(b *testing.B) {
	graph := benchGeometryGraph(benchPyramid, 1000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchSink = TraceGraph(graph, benchRay)
	}
}

func BenchmarkSceneAcceleratorPyramidNodes1000(b *testing.B) {
	accel := NewSceneAccelerator(benchGeometryGraph(benchPyramid, 1000))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchSink = accel.Trace(benchRay)
	}
}

func BenchmarkTraceGraphLinesNodes1000(b *testing.B) {
	graph := benchGeometryGraph(benchLines, 1000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchSink = TraceGraph(graph, benchRay)
	}
}

func BenchmarkSceneAcceleratorLinesNodes1000(b *testing.B) {
	accel := NewSceneAccelerator(benchGeometryGraph(benchLines, 1000))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchSink = accel.Trace(benchRay)
	}
}

func BenchmarkTraceGraphTorusKnotNodes1000(b *testing.B) {
	graph := benchGeometryGraph(benchKnot, 1000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchSink = TraceGraph(graph, benchRay)
	}
}

func BenchmarkSceneAcceleratorTorusKnotNodes1000(b *testing.B) {
	accel := NewSceneAccelerator(benchGeometryGraph(benchKnot, 1000))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchSink = accel.Trace(benchRay)
	}
}

// BenchmarkTraceGraphBuffer2048Triangles walks the triangles in order, which is
// what a one-shot query pays.
func BenchmarkTraceGraphBuffer2048Triangles(b *testing.B) {
	graph := NewGraph(Mesh{ID: "terrain", Geometry: benchBuffer})
	ray := Ray{Origin: Vec3(0.3, 5, 0.2), Direction: Vec3(0, -1, 0)}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchSink = TraceGraph(graph, ray)
	}
}

// BenchmarkSceneAcceleratorBuffer2048Triangles answers the same ray through the
// per-geometry triangle hierarchy the accelerator builds once.
func BenchmarkSceneAcceleratorBuffer2048Triangles(b *testing.B) {
	accel := NewSceneAccelerator(NewGraph(Mesh{ID: "terrain", Geometry: benchBuffer}))
	ray := Ray{Origin: Vec3(0.3, 5, 0.2), Direction: Vec3(0, -1, 0)}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchSink = accel.Trace(ray)
	}
}

// BenchmarkSceneAcceleratorBuildBuffer2048Triangles is the build side of that
// trade: one triangle hierarchy per distinct vertex buffer.
func BenchmarkSceneAcceleratorBuildBuffer2048Triangles(b *testing.B) {
	graph := NewGraph(Mesh{ID: "terrain", Geometry: benchBuffer})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchAccelSink = NewSceneAccelerator(graph)
	}
}

var benchAccelSink *SceneAccelerator

// BenchmarkTorusKnotTessellation measures the one-time cost the knot cache pays
// on the first ray that reaches a new knot size.
func BenchmarkTorusKnotTessellation(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchSoupSink = buildTorusKnotSoup(torusKnotKey{radius: 0.17, tube: 0.045, radial: 16, tubular: 128})
	}
}

var benchSoupSink *triangleSoup

func BenchmarkTraceGraphPoints10000(b *testing.B) {
	positions := make([]Vector3, 10000)
	for i := range positions {
		positions[i] = Vec3(float64(i%100)*0.5-25, 0, float64(i/100)*0.5-25)
	}
	graph := Graph{Nodes: []Node{Points{ID: "cloud", Count: len(positions), Positions: positions, Size: 0.2}}}
	ray := Ray{Origin: Vec3(0, 20, 0), Direction: Vec3(0, -1, 0)}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchSink = TraceGraph(graph, ray)
	}
}
