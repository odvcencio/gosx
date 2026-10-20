package aot

import (
	"bytes"
	"encoding/json"
	"testing"

	"m31labs.dev/gosx/island/program"
)

func TestJSONAdmissionRejectsUnpairedSurrogates(t *testing.T) {
	u := jsonStringUnit(t, "placeholder")
	for _, tc := range []struct{ name, value string }{
		{"lone high", `\ud800`},
		{"lone low", `\udc00`},
		{"reversed pair", `\udc00\ud800`},
		{"two highs", `\ud800\ud800`},
		{"high then BMP escape", `\ud800\u0061`},
		{"high then text", `\ud800x`},
		{"high then escaped backslash", `\ud800\\udc00`},
		{"escaped high then low", `\\ud800\udc00`},
		{"high then escaped quote", `\ud800\"`},
		{"pair then lone low", `\ud83c\udf34\udc00`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := jsonStringInput(t, u, tc.value)
			if !json.Valid(data) {
				t.Fatal("fixture must be syntactically valid JSON")
			}
			if _, err := NewJSONUnit(u.Component, data, u.Contract); err == nil {
				t.Fatal("admitted an unpaired surrogate escape")
			}
			// The VM decoder keeps its existing replacement behavior.
			var legacy program.Program
			if err := json.Unmarshal(data, &legacy); err != nil {
				t.Fatalf("VM JSON decoding changed: %v", err)
			}
		})
	}
}

func TestJSONAdmissionPreservesUnicodeIdentities(t *testing.T) {
	u := jsonStringUnit(t, "placeholder")
	identities := map[string]Unit{}
	for _, tc := range []struct{ name, encoded, decoded string }{
		{"literal replacement", "\ufffd", "\ufffd"},
		{"escaped replacement", `\ufffd`, "\ufffd"},
		{"literal pair", "🌴", "🌴"},
		{"escaped pair", `\ud83c\udf34`, "🌴"},
		{"uppercase pair", `\uD83C\uDF34`, "🌴"},
		{"first pair", `\ud800\udc00`, "\U00010000"},
		{"last pair", `\udbff\udfff`, "\U0010ffff"},
		{"escaped high text", `\\ud800`, `\ud800`},
		{"escaped reversed text", `\\udc00\\ud800`, `\udc00\ud800`},
		{"escaped quotes", `\"\ud83c\udf34\"`, `"🌴"`},
		{"backslash then pair", `\\\ud83c\udf34`, `\🌴`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NewJSONUnit(u.Component, jsonStringInput(t, u, tc.encoded), u.Contract)
			if err != nil {
				t.Fatal(err)
			}
			want := jsonStringUnit(t, tc.decoded)
			if got.Program.Exprs[0].Value != tc.decoded || got.Digest != want.Digest || !bytes.Equal(got.ProgramBytes, want.ProgramBytes) {
				t.Fatal("Unicode text or canonical identity changed during admission")
			}
			identities[tc.name] = got
		})
	}
	if identities["literal replacement"].Digest != identities["escaped replacement"].Digest {
		t.Fatal("replacement character encodings have different identities")
	}
	if identities["literal pair"].Digest != identities["escaped pair"].Digest {
		t.Fatal("paired surrogate and literal scalar have different identities")
	}
	if identities["escaped replacement"].Digest == identities["escaped pair"].Digest || identities["escaped replacement"].ProgramSHA == identities["escaped pair"].ProgramSHA {
		t.Fatal("explicit replacement character and valid pair share an identity")
	}
}

func jsonStringUnit(t *testing.T, value string) Unit {
	t.Helper()
	u := literalUnit(t)
	u.Program.Exprs[0] = program.Expr{Op: program.OpLitString, Type: program.TypeString, Value: value}
	u.Contract.Expressions[0].Kind = String
	return refreshUnit(t, u)
}

func jsonStringInput(t *testing.T, u Unit, escaped string) []byte {
	t.Helper()
	raw, err := json.Marshal(u.Program)
	if err != nil {
		t.Fatal(err)
	}
	needle := []byte(`"value":"placeholder"`)
	if bytes.Count(raw, needle) != 1 {
		t.Fatal("fixture must contain one string placeholder")
	}
	return bytes.Replace(raw, needle, []byte(`"value":"`+escaped+`"`), 1)
}
