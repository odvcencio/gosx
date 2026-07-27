package game

import (
	"runtime"
	"testing"
)

// probeWorldSize is the entity count the query probes run over. One 60 Hz frame
// is 16 milliseconds, and a query over this many entities has to leave almost
// all of it for game logic.
const probeWorldSize = 10_000

func probeWorld(tb testing.TB, size int) *World {
	tb.Helper()
	world := NewWorld()
	for i := 0; i < size; i++ {
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

// maxProbeAllocs and maxProbeBytes are the per-call figures an allocation-free
// call may still measure.
//
// Neither is zero, because ReadMemStats counts the whole process: a background
// goroutine allocating once during a 50-run batch shows up as 0.02 allocations
// and 0.32 bytes per call. Measured on this repository the floor is one stray
// allocation per batch, and usually none.
//
// The smallest failure these bounds must catch is a call that stops reusing the
// caller's buffer and allocates ONE result slice instead. Over 10000 entities
// that measures about 1.0 allocations and 800000 bytes per call, and over 1250
// entities about 1.0 and 106000. So the bounds sit at twice the noise and well
// under half the smallest regression, in both figures.
const (
	maxProbeAllocs = 0.5
	maxProbeBytes  = 1024.0
)

// callWork reports the memory one call to fn allocates, in bytes and in
// allocation count.
//
// This replaces an absolute wall clock budget. The two probes below used to
// assert "at most 200 microseconds per call" and "at most 100 microseconds per
// call", measured with a fastestCall helper copied from semantic. Both carried the
// pattern the semantic package just removed:
//
//   - A busy machine changes the clock, so the bound flakes and teaches a reader
//     to ignore a red build.
//   - The race detector changes it so much that both tests SKIPPED under
//     `make test-race`, where the guard never ran at all.
//
// Allocated memory is the property that actually decides whether these calls fit
// a frame. QueryInto and EntitiesInto exist to append into a caller's buffer
// instead of allocating one, and the regression they guard against is a store
// that boxes each row, which turns one call into thousands of allocations. That
// number does not move with machine load, and it does not move under -race, so
// the guard now runs everywhere.
func callWork(fn func(), runs int) (bytes, allocs float64) {
	fn() // warm any lazy growth, then measure
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for i := 0; i < runs; i++ {
		fn()
	}
	runtime.ReadMemStats(&after)
	return float64(after.TotalAlloc-before.TotalAlloc) / float64(runs),
		float64(after.Mallocs-before.Mallocs) / float64(runs)
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

// TestQueryIntoAllocatesNothingPerCall proves the property a fixed-step game loop
// depends on: a query over the whole world into a reused buffer costs no
// allocation, however many entities it returns.
//
// The measurement is taken twice in one process, and the ratio is what the
// assertion reads:
//
//  1. QueryInto over a reused buffer must allocate nothing at all.
//  2. Query, which passes a nil buffer, must allocate. Step 2 proves the probe
//     reacts to allocation at all, so the zero in step 1 means something. Without
//     it, a broken measurement would report zero for everything and pass.
func TestQueryIntoAllocatesNothingPerCall(t *testing.T) {
	const runs = 50
	world := probeWorld(t, probeWorldSize)
	rows := make([]ComponentRef[Transform], 0, probeWorldSize)

	intoBytes, intoAllocs := callWork(func() {
		rows = QueryInto(world, rows[:0])
		if len(rows) != probeWorldSize {
			t.Fatalf("expected %d rows, got %d", probeWorldSize, len(rows))
		}
	}, runs)
	t.Logf("QueryInto over %d entities = %.0f bytes / %.1f allocs per call",
		probeWorldSize, intoBytes, intoAllocs)

	if intoAllocs > maxProbeAllocs || intoBytes > maxProbeBytes {
		t.Fatalf("QueryInto allocated %.2f objects and %.0f bytes per call over %d entities, "+
			"want at most %.1f and %.0f.\n"+
			"QueryInto exists to append into the caller's buffer. An allocation here means it "+
			"stopped reusing that buffer, or the column now boxes each row, so a fixed-step loop "+
			"pays the garbage collector every frame.",
			intoAllocs, intoBytes, probeWorldSize, maxProbeAllocs, maxProbeBytes)
	}

	// The control. Query passes nil, so it must allocate the result slice.
	queryBytes, queryAllocs := callWork(func() {
		if got := len(Query[Transform](world)); got != probeWorldSize {
			t.Fatalf("expected %d rows, got %d", probeWorldSize, got)
		}
	}, runs)
	t.Logf("Query over %d entities = %.0f bytes / %.1f allocs per call (control)",
		probeWorldSize, queryBytes, queryAllocs)
	if queryAllocs < 1 {
		t.Fatalf("Query allocated %.1f objects per call, so the probe cannot see allocation at all; "+
			"the zero measured for QueryInto proves nothing", queryAllocs)
	}
}

// TestEntitiesIntoAllocatesNothingPerCall proves the same for the entity list,
// with Entities as the control.
func TestEntitiesIntoAllocatesNothingPerCall(t *testing.T) {
	const runs = 50
	world := probeWorld(t, probeWorldSize)
	entities := make([]EntityID, 0, probeWorldSize)

	intoBytes, intoAllocs := callWork(func() {
		entities = EntitiesInto(world, entities[:0])
		if len(entities) != probeWorldSize {
			t.Fatalf("expected %d entities, got %d", probeWorldSize, len(entities))
		}
	}, runs)
	t.Logf("EntitiesInto over %d entities = %.0f bytes / %.1f allocs per call",
		probeWorldSize, intoBytes, intoAllocs)

	if intoAllocs > maxProbeAllocs || intoBytes > maxProbeBytes {
		t.Fatalf("EntitiesInto allocated %.2f objects and %.0f bytes per call over %d entities, "+
			"want at most %.1f and %.0f.\n"+
			"The live list is already ordered, so this call copies memory into the caller's buffer "+
			"and must not allocate.",
			intoAllocs, intoBytes, probeWorldSize, maxProbeAllocs, maxProbeBytes)
	}

	entBytes, entAllocs := callWork(func() {
		if got := len(world.Entities()); got != probeWorldSize {
			t.Fatalf("expected %d entities, got %d", probeWorldSize, got)
		}
	}, runs)
	t.Logf("World.Entities over %d entities = %.0f bytes / %.1f allocs per call (control)",
		probeWorldSize, entBytes, entAllocs)
	if entAllocs < 1 {
		t.Fatalf("World.Entities allocated %.1f objects per call, so the probe cannot see allocation "+
			"at all; the zero measured for EntitiesInto proves nothing", entAllocs)
	}
}

// TestQueryIntoWorkDoesNotGrowWithTheWorld proves the per-call cost is bounded by
// the caller's buffer and not by the store size.
//
// The test takes two measurements of the same code in one process, so machine
// speed and machine load cancel:
//
//  1. Grow the world eight times and query into a reused buffer. The allocation
//     count and the allocated bytes must both stay at the noise floor. A call
//     that allocated its own result slice would show about 106000 bytes per call
//     at the small size and 800000 at the large one.
//  2. Grow the world eight times and query with a NIL buffer. The allocated bytes
//     must grow with it. This proves the probe tracks the row count at all, so the
//     flat result in step 1 means something. Without step 2, a measurement that
//     always reported zero would pass step 1.
func TestQueryIntoWorkDoesNotGrowWithTheWorld(t *testing.T) {
	const (
		small = probeWorldSize / 8
		large = probeWorldSize
		runs  = 50
	)
	sizeFactor := float64(large) / float64(small)

	// Step 1: the reused buffer must stay flat.
	intoWork := func(size int) (float64, float64) {
		world := probeWorld(t, size)
		rows := make([]ComponentRef[Transform], 0, size)
		return callWork(func() {
			rows = QueryInto(world, rows[:0])
			if len(rows) != size {
				t.Fatalf("expected %d rows, got %d", size, len(rows))
			}
		}, runs)
	}
	smallBytes, smallAllocs := intoWork(small)
	largeBytes, largeAllocs := intoWork(large)
	t.Logf("QueryInto into a reused buffer: %d entities = %.0f bytes / %.2f allocs; "+
		"%d entities = %.0f bytes / %.2f allocs",
		small, smallBytes, smallAllocs, large, largeBytes, largeAllocs)

	for _, m := range []struct {
		size   int
		allocs float64
		bytes  float64
	}{{small, smallAllocs, smallBytes}, {large, largeAllocs, largeBytes}} {
		if m.allocs > maxProbeAllocs || m.bytes > maxProbeBytes {
			t.Fatalf("QueryInto over %d entities allocated %.2f objects and %.0f bytes per call, "+
				"want at most %.1f and %.0f; the work must be bounded by the caller's buffer, "+
				"not by the store size", m.size, m.allocs, m.bytes, maxProbeAllocs, maxProbeBytes)
		}
	}

	// Step 2: the nil buffer must react to the row count, or step 1 proves
	// nothing. Query allocates one result slice per call, so its BYTES track the
	// rows even though its allocation count does not.
	nilWork := func(size int) float64 {
		world := probeWorld(t, size)
		bytes, _ := callWork(func() {
			if got := len(Query[Transform](world)); got != size {
				t.Fatalf("expected %d rows, got %d", size, got)
			}
		}, runs)
		return bytes
	}
	smallNil := nilWork(small)
	largeNil := nilWork(large)
	t.Logf("Query with a nil buffer (control): %d entities = %.0f bytes; %d entities = %.0f bytes",
		small, smallNil, large, largeNil)

	// The margin absorbs slice growth rounding, which overshoots at the small
	// size and flattens the measured ratio below the growth in entity count.
	const growthMargin = 2.0
	minGrowth := sizeFactor / growthMargin
	if smallNil <= 0 {
		t.Fatalf("Query with a nil buffer allocated %.0f bytes per call over %d entities; "+
			"the probe cannot see allocation at all", smallNil, small)
	}
	if ratio := largeNil / smallNil; ratio < minGrowth {
		t.Fatalf("growing the world %.0fx moved Query's allocated bytes by only %.2fx "+
			"(want at least %.2fx); the probe no longer tracks the row count, so the flat "+
			"QueryInto result above proves nothing", sizeFactor, ratio, minGrowth)
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
