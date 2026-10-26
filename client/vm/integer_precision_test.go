package vm

import (
	"encoding/json"
	"math"
	"math/rand"
	"strconv"
	"testing"

	"m31labs.dev/gosx/island/program"
	"m31labs.dev/gosx/signal"
)

func exactInteger(t *testing.T, n int64) Value {
	t.Helper()
	return parseJSONValue(json.RawMessage(strconv.FormatInt(n, 10)), program.TypeInt)
}

func TestIntegerPrecisionHydration(t *testing.T) {
	for _, n := range []int64{1<<31 + 1, 1<<53 - 1, 1 << 53, 1<<53 + 1, -(1<<53 + 1), math.MaxInt64, math.MinInt64} {
		text := strconv.FormatInt(n, 10)
		t.Run(text, func(t *testing.T) {
			raw := `{"ID":` + text + `,"Nested":{"IDs":[` + text + `]}}`
			props := parseProps(&program.Program{Props: []program.PropDef{{Name: "ID", Type: program.TypeInt}}}, raw)
			for name, value := range map[string]Value{
				"typed":  props["ID"],
				"root":   props["props"].Map()["ID"],
				"nested": props["Nested"].Map()["IDs"].List()[0],
			} {
				if got := value.String(); got != text {
					t.Errorf("%s = %s, want %s", name, got, text)
				}
			}
			value, err := ValueFromJSON(raw)
			if err != nil {
				t.Fatal(err)
			}
			roundtrip, err := json.Marshal(value.ToAny())
			if err != nil {
				t.Fatal(err)
			}
			var decoded struct {
				ID     int64
				Nested struct{ IDs []int64 }
			}
			if err := json.Unmarshal(roundtrip, &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded.ID != n || len(decoded.Nested.IDs) != 1 || decoded.Nested.IDs[0] != n {
				t.Errorf("roundtrip = %s, want exact %s", roundtrip, text)
			}
			p := &program.Program{Name: "ExactID", Exprs: []program.Expr{{Op: program.OpPropGet, Value: "ID", Type: program.TypeAny}}, Nodes: []program.Node{{Kind: program.NodeExpr, Expr: 0}}, Root: 0}
			island := NewIsland(p, raw)
			defer island.Dispose()
			if got := island.CurrentTree().Nodes[0].Text; got != text {
				t.Errorf("render = %s, want %s", got, text)
			}
		})
	}
}

func TestIntegerPrecisionArithmetic(t *testing.T) {
	pairs := [][2]int64{{1 << 53, 1}, {1<<53 + 1, 3}, {-(1<<53 + 1), 3}, {math.MaxInt64, 1}, {math.MinInt64, -1}, {math.MaxInt64, math.MinInt64}, {math.MinInt64, 0}}
	for _, pair := range pairs {
		a, b := pair[0], pair[1]
		av, bv := exactInteger(t, a), exactInteger(t, b)
		div, mod := int64(0), int64(0)
		if b != 0 {
			div, mod = a/b, a%b
		}
		for op, item := range map[string]struct {
			value Value
			want  int64
		}{
			"add": {av.Add(bv), a + b}, "sub": {av.Sub(bv), a - b}, "mul": {av.Mul(bv), a * b},
			"div": {av.Div(bv), div}, "mod": {av.Mod(bv), mod}, "neg": {av.Neg(), -a},
		} {
			if got, want := item.value.String(), strconv.FormatInt(item.want, 10); got != want {
				t.Errorf("%d %s %d = %s, want %s", a, op, b, got, want)
			}
		}
		for op, item := range map[string]struct {
			value Value
			want  bool
		}{
			"eq": {av.Eq(bv), a == b}, "lt": {av.Lt(bv), a < b}, "gt": {av.Gt(bv), a > b}, "lte": {av.Lte(bv), a <= b}, "gte": {av.Gte(bv), a >= b},
		} {
			if item.value.Truth() != item.want {
				t.Errorf("%d %s %d = %v, want %v", a, op, b, item.value.Truth(), item.want)
			}
		}
	}
	a, b := exactInteger(t, 1<<53), exactInteger(t, 1<<53+1)
	if a.Eq(b).Truth() || !a.Lt(b).Truth() {
		t.Fatal("adjacent integers above float precision compare equal")
	}
}

func TestIntegerPrecisionLiteralsAndReconcile(t *testing.T) {
	p := &program.Program{Exprs: []program.Expr{{Op: program.OpLitInt, Value: "9007199254740993", Type: program.TypeInt}}}
	machine := NewVM(p, nil)
	if got := machine.Eval(0).String(); got != "9007199254740993" {
		t.Errorf("cached literal = %s", got)
	}
	if got := machine.parseIntLiteral(&p.Exprs[0]).String(); got != "9007199254740993" {
		t.Errorf("uncached literal = %s", got)
	}
	island := NewIsland(program.CounterProgram(), `{}`)
	defer island.Dispose()
	state := signal.New(exactInteger(t, 1<<53))
	island.SetSharedSignal("count", state)
	island.Reconcile()
	state.Set(exactInteger(t, 1<<53+1))
	island.Reconcile()
	if got := counterDisplayText(island.CurrentTree()); got != "9007199254740993" {
		t.Fatalf("reconcile reused rounded value: %s", got)
	}
}

func TestIntegerPrecisionIndexDoesNotWrap(t *testing.T) {
	for _, n := range []int64{1 << 32, 1 << 53, math.MaxInt64, math.MinInt64} {
		index := exactInteger(t, n)
		if got := ArrayVal([]Value{StringVal("secret")}).IndexVal(index).String(); got == "secret" {
			t.Errorf("index %d wrapped into array", n)
		}
		if got := StringVal("secret").IndexVal(index).String(); got != "" {
			t.Errorf("index %d wrapped into string: %s", n, got)
		}
	}
}

func TestIntegerPrecisionValueSeams(t *testing.T) {
	seed := uint64(1)
	for i := 0; i < 1024; i++ {
		seed = seed*6364136223846793005 + 1442695040888963407
		n := int64(seed)
		value := Int64Val(n)
		if value.Int64() != n || value.String() != strconv.FormatInt(n, 10) {
			t.Fatalf("integer did not survive: %d => %s", n, value.String())
		}
		if !value.WithControl(ControlReturn).Truth() || value.WithControl(ControlReturn).Int64() != n {
			t.Fatal("control flags damaged integer")
		}
		if got := ValueFromAny(n).String(); got != value.String() {
			t.Fatalf("Go value rounded: %s", got)
		}
		if value.ToFloatVal().Number() != float64(n) {
			t.Fatal("float conversion used partial payload")
		}
	}
	value := Int64Val(1<<53 + 1)
	if !value.Add(FloatVal(0.5)).Eq(FloatVal(float64(int64(1<<53+1)) + 0.5)).Truth() {
		t.Fatal("mixed float arithmetic used partial payload")
	}
	if !ArrayVal([]Value{value}).ContainsVal(value).Truth() || ArrayVal([]Value{value}).ContainsVal(Int64Val(1<<53)).Truth() {
		t.Fatal("collection equality rounded integers")
	}
	key := ObjectVal(map[string]Value{"9007199254740993": StringVal("found")})
	if got := key.IndexVal(value).Text(); got != "found" {
		t.Fatalf("integer map key = %s", got)
	}
	atoi, _ := LookupIntrinsic("strconv.Atoi")
	itoa, _ := LookupIntrinsic("strconv.Itoa")
	parsed, err := atoi([]Value{StringVal("9223372036854775807")})
	if err != nil {
		t.Fatal(err)
	}
	formatted, err := itoa([]Value{parsed})
	if err != nil || formatted.Text() != "9223372036854775807" {
		t.Fatalf("strconv roundtrip: %s, %v", formatted.Text(), err)
	}
	if got := StringVal("9007199254740993").ToIntVal().Int64(); got != 1<<53+1 {
		t.Fatalf("int(string) = %d", got)
	}
	if allocations := testing.AllocsPerRun(100, func() {
		n := value.Add(Int64Val(3)).Sub(Int64Val(2)).Mul(Int64Val(2)).Div(Int64Val(2)).Int64()
		if n != 1<<53+2 {
			panic("wrong integer")
		}
	}); allocations != 0 {
		t.Fatalf("integer operations allocate: %v", allocations)
	}
}

func TestIntegerPrecisionJSONValidationAndFloatEvents(t *testing.T) {
	for _, raw := range []string{`1 2`, `1e400`, `{"nested":[1e400]}`, `[1,]`} {
		if _, err := ValueFromJSON(raw); err == nil {
			t.Errorf("accepted invalid JSON number/document %s", raw)
		}
	}
	event := parseEventData(`{"ID":9007199254740993,"timeStamp":3000000000,"clientX":13}`)
	if event["ID"].String() != "9007199254740993" {
		t.Fatal("event integer rounded")
	}
	if event["timeStamp"].Type != program.TypeFloat || event["timeStamp"].Number() != 3e9 || event["clientX"].Div(IntVal(2)).Number() != 6.5 {
		t.Fatal("event float contract changed")
	}
}

func TestIntegerPrecisionRandomIntrinsics(t *testing.T) {
	seed, _ := LookupIntrinsic("rand.Seed")
	next, _ := LookupIntrinsic("rand.Intn")
	const n int64 = 9007199254740993
	if _, err := seed([]Value{Int64Val(n)}); err != nil {
		t.Fatal(err)
	}
	got, err := next([]Value{Int64Val(n)})
	if err != nil {
		t.Fatal(err)
	}
	want := rand.New(rand.NewSource(n)).Int63n(n)
	if got.Int64() != want {
		t.Fatalf("wide random seed/bound = %d, want %d", got.Int64(), want)
	}
}

func TestIntegerPrecisionReuseKeepsFloatComparison(t *testing.T) {
	negativeZero := math.Copysign(0, -1)
	if sampleDiffers(sampleValue(FloatVal(negativeZero), true), sampleValue(FloatVal(0), true)) {
		t.Fatal("float zero signs must compare equal")
	}
	nan := sampleValue(FloatVal(math.NaN()), true)
	if !sampleDiffers(nan, nan) {
		t.Fatal("NaN must invalidate reuse")
	}
}
