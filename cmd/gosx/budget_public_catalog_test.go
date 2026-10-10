package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"m31labs.dev/gosx/internal/bundlepolicy"
	"m31labs.dev/gosx/internal/pagecaps"
	"m31labs.dev/gosx/server"
)

type publicCatalog struct {
	Routes     []struct{ App string } `json:"routes"`
	AssetRules []struct {
		ID    string `json:"id"`
		Owner string `json:"owner"`
		Kind  string `json:"kind"`
	} `json:"assetRules"`
}

func canonicalPublicCatalog(t *testing.T) publicCatalog {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(testRepoRoot(t), "perf/fixtures/catalog.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var catalog publicCatalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatal(err)
	}
	return catalog
}

// Compare the bodies staged by the production bundle policy. Compression
// sidecars are generated representations of those bodies, not extra assets.
// There are no exemptions: every served public body needs an app-owned rule.
func publicCatalogDifferences(app, publicDir string, catalog publicCatalog) ([]string, error) {
	registered := map[string]string{}
	prefix := "app/" + app + "/public/"
	for _, rule := range catalog.AssetRules {
		if strings.HasPrefix(rule.ID, prefix) {
			if rule.Owner != "app" {
				return nil, fmt.Errorf("public asset is not app-owned: %s", rule.ID)
			}
			registered[strings.TrimPrefix(rule.ID, prefix)] = rule.Kind
		}
	}
	differences := []string{}
	appServer := server.New()
	appServer.SetPublicDir(publicDir)
	handler := appServer.Build()
	err := filepath.WalkDir(publicDir, func(full string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(publicDir, full)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		kind, present := registered[rel]
		if !present {
			differences = append(differences, "unregistered: "+prefix+rel)
		} else {
			// HEAD follows the production public-file MIME policy, including
			// webmanifest and content sniffing, without copying large bodies.
			request := httptest.NewRequest(http.MethodHead, (&url.URL{Path: "/" + rel}).String(), nil)
			request.Header.Set("Accept-Encoding", "identity")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			media := response.Header().Get("Content-Type")
			if response.Code != http.StatusOK || !publicServedKindMatches(kind, media) {
				differences = append(differences, "wrong kind: "+prefix+rel+" ("+kind+", "+media+")")
			}
		}
		delete(registered, rel)
		return nil
	})
	for rel := range registered {
		differences = append(differences, "not served: "+prefix+rel)
	}
	sort.Strings(differences)
	return differences, err
}

// The observed response defines the semantic kind, independently of the
// catalog's declaration. Octet-stream and JSON have ambiguous binary/model
// contracts; the catalog and collector validate their declared bodies.
func publicServedKindMatches(kind, value string) bool {
	media, _, err := mime.ParseMediaType(value)
	if err != nil || !strings.Contains(media, "/") {
		return false
	}
	switch {
	case media == "text/html":
		return kind == "html"
	case pagecaps.ExecutableScriptType(media):
		return kind == "js"
	case media == "text/css":
		return kind == "css"
	case media == "application/wasm":
		return kind == "wasm"
	case strings.HasPrefix(media, "image/"):
		return kind == "image"
	case strings.HasPrefix(media, "font/"), media == "application/font-woff":
		return kind == "font"
	case strings.HasPrefix(media, "video/"), media == "application/vnd.apple.mpegurl":
		return kind == "video"
	case strings.HasPrefix(media, "model/"):
		return kind == "model"
	case media == "application/octet-stream", media == "application/json":
		return kind == "other" || kind == "program" || kind == "model"
	default:
		return kind == "other"
	}
}

func TestCanonicalCatalogCoversEveryServedPublicFile(t *testing.T) {
	catalog := canonicalPublicCatalog(t)
	apps := map[string]bool{}
	for _, route := range catalog.Routes {
		apps[route.App] = true
	}
	for app := range apps {
		t.Run(app, func(t *testing.T) {
			var source string
			switch app {
			case "docs":
				source = filepath.Join(testRepoRoot(t), "examples/gosx-docs")
			case "scaffold":
				source = t.TempDir()
				files, err := scaffoldFilesForTemplate("example.com/public-catalog", initTemplateApp)
				if err != nil {
					t.Fatal(err)
				}
				for _, file := range files {
					if strings.HasPrefix(file.Path, "public/") {
						mustWriteFile(t, filepath.Join(source, file.Path), file.Contents)
					}
				}
			default:
				t.Fatalf("canonical app %s needs a production public-file inventory", app)
			}
			policy, err := bundlepolicy.LoadProjectPolicy(source)
			if err != nil {
				t.Fatal(err)
			}
			staged := filepath.Join(t.TempDir(), "public")
			cfg := bundlepolicy.Config{Allow: policy.Allow, AllowPublic: policy.AllowPublic, Exclude: policy.Exclude}
			if err := bundlepolicy.CopyTree(filepath.Join(source, "public"), staged, bundlepolicy.RootPublic, cfg); err != nil {
				t.Fatal(err)
			}
			differences, err := publicCatalogDifferences(app, staged, catalog)
			if err != nil || len(differences) != 0 {
				t.Fatalf("public catalog differs from production staging: %v\n%s", err, strings.Join(differences, "\n"))
			}
		})
	}
}

func TestPublicCatalogRejectsANewServedFile(t *testing.T) {
	public := t.TempDir()
	mustWriteFile(t, filepath.Join(public, "new.css"), "body{}")
	catalog := publicCatalog{}
	differences, err := publicCatalogDifferences("scaffold", public, catalog)
	if err != nil || len(differences) != 1 || differences[0] != "unregistered: app/scaffold/public/new.css" {
		t.Fatalf("new public file escaped completeness check: %v %v", differences, err)
	}
	catalog.AssetRules = append(catalog.AssetRules, struct {
		ID    string `json:"id"`
		Owner string `json:"owner"`
		Kind  string `json:"kind"`
	}{"app/scaffold/public/new.css", "app", "css"})
	differences, err = publicCatalogDifferences("scaffold", public, catalog)
	if err != nil || len(differences) != 0 {
		t.Fatalf("registered file rejected: %v %v", differences, err)
	}
}

func TestPublicCatalogRejectsWrongSemanticKind(t *testing.T) {
	public := t.TempDir()
	mustWriteFile(t, filepath.Join(public, "entry.js"), `import "/hidden.js";`)
	var catalog publicCatalog
	if err := json.Unmarshal([]byte(`{"assetRules":[{"id":"app/scaffold/public/entry.js","owner":"app","kind":"other"}]}`), &catalog); err != nil {
		t.Fatal(err)
	}
	differences, err := publicCatalogDifferences("scaffold", public, catalog)
	if err != nil || len(differences) != 1 || !strings.HasPrefix(differences[0], "wrong kind: app/scaffold/public/entry.js") {
		t.Fatalf("served script admitted as opaque: %v %v", differences, err)
	}
	catalog.AssetRules[0].Kind = "js"
	differences, err = publicCatalogDifferences("scaffold", public, catalog)
	if err != nil || len(differences) != 0 {
		t.Fatalf("correct script declaration rejected: %v %v", differences, err)
	}
}
