package httpcache

import "testing"

// RFC 9111 §5.2 and RFC 9110 §§5.6.1.2, 5.6.2, 5.6.4: quoted
// commas/pairs belong to arguments, empty members are ignored, and malformed
// arguments cannot turn a restrictive field into a permissive verdict.
func TestCacheDirectiveArguments(t *testing.T) {
	d, valid := ParseDirectives(` , PRIVATE="Set-Cookie, Authorization", extension="a\"b", max-age="60", ,`)
	if !valid || !d.Has("private") || d.Has("authorization") {
		t.Fatal("quoted field-name list split into directives")
	}
	if v, unique := d.UniqueValue("private"); !unique || v != "Set-Cookie, Authorization" {
		t.Fatal("quoted argument changed")
	}
	if v, unique := d.UniqueValue("extension"); !unique || v != `a"b` {
		t.Fatal("quoted pair changed")
	}
	for _, header := range []string{`private="unterminated`, `private="bad"suffix`, `private=`, "private=bad value", "private=\x01", "extension=\"bad\\\x01\"", `extension="bad\"`} {
		if _, valid := ParseDirectives(header); valid {
			t.Fatal("malformed cache field accepted")
		}
	}
	for _, header := range []string{"", " , , \t", "public", "max-age=60"} {
		if _, valid := ParseDirectives(header); !valid {
			t.Fatal("valid cache field rejected")
		}
	}
}

// RFC 9111 §4.2.1 allows treating duplicate freshness information as stale.
func TestDuplicateFreshnessIsStale(t *testing.T) {
	for _, header := range []string{"max-age=60, MAX-AGE=60", "max-age=60, max-age=120", "max-age=120, max-age=60"} {
		d, valid := ParseDirectives(header)
		if _, unique := d.UniqueValue("max-age"); !valid || unique {
			t.Fatal("duplicate freshness information accepted")
		}
	}
}
