package query

import (
	"net/url"
	"reflect"
	"testing"
)

type filters struct {
	Q        string   `query:"q"`
	Page     int      `query:"page,default=1"`
	Limit    int64    `query:"limit,default=20"`
	Open     bool     `query:"open"`
	Ratio    float64  `query:"ratio,default=0.5"`
	Tags     []string `query:"tags"`
	Ignored  string   `query:"-"`
	Untagged string
}

func TestDecodeAppliesDefaultsWhenMissing(t *testing.T) {
	var f filters
	if err := Decode(url.Values{}, &f); err != nil {
		t.Fatalf("Decode empty: %v", err)
	}
	if f.Page != 1 || f.Limit != 20 || f.Ratio != 0.5 {
		t.Fatalf("defaults not applied: %+v", f)
	}
	if f.Q != "" || f.Open || f.Tags != nil {
		t.Fatalf("zero values not preserved: %+v", f)
	}
}

func TestDecodeParsesValues(t *testing.T) {
	v := url.Values{
		"q":     {"hello"},
		"page":  {"3"},
		"limit": {"100"},
		"open":  {"true"},
		"ratio": {"0.75"},
		"tags":  {"a,b", "c"}, // mix of comma + repeated
	}
	var f filters
	if err := Decode(v, &f); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	want := filters{Q: "hello", Page: 3, Limit: 100, Open: true, Ratio: 0.75, Tags: []string{"a", "b", "c"}}
	if !reflect.DeepEqual(f, want) {
		t.Fatalf("got %+v, want %+v", f, want)
	}
}

func TestDecodeBarePresenceIsTrue(t *testing.T) {
	var f filters
	if err := Decode(url.Values{"open": {""}}, &f); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !f.Open {
		t.Fatalf("bare ?open should decode to true, got %+v", f)
	}
}

func TestDecodeInvalidValueErrorsWithFieldName(t *testing.T) {
	var f filters
	err := Decode(url.Values{"page": {"abc"}}, &f)
	if err == nil {
		t.Fatal("expected error for non-numeric page")
	}
	if got := err.Error(); !contains(got, "page") {
		t.Fatalf("error should name the field 'page', got: %s", got)
	}
}

func TestDecodeRequiresPointerToStruct(t *testing.T) {
	var f filters
	if err := Decode(url.Values{}, f); err == nil {
		t.Fatal("expected error for non-pointer dst")
	}
	x := 0
	if err := Decode(url.Values{}, &x); err == nil {
		t.Fatal("expected error for pointer-to-non-struct")
	}
}

func TestEncodeOmitsDefaultsAndIgnored(t *testing.T) {
	f := filters{Q: "hi", Page: 1, Limit: 20, Ratio: 0.5, Ignored: "secret", Untagged: "nope"}
	v, err := Encode(f)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if v.Get("q") != "hi" {
		t.Fatalf("q should be present: %v", v)
	}
	for _, k := range []string{"page", "limit", "ratio", "-", "Ignored", "Untagged"} {
		if v.Has(k) {
			t.Fatalf("key %q should be omitted (default/ignored/untagged): %v", k, v)
		}
	}
}

func TestEncodeDecodeRoundTrips(t *testing.T) {
	orig := filters{Q: "search", Page: 7, Limit: 50, Open: true, Ratio: 0.25, Tags: []string{"x", "y"}}
	v, err := Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	var back filters
	if err := Decode(v, &back); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !reflect.DeepEqual(orig, back) {
		t.Fatalf("round-trip mismatch:\n  orig %+v\n  back %+v", orig, back)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
