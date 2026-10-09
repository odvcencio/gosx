package telemetry

import (
	"errors"
	"math"
	"strings"
	"sync"
	"testing"
	"time"
)

func testCodec[T any](t testing.TB, c DomainCodec[T]) *compiledDomainCodec[T] {
	t.Helper()
	c, bytes, count, empty, err := prepareDomainCodec(c)
	if err != nil {
		t.Fatal(err)
	}
	return freezeDomainCodec(c, bytes, count, empty)
}

func TestDomainFieldsCopyAndReplacement(t *testing.T) {
	type domain struct {
		Ints   []int64
		Floats []float64
		Enums  []string
	}
	var saved *FieldSet
	c := testCodec(t, DomainCodec[domain]{Name: "match", Version: 1, Fields: []FieldDefinition{
		{Name: "wave", Type: FieldInt}, {Name: "ratio", Type: FieldFloat}, {Name: "bot", Type: FieldBool},
		{Name: "elapsed_ms", Type: FieldDuration}, {Name: "mode", Type: FieldEnum, Values: []string{"<team>", "solo"}},
		{Name: "scores", Type: FieldInts}, {Name: "weights", Type: FieldFloats}, {Name: "modes", Type: FieldEnums, Values: []string{"solo"}},
		{Name: "round", Type: FieldObject, Fields: []FieldDefinition{{Name: "a", Type: FieldInt}, {Name: "b", Type: FieldInt}}},
	}, Encode: func(f *FieldSet, v domain) error {
		saved = f
		for _, err := range []error{f.Int("wave", 1), f.Int("wave", 2), f.Float("ratio", 1e-7), f.Bool("bot", false), f.Duration("elapsed_ms", 1234567*time.Nanosecond), f.Enum("mode", "<team>"), f.Ints("scores", v.Ints), f.Floats("weights", v.Floats), f.Enums("modes", v.Enums)} {
			if err != nil {
				return err
			}
		}
		if err := f.Object("round", func(o *FieldSet) error { return o.Int("a", 1) }); err != nil {
			return err
		}
		return f.Object("round", func(o *FieldSet) error { return o.Int("b", 2) })
	}})
	input := domain{[]int64{1, 2}, []float64{.5}, []string{"solo"}}
	pool := new(fieldPool)
	out, err := c.encodeFields(pool, input)
	if err != nil {
		t.Fatal(err)
	}
	input.Ints[0] = 99
	input.Floats[0] = 99
	input.Enums[0] = "private_value"
	b, err := out.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	want := `{"bot":false,"elapsed_ms":1.234567,"mode":"<team>","modes":["solo"],"ratio":1e-7,"round":{"b":2},"scores":[1,2],"wave":2,"weights":[0.5]}`
	if string(b) != want {
		t.Fatalf("projection %s", b)
	}
	if err := saved.Int("wave", 3); !errors.Is(err, ErrClosed) {
		t.Fatalf("retained staging: %v", err)
	}
	old := saved
	second, err := c.encodeFields(pool, domain{[]int64{4}, []float64{5}, []string{"solo"}})
	if err != nil {
		t.Fatal(err)
	}
	second[6].Ints[0] = 88
	if err := old.Int("wave", 99); !errors.Is(err, ErrClosed) {
		t.Fatalf("reused generation accepted stale staging: %v", err)
	}
	b, _ = out.MarshalJSON()
	if string(b) != want {
		t.Fatal("later lease or returned view aliases the first projection")
	}
}

func TestDomainFieldsRejectWholeEncoding(t *testing.T) {
	defs := []FieldDefinition{{Name: "n", Type: FieldInt}, {Name: "f", Type: FieldFloat}, {Name: "m", Type: FieldEnum, Values: []string{"safe"}}, {Name: "a", Type: FieldInts}, {Name: "o", Type: FieldObject}}
	cases := []struct {
		name   string
		call   func(*FieldSet) error
		target error
	}{
		{"unknown", func(f *FieldSet) error { return f.Int("private_key", 1) }, ErrInvalidOptions},
		{"type", func(f *FieldSet) error { return f.Bool("n", true) }, ErrInvalidOptions},
		{"enum", func(f *FieldSet) error { return f.Enum("m", "private_value") }, ErrInvalidOptions},
		{"nan", func(f *FieldSet) error { return f.Float("f", math.NaN()) }, ErrInvalidOptions},
		{"infinity", func(f *FieldSet) error { return f.Float("f", math.Inf(-1)) }, ErrInvalidOptions},
		{"array", func(f *FieldSet) error { return f.Ints("a", make([]int64, 33)) }, ErrFieldBudget},
		{"nil_object", func(f *FieldSet) error { return f.Object("o", nil) }, ErrInvalidOptions},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := testCodec(t, DomainCodec[int]{Name: "match", Version: 1, Fields: defs, Encode: func(f *FieldSet, _ int) error { _ = f.Int("n", 2); _ = tc.call(f); return nil }})
			out, err := c.encodeFields(new(fieldPool), 0)
			if !errors.Is(err, tc.target) || out != nil || strings.Contains(err.Error(), "private_") {
				t.Fatalf("partial or unclassified result: %v %v", out, err)
			}
		})
	}
}

func TestDomainFieldsBudgetIncludesKeys(t *testing.T) {
	defs := make([]FieldDefinition, 64)
	for i := range defs {
		defs[i] = FieldDefinition{Name: string([]byte{'a', byte('a' + i/26), byte('a' + i%26)}) + strings.Repeat("x", 61), Type: FieldInt}
	}
	c := testCodec(t, DomainCodec[int]{Name: "match", Version: 1, Fields: defs, Encode: func(f *FieldSet, _ int) error {
		for _, d := range defs {
			if err := f.Int(d.Name, 0); err != nil {
				return err
			}
		}
		return nil
	}})
	if out, err := c.encodeFields(new(fieldPool), 0); out != nil || !errors.Is(err, ErrFieldBudget) {
		t.Fatalf("keys escaped budget: %v %v", out, err)
	}
}

func TestDomainFieldsLeaseCapacityAndIsolation(t *testing.T) {
	pool := new(fieldPool)
	entered := make(chan struct{}, 8)
	resume := make(chan struct{})
	var wg sync.WaitGroup
	c := testCodec(t, DomainCodec[int]{Name: "match", Version: 1, Fields: []FieldDefinition{{Name: "n", Type: FieldInt}}, Encode: func(f *FieldSet, v int) error { entered <- struct{}{}; <-resume; return f.Int("n", int64(v)) }})
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(v int) {
			defer wg.Done()
			out, err := c.encodeFields(pool, v)
			if err != nil || len(out) != 1 || out[0].Int != int64(v) {
				t.Errorf("isolated lease %d: %v %v", v, out, err)
			}
		}(i)
	}
	for i := 0; i < 8; i++ {
		<-entered
	}
	if out, err := c.encodeFields(pool, 9); out != nil || !errors.Is(err, ErrCapacity) || !strings.Contains(err.Error(), "codec_busy") {
		t.Fatalf("ninth encoder: %v %v", out, err)
	}
	close(resume)
	wg.Wait()
	if pool.mask.Load() != 0 {
		t.Fatal("leaked staging leases")
	}
}

func TestDomainCodecPanicErrorAndRelease(t *testing.T) {
	canary := errors.New("private_encoder_value")
	for _, panicEncoder := range []bool{false, true} {
		pool := new(fieldPool)
		c := testCodec(t, DomainCodec[int]{Name: "match", Version: 1, Fields: []FieldDefinition{}, Encode: func(*FieldSet, int) error {
			if panicEncoder {
				panic(canary)
			}
			return canary
		}})
		out, err := c.encodeFields(pool, 0)
		if out != nil || err == nil || strings.Contains(err.Error(), canary.Error()) || pool.mask.Load() != 0 {
			t.Fatalf("encoder boundary %v %v", out, err)
		}
		if !panicEncoder && !errors.Is(err, canary) {
			t.Fatal("lost inspectable encoder cause")
		}
	}
}

func FuzzDomainFields(f *testing.F) {
	f.Add([]byte{0, 1, 2})
	f.Add([]byte{255, 33, 0})
	f.Add([]byte{})
	c := testCodec(f, DomainCodec[[]byte]{Name: "match", Version: 1, Fields: []FieldDefinition{{Name: "n", Type: FieldInt}, {Name: "values", Type: FieldInts}}, Encode: func(s *FieldSet, b []byte) error {
		if len(b) > 0 && b[0]&1 == 1 {
			_ = s.Float("n", math.Inf(1))
		}
		values := make([]int64, min(len(b), 33))
		for i := range values {
			values[i] = int64(b[i])
		}
		if err := s.Ints("values", values); err != nil {
			return err
		}
		return s.Int("n", int64(len(b)))
	}})
	pool := new(fieldPool)
	f.Fuzz(func(t *testing.T, b []byte) {
		out, err := c.encodeFields(pool, b)
		if pool.mask.Load() != 0 {
			t.Fatal("leaked lease")
		}
		if err != nil {
			if out != nil {
				t.Fatal("partial projection")
			}
			return
		}
		encoded, err := out.MarshalJSON()
		if err != nil || len(encoded) > 4096 {
			t.Fatalf("invalid accepted projection: %v", err)
		}
	})
}

func BenchmarkDomainCodec(b *testing.B) {
	c := testCodec(b, DomainCodec[int]{Name: "match", Version: 1, Fields: []FieldDefinition{{Name: "score", Type: FieldInt}}, Encode: func(f *FieldSet, v int) error { return f.Int("score", int64(v)) }})
	pool := new(fieldPool)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.encodeFields(pool, i); err != nil {
			b.Fatal(err)
		}
	}
}
