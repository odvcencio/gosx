package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"m31labs.dev/gosx/internal/bundlepolicy"
)

type publicCatalog struct {
	Routes     []struct{ App string } `json:"routes"`
	AssetRules []struct {
		ID    string `json:"id"`
		Owner string `json:"owner"`
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
	registered := map[string]bool{}
	prefix := "app/" + app + "/public/"
	for _, rule := range catalog.AssetRules {
		if strings.HasPrefix(rule.ID, prefix) {
			if rule.Owner != "app" {
				return nil, fmt.Errorf("public asset is not app-owned: %s", rule.ID)
			}
			registered[strings.TrimPrefix(rule.ID, prefix)] = true
		}
	}
	differences := []string{}
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
		if !registered[rel] {
			differences = append(differences, "unregistered: "+prefix+rel)
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
	}{"app/scaffold/public/new.css", "app"})
	differences, err = publicCatalogDifferences("scaffold", public, catalog)
	if err != nil || len(differences) != 0 {
		t.Fatalf("registered file rejected: %v %v", differences, err)
	}
}
