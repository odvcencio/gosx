package vm

import (
	"errors"
	"fmt"
	"math"
	"math/rand"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// identicalValues reports whether two decoded Values are the same in every
// respect a reader can see: type, tag, numeric bits, text, and the shape and
// nil-ness of every array and object, at any depth.
func identicalValues(a, b Value) bool {
	if a.Type != b.Type || a.tag != b.tag || a.n != b.n || math.Float64bits(a.num) != math.Float64bits(b.num) {
		return false
	}
	if a.isList() {
		left, right := a.list(), b.list()
		if (left == nil) != (right == nil) || len(left) != len(right) {
			return false
		}
		for i := range left {
			if !identicalValues(left[i], right[i]) {
				return false
			}
		}
		return true
	}
	if a.isMap() {
		left, right := a.dict(), b.dict()
		if (left == nil) != (right == nil) || len(left) != len(right) {
			return false
		}
		for key, value := range left {
			other, ok := right[key]
			if !ok || !identicalValues(value, other) {
				return false
			}
		}
		return true
	}
	return a.text() == b.text()
}

// checkFastAgainstReference holds the contract of decodeJSONFast: whatever it
// accepts, the reference decoder accepts, with an identical Value. It returns
// whether the fast decoder took the text.
func checkFastAgainstReference(t *testing.T, text string) bool {
	t.Helper()
	fast, ok := decodeJSONFast(text)
	reference, err := valueFromJSONReference(text)
	if ok {
		if err != nil {
			t.Fatalf("fast decoder accepted %q, reference rejected it: %v", text, err)
		}
		if !identicalValues(fast, reference) {
			t.Fatalf("fast and reference decodes differ for %q:\nfast      %s\nreference %s", text, fast.String(), reference.String())
		}
	}
	// ValueFromJSON never changes what a caller sees: same value, same error.
	got, gotErr := ValueFromJSON(text)
	if (gotErr == nil) != (err == nil) {
		t.Fatalf("ValueFromJSON(%q) error = %v, reference = %v", text, gotErr, err)
	}
	// The reference reports whichever out-of-range number it meets first, and
	// it walks objects in map order, so its message for those varies per run.
	// Every other message is fixed and must reach the caller unchanged.
	var rangeError *strconv.NumError
	if err != nil && !errors.As(err, &rangeError) && gotErr.Error() != err.Error() {
		t.Fatalf("ValueFromJSON(%q) error = %v, reference = %v", text, gotErr, err)
	}
	if err == nil && !identicalValues(got, reference) {
		t.Fatalf("ValueFromJSON(%q) differs from the reference decode", text)
	}
	return ok
}

func TestFastJSONDecodeTakesOrdinaryDocuments(t *testing.T) {
	for _, text := range []string{
		`null`, `true`, `false`, `0`, `-0`, `7`, `-12`, `1.5`, `-0.25`, `1e3`, `1E3`, `2.50e+2`, `1e-2`, `2.0`, `100000000000000000000`,
		`9007199254740993`, `-9007199254740993`, `9223372036854775807`, `-9223372036854775808`, `9223372036854775808`,
		`0.1`, `0.30000000000000004`, `1e-400`, `123456789012345678901234567890`,
		`""`, `"x"`, `"hello world"`, `"é日本語"`, `"😀"`, `"a\"b"`, `"a\\b"`, `"a\/b"`, `"\b\f\n\r\t"`, `"\u0000"`, `"\u00e9"`, `"\u003cb\u003e"`, `"tab\there"`,
		`[]`, `[ ]`, `[1]`, `[1,2,3]`, `[ 1 , 2 ]`, `[[],[[]],[[],[]]]`, `[null,true,false,"x",1.5,{}]`,
		`{}`, `{ }`, `{"a":1}`, `{"a":1,"b":[1,2],"c":{"d":null}}`, "{\n\t\"a\" : 1 ,\r\n\"b\" : [ 1 , 2 ]\n}", `{"":1}`, `{"a":1,"a":2}`, `{"é":"日本","\u00e9":1}`,
		`{"$bv.hud":{"tick":4,"cash":{"u":5,"v":247360000},"systems":[{"id":"power","value":{"u":6,"v":0}}]}}`,
		"  \t\r\n7  ", `[1,[2,[3,[4]]]]`,
	} {
		if !checkFastAgainstReference(t, text) {
			t.Errorf("fast decoder declined ordinary text %q", text)
		}
	}
}

func TestFastJSONDecodeLeavesHardTextToTheReference(t *testing.T) {
	deep := strings.Repeat("[", 200) + strings.Repeat("]", 200)
	for name, text := range map[string]string{
		"empty":                "",
		"whitespace only":      "  ",
		"unterminated object":  `{"a":1`,
		"unterminated array":   `[1,2`,
		"unterminated string":  `"abc`,
		"trailing comma":       `[1,2,]`,
		"trailing key comma":   `{"a":1,}`,
		"missing colon":        `{"a" 1}`,
		"single quotes":        `{'a':1}`,
		"bare word":            `nul`,
		"bad literal tail":     `truex`,
		"two values":           `1 2`,
		"trailing garbage":     `{"a":1} x`,
		"leading zero":         `01`,
		"plus sign":            `+1`,
		"dot only":             `.5`,
		"trailing dot":         `1.`,
		"bare exponent":        `1e`,
		"lone minus":           `-`,
		"NaN":                  `NaN`,
		"infinity":             `Infinity`,
		"range overflow":       `1e400`,
		"negative overflow":    `-1e400`,
		"byte order mark":      "\ufeff{}",
		"raw newline":          "\"a\nb\"",
		"raw tab":              "\"a\tb\"",
		"bad escape":           `"\x41"`,
		"short unicode escape": `"\u12"`,
		"bad unicode escape":   `"\u12zz"`,
		"surrogate pair":       `"\ud83d\ude00"`,
		"lone high surrogate":  `"\ud83d"`,
		"lone low surrogate":   `"\ude00"`,
		"bad utf-8 in string":  "\"\xff\"",
		"bad utf-8 after esc":  "\"\\n\xc3\"",
		"bad utf-8 in key":     "{\"\xff\":1}",
		"too deep":             deep,
		"comment":              `{"a":1}//x`,
		"object as key":        `{{}:1}`,
		"array of colons":      `[:]`,
	} {
		t.Run(name, func(t *testing.T) {
			if checkFastAgainstReference(t, text) {
				t.Fatalf("fast decoder took %q", text)
			}
		})
	}
}

func TestFastJSONDecodeDepthLimit(t *testing.T) {
	for _, depth := range []int{1, 10, fastDecodeMaxDepth, fastDecodeMaxDepth + 1, fastDecodeMaxDepth + 2, 1000} {
		text := strings.Repeat("[", depth) + strings.Repeat("]", depth)
		took := checkFastAgainstReference(t, text)
		if took != (depth <= fastDecodeMaxDepth+1) {
			t.Errorf("depth %d: fast decoder took = %v", depth, took)
		}
	}
}

func TestFastJSONDecodeDoesNotAliasItsInput(t *testing.T) {
	text := []byte(`{"name":"voyage","tags":["a","bc"]}`)
	value, ok := decodeJSONFast(string(text))
	if !ok {
		t.Fatal("declined")
	}
	for i := range text {
		text[i] = 'X'
	}
	if got := value.Map()["name"].Text(); got != "voyage" {
		t.Fatalf("string value changed with its input: %q", got)
	}
	if _, ok := value.Map()["name"]; !ok {
		t.Fatal("key lost")
	}
}

// jsonGenerator writes random JSON text.
type jsonGenerator struct {
	rng     *rand.Rand
	exotic  bool // surrogate escapes, bad UTF-8, out-of-range numbers: text the fast decoder must decline
	repeats bool // objects may repeat a name
}

func (g *jsonGenerator) space() string {
	if g.rng.Intn(4) != 0 {
		return ""
	}
	return []string{" ", "\n", "\t", "\r\n", "  "}[g.rng.Intn(5)]
}

func (g *jsonGenerator) number() string {
	switch g.rng.Intn(12) {
	case 0:
		return "0"
	case 1:
		return "-0"
	case 2:
		return fmt.Sprint(g.rng.Int63n(1000) - 500)
	case 3:
		return fmt.Sprint(g.rng.Int63() - g.rng.Int63())
	case 4:
		return fmt.Sprintf("%d.%d", g.rng.Intn(100), g.rng.Intn(1000))
	case 5:
		return fmt.Sprintf("-%d.%03d", g.rng.Intn(100), g.rng.Intn(1000))
	case 6:
		return fmt.Sprintf("%de%d", g.rng.Intn(100), g.rng.Intn(30)-10)
	case 7:
		return fmt.Sprintf("%d.%dE+%d", g.rng.Intn(10), g.rng.Intn(10), g.rng.Intn(25))
	case 8:
		return fmt.Sprintf("%d.0", g.rng.Intn(1000))
	case 9:
		return fmt.Sprint(g.rng.Uint64())
	case 10:
		if g.exotic {
			return []string{"1e400", "-1e999", "123456789012345678901234567890e300"}[g.rng.Intn(3)]
		}
		return fmt.Sprintf("%d.%d", g.rng.Intn(10), g.rng.Intn(100000000))
	default:
		return fmt.Sprintf("%.17g", g.rng.NormFloat64()*math.Pow(10, float64(g.rng.Intn(20)-10)))
	}
}

func (g *jsonGenerator) str() string {
	var b strings.Builder
	b.WriteByte('"')
	for n := g.rng.Intn(12); n > 0; n-- {
		switch g.rng.Intn(14) {
		case 0:
			b.WriteString(`\"`)
		case 1:
			b.WriteString(`\\`)
		case 2:
			b.WriteString(`\n`)
		case 3:
			b.WriteString(`\u003c`)
		case 4:
			b.WriteString(`\u00e9`)
		case 5:
			b.WriteString(`\/`)
		case 6:
			b.WriteString("é日本")
		case 7:
			b.WriteString("😀")
		case 8:
			b.WriteString(`\u0001`)
		case 9:
			if g.exotic {
				b.WriteString([]string{`\ud83d\ude00`, `\ud83d`, `\ude00`, "\xff", "\xc3"}[g.rng.Intn(5)])
			} else {
				b.WriteString("pq")
			}
		default:
			b.WriteByte(byte('a' + g.rng.Intn(26)))
		}
	}
	b.WriteByte('"')
	return b.String()
}

func (g *jsonGenerator) value(depth int) string {
	choices := 9
	if depth >= 4 {
		choices = 6
	}
	switch g.rng.Intn(choices) {
	case 0:
		return "null"
	case 1:
		return "true"
	case 2:
		return "false"
	case 3, 4:
		return g.number()
	case 5:
		return g.str()
	case 6, 7:
		var b strings.Builder
		b.WriteString("[" + g.space())
		for i, n := 0, g.rng.Intn(5); i < n; i++ {
			if i > 0 {
				b.WriteString("," + g.space())
			}
			b.WriteString(g.value(depth+1) + g.space())
		}
		b.WriteString("]")
		return b.String()
	default:
		var b strings.Builder
		b.WriteString("{" + g.space())
		seen, written := map[string]bool{}, 0
		for i, n := 0, g.rng.Intn(5); i < n; i++ {
			key := g.str()
			if g.rng.Intn(6) == 0 {
				key = `"k` + fmt.Sprint(g.rng.Intn(3)) + `"`
			}
			// A repeated name is legal JSON; g.repeats asks for it on purpose.
			if name, err := valueFromJSONReference(key); err == nil && !g.repeats {
				if seen[name.Text()] {
					continue
				}
				seen[name.Text()] = true
			}
			if written > 0 {
				b.WriteString("," + g.space())
			}
			written++
			b.WriteString(key + g.space() + ":" + g.space() + g.value(depth+1) + g.space())
		}
		b.WriteString("}")
		return b.String()
	}
}

// damageBytes are the bytes a random edit puts into a valid document.
const damageBytes = "{}[],:\"\\ 0-e.tfn\x00\xff"

func TestFastJSONDecodeMatchesReferenceOnRandomText(t *testing.T) {
	rng := rand.New(rand.NewSource(20261028))
	taken, total := 0, 0
	for _, exotic := range []bool{false, true} {
		g := &jsonGenerator{rng: rng, exotic: exotic, repeats: true}
		for i := 0; i < 6000; i++ {
			text := g.space() + g.value(0) + g.space()
			total++
			if checkFastAgainstReference(t, text) {
				taken++
			} else if !exotic {
				t.Fatalf("fast decoder declined ordinary text %q", text)
			}
			// A damaged copy must never turn into something the fast decoder
			// takes and the reference does not.
			for k := 0; k < 3 && len(text) > 0; k++ {
				damaged := []byte(text)
				at := rng.Intn(len(damaged))
				switch rng.Intn(3) {
				case 0:
					damaged = append(damaged[:at], damaged[at+1:]...)
				case 1:
					damaged[at] = damageBytes[rng.Intn(len(damageBytes))]
				default:
					damaged = append(damaged[:at], append([]byte{damageBytes[rng.Intn(len(damageBytes))]}, damaged[at:]...)...)
				}
				checkFastAgainstReference(t, string(damaged))
			}
		}
	}
	if taken < total/2 {
		t.Fatalf("fast decoder took only %d of %d documents", taken, total)
	}
}

func FuzzValueFromJSONFastMatchesReference(f *testing.F) {
	for _, seed := range []string{`{"a":[1,2,{"b":null}],"c":"d\u00e9"}`, `[1e3,-0,0.5,"x"]`, `"\ud83d\ude00"`, `{"a":1,"a":2}`, `[`, `{"a":}`, "\"\xff\"", `1e400`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		if !utf8.ValidString(text) && len(text) > 1<<16 {
			t.Skip()
		}
		checkFastAgainstReference(t, text)
	})
}

func TestNumberValueFromTokenMatchesTheDecoder(t *testing.T) {
	for _, token := range []string{"0", "-0", "7", "-7", "123456789012345678", "9007199254740993", "9223372036854775807", "-9223372036854775808",
		"9223372036854775808", "1.5", "-1.5", "1e3", "1E-3", "2.0", "1e30", "0.1", "1e-400", "123456789012345678901234567890"} {
		got, ok := numberValueFromToken(token)
		want, err := valueFromJSONReference(token)
		if err != nil || !ok || !identicalValues(got, want) {
			t.Errorf("%s: got %s ok=%v, want %s err=%v", token, got.String(), ok, want.String(), err)
		}
	}
	if _, ok := numberValueFromToken("1e400"); ok {
		t.Error("out-of-range token reported ok")
	}
}
