package scene

import (
	"math"
	"testing"

	"m31labs.dev/gosx/physics"
)

func TestTerrainColliderMatchesHeightAt(t *testing.T) {
	terrain := &Terrain{Heightmap: GenerateHeightmap(TerrainNoise{Seed: 5, Columns: 65, Rows: 49}), Position: Vector3{X: 30, Y: -4, Z: -12}, Width: 128, Depth: 96, HeightScale: 25}
	config, err := terrain.ColliderConfig()
	if err != nil {
		t.Fatal(err)
	}
	world := physics.NewWorld(physics.WorldConfig{Gravity: physics.Vec3{Y: -10}, FixedTimestep: 1.0 / 60})
	collider := world.AddCollider(config)
	if err := collider.Err(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 200; i++ {
		x := 30 - 63 + float64(i%20)*6.3 + float64(i)*0.011
		z := -12 - 47 + float64(i/20)*9.4 + float64(i%7)*0.13
		got, ok := collider.HeightAt(x, z)
		if !ok {
			t.Fatalf("collider missed (%v, %v)", x, z)
		}
		if want := terrain.HeightAt(x, z); math.Abs(got-want) > 1e-9 {
			t.Fatalf("collider height %v, terrain %v at (%v, %v)", got, want, x, z)
		}
	}
	if _, err := (&Terrain{}).ColliderConfig(); err == nil {
		t.Fatal("terrain without a heightmap built a collider")
	}
}
