package sceneprobe

import "testing"

func TestDecodeReadsDefer(t *testing.T) {
	p := Decode([]byte(`{"defer":{"until":"load","timeoutMs":900,"inline":true,"public":true},"scene":{}}`))
	if p.Defer == nil || p.Defer.Until != "load" || p.Defer.TimeoutMS != 900 || !p.Defer.Inline || !p.Defer.Public {
		t.Fatalf("defer = %+v", p.Defer)
	}
	if Decode([]byte(`{"scene":{}}`)).Defer != nil {
		t.Fatal("no defer key must decode to nil")
	}
	if Decode([]byte(`{"defer":"yes"}`)).Defer != nil {
		t.Fatal("a malformed defer must fall back to eager loading")
	}
}
