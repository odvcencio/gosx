package route

import (
	"errors"
	"reflect"
	"testing"
)

func TestTryCallValueBuildsStructAndVariadicArgs(t *testing.T) {
	type props struct {
		Name  string
		Count int
	}

	fn := func(input props, suffix ...string) string {
		return input.Name + ":" + string(rune('0'+input.Count)) + ":" + suffix[0] + "," + suffix[1]
	}

	got, ok := tryCallValue(fn, []any{
		map[string]any{"name": "docs", "count": "2"},
		"alpha",
		"beta",
	})
	if !ok {
		t.Fatal("expected reflected call to succeed")
	}
	if got != "docs:2:alpha,beta" {
		t.Fatalf("unexpected result: %#v", got)
	}
}

func TestTryCallValueRejectsErrorResult(t *testing.T) {
	fn := func(string) (string, error) {
		return "", errors.New("boom")
	}

	got, ok := tryCallValue(fn, []any{"x"})
	if ok {
		t.Fatalf("expected reflected call to fail, got %#v", got)
	}
}

func TestIndirectValueHelpersSupportPointerCollections(t *testing.T) {
	values := []string{"zero", "one", "two"}
	if got := indexValue(&values, 1); got != "one" {
		t.Fatalf("indexValue returned %#v", got)
	}

	record := struct{ Name string }{Name: "gosx"}
	if got := selectValue(&record, "Name"); got != "gosx" {
		t.Fatalf("selectValue returned %#v", got)
	}

	lookup := map[string]int{"count": 3}
	if got, ok := mapLookup(&lookup, "count"); !ok || !reflect.DeepEqual(got, 3) {
		t.Fatalf("mapLookup returned (%#v, %v)", got, ok)
	}
}
