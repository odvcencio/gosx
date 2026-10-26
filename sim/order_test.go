package sim

import (
	"reflect"
	"testing"
)

func TestSortedPlayerIDsAndEachInput(t *testing.T) {
	in := map[string]Input{"b": {}, "c": {}, "a": {}}
	if got := SortedPlayerIDs(in); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatalf("got %v", got)
	}
	var seen []string
	EachInput(in, func(id string, _ Input) { seen = append(seen, id) })
	if !reflect.DeepEqual(seen, []string{"a", "b", "c"}) {
		t.Fatalf("got %v", seen)
	}
	if len(SortedPlayerIDs(nil)) != 0 {
		t.Fatal("nil map should give empty slice")
	}
}
