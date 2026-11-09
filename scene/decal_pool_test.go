package scene

import (
	"math"
	"testing"
)

func TestDecalPoolRecyclesOldestFirst(t *testing.T) {
	pool := NewDecalPool(DecalPoolConfig{Name: "p", Capacity: 3})
	for i := 0; i < 5; i++ {
		pool.Spawn(DecalSpawn{Position: Vec3(float64(i), 0, 0), Layer: i})
	}
	if pool.Len() != 3 {
		t.Fatalf("Len = %d, want 3", pool.Len())
	}
	records := pool.Records()
	for i, wantX := range []float32{2, 3, 4} {
		if got := records[i*DecalRecordStride]; got != wantX {
			t.Errorf("record %d x = %v, want %v (oldest first)", i, got, wantX)
		}
	}
}

func TestDecalPoolFadeAndExpiry(t *testing.T) {
	pool := NewDecalPool(DecalPoolConfig{Name: "p", Capacity: 4, Life: 4, FadeOut: 2})
	pool.Spawn(DecalSpawn{})
	pool.Update(1)
	if got := pool.Records()[3]; got != 1 {
		t.Fatalf("opacity before fade = %v, want 1", got)
	}
	pool.Update(2) // age 3: halfway through the 2 s fade
	if got := pool.Records()[3]; math.Abs(float64(got)-0.5) > 1e-6 {
		t.Fatalf("opacity mid-fade = %v, want 0.5", got)
	}
	pool.Update(1)
	if pool.Len() != 0 || len(pool.Records()) != 0 {
		t.Fatalf("expired decal still live: len %d", pool.Len())
	}
}

func TestDecalPoolRevisionOnlyChangesWithData(t *testing.T) {
	pool := NewDecalPool(DecalPoolConfig{Name: "p", Capacity: 2, Life: 10})
	pool.Spawn(DecalSpawn{})
	pool.Records()
	rev := pool.Revision()
	pool.Update(1)
	pool.Records()
	if pool.Revision() != rev {
		t.Fatalf("revision changed without visible change: %d -> %d", rev, pool.Revision())
	}
	pool.Spawn(DecalSpawn{})
	pool.Records()
	if pool.Revision() == rev {
		t.Fatal("revision did not change after Spawn")
	}
}

func TestDecalPoolSteadyStateDoesNotAllocate(t *testing.T) {
	pool := NewDecalPool(DecalPoolConfig{Name: "p", Capacity: 64, Life: 2, FadeOut: 0.5})
	spawn := DecalSpawn{Position: Vec3(1, 2, 3), Normal: Vec3(0, 1, 0), Rotation: 0.3, Size: Vec3(0.5, 0.5, 1), Tint: [4]float32{1, 0.5, 0.5, 1}}
	for i := 0; i < 200; i++ { // reach steady state: pool full and recycling
		pool.Spawn(spawn)
	}
	allocs := testing.AllocsPerRun(1000, func() {
		pool.Spawn(spawn)
		pool.Update(1.0 / 60)
		_ = pool.Records()
	})
	if allocs != 0 {
		t.Fatalf("steady-state spawn+update+records allocates %v times, want 0", allocs)
	}
}

func TestDecalOrientationFacesNormal(t *testing.T) {
	cases := []Vector3{Vec3(0, 1, 0), Vec3(1, 0, 0), Vec3(0, 0, 1), Vec3(0, 0, -1), Vec3(1, 1, 1), {}}
	for _, normal := range cases {
		for _, spin := range []float64{0, 1.2} {
			x, y, z, w := decalOrientation(normal, spin)
			// Rotate local +Z by the quaternion: v' = v + 2w(q x v) + 2 q x (q x v).
			vx, vy, vz := 0.0, 0.0, 1.0
			qx, qy, qz, qw := float64(x), float64(y), float64(z), float64(w)
			tx, ty, tz := 2*(qy*vz-qz*vy), 2*(qz*vx-qx*vz), 2*(qx*vy-qy*vx)
			rx := vx + qw*tx + (qy*tz - qz*ty)
			ry := vy + qw*ty + (qz*tx - qx*tz)
			rz := vz + qw*tz + (qx*ty - qy*tx)
			want := normal
			if want == (Vector3{}) {
				want = Vec3(0, 1, 0)
			}
			l := math.Sqrt(want.X*want.X + want.Y*want.Y + want.Z*want.Z)
			if math.Abs(rx-want.X/l) > 1e-5 || math.Abs(ry-want.Y/l) > 1e-5 || math.Abs(rz-want.Z/l) > 1e-5 {
				t.Errorf("normal %v spin %v: +Z maps to (%v,%v,%v)", normal, spin, rx, ry, rz)
			}
			if n := qx*qx + qy*qy + qz*qz + qw*qw; math.Abs(n-1) > 1e-5 {
				t.Errorf("quaternion not unit: %v", n)
			}
		}
	}
}

func TestDecalsLowerDropsUnknownMaterialAndDedupes(t *testing.T) {
	ir := Decals{
		Materials: []DecalMaterial{{Name: "a"}, {Name: "a", Albedo: "/dup.png"}, {Name: "b", Columns: 4, Rows: 4}},
		Items:     []ProjectedDecal{{Material: "b"}, {Material: "missing"}, {Material: "a", Tint: "#00ff00"}},
	}.lower()
	if len(ir.Materials) != 2 || ir.Materials[0].Albedo != "" {
		t.Fatalf("materials = %+v, want first duplicate kept", ir.Materials)
	}
	if len(ir.Records) != 2*DecalRecordStride {
		t.Fatalf("records = %d floats, want two records", len(ir.Records))
	}
	if ir.Records[18] != 1 || ir.Records[DecalRecordStride+18] != 0 {
		t.Errorf("material indexes = %v, %v; want 1, 0", ir.Records[18], ir.Records[DecalRecordStride+18])
	}
	if ir.Records[DecalRecordStride+13] != 1 || ir.Records[DecalRecordStride+12] != 0 {
		t.Errorf("tint not parsed: %v", ir.Records[DecalRecordStride+12:DecalRecordStride+16])
	}
	if ir.MaxDistance != DefaultDecalMaxDistance {
		t.Errorf("MaxDistance = %v", ir.MaxDistance)
	}
	m := ir.Materials[1]
	if m.EdgeFade != DefaultDecalEdgeFade || m.AngleFadeStart != DefaultDecalAngleFadeStart || m.Roughness != DefaultDecalRoughness || m.Columns != 4 {
		t.Errorf("defaults not applied: %+v", m)
	}
}

func TestMotionBlurLowerDefaultsAndClamps(t *testing.T) {
	ir := MotionBlur{}.lower()
	if ir.Quality != "medium" || ir.Samples != 10 || ir.Strength != 0.5 || ir.MaxBlur != 32 {
		t.Errorf("defaults = %+v", ir)
	}
	ir = MotionBlur{Quality: "LOW", Strength: 9, MaxBlur: 900}.lower()
	if ir.Quality != "low" || ir.Strength != 1 || ir.MaxBlur != 128 || ir.Samples != 6 {
		t.Errorf("clamped = %+v", ir)
	}
}

func BenchmarkDecalPoolSpawn(b *testing.B) {
	pool := NewDecalPool(DecalPoolConfig{Name: "p", Capacity: 256, Life: 5, FadeOut: 1})
	spawn := DecalSpawn{Position: Vec3(1, 2, 3), Normal: Vec3(0, 1, 0), Size: Vec3(0.3, 0.3, 0.6)}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		pool.Spawn(spawn)
	}
}
