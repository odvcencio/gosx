package auth

import "testing"

func TestConstantTimeStringEqual(t *testing.T) {
	if !constantTimeStringEqual("state-token", "state-token") {
		t.Fatal("expected equal strings to match")
	}
	for _, tc := range []struct {
		name string
		a    string
		b    string
	}{
		{name: "different content", a: "state-token", b: "state-tokem"},
		{name: "different length", a: "state-token", b: "state-token-extra"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if constantTimeStringEqual(tc.a, tc.b) {
				t.Fatalf("expected %q and %q not to match", tc.a, tc.b)
			}
		})
	}
}

func TestConstantTimeBytesEqual(t *testing.T) {
	if !constantTimeBytesEqual([]byte{1, 2, 3}, []byte{1, 2, 3}) {
		t.Fatal("expected equal byte slices to match")
	}
	for _, tc := range []struct {
		name string
		a    []byte
		b    []byte
	}{
		{name: "different content", a: []byte{1, 2, 3}, b: []byte{1, 2, 4}},
		{name: "different length", a: []byte{1, 2, 3}, b: []byte{1, 2, 3, 4}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if constantTimeBytesEqual(tc.a, tc.b) {
				t.Fatalf("expected %v and %v not to match", tc.a, tc.b)
			}
		})
	}
}
