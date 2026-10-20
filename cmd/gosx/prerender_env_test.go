package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrerenderPreservesNumericPortAndPublicOrigin(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "app", "page.gsx"), `package app
component Page() { return <p>origin</p> }
`)
	mustWriteFile(t, filepath.Join(dir, "main.go"), `package main
import (
    "encoding/base64"
    "fmt"
    "log"
    "net"
    "net/http"
    "os"
)
func main() {
    secret, err := base64.RawURLEncoding.DecodeString(os.Getenv("SESSION_SECRET"))
    if err != nil || len(secret) != 32 { log.Fatal("missing disposable secret") }
    origin := os.Getenv("PUBLIC_URL")
    if origin == "" { origin = "https://default.example.test" }
    http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        if r.URL.Path != "/" { w.WriteHeader(http.StatusNotFound) }
        fmt.Fprintf(w, "<html><head><link rel=\"canonical\" href=\"%s/\"><meta property=\"og:url\" content=\"%s/\"></head><body>origin</body></html>", origin, origin)
    })
    port := os.Getenv("PORT")
    addr := net.JoinHostPort("127.0.0.1", port)
    if os.Getenv("TEST_EXPORT_LISTENER") == "colon" { addr = ":" + port }
    if os.Getenv("TEST_EXPORT_LISTENER") == "override" { addr = os.Getenv("GOSX_LISTEN_ADDR") }
    log.Fatal(http.ListenAndServe(addr, nil))
}
`)
	binary := filepath.Join(dir, "app-server")
	cmd := exec.Command("go", "build", "-o", binary, filepath.Join(dir, "main.go"))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fixture: %v\n%s", err, output)
	}
	t.Setenv("SESSION_SECRET", "change-me-in-production")
	t.Setenv("GOSX_ENV", "production")
	t.Setenv("GOSX_LISTEN_ADDR", "192.0.2.1:8080")
	for _, inheritedPort := range []string{"", "8080", "0.0.0.0:8080"} {
		for _, listener := range []string{"join", "colon", "override"} {
			for _, publicURL := range []string{"", "https://explicit.example.test"} {
				t.Run(inheritedPort+"/"+listener+"/"+publicURL, func(t *testing.T) {
					if inheritedPort == "" {
						t.Setenv("PORT", "")
						if err := os.Unsetenv("PORT"); err != nil {
							t.Fatal(err)
						}
					} else {
						t.Setenv("PORT", inheritedPort)
					}
					if publicURL == "" {
						t.Setenv("PUBLIC_URL", "")
						if err := os.Unsetenv("PUBLIC_URL"); err != nil {
							t.Fatal(err)
						}
					} else {
						t.Setenv("PUBLIC_URL", publicURL)
					}
					t.Setenv("TEST_EXPORT_LISTENER", listener)
					outputDir := filepath.Join(t.TempDir(), "static")
					if _, err := prerenderStaticBundle(staticExportOptions{
						AppRoot: dir, OutputDir: outputDir, BinaryPath: binary,
					}); err != nil {
						t.Fatal(err)
					}
					origin := publicURL
					if origin == "" {
						origin = "https://default.example.test"
					}
					html := readFile(t, filepath.Join(outputDir, "index.html"))
					for _, want := range []string{`href="` + origin + `/"`, `content="` + origin + `/"`} {
						if !strings.Contains(html, want) {
							t.Fatalf("missing %s in %s", want, html)
						}
					}
					if strings.Contains(html, "127.0.0.1") {
						t.Fatal("temporary origin persisted in export")
					}
					if os.Getenv("SESSION_SECRET") != "change-me-in-production" {
						t.Fatal("parent secret changed")
					}
				})
			}
		}
	}
}
