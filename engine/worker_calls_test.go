package engine

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"
)

func jsonKeys(t *testing.T, v any) []string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Island records are keyed by Go field name, so the wire keys of the call,
// result and status types must stay the field names. Adding a json tag here
// would silently break every island that reads them.
func TestWorkerWireKeysAreGoFieldNames(t *testing.T) {
	cases := []struct {
		name string
		v    any
		want []string
	}{
		{"WorkerStatus", WorkerStatus{}, []string{"Reason", "State", "Thread"}},
		{"Call", Call[struct{ X int }]{}, []string{"Args", "ID", "Name"}},
		{"Result", Result[struct{ X int }]{}, []string{"Error", "ID", "Name", "Recent", "Seen", "Value"}},
		{"ResultRef", ResultRef{}, []string{"Error", "ID", "Kept"}},
		{"ResultRef with a value", ResultRef{Kept: true, Value: json.RawMessage(`1`)}, []string{"Error", "ID", "Kept", "Value"}},
	}
	for _, c := range cases {
		if got := jsonKeys(t, c.v); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s wire keys = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestValidateWorkerMode(t *testing.T) {
	if err := ValidateWorkerMode(WorkerRequired, false, ThreadWorker); err != nil {
		t.Fatalf("required on a worker engine: %v", err)
	}
	if err := ValidateWorkerMode(WorkerRequired, false, ThreadMain); err == nil {
		t.Fatal("required on a main-thread engine must be rejected")
	}
	if err := ValidateWorkerMode(WorkerPreferred, true, ThreadMain); err == nil {
		t.Fatal("force fallback on a main-thread engine must be rejected")
	}
	if err := ValidateWorkerMode("sometimes", false, ThreadWorker); err == nil {
		t.Fatal("unknown mode must be rejected")
	}
}
