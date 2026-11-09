package identity

import (
	"testing"

	"m31labs.dev/gosx/room/store"
)

// FuzzVerify checks that no token string panics the parser or verifies.
func FuzzVerify(f *testing.F) {
	f.Add("")
	f.Add("v1.k2.AAAA.BBBB")
	f.Add("v1...." + "x")
	st, err := store.Open(f.TempDir(), store.Options{})
	if err != nil {
		f.Fatal(err)
	}
	defer st.Close()
	svc, _ := New(st, Config{Keys: testKeys})
	f.Fuzz(func(t *testing.T, tok string) {
		if _, err := svc.Verify(tok); err == nil {
			t.Fatalf("accepted forged token %q", tok)
		}
		_ = NormalizeCode(tok)
	})
}
