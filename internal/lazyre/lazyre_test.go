package lazyre

import (
	"regexp"
	"sync"
	"testing"
)

func TestLazyPatternMatchesLikeRegexp(t *testing.T) {
	const expr = `^(?:([0-9]+)h)?(?:([0-9]+)m)?(?P<seconds>(?:[0-9]+)s)?$`
	lazy, eager := New(expr), regexp.MustCompile(expr)
	for _, text := range []string{"", "1h", "1h30m", "2m5s", "x", "1h 30m", "10s"} {
		if got, want := lazy.MatchString(text), eager.MatchString(text); got != want {
			t.Errorf("MatchString(%q) = %v, want %v", text, got, want)
		}
		got, want := lazy.FindStringSubmatch(text), eager.FindStringSubmatch(text)
		if len(got) != len(want) {
			t.Fatalf("FindStringSubmatch(%q) = %q, want %q", text, got, want)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("FindStringSubmatch(%q) = %q, want %q", text, got, want)
			}
		}
	}
	if got, want := lazy.SubexpIndex("seconds"), eager.SubexpIndex("seconds"); got != want {
		t.Errorf("SubexpIndex = %d, want %d", got, want)
	}
}

func TestMalformedPatternPanicsOnFirstUseNotInNew(t *testing.T) {
	bad := New(`(`)
	defer func() {
		if recover() == nil {
			t.Fatal("a malformed pattern did not panic on first use")
		}
	}()
	bad.MatchString("x")
}

func TestPatternCompilesOnceUnderConcurrentUse(t *testing.T) {
	p := New(`^[a-z]+$`)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !p.MatchString("abc") || p.MatchString("ABC") {
				t.Error("wrong match result")
			}
		}()
	}
	wg.Wait()
	first := p.compiled()
	if first != p.compiled() {
		t.Fatal("the pattern compiled more than once")
	}
}
