package host

import (
	"encoding/json"
	"reflect"
	"testing"
)

// An island decodes Recent into typed records, and a key that is missing decodes
// as the zero value. A reply whose value did not fit the byte budget therefore
// looks like a successful reply with a zero value, unless the entry says that it
// kept its value.
func TestRecentEntriesSayWhetherTheyKeptTheirValue(t *testing.T) {
	type big struct{ S string }
	f := &fakeCalls{}
	_, _ = ServeCallsOn(f, "c", CallOptions{Recent: 5, RecentBytes: 60}, func(name string, _ addArgs) (big, error) {
		switch name {
		case "boom":
			return big{}, errString("no such thing")
		case "zero":
			return big{}, nil
		}
		return big{S: "xxxxxxxxxxxxxxxxxxxx"}, nil // {"S":"xxxxxxxxxxxxxxxxxxxx"} is 28 bytes
	})
	f.send(t, `[{"ID":1,"Name":"a"},{"ID":2,"Name":"boom"},{"ID":3,"Name":"a"},{"ID":4,"Name":"zero"},{"ID":5,"Name":"a"}]`)
	writes := f.writesCopy()
	var last struct {
		Recent []struct {
			ID    uint64
			Error string
			Kept  bool
			Value json.RawMessage
		}
	}
	if err := json.Unmarshal(writes[len(writes)-1], &last); err != nil {
		t.Fatal(err)
	}
	var got [][3]any // ID, Kept, has a Value
	for _, r := range last.Recent {
		got = append(got, [3]any{r.ID, r.Kept, r.Value != nil})
	}
	// The budget (60 bytes) holds two 28-byte values, so call 1 and call 3 lost
	// theirs; the zero value {"S":""} is 8 bytes and fits next to call 5's.
	want := [][3]any{{uint64(1), false, false}, {uint64(2), false, false}, {uint64(3), false, false}, {uint64(4), true, true}, {uint64(5), true, true}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Recent (ID, Kept, has Value) = %v, want %v", got, want)
	}
	if last.Recent[1].Error == "" {
		t.Errorf("the error reply lost its error: %+v", last.Recent[1])
	}
}
