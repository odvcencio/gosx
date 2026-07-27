package scene

import (
	"encoding/json"
	"math"
	"math/rand/v2"
	"reflect"
	"testing"
)

// negativeZero is written this way on purpose. A literal -0.0 is the constant
// zero in Go, so the sign has to come from a runtime call.
var negativeZero = math.Copysign(0, -1)

// TestSceneRecordEqualMatchesJSONSemantics pins the comparison against the JSON
// wire semantics, case by case. Each case states the answer the encoder gives and
// the test checks both the reference comparison and the live one, so a case can
// never carry a wrong expectation and pass.
//
// A wrong "equal" here is a silently lost edit: diffSceneRecords skips the record
// and no command ships. So the cases concentrate on the shapes where a
// hand-written comparison goes wrong.
func TestSceneRecordEqualMatchesJSONSemantics(t *testing.T) {
	opacityZero := 0.0
	opacityOne := 1.0
	opacityOneAgain := 1.0

	t.Run("ObjectIR", func(t *testing.T) {
		cases := []struct {
			name      string
			a, b      ObjectIR
			wantEqual bool
			why       string
		}{
			{
				name: "signed zero in an omitempty field", a: ObjectIR{ID: "a", X: negativeZero}, b: ObjectIR{ID: "a", X: 0},
				wantEqual: true,
				why:       "omitempty drops both, because -0.0 == 0 in Go, so the wire bytes match",
			},
			{
				name: "nil pointer against a pointer to zero", a: ObjectIR{ID: "a"}, b: ObjectIR{ID: "a", Opacity: &opacityZero},
				wantEqual: false,
				why:       "the pointer field omits nil and writes 0, which are different bytes",
			},
			{
				name: "two pointers to equal values", a: ObjectIR{ID: "a", Opacity: &opacityOne}, b: ObjectIR{ID: "a", Opacity: &opacityOneAgain},
				wantEqual: true,
				why:       "the encoder writes the pointee, not the address",
			},
			{
				name: "NaN against itself", a: ObjectIR{ID: "a", X: math.NaN()}, b: ObjectIR{ID: "a", X: math.NaN()},
				wantEqual: false,
				why:       "the encoder rejects NaN, so a record holding one differs from every record, itself included",
			},
			{
				name: "nil slice against an empty slice", a: ObjectIR{ID: "a"}, b: ObjectIR{ID: "a", Points: []Vector3{}},
				wantEqual: true,
				why:       "omitempty drops a slice of length zero either way",
			},
			{
				name: "nil map against an entry", a: ObjectIR{ID: "a"}, b: ObjectIR{ID: "a", CustomUniforms: map[string]any{"k": 1.0}},
				wantEqual: false,
				why:       "one side writes the map and the other omits it",
			},
			{
				name: "same map entries", a: ObjectIR{ID: "a", CustomUniforms: map[string]any{"k": 1.0, "j": "x"}},
				b:         ObjectIR{ID: "a", CustomUniforms: map[string]any{"j": "x", "k": 1.0}},
				wantEqual: true,
				why:       "the encoder sorts map keys, so insertion order does not reach the wire",
			},
			{
				name: "one map value differs", a: ObjectIR{ID: "a", CustomUniforms: map[string]any{"k": 1.0}},
				b:         ObjectIR{ID: "a", CustomUniforms: map[string]any{"k": 2.0}},
				wantEqual: false,
				why:       "a changed uniform has to reach the wire",
			},
			{
				name: "nested slice element differs", a: ObjectIR{ID: "a", Points: []Vector3{{X: 1}, {Y: 2}}},
				b:         ObjectIR{ID: "a", Points: []Vector3{{X: 1}, {Y: 3}}},
				wantEqual: false,
				why:       "a nested field change has to reach the wire",
			},
			{
				name: "transform field differs", a: ObjectIR{ID: "a", X: 1}, b: ObjectIR{ID: "a", X: 1.0000000001},
				wantEqual: false,
				why:       "a tiny move is still a move",
			},
			{
				name: "nested struct field differs", a: ObjectIR{ID: "a", Transition: TransitionIR{In: TransitionTimingIR{Duration: 100}}},
				b:         ObjectIR{ID: "a", Transition: TransitionIR{In: TransitionTimingIR{Duration: 200}}},
				wantEqual: false,
				why:       "a nested struct is part of the record",
			},
		}
		for _, testCase := range cases {
			t.Run(testCase.name, func(t *testing.T) {
				assertComparison(t, testCase.a, testCase.b, testCase.wantEqual, testCase.why)
			})
		}
	})

	t.Run("CompressedArray", func(t *testing.T) {
		cases := []struct {
			name      string
			a, b      CompressedArray
			wantEqual bool
			why       string
		}{
			{
				name: "signed zero in a field without omitempty", a: CompressedArray{MaxVal: 0}, b: CompressedArray{MaxVal: float32(negativeZero)},
				wantEqual: false,
				why:       "maxVal always ships, and the encoder writes 0 against -0",
			},
			{
				name: "nil byte slice against an empty one", a: CompressedArray{}, b: CompressedArray{Packed: []byte{}},
				wantEqual: false,
				why:       "packed has no omitempty, so it ships as null against an empty base64 string",
			},
			{
				name: "same packed bytes", a: CompressedArray{Packed: []byte{1, 2, 3}}, b: CompressedArray{Packed: []byte{1, 2, 3}},
				wantEqual: true,
				why:       "equal bytes encode to the same base64",
			},
			{
				name: "one packed byte differs", a: CompressedArray{Packed: []byte{1, 2, 3}}, b: CompressedArray{Packed: []byte{1, 2, 4}},
				wantEqual: false,
				why:       "a quantized lane change has to reach the wire",
			},
		}
		for _, testCase := range cases {
			t.Run(testCase.name, func(t *testing.T) {
				assertComparison(t, testCase.a, testCase.b, testCase.wantEqual, testCase.why)
			})
		}
	})

	t.Run("PointsIR dense floats", func(t *testing.T) {
		base := []float64{0, 1.5, -2.25, 1e-300}
		shifted := []float64{0, 1.5, -2.25, 1e-300 * 2}
		signed := []float64{negativeZero, 1.5, -2.25, 1e-300}
		assertComparison(t, PointsIR{ID: "p", Positions: base}, PointsIR{ID: "p", Positions: append([]float64(nil), base...)}, true,
			"a copied buffer encodes to the same numbers")
		assertComparison(t, PointsIR{ID: "p", Positions: base}, PointsIR{ID: "p", Positions: shifted}, false,
			"one moved point has to reach the wire")
		assertComparison(t, PointsIR{ID: "p", Positions: base}, PointsIR{ID: "p", Positions: signed}, false,
			"a slice element ships even when it is zero, so -0 against 0 is a byte change")
	})

	t.Run("PostEffectIR interface slice", func(t *testing.T) {
		assertComparison(t, []PostEffectIR{BloomIR{Threshold: 0.7}}, []PostEffectIR{VignetteIR{Intensity: 0.7}}, false,
			"two effect types write different kinds")
		assertComparison(t, []PostEffectIR{BloomIR{Strength: 0.5}}, []PostEffectIR{BloomIR{}}, true,
			"the bloom marshaler substitutes 0.5 for a zero strength, so two different structs write the same bytes")
		assertComparison(t, []PostEffectIR{BloomIR{}, FXAAIR{}}, []PostEffectIR{FXAAIR{}, BloomIR{}}, false,
			"post chain order is semantic")
		assertComparison(t, []PostEffectIR(nil), []PostEffectIR{}, false,
			"a bare nil chain encodes as null against an empty array; only a struct field with omitempty drops both")
	})
}

// TestProvenJSONEqualIsOneWay pins the fast path's contract: true means the bytes
// must match, and false means "not proven", never "different". Every pair below
// that the encoder calls equal and the fast path cannot prove is a pair the
// comparison must still call equal, which it can only do by falling back to the
// encoder.
func TestProvenJSONEqualIsOneWay(t *testing.T) {
	cases := []struct {
		name        string
		a, b        any
		wantProven  bool
		wantOnWire  bool
		explanation string
	}{
		{
			name: "identical records", a: ObjectIR{ID: "a", X: 1}, b: ObjectIR{ID: "a", X: 1},
			wantProven: true, wantOnWire: true,
			explanation: "the fast path has to settle the common case, or the diff pays the encoder for every record",
		},
		{
			name: "signed zero", a: ObjectIR{ID: "a", X: negativeZero}, b: ObjectIR{ID: "a", X: 0},
			wantProven: false, wantOnWire: true,
			explanation: "different bits, same bytes: only the encoder knows omitempty drops both",
		},
		{
			name: "NaN against itself", a: ObjectIR{ID: "a", X: math.NaN()}, b: ObjectIR{ID: "a", X: math.NaN()},
			wantProven: false, wantOnWire: false,
			explanation: "the encoder rejects it, so the fast path must not claim equality",
		},
		{
			name: "infinity against itself", a: ObjectIR{ID: "a", X: math.Inf(1)}, b: ObjectIR{ID: "a", X: math.Inf(1)},
			wantProven: false, wantOnWire: false,
			explanation: "same reason as NaN",
		},
		{
			name: "nil slice against empty slice", a: ObjectIR{ID: "a"}, b: ObjectIR{ID: "a", Points: []Vector3{}},
			wantProven: false, wantOnWire: true,
			explanation: "null against [] unless omitempty drops both, and only the encoder applies omitempty",
		},
		{
			name: "default substituted by a marshaler", a: []PostEffectIR{BloomIR{Strength: 0.5}}, b: []PostEffectIR{BloomIR{}},
			wantProven: false, wantOnWire: true,
			explanation: "the marshaler fills a default, so equal bytes come from different structs",
		},
		{
			name: "different dynamic types", a: []PostEffectIR{FXAAIR{}}, b: []PostEffectIR{VignetteIR{}},
			wantProven: false, wantOnWire: false,
			explanation: "an interface holding two different types writes different keys",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// Elem twice: past the pointer to the any, then past the any itself,
			// so the walk sees the record and not the interface box.
			proven := provenJSONEqual(reflect.ValueOf(&testCase.a).Elem().Elem(), reflect.ValueOf(&testCase.b).Elem().Elem())
			if proven != testCase.wantProven {
				t.Errorf("provenJSONEqual = %t, want %t (%s)", proven, testCase.wantProven, testCase.explanation)
			}
			onWire := referenceMarshalEqual(testCase.a, testCase.b)
			if onWire != testCase.wantOnWire {
				t.Errorf("encoder says equal = %t, want %t (%s)", onWire, testCase.wantOnWire, testCase.explanation)
			}
			if proven && !onWire {
				t.Error("the fast path claimed equality for values the encoder writes differently: a silently lost edit")
			}
		})
	}
}

// TestSceneRecordEqualAgreesWithMarshalComparison is the differential test. For
// every record type the diff compares, it generates pairs and requires the live
// comparison to give the reference comparison's answer.
//
// It runs three loops, because each one catches a different mistake:
//
//   - An exact copy, generated without a NaN or an infinity. The answer is known,
//     and the fast path must settle it without encoding. A comparison that always
//     fell back would be correct and would undo the whole speedup, so this loop
//     fails when the fast path stops paying.
//   - A pair with every field re-randomized. This is the broad sweep, and it
//     covers a NaN, an infinity, a signed zero, a nil against an empty slice or
//     map, an unset pointer, and an interface holding a different dynamic type.
//   - A pair that differs in exactly one field, once per field of the record.
//     This is the loop that catches a forgotten field: a comparison that skipped
//     field 57 of a hundred would pass the broad sweep, because a fully
//     re-randomized pair almost always differs somewhere else too.
func TestSceneRecordEqualAgreesWithMarshalComparison(t *testing.T) {
	t.Run("ObjectIR", func(t *testing.T) { assertDifferential[ObjectIR](t, 600) })
	t.Run("ModelIR", func(t *testing.T) { assertDifferential[ModelIR](t, 400) })
	t.Run("PointsIR", func(t *testing.T) { assertDifferential[PointsIR](t, 400) })
	t.Run("LightIR", func(t *testing.T) { assertDifferential[LightIR](t, 400) })
	t.Run("LabelIR", func(t *testing.T) { assertDifferential[LabelIR](t, 400) })
	t.Run("SpriteIR", func(t *testing.T) { assertDifferential[SpriteIR](t, 400) })
	t.Run("HTMLIR", func(t *testing.T) { assertDifferential[HTMLIR](t, 400) })
	t.Run("EnvironmentIR", func(t *testing.T) { assertDifferential[EnvironmentIR](t, 400) })
	t.Run("InstancedMeshIR", func(t *testing.T) { assertDifferential[InstancedMeshIR](t, 400) })
	t.Run("InstancedGLBMeshIR", func(t *testing.T) { assertDifferential[InstancedGLBMeshIR](t, 400) })
	t.Run("AnimationClipIR", func(t *testing.T) { assertDifferential[AnimationClipIR](t, 400) })
	t.Run("ComputeParticlesIR", func(t *testing.T) { assertDifferential[ComputeParticlesIR](t, 400) })
	t.Run("CompressedArray", func(t *testing.T) { assertDifferential[CompressedArray](t, 400) })
	t.Run("IRMaterial", func(t *testing.T) { assertDifferential[IRMaterial](t, 400) })
	t.Run("IRCamera", func(t *testing.T) { assertDifferential[IRCamera](t, 400) })
	t.Run("PostEffectIRSlice", func(t *testing.T) { assertDifferential[[]PostEffectIR](t, 400) })
}

// TestDiffCommandsMatchesTheReferenceAlgorithm proves the rewritten diff emits
// exactly what the original emitted, command for command and byte for byte.
//
// The comparison inside the diff changed, and the record maps now hold pointers.
// Both changes exist to remove work, and the only honest proof that no correct
// work was removed is to run the original beside it. The generated scenes cover a
// created record, a removed record, a changed record, an unchanged record, and
// every collection the diff compares as a whole.
func TestDiffCommandsMatchesTheReferenceAlgorithm(t *testing.T) {
	rng := rand.New(rand.NewPCG(0xd1ff, 0xa11ce))
	generator := &recordGenerator{rng: rng}
	ids := []string{"a", "b", "c", "d"}

	var withCommands int
	for iteration := range 400 {
		previous := randomDiffScene(generator, ids)
		next := randomDiffScene(generator, ids)
		// Carry some records over unchanged, so the unchanged path runs.
		if len(previous.Objects) > 0 && len(next.Objects) > 0 {
			next.Objects[0] = previous.Objects[0]
		}

		want, err := MarshalCommands(referenceDiffCommands(previous, next))
		if err != nil {
			t.Fatalf("iteration %d: marshal reference commands: %v", iteration, err)
		}
		got, err := MarshalCommands(DiffCommands(previous, next))
		if err != nil {
			t.Fatalf("iteration %d: marshal commands: %v", iteration, err)
		}
		if string(got) != string(want) {
			t.Fatalf("iteration %d: the diff no longer matches the original\n got %s\nwant %s", iteration, got, want)
		}
		if len(got) > 2 {
			withCommands++
		}
	}
	if withCommands == 0 {
		t.Fatal("every generated pair produced an empty command list; the test proves nothing")
	}
	t.Logf("%d of 400 generated pairs produced commands", withCommands)
}

// randomDiffScene builds a scene from a small ID pool, so two scenes share record
// identities and the diff meets a create, a remove, and a change.
func randomDiffScene(g *recordGenerator, ids []string) SceneIR {
	var ir SceneIR
	ir.Schema = SceneIRSchema
	for _, id := range ids {
		if g.rng.IntN(3) == 0 {
			continue
		}
		switch g.rng.IntN(5) {
		case 0:
			record := ObjectIR{}
			g.fill(reflect.ValueOf(&record).Elem(), 1)
			record.ID = id
			ir.Objects = append(ir.Objects, record)
		case 1:
			record := LabelIR{}
			g.fill(reflect.ValueOf(&record).Elem(), 1)
			record.ID = id
			ir.Labels = append(ir.Labels, record)
		case 2:
			record := SpriteIR{}
			g.fill(reflect.ValueOf(&record).Elem(), 1)
			record.ID = id
			ir.Sprites = append(ir.Sprites, record)
		case 3:
			record := HTMLIR{}
			g.fill(reflect.ValueOf(&record).Elem(), 1)
			record.ID = id
			ir.HTML = append(ir.HTML, record)
		default:
			record := LightIR{}
			g.fill(reflect.ValueOf(&record).Elem(), 1)
			record.ID = id
			ir.Lights = append(ir.Lights, record)
		}
	}
	g.fill(reflect.ValueOf(&ir.Environment).Elem(), 1)
	if g.rng.IntN(2) == 0 {
		g.fill(reflect.ValueOf(&ir.Models).Elem(), 1)
	}
	if g.rng.IntN(2) == 0 {
		g.fill(reflect.ValueOf(&ir.Points).Elem(), 1)
	}
	if g.rng.IntN(2) == 0 {
		g.fill(reflect.ValueOf(&ir.ComputeParticles).Elem(), 1)
	}
	if g.rng.IntN(2) == 0 {
		g.fill(reflect.ValueOf(&ir.InstancedMeshes).Elem(), 1)
	}
	if g.rng.IntN(2) == 0 {
		g.fill(reflect.ValueOf(&ir.InstancedGLBMeshes).Elem(), 1)
	}
	if g.rng.IntN(2) == 0 {
		g.fill(reflect.ValueOf(&ir.Animations).Elem(), 1)
	}
	if g.rng.IntN(2) == 0 {
		g.fill(reflect.ValueOf(&ir.PostEffects).Elem(), 1)
	}
	ir.PostFXMaxPixels = []int{0, PostFXMaxPixels720p, PostFXMaxPixels1080p}[g.rng.IntN(3)]
	return ir
}

func assertComparison[T any](t *testing.T, a, b T, wantEqual bool, why string) {
	t.Helper()
	reference := referenceMarshalEqual(a, b)
	if reference != wantEqual {
		aj, _ := json.Marshal(a)
		bj, _ := json.Marshal(b)
		t.Errorf("the encoder says equal = %t, want %t (%s)\n a = %s\n b = %s", reference, wantEqual, why, aj, bj)
	}
	live := sceneRecordJSONEqual(a, b)
	if live != wantEqual {
		t.Errorf("sceneRecordJSONEqual = %t, want %t (%s)", live, wantEqual, why)
	}
	// Symmetry is part of the contract. diffSceneRecords compares in one
	// direction only, so an asymmetric comparison would make the command list
	// depend on which scene arrived first.
	if reverse := sceneRecordJSONEqual(b, a); reverse != live {
		t.Errorf("sceneRecordJSONEqual is asymmetric: forward %t, reverse %t", live, reverse)
	}
}

// assertDifferential generates pairs of T and compares both implementations.
func assertDifferential[T any](t *testing.T, iterations int) {
	t.Helper()
	// A fixed seed keeps a failure reproducible: a regression found here
	// reappears on the next run instead of hiding until a lucky seed.
	rng := rand.New(rand.NewPCG(0x5ce4e, uint64(iterations)))
	finite := &recordGenerator{rng: rng}
	loose := &recordGenerator{rng: rng, nonFinite: true}

	var provenCopies, copies int
	for iteration := range iterations {
		var record T
		finite.fill(reflect.ValueOf(&record).Elem(), 0)
		if _, err := json.Marshal(record); err != nil {
			t.Fatalf("iteration %d: the finite generator produced a record the encoder rejects: %v", iteration, err)
		}
		copies++
		duplicate := record
		if provenJSONEqual(reflect.ValueOf(&record).Elem(), reflect.ValueOf(&duplicate).Elem()) {
			provenCopies++
		}
		if !sceneRecordJSONEqual(record, duplicate) {
			t.Fatalf("iteration %d: a record differs from its own copy", iteration)
		}
	}
	if copies == 0 {
		t.Fatal("the generator produced nothing; the test proves nothing")
	}
	if provenCopies != copies {
		t.Errorf("the fast path settled %d of %d exact copies; it must settle every one, or the diff still pays the encoder per record",
			provenCopies, copies)
	}

	var equalPairs, unequalPairs int
	report := func(kind string, index int, a, b T) {
		t.Helper()
		want := referenceMarshalEqual(a, b)
		got := sceneRecordJSONEqual(a, b)
		if want {
			equalPairs++
		} else {
			unequalPairs++
		}
		if got == want {
			return
		}
		aj, aerr := json.Marshal(a)
		bj, berr := json.Marshal(b)
		t.Errorf("%s %d: sceneRecordJSONEqual = %t, encoder says %t\n a = %s (%v)\n b = %s (%v)",
			kind, index, got, want, aj, aerr, bj, berr)
	}

	for iteration := range iterations {
		var a T
		loose.fill(reflect.ValueOf(&a).Elem(), 0)
		b := a
		loose.fill(reflect.ValueOf(&b).Elem(), 0)
		report("broad pair", iteration, a, b)
	}

	// One field at a time. Only a struct has fields to walk; a slice type gets
	// the broad sweep only.
	if reflect.TypeFor[T]().Kind() == reflect.Struct {
		fields := reflect.TypeFor[T]().NumField()
		for index := range fields {
			for attempt := range 12 {
				var a T
				finite.fill(reflect.ValueOf(&a).Elem(), 0)
				b := a
				field := reflect.ValueOf(&b).Elem().Field(index)
				if !field.CanSet() {
					break
				}
				loose.fill(field, 0)
				report("field "+reflect.TypeFor[T]().Field(index).Name+" attempt", attempt, a, b)
			}
		}
	}

	// Both answers have to appear, or a comparison that always returned one of
	// them would pass this test.
	if equalPairs == 0 {
		t.Error("no generated pair encoded equal; the equal branch went untested")
	}
	if unequalPairs == 0 {
		t.Error("no generated pair encoded unequal; the unequal branch went untested")
	}
}

// recordGenerator writes values into a record by reflection. It leans on the
// shapes a comparison gets wrong: a signed zero, a nil against an empty slice or
// map, an unset pointer, and an interface holding a different dynamic type.
type recordGenerator struct {
	rng *rand.Rand
	// nonFinite lets a NaN or an infinity appear. The encoder rejects both, so a
	// record holding one compares unequal to everything. Keep it off when the
	// test needs a record the encoder accepts.
	nonFinite bool
}

func (g *recordGenerator) fill(value reflect.Value, depth int) {
	if depth > 4 || !value.CanSet() {
		return
	}
	switch value.Kind() {
	case reflect.Bool:
		value.SetBool(g.rng.IntN(2) == 0)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value.SetInt([]int64{0, 1, -1, 7, 1 << 40}[g.rng.IntN(5)])
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		value.SetUint([]uint64{0, 1, 2, 255}[g.rng.IntN(4)])
	case reflect.Float32, reflect.Float64:
		value.SetFloat(g.float())
	case reflect.String:
		value.SetString([]string{"", "a", "b", "#0f0f0fff", "mesh-1"}[g.rng.IntN(5)])
	case reflect.Pointer:
		if g.rng.IntN(3) == 0 {
			value.SetZero()
			return
		}
		created := reflect.New(value.Type().Elem())
		g.fill(created.Elem(), depth+1)
		value.Set(created)
	case reflect.Slice:
		switch g.rng.IntN(4) {
		case 0:
			value.SetZero()
		case 1:
			value.Set(reflect.MakeSlice(value.Type(), 0, 0))
		default:
			length := 1 + g.rng.IntN(3)
			created := reflect.MakeSlice(value.Type(), length, length)
			for index := range length {
				g.fill(created.Index(index), depth+1)
			}
			value.Set(created)
		}
	case reflect.Array:
		for index := range value.Len() {
			g.fill(value.Index(index), depth+1)
		}
	case reflect.Map:
		switch g.rng.IntN(4) {
		case 0:
			value.SetZero()
		case 1:
			value.Set(reflect.MakeMap(value.Type()))
		default:
			created := reflect.MakeMap(value.Type())
			for range 1 + g.rng.IntN(2) {
				key := reflect.New(value.Type().Key()).Elem()
				g.fill(key, depth+1)
				entry := reflect.New(value.Type().Elem()).Elem()
				g.fill(entry, depth+1)
				created.SetMapIndex(key, entry)
			}
			value.Set(created)
		}
	case reflect.Struct:
		for index := range value.NumField() {
			g.fill(value.Field(index), depth+1)
		}
	case reflect.Interface:
		value.Set(g.iface(value.Type(), depth))
	default:
		value.SetZero()
	}
}

func (g *recordGenerator) float() float64 {
	if g.nonFinite {
		switch g.rng.IntN(60) {
		case 0:
			return math.NaN()
		case 1:
			return math.Inf(1)
		case 2:
			return math.Inf(-1)
		}
	}
	switch g.rng.IntN(6) {
	case 0:
		return 0
	case 1:
		return negativeZero
	case 2:
		return 1
	case 3:
		return -1
	case 4:
		return 1e-300
	default:
		return g.rng.Float64()
	}
}

// iface fills an interface field. A PostEffectIR gets one of the concrete effect
// types, with random fields, so the comparison meets the dynamic-type case the
// wire carries. Any other interface gets a plain JSON scalar or a nil.
func (g *recordGenerator) iface(target reflect.Type, depth int) reflect.Value {
	if target == reflect.TypeFor[PostEffectIR]() {
		concrete := []reflect.Type{
			reflect.TypeFor[TonemapIR](), reflect.TypeFor[BloomIR](), reflect.TypeFor[VignetteIR](),
			reflect.TypeFor[ColorGradeIR](), reflect.TypeFor[SSAOIR](), reflect.TypeFor[DOFIR](),
			reflect.TypeFor[FXAAIR](), reflect.TypeFor[CustomPostIR](),
		}
		created := reflect.New(concrete[g.rng.IntN(len(concrete))]).Elem()
		g.fill(created, depth+1)
		return created.Convert(target)
	}
	switch g.rng.IntN(5) {
	case 0:
		return reflect.Zero(target)
	case 1:
		return reflect.ValueOf(g.float())
	case 2:
		return reflect.ValueOf([]string{"", "a", "b"}[g.rng.IntN(3)])
	case 3:
		return reflect.ValueOf(g.rng.IntN(2) == 0)
	default:
		return reflect.ValueOf(map[string]any{"k": g.rng.Float64()})
	}
}
