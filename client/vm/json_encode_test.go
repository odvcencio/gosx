package vm

import (
	"encoding/json"
	"math"
	"math/rand"
	"strings"
	"testing"
	"unicode/utf8"

	"m31labs.dev/gosx/island/program"
)

// reference is what AppendJSON must write: the text json.Marshal gives for the
// plain Go values ToAny builds.
func reference(v Value) (string, error) {
	data, err := json.Marshal(v.ToAny())
	return string(data), err
}

// checkEncoded holds the contract of AppendJSON: the same text, byte for byte,
// and an error exactly where json.Marshal returns one.
func checkEncoded(t *testing.T, v Value, label string) {
	t.Helper()

	want, wantErr := reference(v)
	got, err := AppendJSON(nil, v)
	if (err != nil) != (wantErr != nil) {
		t.Fatalf("%s: AppendJSON error = %v, json.Marshal error = %v", label, err, wantErr)
	}
	if wantErr == nil && string(got) != want {
		t.Fatalf("%s: AppendJSON wrote\n%s\njson.Marshal wrote\n%s", label, got, want)
	}
	// Appending keeps what is already in the buffer.
	if wantErr == nil {
		prefixed, err := AppendJSON([]byte("prefix:"), v)
		if err != nil || string(prefixed) != "prefix:"+want {
			t.Fatalf("%s: appended to a prefix wrote %q", label, prefixed)
		}
	}
}

func TestAppendJSONMatchesMarshalOnScalars(t *testing.T) {
	floats := []float64{
		0, math.Copysign(0, -1), 1, -1, 0.1, -0.1, 1.5, 100, 1e6, 1e20, 1e21, 1.5e21, 123456789.123456789, 1e-5, 1e-6, 9.99e-7, 1e-7, 1.5e-7, 5e-324,
		math.MaxFloat64, -math.MaxFloat64, math.SmallestNonzeroFloat64, 0.30000000000000004, 2.5e-10, 1e300, 123456789012345680000, 12345678901234567890123,
		math.Pi, -math.E, 1 << 53, 1<<53 + 2, 4.35, 0.000001, 0.0000001234,
	}
	for _, f := range floats {
		checkEncoded(t, FloatVal(f), "float "+jsonOf(f))
	}
	for _, n := range []int64{0, 1, -1, 42, math.MaxInt32, math.MinInt32, math.MaxInt32 + 1, math.MaxInt64, math.MinInt64, 9007199254740993} {
		checkEncoded(t, Int64Val(n), "int "+jsonOf(n))
	}
	checkEncoded(t, IntVal(7), "IntVal 7")
	checkEncoded(t, BoolVal(true), "true")
	checkEncoded(t, BoolVal(false), "false")
	checkEncoded(t, ZeroValue(program.TypeAny), "null")
	checkEncoded(t, ZeroValue(program.TypeString), "zero string")
	checkEncoded(t, ZeroValue(program.TypeInt), "zero int")
	checkEncoded(t, ZeroValue(program.TypeFloat), "zero float")
	checkEncoded(t, ZeroValue(program.TypeBool), "zero bool")
}

func jsonOf(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}

func TestAppendJSONMatchesMarshalOnStrings(t *testing.T) {
	for _, s := range []string{
		"", "plain", "with space", `quote " and backslash \`, "tab\tnewline\ncarriage\rbackspace\bformfeed\f", "\x00\x01\x1f\x7f",
		"<script>alert('x') && 1 > 0</script>", "é日本語😀", "line\u2028separator\u2029paragraph", "\u2027\u202a",
		"bad \xff utf8", "\xc3", "\xe2\x82", "\xf0\x9f\x98", "trail\xc3(", "\xed\xa0\x80", "\xf4\x90\x80\x80", "mixed é \xff 日 \xc3 <&>",
		strings.Repeat("a", 1000), strings.Repeat("<", 100), "\"\\/",
	} {
		checkEncoded(t, StringVal(s), "string "+jsonOf(s))
		want, _ := json.Marshal(s)
		if got := string(AppendJSONString(nil, s)); got != string(want) {
			t.Errorf("AppendJSONString(%q) = %s, json.Marshal gives %s", s, got, want)
		}
	}
	// Every code point below 0x300, then a sample above, one at a time and inside a longer string.
	for r := rune(0); r < 0x300; r++ {
		s := string(r)
		checkEncoded(t, StringVal("a"+s+"b"), "rune "+jsonOf(s))
	}
	for _, r := range []rune{0x7ff, 0x800, 0xd7ff, 0xe000, 0xfffd, 0xffff, 0x10000, 0x1f600, 0x10ffff} {
		checkEncoded(t, StringVal("a"+string(r)+"b"), "rune "+jsonOf(string(r)))
	}
}

func TestAppendJSONMatchesMarshalOnContainers(t *testing.T) {
	checkEncoded(t, ArrayVal([]Value{}), "empty array")
	checkEncoded(t, ArrayVal(nil), "nil array payload")
	checkEncoded(t, ObjectVal(map[string]Value{}), "empty object")
	checkEncoded(t, ObjectVal(nil), "nil object payload")
	checkEncoded(t, ArrayVal([]Value{IntVal(1), StringVal("a"), ZeroValue(program.TypeAny), BoolVal(true), FloatVal(1.5), ArrayVal(nil), ObjectVal(nil)}), "mixed array")
	checkEncoded(t, ObjectVal(map[string]Value{
		"b": IntVal(2), "a": IntVal(1), "": IntVal(0), "é": IntVal(3), "<tag>": IntVal(4), "A": IntVal(5), "a b": IntVal(6), "\x00": IntVal(7), "日本": IntVal(8),
		"\"quoted\"": StringVal("v"), "bad\xffkey": IntVal(9), "line\u2028": IntVal(10), "k10": IntVal(11), "k2": IntVal(12), "k1": IntVal(13),
	}), "object with keys in every class")
	// More members than the encoder's first buffer holds.
	many := map[string]Value{}
	for i := 0; i < 100; i++ {
		many[strings.Repeat("k", i%7)+string(rune('a'+i%26))+jsonOf(i)] = IntVal(i)
	}
	checkEncoded(t, ObjectVal(many), "100 members")
	checkEncoded(t, decodeFor(t, `{"$bv.hud":{"tick":4,"cash":{"u":5,"v":247360000},"systems":[{"id":"power","value":{"u":6,"v":0},"gauge":{"name":"x","scope":1}}]},"$a":[[],[[]],{}]}`), "view-shaped")
}

func decodeFor(t *testing.T, text string) Value {
	t.Helper()

	v, err := valueFromJSONReference(text)
	if err != nil {
		t.Fatalf("%q: %v", text, err)
	}
	return v
}

func TestAppendJSONRefusesWhatMarshalRefuses(t *testing.T) {
	for _, f := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		for name, v := range map[string]Value{
			"bare":    FloatVal(f),
			"array":   ArrayVal([]Value{IntVal(1), FloatVal(f)}),
			"object":  ObjectVal(map[string]Value{"a": IntVal(1), "z": ObjectVal(map[string]Value{"deep": FloatVal(f)})}),
			"nested0": ArrayVal([]Value{ArrayVal([]Value{ArrayVal([]Value{FloatVal(f)})})}),
		} {
			if _, err := AppendJSON(nil, v); err == nil {
				t.Errorf("%s %v: no error", name, f)
			}
			if _, err := reference(v); err == nil {
				t.Fatalf("%s %v: json.Marshal accepted it, so this test no longer pins its behavior", name, f)
			}
		}
	}
}

func TestAppendJSONStandsInForWhatNestsTooDeep(t *testing.T) {
	// A Value can contain itself; ToAny cuts the recursion off with a string, and so must AppendJSON.
	loop := ObjectVal(map[string]Value{})
	loop.Map()["self"] = loop
	loop.Map()["n"] = IntVal(1)
	checkEncoded(t, loop, "object that contains itself")
	ring := ArrayVal([]Value{IntVal(1), IntVal(2)})
	ring.List()[1] = ring
	checkEncoded(t, ring, "array that contains itself")
	// A tree exactly at and just past the depth limit.
	for _, depth := range []int{maxValueRecursionDepth - 1, maxValueRecursionDepth, maxValueRecursionDepth + 1, maxValueRecursionDepth + 5} {
		v := StringVal("leaf")
		for i := 0; i < depth; i++ {
			v = ArrayVal([]Value{v})
		}
		checkEncoded(t, v, "array nested "+jsonOf(depth)+" deep")
		w := IntVal(5)
		for i := 0; i < depth; i++ {
			w = ObjectVal(map[string]Value{"k": w})
		}
		checkEncoded(t, w, "object nested "+jsonOf(depth)+" deep")
	}
}

func TestAppendJSONMatchesMarshalOnDecodedDocuments(t *testing.T) {
	rng := rand.New(rand.NewSource(20261029))
	for _, exotic := range []bool{false, true} {
		g := &jsonGenerator{rng: rng, exotic: exotic, repeats: true}
		for i := 0; i < 4000; i++ {
			text := g.space() + g.value(0) + g.space()
			v, err := valueFromJSONReference(text)
			if err != nil {
				continue // exotic text the reference rejects
			}
			checkEncoded(t, v, text)
			// And what comes back from the encoder decodes to the same value.
			encoded, err := AppendJSON(nil, v)
			if err != nil {
				t.Fatalf("%q: %v", text, err)
			}
			again, err := ValueFromJSON(string(encoded))
			if err != nil || !EqualJSON(v, again) {
				t.Fatalf("%q encodes to %s, which decodes to %v (%v)", text, encoded, again.String(), err)
			}
		}
	}
}

// randomValue builds a Value directly, with floats from random bit patterns,
// strings of random bytes and ints across the whole int64 range.
func randomValue(rng *rand.Rand, depth int) Value {
	choices := 9
	if depth >= 5 {
		choices = 6
	}
	switch rng.Intn(choices) {
	case 0:
		return ZeroValue(program.TypeAny)
	case 1:
		return BoolVal(rng.Intn(2) == 0)
	case 2:
		return Int64Val(int64(rng.Uint64()) >> uint(rng.Intn(64)))
	case 3:
		return FloatVal(math.Float64frombits(rng.Uint64()))
	case 4:
		return FloatVal((rng.Float64() - 0.5) * math.Pow(10, float64(rng.Intn(60)-30)))
	case 5:
		b := make([]byte, rng.Intn(12))
		for i := range b {
			switch rng.Intn(5) {
			case 0:
				b[i] = byte(rng.Intn(256))
			case 1:
				b[i] = "<>&\"\\/"[rng.Intn(6)]
			case 2:
				b[i] = byte(rng.Intn(32))
			default:
				b[i] = byte('a' + rng.Intn(26))
			}
		}
		s := string(b)
		if rng.Intn(4) == 0 {
			s += string([]rune{rune(0x2028 + rng.Intn(2)), rune(rng.Intn(0x110000))})
		}
		return StringVal(s)
	case 6, 7:
		items := make([]Value, rng.Intn(5))
		for i := range items {
			items[i] = randomValue(rng, depth+1)
		}
		return ArrayVal(items)
	default:
		fields := map[string]Value{}
		for i, n := 0, rng.Intn(6); i < n; i++ {
			fields[randomValue(rng, 6).String()] = randomValue(rng, depth+1)
		}
		return ObjectVal(fields)
	}
}

func TestAppendJSONMatchesMarshalOnRandomValues(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	errors := 0
	for i := 0; i < 20000; i++ {
		v := randomValue(rng, 0)
		if _, err := reference(v); err != nil {
			errors++
		}
		checkEncoded(t, v, "random value "+jsonOf(i))
	}
	if errors == 0 || errors > 5000 {
		t.Fatalf("%d of the random values hold a float that is not finite; the corpus should hold some but not most", errors)
	}
}

func FuzzAppendJSONMatchesMarshal(f *testing.F) {
	for _, seed := range []string{`{"a":[1,2.5,"x\u00e9<"],"b":{"c":null,"d":true}}`, `[1e21,1e-7,-0,123456789.123456789]`, `"\u2028\ud83d\ude00"`, `{"":{"":[]}}`, `0.1`} {
		f.Add(seed, uint64(0), "")
	}
	f.Add("[]", math.Float64bits(math.NaN()), "\xff<&>")
	f.Add("[]", math.Float64bits(1e21), "line\u2028")
	f.Fuzz(func(t *testing.T, text string, bits uint64, s string) {
		var parts []Value
		if v, err := valueFromJSONReference(text); err == nil {
			parts = append(parts, v)
		}
		parts = append(parts, FloatVal(math.Float64frombits(bits)), StringVal(s), ObjectVal(map[string]Value{s: Int64Val(int64(bits))}))
		if !utf8.ValidString(s) && len(s) > 1<<12 {
			t.Skip()
		}
		for _, v := range parts {
			checkEncoded(t, v, "fuzz")
		}
		checkEncoded(t, ArrayVal(parts), "fuzz array")
	})
}
