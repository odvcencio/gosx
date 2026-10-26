//go:build darwin && !ios && (amd64 || arm64)

package desktop

import (
	"strings"
	"testing"
)

// NSString in and out: nsString builds an autoreleased NSString from Go text and
// cfStringToString (through goString) copies one back. Text that stringWithUTF8String:
// cannot carry is repaired rather than dropped.
func TestDarwinNSStringRoundTrip(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"empty", "", ""},
		{"ascii", "GoSX desktop app", "GoSX desktop app"},
		{"latin", "héllo wörld", "héllo wörld"},
		{"cjk", "日本語のタイトル", "日本語のタイトル"},
		{"emoji", "mac 🍎 window 🪟", "mac 🍎 window 🪟"},
		{"newlines and quotes", "line1\nline2\t\"quoted\" 'single' \\", "line1\nline2\t\"quoted\" 'single' \\"},
		{"html", "<body style=\"background:#10151c\">Starting the engine...</body>", "<body style=\"background:#10151c\">Starting the engine...</body>"},
		{"invalid utf8", "a\xffb", "a�b"},
		{"embedded NUL", "a\x00b", "a�b"},
		{"long", strings.Repeat("0123456789abcdef", 1<<16), strings.Repeat("0123456789abcdef", 1<<16)}, // 1 MiB
	} {
		t.Run(tc.name, func(t *testing.T) {
			withPool(func() {
				ns := nsString(tc.in)
				if ns == 0 {
					t.Fatalf("nsString(%q) = nil", tc.in)
				}
				got, ok := cfStringToString(ns)
				if !ok {
					t.Fatalf("cfStringToString failed for %q", tc.in)
				}
				if got != tc.want {
					t.Errorf("round trip = %q, want %q", got, tc.want)
				}
			})
		})
	}
}

func TestDarwinNSStringLengthIsUTF16(t *testing.T) {
	// -[NSString length] counts UTF-16 units, so the emoji (a surrogate pair)
	// is 2 and cfStringToString must size its buffer by encoded bytes, not units.
	withPool(func() {
		ns := nsString("a🍎é")
		if got := send0(ns, objcSel("length")); got != 4 {
			t.Errorf("length = %d, want 4 UTF-16 units", got)
		}
		if got := goString(ns); got != "a🍎é" {
			t.Errorf("goString = %q", got)
		}
	})
}

func TestDarwinCFStringToStringRejectsNil(t *testing.T) {
	if s, ok := cfStringToString(0); ok || s != "" {
		t.Errorf("cfStringToString(0) = %q, %v, want \"\", false", s, ok)
	}
}

func TestDarwinNSURL(t *testing.T) {
	withPool(func() {
		u := nsURL("http://127.0.0.1:8080/path?q=1")
		if u == 0 {
			t.Fatal("nsURL returned nil for a valid URL")
		}
		if got := goString(send0(u, objcSel("absoluteString"))); got != "http://127.0.0.1:8080/path?q=1" {
			t.Errorf("absoluteString = %q", got)
		}
		if got := goString(send0(u, objcSel("host"))); got != "127.0.0.1" {
			t.Errorf("host = %q", got)
		}
	})
}

func TestDarwinObjCClassAndSelectorCaches(t *testing.T) {
	if objcClass("NSObject") == 0 || objcClass("NSWindow") == 0 || objcClass("WKWebView") == 0 {
		t.Fatal("AppKit or WebKit classes are not available: the framework imports did not load them")
	}
	if objcClass("NoSuchClassGoSX") != 0 {
		t.Error("objcClass invented a class")
	}
	if a, b := objcSel("setTitle:"), objcSel("setTitle:"); a == 0 || a != b {
		t.Errorf("objcSel is not stable: %#x vs %#x", a, b)
	}
	if objcSel("setTitle:") == objcSel("setDelegate:") {
		t.Error("distinct selectors collide")
	}
}

func TestDarwinObjCRefReleasesOnce(t *testing.T) {
	obj := objcNew(objcClass("NSObject"))
	ref := ownObjC(obj)
	extra := objcRetain(obj) // a second reference of our own, so the object survives the test
	if got := retainCount(obj); got != 2 {
		t.Fatalf("retainCount = %d, want 2", got)
	}
	ref.Release()
	ref.Release()
	(*objcRef)(nil).Release() // nil receiver is a no-op
	if got := retainCount(obj); got != 1 {
		t.Errorf("retainCount after two Release calls = %d, want 1: Release must run once", got)
	}
	objcRelease(extra)
	if ownObjC(0) != nil {
		t.Error("ownObjC(0) is not nil")
	}
}

func TestDarwinColorFromBackgroundOption(t *testing.T) {
	color, canonical, ok, err := parseBackgroundColor("#10151c")
	if err != nil || !ok || canonical != "#10151c" {
		t.Fatalf("parseBackgroundColor = %+v %q %v %v", color, canonical, ok, err)
	}
	withPool(func() {
		if c := nsColorSRGB(float64(color.R)/255, float64(color.G)/255, float64(color.B)/255, 1); c == 0 {
			t.Error("nsColorSRGB returned nil")
		}
	})
}

func TestDarwinPlatformAvailable(t *testing.T) {
	if err := Available(); err != nil {
		t.Fatalf("Available() = %v, want nil: AppKit and WebKit are part of macOS", err)
	}
}
