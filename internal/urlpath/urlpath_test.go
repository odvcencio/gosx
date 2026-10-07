package urlpath

import "testing"

func TestNormalizeAndResolve(t *testing.T) {
	for _, bad := range []string{"//evil.test", "relative", "/a/../b", "/a//b", "/a?x", "/a#x", "/a%2fb", "/a\\b", "/a;", "/a\n", "/a\x00", "/é", "/a\"b"} {
		if _, err := Normalize(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	for _, tc := range []struct{ in, want string }{{"", ""}, {"/", ""}, {"/.proxy/game/", "/.proxy/game"}} {
		got, err := Normalize(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("Normalize(%q) = %q, %v", tc.in, got, err)
		}
	}
	for _, tc := range []struct{ in, want string }{
		{"/", "/.proxy/game/"}, {"/next?q=1#top", "/.proxy/game/next?q=1#top"},
		{"/.proxy/game", "/.proxy/game/.proxy/game"}, {"/.proxy/game?q=1", "/.proxy/game/.proxy/game?q=1"},
		{"/.proxy/game/next", "/.proxy/game/.proxy/game/next"}, {"/.proxy/games", "/.proxy/game/.proxy/games"},
		{"//cdn.example/a", "//cdn.example/a"}, {"https://cdn.example/a", "https://cdn.example/a"},
		{"wss://socket.example/ws", "wss://socket.example/ws"}, {"#top", "#top"}, {"image.png", "image.png"},
	} {
		if got := URL("/.proxy/game", tc.in); got != tc.want {
			t.Errorf("URL(%q)=%q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestURLPrefixCollidesWithInternalRoute(t *testing.T) {
	for _, route := range []string{"/news", "/news/", "/news/details", "/news?q=1#top", "/news/details?q=1#top"} {
		if got, want := URL("/news", route), "/news"+route; got != want {
			t.Errorf("URL(%q, %q) = %q, want %q", "/news", route, got, want)
		}
	}
}
