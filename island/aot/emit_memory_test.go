package aot

import (
	"encoding/base64"
	"encoding/binary"
	"strings"
	"testing"

	"m31labs.dev/gosx/internal/wasmgen"
	"m31labs.dev/gosx/island/program"
)

func arenaTestModule(t *testing.T, u Unit, roots uint32, expression program.ExprID) *expressionEmitter {
	t.Helper()
	e, err := emitArenaExpressions(u, roots)
	if err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"begin", "commit", "abort", "store"} {
		e.module.Exports = append(e.module.Exports, wasmgen.Export{Name: name, Function: e.transactions[i]})
	}
	e.module.Exports = append(e.module.Exports, wasmgen.Export{Name: "evaluate", Function: e.functions[expression]})
	for _, item := range []struct {
		name   string
		global uint32
	}{{"committed", committedBaseGlobal}, {"working", arenaBaseGlobal}, {"cursor", allocationGlobal},
		{"live", workingStringsGlobal}, {"committedLive", committedStringsGlobal}, {"pending", pendingGlobal}, {"status", errorGlobal}} {
		index := uint32(len(e.module.Imports) + len(e.module.Functions))
		var b instructions
		b.index(0x23, item.global)
		b.op(0x0b)
		e.module.Functions = append(e.module.Functions, wasmgen.Function{Signature: i32Signature(0), Body: b})
		e.module.Exports = append(e.module.Exports, wasmgen.Export{Name: item.name, Function: index})
	}
	return e
}

func scalarTransport(typ program.ExprType, flags uint32, number int64, text string) string {
	record := make([]byte, valueBytes+len(text))
	binary.LittleEndian.PutUint32(record, uint32(typ))
	binary.LittleEndian.PutUint32(record[4:], flags)
	binary.LittleEndian.PutUint64(record[8:], uint64(number))
	if text != "" {
		binary.LittleEndian.PutUint32(record[16:], 32768+valueBytes)
		binary.LittleEndian.PutUint32(record[20:], uint32(len(text)))
		copy(record[valueBytes:], text)
	}
	return base64.StdEncoding.EncodeToString(record)
}

func TestEmitArenaCommitAbortAndPendingOwnership(t *testing.T) {
	u, root := integerUnit(t, program.OpAdd, 1, 1)
	e := arenaTestModule(t, u, 1, root)
	var got []int32
	runExpressionModule(t, e.module, `
  const api = instance.exports, results = [];
  const read = which => view.getInt32(api[which]() + 8, true);
  memory.set(Buffer.from(data, 'base64'), 32768);
  results.push(api.begin(1, 0, 0), api.commit(0, 0), api.store(0, 32768));
  results.push(api.begin(0, 0, 1), api.store(0, 32768), api.commit(0, 0), read('committed'));
  results.push(api.begin(1, 0, 0), api.store(0, api.evaluate(0)), read('working'), read('committed'));
  results.push(api.begin(2, 0, 0), api.commit(0, 0), api.pending(), api.commit(2, 0));
  results.push(api.abort(), api.abort(), read('committed'), api.pending());
  results.push(api.begin(1, 0, 0), api.store(0, api.evaluate(0)), api.commit(1, 0),
    api.commit(1, 0), read('committed'), api.commit(2, 0), api.begin(0, 0, 0), api.begin(2, 0, 1));
  process.stdout.write(JSON.stringify(results));`, scalarTransport(program.TypeInt, 0, 7, ""), &got)
	want := []int32{8, 8, 7, 0, 0, 0, 7, 0, 0, 2, 7, 7, 0, 1, 8, 0, 0, 7, 0, 0, 0, 0, 0, 2, 8, 8, 8}
	if len(got) != len(want) {
		t.Fatalf("transaction trace length: %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("transaction checkpoint %d: %v want %v", i, got, want)
		}
	}
}

func TestEmitRootFailuresPreserveCommittedAndPreparedValues(t *testing.T) {
	u := literalUnit(t)
	e := arenaTestModule(t, u, 1, 0)
	requests := []string{scalarTransport(program.TypeInt, 0, 11, ""),
		scalarTransport(program.TypeFloat, 0, 0, ""), scalarTransport(program.TypeInt, 0, 2147483648, ""),
		scalarTransport(program.TypeString, 1, 0, strings.Repeat("x", 4097))}
	var got []uint32
	runExpressionModule(t, e.module, `
  const api = instance.exports, results = [];
  memory.set(Buffer.from(data[0], 'base64'), 32768);
  api.begin(0, 0, 1); api.store(0, 32768); api.commit(0, 0);
  const committed = Buffer.from(memory.slice(api.committed(), api.committed() + 24)).toString('hex');
  for (let index = 1; index < data.length; index++) {
    api.begin(index, 0, 0);
    const prepared = Buffer.from(memory.slice(api.working(), api.working() + 24)).toString('hex');
    memory.set(Buffer.from(data[index], 'base64'), 32768);
    results.push(api.store(0, 32768), api.commit(index, 0),
      Buffer.from(memory.slice(api.working(), api.working() + 24)).toString('hex') === prepared ? 1 : 0,
      Buffer.from(memory.slice(api.committed(), api.committed() + 24)).toString('hex') === committed ? 1 : 0);
    api.abort();
  }
  for (const pointer of [0, -1, 196585]) {
    api.begin(4, 0, 0);
    results.push(api.store(0, pointer), api.commit(4, 0));
    api.abort();
  }
  api.begin(5, 0, 0);
  results.push(api.store(1, 32768), api.commit(5, 0));
  api.abort();
  process.stdout.write(JSON.stringify(results));`, requests, &got)
	want := []uint32{2, 2, 1, 1, 3, 3, 1, 1, 4, 4, 1, 1, 2, 2, 2, 2, 2, 2, 2, 2}
	if len(got) != len(want) {
		t.Fatalf("failure trace length: %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("failure checkpoint %d: %v want %v", i, got, want)
		}
	}
}

func TestEmitArenaReclaimsHighWaterAcrossLargeUpdates(t *testing.T) {
	u, root := stringUnit(t, program.OpConcat, strings.Repeat("x", 4095), "y")
	e := arenaTestModule(t, u, 1, root)
	var got struct {
		Iterations, Cursor, MemoryBytes, Flags, Length uint32
		Text                                           string
		CannotGrow                                     bool
	}
	text := strings.Repeat("x", 4095) + "y"
	runExpressionModule(t, e.module, `
  const api = instance.exports;
  memory.set(Buffer.from(data, 'base64'), 32768);
  if (api.begin(0, 0, 1) || api.store(0, 32768) || api.commit(0, 0)) throw new Error('initialization');
  let cursor = 0;
  for (let sequence = 1; sequence <= 500; sequence++) {
    const previous = api.committed();
    if (api.begin(sequence, 0, 0) || api.store(0, api.evaluate(0))) throw new Error('prepare');
    if (api.committed() !== previous || api.live() !== 4096) throw new Error('premature state change');
    if (cursor && cursor !== api.cursor()) throw new Error('growing high water');
    cursor = api.cursor();
    if (api.commit(sequence, 0) || api.committed() === previous || api.committedLive() !== 4096) throw new Error('commit');
  }
  const base = api.committed();
  const pointer = view.getUint32(base + 16, true), length = view.getUint32(base + 20, true);
  let cannotGrow = false;
  try { api.memory.grow(1); } catch (error) { cannotGrow = error instanceof RangeError; }
  process.stdout.write(JSON.stringify({Iterations: 500, Cursor: cursor, MemoryBytes: memory.length,
    Flags: view.getUint32(base + 4, true), Length: length,
    Text: Buffer.from(memory.slice(pointer, pointer + length)).toString('base64'), CannotGrow: cannotGrow}));`,
		scalarTransport(program.TypeString, 1, 0, text), &got)
	decoded, _ := base64.StdEncoding.DecodeString(got.Text)
	if got.Iterations != 500 || got.Cursor != e.reserved+3*4096+valueBytes || got.MemoryBytes != 196608 || !got.CannotGrow || got.Flags != 1 || got.Length != 4096 || string(decoded) != text {
		t.Fatalf("bounded high water: %+v", got)
	}
}

func TestEmitLiveStringBudgetCountsReplacement(t *testing.T) {
	u := literalUnit(t)
	e := arenaTestModule(t, u, 5, 0)
	var got []uint32
	runExpressionModule(t, e.module, `
  const api = instance.exports, results = [];
  memory.set(Buffer.from(data, 'base64'), 32768);
  results.push(api.begin(0, 0, 1));
  for (let slot = 0; slot < 4; slot++) results.push(api.store(slot, 32768));
  results.push(api.live(), api.commit(0, 0), api.committedLive());
  results.push(api.begin(1, 0, 0), api.store(4, 32768), api.live(), api.commit(1, 0), api.committedLive());
  api.abort();
  results.push(api.begin(2, 0, 0), api.store(0, 32768), api.live(), api.commit(2, 0), api.committedLive());
  process.stdout.write(JSON.stringify(results));`, scalarTransport(program.TypeString, 1, 0, strings.Repeat("x", 4096)), &got)
	want := []uint32{0, 0, 0, 0, 0, 16384, 0, 16384, 0, 4, 16384, 4, 16384, 0, 0, 16384, 0, 16384}
	if len(got) != len(want) {
		t.Fatalf("live string trace length: %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("live string checkpoint %d: %v want %v", i, got, want)
		}
	}
}

func TestEmitSequenceWordsGapsAndSingleInitialization(t *testing.T) {
	u := literalUnit(t)
	e := arenaTestModule(t, u, 0, 0)
	var got []uint32
	runExpressionModule(t, e.module, `
  const api = instance.exports, results = [api.begin(0, 0, 2), api.begin(0, 0, 1), api.commit(0, 0)];
  for (const words of data) {
    results.push(api.begin(words[0], words[1], 0), api.commit(words[0], words[1]), api.commit(words[0], words[1]));
  }
  results.push(api.begin(0, 0, 0), api.begin(-1, -1, 0), api.begin(0, 0, 1));
  process.stdout.write(JSON.stringify(results));`, [][2]uint32{{0xffffffff, 0}, {0, 1}, {5, 1}, {0xffffffff, 0x7fffffff}, {0, 0x80000000}, {0xffffffff, 0xffffffff}}, &got)
	if len(got) != 24 || got[0] != 2 || got[21] != 8 || got[22] != 8 || got[23] != 8 {
		t.Fatalf("sequence word trace: %v", got)
	}
	for _, status := range got[1:21] {
		if status != 0 {
			t.Fatalf("sequence gap/carry failed: %v", got)
		}
	}
	aborted := arenaTestModule(t, u, 0, 0)
	var attempt []uint32
	runExpressionModule(t, aborted.module, `
  process.stdout.write(JSON.stringify([instance.exports.begin(0, 0, 1), instance.exports.abort(),
    instance.exports.begin(0, 0, 1), instance.exports.begin(1, 0, 0)]));`, nil, &attempt)
	if len(attempt) != 4 || attempt[0] != 0 || attempt[1] != 0 || attempt[2] != 8 || attempt[3] != 8 {
		t.Fatalf("initialization attempted twice: %v", attempt)
	}
}

func TestEmitRootLayoutCeiling(t *testing.T) {
	u := literalUnit(t)
	if e, err := emitArenaExpressions(u, 513); err == nil || e != nil {
		t.Fatal("accepted excessive root layout")
	}
	e, err := emitArenaExpressions(u, 512)
	if err != nil || e.reserved != 513*valueBytes || e.module.Globals[workingBaseGlobal].Initial != 131072+512*valueBytes {
		t.Fatalf("maximum root reservation: %v", err)
	}
}

func TestEmitRootCopiesPreserveZeroKindsAndInputOwnership(t *testing.T) {
	u := literalUnit(t)
	e := arenaTestModule(t, u, 4, 0)
	var got []uint32
	runExpressionModule(t, e.module, `
  const api = instance.exports, results = [];
  api.begin(0, 0, 1);
  for (let slot = 0; slot < data.length; slot++) {
    memory.set(Buffer.from(data[slot], 'base64'), 32768);
    results.push(api.store(slot, 32768));
  }
  memory.fill(165, 32768, 32864);
  results.push(api.commit(0, 0), api.begin(1, 0, 0));
  for (const base of [api.committed(), api.working()]) {
    for (let slot = 0; slot < 3; slot++) results.push(view.getUint32(base + slot * 24, true),
      view.getUint32(base + slot * 24 + 4, true));
    const pointer = view.getUint32(base + 3 * 24 + 16, true), length = view.getUint32(base + 3 * 24 + 20, true);
    results.push(length, Buffer.from(memory.slice(pointer, pointer + length)).toString('hex') === '68c3a96c6c6f20f09f8cb4' ? 1 : 0);
  }
  results.push(api.abort(), api.committedLive());
  process.stdout.write(JSON.stringify(results));`, []string{
		scalarTransport(program.TypeString, 0, 0, ""), scalarTransport(program.TypeString, 1, 0, ""),
		scalarTransport(program.TypeAny, 0, 0, ""), scalarTransport(program.TypeString, 1, 0, "héllo 🌴"),
	}, &got)
	want := []uint32{0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 5, 0, 11, 1, 0, 0, 0, 1, 5, 0, 11, 1, 0, 11}
	if len(got) != len(want) {
		t.Fatalf("zero-kind/owned-string trace length: %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("owned-string checkpoint %d: %v want %v", i, got, want)
		}
	}
}
