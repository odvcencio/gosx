package game

import (
	"testing"
	"time"
)

// frameBudget is one 60 Hz frame. A query over the whole world must take a
// small fraction of it, because game logic still has to run.
const frameBudget = 16 * time.Millisecond

func probeWorld10k(tb testing.TB) *World {
	tb.Helper()
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

// fastestCall reports the shortest per-call time across several substantial
// batches. It calibrates with the fastest of three samples so a scheduler pause
// cannot trick it into selecting a tiny, noisy batch. The final shortest batch
// then resists interference from other work on a shared CI runner.
func fastestCall(batches int, fn func()) time.Duration {
	const (
		minBatchTime  = 20 * time.Millisecond
		maxIterations = 1 << 20
	)

	// Warm up so the first measured call does not pay lazy growth costs.
	fn()
	iterations := 16
	for iterations < maxIterations {
		calibrationBest := time.Duration(1 << 62)
		for sample := 0; sample < 3; sample++ {
			elapsed := callBatch(iterations, fn)
			if elapsed < calibrationBest {
				calibrationBest = elapsed
			}
		}
		if calibrationBest >= minBatchTime {
			break
		}
		iterations *= 2
	}

	best := time.Duration(1 << 62)
	for batch := 0; batch < batches; batch++ {
		elapsed := callBatch(iterations, fn) / time.Duration(iterations)
		if elapsed < best {
			best = elapsed
		}
	}
	return best
}

func callBatch(iterations int, fn func()) time.Duration {
	start := time.Now()
	for i := 0; i < iterations; i++ {
		fn()
	}
	return time.Since(start)
}

// TestSetComponentDoesNotAllocate proves that writing a typed component must not
// box the value on the heap. A map of any per component type allocates for every
// non-trivial struct.
func TestSetComponentDoesNotAllocate(t *testing.T) {
	world := NewWorld()
	id := world.Spawn()
	SetComponent(world, id, Transform{})

	allocs := testing.AllocsPerRun(200, func() {
		SetComponent(world, id, Transform{Position: Vec3{X: 1, Y: 2, Z: 3}})
	})
	if allocs != 0 {
		t.Fatalf("SetComponent allocated %.1f objects per call, want 0", allocs)
	}
}

// TestQueryIntoDoesNotAllocate locks the reusable destination contract that
// keeps a world query out of the per-frame allocator and garbage collector.
func TestQueryIntoDoesNotAllocate(t *testing.T) {
	world := probeWorld10k(t)
	rows := make([]ComponentRef[Transform], 0, 10_000)

	allocs := testing.AllocsPerRun(200, func() {
		rows = QueryInto(world, rows[:0])
	})
	if len(rows) != 10_000 {
		t.Fatalf("expected 10000 rows, got %d", len(rows))
	}
	if allocs != 0 {
		t.Fatalf("QueryInto allocated %.1f objects per call, want 0", allocs)
	}
}

// TestQueryIntoFitsFrameBudget proves that an allocation-free query over 10000
// entities remains a bounded contiguous-memory operation. The same-sized copy
// is a hardware-local control: it catches a return to map walks or sorting
// without assuming every shared CI runner has the same memory bandwidth.
func TestQueryIntoFitsFrameBudget(t *testing.T) {
	if raceEnabled {
		t.Skip("wall clock budget does not hold under the race detector")
	}
	world := probeWorld10k(t)
	rows := make([]ComponentRef[Transform], 0, 10_000)
	reference := QueryInto[Transform](world, nil)
	copyRows := make([]ComponentRef[Transform], len(reference))

	fastest := fastestCall(8, func() {
		rows = QueryInto(world, rows[:0])
	})
	copyFastest := fastestCall(8, func() {
		copy(copyRows, reference)
	})
	if len(rows) != 10_000 {
		t.Fatalf("expected 10000 rows, got %d", len(rows))
	}
	if copyRows[len(copyRows)-1].Entity != reference[len(reference)-1].Entity {
		t.Fatal("same-sized copy control did not preserve the final entity")
	}

	const maxCopyMultiple = 8
	catastrophicBudget := frameBudget / 8
	t.Logf("QueryInto over 10000 entities = %v per call; same-sized copy = %v", fastest, copyFastest)
	if fastest > copyFastest*maxCopyMultiple {
		t.Fatalf("QueryInto takes %v per call, more than %dx the same-sized copy (%v)",
			fastest, maxCopyMultiple, copyFastest)
	}
	if fastest > catastrophicBudget {
		t.Fatalf("QueryInto takes %v per call, exceeds catastrophic ceiling %v",
			fastest, catastrophicBudget)
	}
}

// TestEntitiesIntoFitsFrameBudget proves the same for the entity list.
func TestEntitiesIntoFitsFrameBudget(t *testing.T) {
	if raceEnabled {
		t.Skip("wall clock budget does not hold under the race detector")
	}
	world := probeWorld10k(t)
	entities := make([]EntityID, 0, 10_000)

	fastest := fastestCall(8, func() {
		entities = EntitiesInto(world, entities[:0])
	})
	if len(entities) != 10_000 {
		t.Fatalf("expected 10000 entities, got %d", len(entities))
	}
	budget := frameBudget / 160
	t.Logf("EntitiesInto over 10000 entities = %v per call", fastest)
	if fastest > budget {
		t.Fatalf("EntitiesInto takes %v per call, want at most %v", fastest, budget)
	}
}

// TestQueryIntoStaysOrderedAfterOutOfOrderInserts locks the documented order
// guarantee so the storage change cannot silently drop it.
func TestQueryIntoStaysOrderedAfterOutOfOrderInserts(t *testing.T) {
	world := NewWorld()
	ids := make([]EntityID, 6)
	for i := range ids {
		ids[i] = world.Spawn()
	}
	for _, i := range []int{4, 0, 5, 2, 1, 3} {
		SetComponent(world, ids[i], Transform{Position: Vec3{X: float64(i)}})
	}

	rows := QueryInto[Transform](world, nil)
	if len(rows) != 6 {
		t.Fatalf("expected 6 rows, got %d", len(rows))
	}
	for i, row := range rows {
		if row.Entity != ids[i] {
			t.Fatalf("row %d = entity %d, want %d", i, row.Entity, ids[i])
		}
		if row.Value.Position.X != float64(i) {
			t.Fatalf("row %d value = %v, want X=%d", i, row.Value, i)
		}
	}

	world.Despawn(ids[2])
	world.Despawn(ids[0])
	rows = QueryInto(world, rows[:0])
	want := []EntityID{ids[1], ids[3], ids[4], ids[5]}
	if len(rows) != len(want) {
		t.Fatalf("expected %d rows after despawn, got %d", len(want), len(rows))
	}
	for i, row := range rows {
		if row.Entity != want[i] {
			t.Fatalf("row %d = entity %d, want %d", i, row.Entity, want[i])
		}
	}
}

// TestUntypedSetInteroperatesWithTypedRead locks the mixed access path: a value
// written through World.Set must be readable through GetComponent and Query.
func TestUntypedSetInteroperatesWithTypedRead(t *testing.T) {
	world := NewWorld()
	first := world.Spawn()
	second := world.Spawn()

	if !world.Set(first, Transform{Position: Vec3{X: 7}}) {
		t.Fatal("expected untyped Set to succeed")
	}
	if got, ok := GetComponent[Transform](world, first); !ok || got.Position.X != 7 {
		t.Fatalf("typed read of untyped Set = %#v ok=%v", got, ok)
	}
	if !SetComponent(world, second, Transform{Position: Vec3{X: 9}}) {
		t.Fatal("expected typed Set to succeed")
	}
	rows := Query[Transform](world)
	if len(rows) != 2 || rows[0].Value.Position.X != 7 || rows[1].Value.Position.X != 9 {
		t.Fatalf("mixed rows = %#v", rows)
	}
	if got, ok := GetComponent[Transform](world, second); !ok || got.Position.X != 9 {
		t.Fatalf("typed read after typed Set = %#v ok=%v", got, ok)
	}
	if !world.Set(second, Transform{Position: Vec3{X: 11}}) {
		t.Fatal("expected untyped overwrite to succeed")
	}
	if got, _ := GetComponent[Transform](world, second); got.Position.X != 11 {
		t.Fatalf("untyped overwrite = %#v", got)
	}
}
