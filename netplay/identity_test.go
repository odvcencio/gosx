package netplay

import "testing"

func idOf(t *testing.T, goV, detV string, src, data []IdentityFile) [32]byte {
	t.Helper()
	id, err := ComputeIdentity(goV, detV, src, data)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestIdentityDependsOnEveryInput(t *testing.T) {
	src := []IdentityFile{{"step.go", []byte("package sim\n")}, {"state.go", []byte("package sim\n// x\n")}}
	data := []IdentityFile{{"moves.json", []byte(`{"a":1}`)}}
	base := idOf(t, "go1.26.0", "v1", src, data)
	if base != idOf(t, "go1.26.0", "v1", src, data) {
		t.Fatal("not deterministic")
	}
	changed := map[string][32]byte{
		"go version":  idOf(t, "go1.26.1", "v1", src, data),
		"det version": idOf(t, "go1.26.0", "v2", src, data),
		"no det":      idOf(t, "go1.26.0", "", src, data),
		"source edit": idOf(t, "go1.26.0", "v1", []IdentityFile{src[0], {"state.go", []byte("package sim\n// y\n")}}, data),
		"data edit":   idOf(t, "go1.26.0", "v1", src, []IdentityFile{{"moves.json", []byte(`{"a":2}`)}}),
		"rename":      idOf(t, "go1.26.0", "v1", []IdentityFile{src[0], {"other.go", src[1].Data}}, data),
		"data as src": idOf(t, "go1.26.0", "v1", append(append([]IdentityFile(nil), src...), data[0]), nil),
	}
	for name, id := range changed {
		if id == base {
			t.Errorf("%s did not change the identity", name)
		}
	}
}

func TestIdentityIgnoresListOrderAndSourceLineEndings(t *testing.T) {
	a := idOf(t, "go1", "", []IdentityFile{{"a.go", []byte("x\ny\n")}, {"b.go", []byte("z")}}, nil)
	b := idOf(t, "go1", "", []IdentityFile{{"b.go", []byte("z")}, {"a.go", []byte("x\r\ny\r\n")}}, nil)
	if a != b {
		t.Fatal("order or CRLF changed the identity")
	}
	// Data files are hashed as they are.
	c := idOf(t, "go1", "", nil, []IdentityFile{{"d.bin", []byte("x\ny")}})
	d := idOf(t, "go1", "", nil, []IdentityFile{{"d.bin", []byte("x\r\ny")}})
	if c == d {
		t.Fatal("data line endings were folded")
	}
}

func TestIdentityRejectsDuplicateNames(t *testing.T) {
	if _, err := ComputeIdentity("go1", "", []IdentityFile{{"a.go", nil}, {"a.go", nil}}, nil); err == nil {
		t.Fatal("accepted duplicate names")
	}
}
