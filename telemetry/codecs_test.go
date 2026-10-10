package telemetry

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx/telemetry/schema"
)

func TestDomainCodecDeclarations(t *testing.T) {
	base := DomainCodec[int]{Name: "match", Version: 1, Fields: []FieldDefinition{{Name: "n", Type: FieldInt}}, Encode: func(*FieldSet, int) error { return nil }}
	if n := testing.AllocsPerRun(100, func() { _, _, _, _, _ = prepareDomainCodec(base) }); n != 0 {
		t.Fatalf("descriptor preparation allocates before reservation: %v", n)
	}
	cases := []struct {
		name string
		edit func(*DomainCodec[int])
	}{
		{"name", func(c *DomainCodec[int]) { c.Name = "private-name" }},
		{"version", func(c *DomainCodec[int]) { c.Version = 0 }},
		{"encoder", func(c *DomainCodec[int]) { c.Encode = nil }},
		{"missing_schema", func(c *DomainCodec[int]) { c.Fields = nil }},
		{"key", func(c *DomainCodec[int]) { c.Fields = []FieldDefinition{{Name: "private-key"}} }},
		{"type", func(c *DomainCodec[int]) { c.Fields = []FieldDefinition{{Name: "n", Type: 255}} }},
		{"duplicate", func(c *DomainCodec[int]) { c.Fields = []FieldDefinition{{Name: "n"}, {Name: "n"}} }},
		{"enum_missing", func(c *DomainCodec[int]) { c.Fields = []FieldDefinition{{Name: "n", Type: FieldEnum}} }},
		{"enum_duplicate", func(c *DomainCodec[int]) {
			c.Fields = []FieldDefinition{{Name: "n", Type: FieldEnum, Values: []string{"a", "a"}}}
		}},
		{"enum_bytes", func(c *DomainCodec[int]) {
			c.Fields = []FieldDefinition{{Name: "n", Type: FieldEnum, Values: []string{strings.Repeat("p", 65)}}}
		}},
		{"enum_utf8", func(c *DomainCodec[int]) {
			c.Fields = []FieldDefinition{{Name: "n", Type: FieldEnum, Values: []string{"\xff"}}}
		}},
		{"enum_wrong_type", func(c *DomainCodec[int]) { c.Fields = []FieldDefinition{{Name: "n", Values: []string{"a"}}} }},
		{"object_wrong_type", func(c *DomainCodec[int]) { c.Fields = []FieldDefinition{{Name: "n", Fields: base.Fields}} }},
		{"depth", func(c *DomainCodec[int]) {
			c.Fields = []FieldDefinition{{Name: "a", Type: FieldObject, Fields: []FieldDefinition{{Name: "b", Type: FieldObject, Fields: []FieldDefinition{{Name: "c", Type: FieldObject}}}}}}
		}},
		{"field_cap", func(c *DomainCodec[int]) { c.Fields = make([]FieldDefinition, 65) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := base
			tc.edit(&c)
			_, _, _, _, err := prepareDomainCodec(c)
			var config *ConfigError
			if !errors.Is(err, ErrInvalidOptions) || !errors.As(err, &config) || strings.Contains(err.Error(), "private-") {
				t.Fatalf("declaration: %v", err)
			}
		})
	}
	c := testCodec(t, DomainCodec[NoFields]{})
	if c.name != "none" || c.version != 1 {
		t.Fatal("zero NoFields normalization")
	}
	if out, err := c.encodeFields(nil, NoFields{}); out != nil || err != nil {
		t.Fatalf("empty codec: %v %v", out, err)
	}
	if n := testing.AllocsPerRun(100, func() { _, _ = c.encodeFields(nil, NoFields{}) }); n != 0 {
		t.Fatalf("empty codec allocates: %v", n)
	}
}

func TestDomainCodecFreezesDescriptors(t *testing.T) {
	values := []string{"safe"}
	defs := []FieldDefinition{{Name: "mode", Type: FieldEnum, Values: values}}
	c := testCodec(t, DomainCodec[int]{Name: "match", Version: 1, Fields: defs, Encode: func(f *FieldSet, _ int) error { return f.Enum("mode", "safe") }})
	values[0] = "changed"
	defs[0].Name = "changed"
	defs[0].Fields = []FieldDefinition{{Name: "unexpected"}}
	out, err := c.encodeFields(new(fieldPool), 0)
	if err != nil || len(out) != 1 || out[0].Name != "mode" || out[0].Enum != "safe" {
		t.Fatalf("descriptor alias: %v %v", out, err)
	}
}

func TestDomainCodecOwnedMemory(t *testing.T) {
	var before, after runtime.MemStats
	c := DomainCodec[int]{Name: "match", Version: 1, Fields: []FieldDefinition{}, Encode: func(*FieldSet, int) error { return nil }}
	for i := 0; i < 64; i++ {
		values := make([]string, 128)
		for j := range values {
			values[j] = string([]byte{'v', byte('a' + j/26), byte('a' + j%26)})
		}
		c.Fields = append(c.Fields, FieldDefinition{Name: string([]byte{'f', byte('a' + i/26), byte('a' + i%26)}), Type: FieldEnum, Values: values})
	}
	c, charge, count, empty, err := prepareDomainCodec(c)
	if err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	runtime.ReadMemStats(&before)
	frozen := freezeDomainCodec(c, charge, count, empty)
	pool := new(fieldPool)
	runtime.GC()
	runtime.ReadMemStats(&after)
	retained := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	if retained > charge+fieldPoolBytes {
		t.Fatalf("retained %d exceeds reserved %d", retained, charge+fieldPoolBytes)
	}
	runtime.KeepAlive(frozen)
	runtime.KeepAlive(pool)
	runtime.KeepAlive(c)
}

func TestDomainCodecWrongTypeDoesNotCompile(t *testing.T) {
	if runtime.GOOS == "js" {
		t.Skip("native compiler fixture; portable typed fixture also executes")
	}
	file := filepath.Join(t.TempDir(), "wrong.go")
	source := `package wrong
import "m31labs.dev/gosx/telemetry"
type Match struct{Wave int64}
type Seat struct{Bot bool}
var _ telemetry.DomainCodec[Match] = telemetry.DomainCodec[Seat]{}
`
	if err := os.WriteFile(file, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	// The child compiles telemetry's dependencies itself; with a cold build
	// cache (a new go.sum cache key in CI) that alone can exceed a minute.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "test", file)
	cmd.Env = append(os.Environ(), "GOWORK=off")
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "cannot use") || !strings.Contains(string(out), "DomainCodec") {
		t.Fatalf("wrong application type accepted or compiler failed independently: %v\n%s", err, out)
	}
}

// The positive fixture uses the real public generic constructor types and
// setters, and executes through the same compiled codec on native and wasm.
func TestTypedDomainCodecFixture(t *testing.T) {
	type match struct {
		Wave int64
		Mode string
	}
	c := testCodec(t, DomainCodec[match]{Name: "match", Version: 1, Fields: []FieldDefinition{{Name: "wave", Type: FieldInt}, {Name: "mode", Type: FieldEnum, Values: []string{"team", "solo"}}}, Encode: func(f *FieldSet, v match) error {
		if err := f.Int("wave", v.Wave); err != nil {
			return err
		}
		return f.Enum("mode", v.Mode)
	}})
	out, err := c.encodeFields(new(fieldPool), match{3, "team"})
	if err != nil {
		t.Fatal(err)
	}
	want := schema.Fields{{Name: "mode", Type: FieldEnum, Enum: "team"}, {Name: "wave", Type: FieldInt, Int: 3}}
	a, _ := out.MarshalJSON()
	b, _ := want.MarshalJSON()
	if string(a) != string(b) {
		t.Fatalf("typed fixture %s", a)
	}
}
