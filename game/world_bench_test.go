package game

import "testing"

func benchmarkWorld10k(b *testing.B) *World {
	b.Helper()
	world := NewWorld()
	for i := 0; i < 10_000; i++ {
		id := world.Spawn()
		SetComponent(world, id, Transform{
			Position: Vec3{X: float64(i), Y: float64(i % 64), Z: float64(i % 11)},
			Scale:    Vec3{X: 1, Y: 1, Z: 1},
		})
		if i%3 == 0 {
			SetComponent(world, id, Velocity{Linear: Vec3{X: 1}})
		}
	}
	return world
}

func BenchmarkWorldQuery10k(b *testing.B) {
	world := benchmarkWorld10k(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rows := Query[Transform](world)
		if len(rows) != 10_000 {
			b.Fatalf("expected 10000 rows, got %d", len(rows))
		}
	}
}

func BenchmarkWorldQueryInto10k(b *testing.B) {
	world := benchmarkWorld10k(b)
	rows := make([]ComponentRef[Transform], 0, 10_000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rows = QueryInto(world, rows[:0])
		if len(rows) != 10_000 {
			b.Fatalf("expected 10000 rows, got %d", len(rows))
		}
	}
}

func BenchmarkWorldEntitiesInto10k(b *testing.B) {
	world := benchmarkWorld10k(b)
	entities := make([]EntityID, 0, 10_000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		entities = EntitiesInto(world, entities[:0])
		if len(entities) != 10_000 {
			b.Fatalf("expected 10000 entities, got %d", len(entities))
		}
	}
}

func BenchmarkWorldGetComponent10k(b *testing.B) {
	world := benchmarkWorld10k(b)
	ids := EntitiesInto(world, nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := GetComponent[Transform](world, ids[i%len(ids)]); !ok {
			b.Fatal("expected component")
		}
	}
}

func BenchmarkWorldSetComponent10k(b *testing.B) {
	world := benchmarkWorld10k(b)
	ids := EntitiesInto(world, nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SetComponent(world, ids[i%len(ids)], Transform{Position: Vec3{X: float64(i)}})
	}
}

func BenchmarkWorldSpawnAndSet(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		world := NewWorld()
		b.StartTimer()
		for j := 0; j < 1000; j++ {
			id := world.Spawn()
			SetComponent(world, id, Transform{Position: Vec3{X: float64(j)}})
		}
	}
}

func BenchmarkWorldDespawn1k(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		world := NewWorld()
		ids := make([]EntityID, 0, 1000)
		for j := 0; j < 1000; j++ {
			id := world.Spawn()
			ids = append(ids, id)
			SetComponent(world, id, Transform{Position: Vec3{X: float64(j)}})
		}
		b.StartTimer()
		for _, id := range ids {
			world.Despawn(id)
		}
	}
}
