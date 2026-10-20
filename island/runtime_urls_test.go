package island

import (
	neturl "net/url"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/buildmanifest"
)

func TestVersionCompatRuntimePath(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
		hash string
		want string
	}{
		{"canonical", "/gosx/bootstrap.js", " abc123 ", "/gosx/bootstrap.js?v=abc123"},
		{"hash escaping", "/gosx/bootstrap.js", "a+b /&", "/gosx/bootstrap.js?v=a%2Bb+%2F%26"},
		{"empty hash", "/gosx/bootstrap.js?lang=en", " ", "/gosx/bootstrap.js?lang=en"},
		{"query", "/gosx/bootstrap.js?lang=en", "abc", "/gosx/bootstrap.js?lang=en&v=abc"},
		{"empty version", "/gosx/bootstrap.js?v=&lang=en", "abc", "/gosx/bootstrap.js?lang=en&v=abc"},
		{"existing version", "/gosx/bootstrap.js?v=custom&lang=en", "abc", "/gosx/bootstrap.js?v=custom&lang=en"},
		{"fragment", "/gosx/bootstrap.js#module", "abc", "/gosx/bootstrap.js?v=abc#module"},
		{"escaped path", "/gosx/%62ootstrap.js", "abc", "/gosx/%62ootstrap.js?v=abc"},
		{"external", "https://cdn.example/gosx/bootstrap.js", "abc", "https://cdn.example/gosx/bootstrap.js"},
		{"protocol relative", "//cdn.example/gosx/bootstrap.js", "abc", "//cdn.example/gosx/bootstrap.js"},
		{"opaque", "data:/gosx/bootstrap.js", "abc", "data:/gosx/bootstrap.js"},
		{"base path", "/app/gosx/bootstrap.js", "abc", "/app/gosx/bootstrap.js"},
		{"hashed", "/app/assets/bootstrap.abc.js", "abc", "/app/assets/bootstrap.abc.js"},
		{"relative", "gosx/bootstrap.js", "abc", "gosx/bootstrap.js"},
		{"malformed escape", "/gosx/%boot.js", "abc", "/gosx/%boot.js"},
		{"control character", "/gosx/bootstrap.js\n", "abc", "/gosx/bootstrap.js\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := (&Renderer{}).versionCompatRuntimePath(tc.path, tc.hash); got != tc.want {
				t.Fatalf("versionCompatRuntimePath(%q, %q) = %q, want %q", tc.path, tc.hash, got, tc.want)
			}
		})
	}
}

// The fast path must agree with net/url for arbitrary strings, including
// malformed URLs: matching a different path can attach the wrong SRI digest.
func FuzzCompatRuntimePath(f *testing.F) {
	for _, path := range []string{
		"", "/", "//", "///gosx/bootstrap.js", "/gosx/bootstrap.js",
		"/app/assets/bootstrap.abc.js", "/gosx/bootstrap.js?v=abc#module",
		"https://cdn.example/gosx/bootstrap.js", "//cdn.example/gosx/bootstrap.js",
		"/gosx/%62ootstrap.js", "/gosx/%zz.js", "\n/gosx/bootstrap.js\t",
		"/gosx/bootstrap.js\x00", "/gosx/日本語.js", "relative/path.js",
		"data:/gosx/bootstrap.js", "?query", "#fragment",
	} {
		f.Add(path)
	}
	f.Fuzz(func(t *testing.T, path string) {
		want := strings.TrimSpace(path)
		if parsed, err := neturl.Parse(path); err == nil {
			want = strings.TrimSpace(parsed.Path)
		}
		if got := compatRuntimePath(path); got != want {
			t.Fatalf("compatRuntimePath(%q) = %q, want %q", path, got, want)
		}
	})
}

func TestRuntimeAssetURLsKeepIntegrityBoundToManifest(t *testing.T) {
	SetManifestRoot("")
	t.Cleanup(ResetManifestRoot)
	for _, base := range []string{"/gosx/assets", "/app/gosx/assets", "/app%20name/assets", "https://cdn.example/assets", "//cdn.example/assets"} {
		t.Run(base, func(t *testing.T) {
			manifest := &buildmanifest.Manifest{Runtime: buildmanifest.RuntimeAssets{
				BootstrapLite: buildmanifest.HashedAsset{
					File: "bootstrap-lite.first.js", Hash: "first", Integrity: "sha256-first",
				},
			}}
			r := NewRenderer("first-request")
			if err := r.ApplyBuildManifest(manifest, base); err != nil {
				t.Fatal(err)
			}
			r.EnableBootstrap()
			original := r.bootstrapLitePath
			checkRuntimeScript(t, r, original, "sha256-first", "first-nonce")

			// Query and fragment components do not change the bytes identified
			// by the manifest. A different file must not inherit its digest.
			r.SetBootstrapLitePath(original + "?delivery=cdn#script")
			checkRuntimeScript(t, r, original+"?delivery=cdn#script", "sha256-first", "first-nonce")
			r.SetBootstrapLitePath(base + "/unrelated.js")
			if head := gosx.RenderHTML(r.PageHeadWithNonce("first-nonce")); strings.Contains(head, "integrity=") {
				t.Fatalf("unrelated script inherited SRI: %s", head)
			}

			// A second renderer shares input metadata, not setters or nonces.
			other := NewRenderer("second-request")
			if err := other.ApplyBuildManifest(manifest, base); err != nil {
				t.Fatal(err)
			}
			other.EnableBootstrap()
			checkRuntimeScript(t, other, original, "sha256-first", "second-nonce")
			if head := gosx.RenderHTML(other.PageHeadWithNonce("second-nonce")); strings.Contains(head, "first-nonce") {
				t.Fatalf("request nonce leaked: %s", head)
			}

			// Reapplying a manifest must resolve its new paths and hashes;
			// the updated renderer must stop trusting the replaced filename.
			replacement := &buildmanifest.Manifest{Runtime: buildmanifest.RuntimeAssets{
				BootstrapLite: buildmanifest.HashedAsset{
					File: "bootstrap-lite.second.js", Hash: "second", Integrity: "sha256-second",
				},
			}}
			if err := r.ApplyBuildManifest(replacement, base); err != nil {
				t.Fatal(err)
			}
			checkRuntimeScript(t, r, strings.Replace(original, "first", "second", 1), "sha256-second", "new-nonce")
			if got := r.compatRuntimeIntegrity(original); got != "" {
				t.Fatalf("replaced filename still trusted with integrity %q", got)
			}
			r.SetBootstrapLitePath("/gosx/bootstrap-lite.js")
			checkRuntimeScript(t, r, "/gosx/bootstrap-lite.js?v=second", "sha256-second", "new-nonce")
			checkRuntimeScript(t, other, original, "sha256-first", "second-nonce")
		})
	}
}

func checkRuntimeScript(t *testing.T, r *Renderer, path, integrity, nonce string) {
	t.Helper()
	head := gosx.RenderHTML(r.PageHeadWithNonce(nonce))
	for _, want := range []string{`src="` + path + `"`, `integrity="` + integrity + `"`, `nonce="` + nonce + `"`} {
		if !strings.Contains(head, want) {
			t.Fatalf("runtime script missing %q: %s", want, head)
		}
	}
}
