package hydrate

import (
	"encoding/json"
	"testing"
)

func TestManifestExactIntegerProps(t *testing.T) {
	for _, raw := range []string{
		`{"ID":9007199254740993}`, `{"nested":[{"ID":-9007199254740993}]}`,
		`{"ID":9223372036854775807}`, `{"ID":-9223372036854775808}`,
	} {
		for _, entry := range []any{IslandEntry{Props: json.RawMessage(raw)}, ComputeIslandEntry{Props: json.RawMessage(raw)}} {
			data, err := json.Marshal(entry)
			if err != nil {
				t.Fatal(err)
			}
			var decoded struct {
				Props     json.RawMessage
				PropsJSON string
			}
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded.PropsJSON != raw || string(decoded.Props) != raw {
				t.Fatalf("lost original props: %s", data)
			}
		}
	}
}

func TestManifestOrdinaryPropsHaveOneSnapshot(t *testing.T) {
	for _, raw := range []string{`{}`, `null`, `{"count":42,"ratio":1.25}`, `{"ID":9007199254740991}`, `{"text":"9007199254740993","escaped":"\\\" 9223372036854775807"}`} {
		data, err := json.Marshal(IslandEntry{Props: json.RawMessage(raw)})
		if err != nil {
			t.Fatal(err)
		}
		var decoded map[string]json.RawMessage
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatal(err)
		}
		if _, ok := decoded["propsJSON"]; ok {
			t.Fatalf("unnecessary second snapshot: %s", data)
		}
	}
}

func TestManifestExactPropsRemainScriptSafe(t *testing.T) {
	raw := `{"ID":9007199254740993,"text":"</script><script>alert(1)</script>"}`
	data, err := json.Marshal(IslandEntry{Props: json.RawMessage(raw)})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < len(data); i++ {
		if data[i] == '<' {
			t.Fatalf("unescaped script text: %s", data)
		}
	}
	var decoded struct{ PropsJSON string }
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.PropsJSON != raw {
		t.Fatalf("changed raw props: %s", decoded.PropsJSON)
	}
}
