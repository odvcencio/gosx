package physics

import (
	"hash/fnv"
	"math"
	"testing"
)

// rollingHeights returns a columns x rows grid of smooth hills spanning
// width x depth, plus the function it samples.
func rollingHeights(columns, rows int, width, depth float64) ([]float64, func(x, z float64) float64) {
	f := func(x, z float64) float64 {
		// Explicit conversions keep the inputs identical on fusing builds.
		return float64(1.5*math.Sin(float64(x*0.35))) + math.Cos(float64(z*0.27)) + float64(0.04*x)
	}
	heights := make([]float64, columns*rows)
	for row := 0; row < rows; row++ {
		for column := 0; column < columns; column++ {
			x := -width/2 + float64(column)*width/float64(columns-1)
			z := -depth/2 + float64(row)*depth/float64(rows-1)
			heights[row*columns+column] = f(x, z)
		}
	}
	return heights, f
}

// heightfieldMesh builds the triangle mesh of the same grid, with the same
// diagonal, as a reference.
func heightfieldMesh(heights []float64, columns, rows int, width, depth float64) ([]Vec3, []int) {
	vertices := make([]Vec3, 0, columns*rows)
	for row := 0; row < rows; row++ {
		for column := 0; column < columns; column++ {
			vertices = append(vertices, Vec3{
				X: -width/2 + float64(column)*width/float64(columns-1),
				Y: heights[row*columns+column],
				Z: -depth/2 + float64(row)*depth/float64(rows-1),
			})
		}
	}
	indices := make([]int, 0, (columns-1)*(rows-1)*6)
	for row := 0; row < rows-1; row++ {
		for column := 0; column < columns-1; column++ {
			v00 := row*columns + column
			v10, v01, v11 := v00+1, v00+columns, v00+columns+1
			indices = append(indices, v00, v11, v10, v00, v01, v11)
		}
	}
	return vertices, indices
}

func TestHeightfieldValidation(t *testing.T) {
	bad := []ColliderConfig{
		{Shape: ShapeHeightfield, Columns: 1, Rows: 4, Heights: make([]float64, 4), Width: 1, Depth: 1},
		{Shape: ShapeHeightfield, Columns: 2, Rows: 2, Heights: make([]float64, 3), Width: 1, Depth: 1},
		{Shape: ShapeHeightfield, Columns: 2, Rows: 2, Heights: make([]float64, 4), Width: 0, Depth: 1},
		{Shape: ShapeHeightfield, Columns: 2, Rows: 2, Heights: []float64{0, math.NaN(), 0, 0}, Width: 1, Depth: 1},
	}
	for i, config := range bad {
		if NewCollider(config).Err() == nil {
			t.Errorf("case %d: invalid heightfield accepted", i)
		}
	}
	good := NewCollider(ColliderConfig{Shape: ShapeHeightfield, Columns: 2, Rows: 2, Heights: []float64{0, 1, 2, 3}, Width: 4, Depth: 2})
	if err := good.Err(); err != nil {
		t.Fatal(err)
	}
	if got := good.Shape.String(); got != "heightfield" {
		t.Fatalf("String = %q", got)
	}
	box := good.AABB()
	if box.Min != (Vec3{X: -2, Y: 0, Z: -1}) || box.Max != (Vec3{X: 2, Y: 3, Z: 1}) {
		t.Fatalf("AABB = %+v", box)
	}
}

func TestHeightfieldHeightAtUsesTheSharedDiagonal(t *testing.T) {
	// One cell with only the (1,1) corner raised: the diagonal (0,0)-(1,1)
	// puts the centre at half that height.
	c := NewCollider(ColliderConfig{Shape: ShapeHeightfield, Columns: 2, Rows: 2, Heights: []float64{0, 0, 0, 4}, Width: 2, Depth: 2, Offset: Vec3{X: 10, Y: 1, Z: -3}})
	if y, ok := c.HeightAt(10, -3); !ok || math.Abs(y-3) > 1e-12 {
		t.Fatalf("centre height = %v, %v; want 3 (offset 1 + half of 4)", y, ok)
	}
	if y, _ := c.HeightAt(10.5, -3.5); math.Abs(y-2) > 1e-12 {
		t.Fatalf("(fx 0.75, fz 0.25) height = %v, want 2", y)
	}
	if _, ok := c.HeightAt(40, 0); ok {
		t.Fatal("a point off the grid reported a height")
	}
	if got := c.heightfield.heightAt(0.5, -0.5); math.Abs(got-1) > 1e-12 {
		t.Fatalf("local heightAt = %v, want 1", got)
	}
}

func TestHeightfieldRaycastMatchesTriangleMesh(t *testing.T) {
	const columns, rows, width, depth = 33, 25, 40.0, 30.0
	heights, _ := rollingHeights(columns, rows, width, depth)
	field := NewCollider(ColliderConfig{Shape: ShapeHeightfield, Heights: heights, Columns: columns, Rows: rows, Width: width, Depth: depth, Offset: Vec3{X: 3, Z: -2}})
	vertices, indices := heightfieldMesh(heights, columns, rows, width, depth)
	mesh := NewCollider(ColliderConfig{Shape: ShapeTriangleMesh, Vertices: vertices, Indices: indices, Offset: Vec3{X: 3, Z: -2}})
	if !field.RaycastSupported() {
		t.Fatal("heightfield reports no raycast support")
	}
	seed := uint64(1)
	next := func() float64 {
		seed = seed*6364136223846793005 + 1442695040888963407
		return float64(seed>>11) / float64(uint64(1)<<53)
	}
	hits := 0
	for i := 0; i < 400; i++ {
		origin := Vec3{X: 3 + (next()-0.5)*60, Y: 4 + next()*10, Z: -2 + (next()-0.5)*45}
		dir := Vec3{X: next() - 0.5, Y: -0.2 - next(), Z: next() - 0.5}
		if i%7 == 0 {
			dir = Vec3{Y: -1}
		}
		ray := Ray{Origin: origin, Direction: dir}
		a, okA := field.Raycast(ray, 200)
		b, okB := mesh.Raycast(ray, 200)
		if okA != okB {
			t.Fatalf("ray %d: heightfield hit=%v mesh hit=%v (origin %+v dir %+v)", i, okA, okB, origin, dir)
		}
		if !okA {
			continue
		}
		hits++
		if math.Abs(a.Distance-b.Distance) > 1e-9 || a.Normal.Sub(b.Normal).Len() > 1e-9 {
			t.Fatalf("ray %d: heightfield %+v, mesh %+v", i, a, b)
		}
	}
	if hits < 100 {
		t.Fatalf("only %d of 400 rays hit; the test does not exercise the walk", hits)
	}
}

func TestHeightfieldCollideMatchesTriangleMesh(t *testing.T) {
	const columns, rows, width, depth = 17, 17, 16.0, 16.0
	heights, f := rollingHeights(columns, rows, width, depth)
	field := NewCollider(ColliderConfig{Shape: ShapeHeightfield, Heights: heights, Columns: columns, Rows: rows, Width: width, Depth: depth})
	vertices, indices := heightfieldMesh(heights, columns, rows, width, depth)
	mesh := NewCollider(ColliderConfig{Shape: ShapeTriangleMesh, Vertices: vertices, Indices: indices})
	for _, shape := range []ColliderConfig{{Shape: ShapeSphere, Radius: 0.6}, {Shape: ShapeBox, Width: 1, Height: 0.8, Depth: 1.2}, {Shape: ShapeCapsule, Radius: 0.3, Height: 1}} {
		for _, p := range [][2]float64{{0, 0}, {-3.3, 2.1}, {5.2, -4.4}} {
			config := shape
			config.Offset = Vec3{X: p[0], Y: f(p[0], p[1]) + 0.2, Z: p[1]}
			body := NewCollider(config)
			a := CollideAll(field, body, nil)
			b := CollideAll(mesh, body, nil)
			if len(a) == 0 || len(a) != len(b) {
				t.Fatalf("%v at %v: heightfield %d manifolds, mesh %d", shape.Shape, p, len(a), len(b))
			}
			// The triangles and the solver are the mesh path's; only the
			// candidate order differs, which can swap a face normal for the
			// neighbouring face's at a shared vertex. Match contacts by depth.
			for i := range a {
				matched := false
				for j := range b {
					if math.Abs(a[i].Points[0].Penetration-b[j].Points[0].Penetration) < 1e-9 && a[i].Normal.Sub(b[j].Normal).Len() < 0.05 {
						matched = true
					}
				}
				if !matched {
					t.Fatalf("%v at %v: heightfield contact %+v has no mesh twin in %+v", shape.Shape, p, a[i], b)
				}
				if shape.Shape == ShapeSphere && a[i].Normal.Y <= 0 {
					t.Fatalf("sphere at %v: normal %+v points down", p, a[i].Normal)
				}
			}
			// The flipped order returns the same contacts with normals reversed.
			flipped := CollideAll(body, field, nil)
			if len(flipped) != len(a) || flipped[0].Normal.Add(a[0].Normal).Len() > 1e-9 {
				t.Fatalf("%v at %v: flipped manifolds %+v", shape.Shape, p, flipped)
			}
		}
	}
}

// heightfieldDropWorld drops a sphere and a box onto rolling hills and steps
// two seconds.
func heightfieldDropWorld() (*World, *RigidBody, *RigidBody, func(x, z float64) float64) {
	const columns, rows, width, depth = 65, 65, 64.0, 64.0
	heights, f := rollingHeights(columns, rows, width, depth)
	world := NewWorld(WorldConfig{Gravity: Vec3{Y: -10}, FixedTimestep: 1.0 / 60.0, SolverIter: 10})
	world.AddCollider(ColliderConfig{Shape: ShapeHeightfield, Heights: heights, Columns: columns, Rows: rows, Width: width, Depth: depth})
	ball := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{X: 2, Y: f(2, 3) + 3, Z: 3}, Friction: 0.6})
	ball.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 0.5})
	crate := world.AddBody(BodyConfig{Mass: 2, Position: Vec3{X: -6, Y: f(-6, -5) + 4, Z: -5}, Friction: 0.8})
	crate.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 1, Height: 1, Depth: 1})
	for i := 0; i < 120; i++ {
		world.Step(1.0 / 60.0)
	}
	return world, ball, crate, f
}

func TestHeightfieldBodiesLandOnTheSurface(t *testing.T) {
	world, ball, crate, _ := heightfieldDropWorld()
	if err := world.Err(); err != nil {
		t.Fatalf("world diagnostics: %v", err)
	}
	field := world.Colliders()[0]
	for name, body := range map[string]*RigidBody{"ball": ball, "crate": crate} {
		ground, ok := field.HeightAt(body.Position.X, body.Position.Z)
		if !ok {
			t.Fatalf("%s left the terrain: %+v", name, body.Position)
		}
		// Above the surface (no tunnelling) and within one body size of it.
		if gap := body.Position.Y - ground; gap < 0.3 || gap > 1.2 {
			t.Fatalf("%s centre is %.3f m above the ground at %+v", name, gap, body.Position)
		}
	}
}

func TestHeightfieldSimulationIsDeterministic(t *testing.T) {
	_, a1, b1, _ := heightfieldDropWorld()
	_, a2, b2, _ := heightfieldDropWorld()
	if a1.Position != a2.Position || b1.Position != b2.Position || b1.Rotation != b2.Rotation {
		t.Fatal("two identical runs diverged")
	}
	// The golden digest pins the bits across platforms (amd64, arm64,
	// js/wasm). Update it only on an intended solver or heightfield change.
	sum := fnv.New64a()
	for _, v := range []float64{a1.Position.X, a1.Position.Y, a1.Position.Z, b1.Position.X, b1.Position.Y, b1.Position.Z, b1.Rotation.X, b1.Rotation.Y, b1.Rotation.Z, b1.Rotation.W} {
		bits := math.Float64bits(v)
		for i := 0; i < 8; i++ {
			sum.Write([]byte{byte(bits >> (8 * i))})
		}
	}
	if got := sum.Sum64(); got != heightfieldDropGolden {
		t.Fatalf("drop digest = %#x, want %#x", got, uint64(heightfieldDropGolden))
	}
}

const heightfieldDropGolden = 0x1fde9a273df23516

func TestHeightfieldGatherIsBounded(t *testing.T) {
	heights := make([]float64, 101*101)
	field := NewCollider(ColliderConfig{Shape: ShapeHeightfield, Heights: heights, Columns: 101, Rows: 101, Width: 100, Depth: 100})
	got := field.heightfield.gather(AABB{Min: Vec3{X: -50, Y: -1, Z: -50}, Max: Vec3{X: 50, Y: 1, Z: 50}}, nil)
	if len(got) != heightfieldMaxTriangles {
		t.Fatalf("gather returned %d triangles, want the %d cap", len(got), heightfieldMaxTriangles)
	}
	if got := field.heightfield.gather(AABB{Min: Vec3{X: -50, Y: 5, Z: -50}, Max: Vec3{X: 50, Y: 6, Z: 50}}, nil); len(got) != 0 {
		t.Fatalf("a box above the surface gathered %d triangles", len(got))
	}
}
