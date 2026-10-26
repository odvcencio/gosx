package route

import (
	"go/parser"
	"reflect"
	"testing"
)

func TestMapIndexMissingReturnsTypedZero(t *testing.T) {
	type count int64
	type record struct {
		Count int
		Label string
	}
	cases := []struct {
		name   string
		target any
		want   any
	}{
		{"integer", map[string]int{}, int(0)},
		{"boolean", map[string]bool{}, false},
		{"float", map[string]float64{}, float64(0)},
		{"named", map[string]count{}, count(0)},
		{"record", map[string]record{}, record{}},
		{"pointer", map[string]*record{}, (*record)(nil)},
		{"slice", map[string][]int{}, []int(nil)},
		{"interface", map[string]any{}, nil},
		{"nil-map", map[string]int(nil), int(0)},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			got := indexValue(item.target, "missing")
			if !reflect.DeepEqual(got, item.want) {
				t.Fatalf("got %#v (%T), want %#v (%T)", got, got, item.want, item.want)
			}
		})
	}
}

type mapNilStringer struct{}

func (value *mapNilStringer) String() string {
	if value == nil {
		panic("nil String method receiver")
	}
	return "present"
}

func TestMapIndexNilZeroExpressions(t *testing.T) {
	for _, target := range []any{
		map[string]*int{}, map[string][]int{}, map[string]map[string]int{},
		map[string]chan int{}, map[string]func(){}, map[string]any{},
	} {
		for expression, want := range map[string]bool{
			`values["missing"] == nil`: true,
			`nil == values["missing"]`: true,
			`values["missing"] != nil`: false,
		} {
			expr, err := parser.ParseExpr(expression)
			if err != nil {
				t.Fatal(err)
			}
			got := lowerFileExpr(expr)(fileRenderEnv{values: map[string]any{"values": target}})
			if got != want {
				t.Errorf("%T: %s = %v, want %v", target, expression, got, want)
			}
		}
	}
	if equalValues([]int{}, nil) || equalValues(new(int), nil) {
		t.Fatal("non-nil empty slice or pointer compared equal to nil")
	}
}

func TestMapIndexNilZeroRendersEmpty(t *testing.T) {
	for _, target := range []any{
		map[string]*int{}, map[string][]int{}, map[string]map[string]int{},
		map[string]chan int{}, map[string]func(){}, map[string]*mapNilStringer{},
	} {
		if got := stringifyValue(indexValue(target, "missing")); got != "" {
			t.Errorf("%T: nil map value rendered as %q", target, got)
		}
	}
	if got := stringifyValue(&mapNilStringer{}); got != "present" {
		t.Fatalf("non-nil Stringer rendered as %q", got)
	}
}

func TestMapIndexUnhashableKeyFailsSoft(t *testing.T) {
	for _, key := range []any{[]int{1}, map[string]int{"a": 1}, func() {}, struct{ Values []int }{[]int{1}}, [1]any{[]int{1}}} {
		if got := indexValue(map[any]int{}, key); got != nil {
			t.Fatalf("invalid key returned %v", got)
		}
	}
}

func TestMapIndexPreservesPointerIdentity(t *testing.T) {
	a, b := 42, 42
	target := map[*int]string{&a: "first", &b: "second", nil: "nil"}
	for _, item := range []struct {
		key  any
		want string
	}{{&a, "first"}, {&b, "second"}, {(*int)(nil), "nil"}, {nil, "nil"}} {
		if got := indexValue(target, item.key); got != item.want {
			t.Fatalf("pointer key %v = %#v, want %q", item.key, got, item.want)
		}
	}
	if a != 42 || b != 42 {
		t.Fatal("lookup mutated pointer keys")
	}
	type pointerKey *int
	if got := indexValue(map[pointerKey]string{pointerKey(&a): "named"}, &a); got != "named" {
		t.Fatalf("converted pointer key lost identity: %#v", got)
	}
}

func TestMapIndexNilInterfaceKey(t *testing.T) {
	target := map[any]string{nil: "present"}
	if got := indexValue(target, nil); got != "present" {
		t.Fatalf("nil interface key = %#v", got)
	}
	if got := indexValue(map[int]string{0: "zero"}, nil); got != nil {
		t.Fatalf("nil coerced into integer zero: %#v", got)
	}
	if got := indexValue(map[string]string{"": "empty"}, nil); got != nil {
		t.Fatalf("nil coerced into empty string: %#v", got)
	}
	if got := indexValue(map[chan int]string{nil: "channel"}, nil); got != "channel" {
		t.Fatalf("nil channel key = %#v", got)
	}
}

func TestMapIndexRejectsIncompatibleInterfaceKey(t *testing.T) {
	if got := indexValue(map[error]int{}, 3); got != nil {
		t.Fatalf("incompatible interface key returned %#v", got)
	}
}

func TestMapIndexChecksArrayConversionLength(t *testing.T) {
	for _, target := range []any{map[[2]int]string{}, map[*[2]int]string{}} {
		if got := indexValue(target, []int{1}); got != nil {
			t.Fatalf("short slice key returned %#v", got)
		}
	}
	if got := indexValue(map[[2]int]string{{1, 2}: "array"}, []int{1, 2}); got != "array" {
		t.Fatalf("valid array key conversion = %#v", got)
	}
}

func TestMapIndexPreservesSelectorPresence(t *testing.T) {
	for _, target := range []any{map[string]int{}, map[string]float64{}} {
		if value, present := mapLookup(target, "missing"); present || value != nil {
			t.Fatalf("missing selector lookup = (%#v, %v)", value, present)
		}
	}
	if value, present := mapLookup(map[string]int{"zero": 0}, "zero"); !present || value != 0 {
		t.Fatalf("present zero selector lookup = (%#v, %v)", value, present)
	}
}

func TestIndexNilSequentialKeyStillFailsSoft(t *testing.T) {
	for _, target := range []any{[]int{42}, [1]int{42}, "first"} {
		if got := indexValue(target, nil); got != nil {
			t.Fatalf("nil sequential index returned %#v", got)
		}
	}
}
