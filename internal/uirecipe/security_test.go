package uirecipe

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"
	"unicode"
)

func TestPortablePaths(t *testing.T) {
	for _, value := range []string{"C:/escape.css", "C:escape.css", "a/NUL.css", "a/con", "COM1/x.css", "lpt9.css", "com¹.css", "a/name.", "a/name ", "a/x\x00.css", "a/x\x1b.css", "a/x\r.css", "a/x\u202e.css", "a/x:stream", "/absolute", "../up", "a\\b", "a//b", "a/./b", "a/../b", "a/aux.txt", "a/CONOUT$", "a/?", "a/*", "a/\xff"} {
		t.Run(fmt.Sprintf("%q", value), func(t *testing.T) {
			if validateCatalogPath("target", value) == nil {
				t.Fatalf("accepted %q", value)
			}
		})
	}
	for _, value := range []string{"app/ui/button.gsx", "public/ui/tokens.css", ".gosx/ui/manifest.json", "a/compile.css"} {
		if err := validateCatalogPath("target", value); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStrictSingleJSONDocuments(t *testing.T) {
	for _, data := range []string{`{"version":1,"version":2}`, `{"a":{"x":1,"x":2}}`} {
		var target any
		if err := decodeDocument([]byte(data), &target); err == nil {
			t.Fatalf("accepted duplicate members: %s", data)
		}
	}
	for _, trailing := range []string{"{}", "null", "false", "garbage"} {
		var target struct {
			Version int `json:"version"`
		}
		if err := decodeDocument([]byte(`{"version":1}`+trailing), &target); err == nil {
			t.Fatalf("accepted trailing %s", trailing)
		}
		fsys := fstest.MapFS{"manifest.json": &fstest.MapFile{Data: []byte(testCatalogManifest(`{"name":"x","version":"1.0.0","description":"x","files":[{"source":"x.css","target":"public/ui/x.css"}]}`) + trailing)}, "x.css": &fstest.MapFile{Data: []byte(".x {}\n")}}
		if _, err := loadCatalog(fsys); err == nil {
			t.Fatalf("catalog accepted trailing %s", trailing)
		}
	}
}

func TestInstalledManifestRejectsInvalidOwnership(t *testing.T) {
	catalog := mustCatalog(t)
	closure, _ := catalog.closure("button")
	base := catalog.mergeInstalledManifest(installedManifest{}, closure)
	cases := map[string]func(*installedManifest){
		"source":             func(m *installedManifest) { m.Source = "elsewhere" },
		"license":            func(m *installedManifest) { m.License = "other" },
		"provenance":         func(m *installedManifest) { m.Provenance = "unknown" },
		"future catalog":     func(m *installedManifest) { m.CatalogVersion = "1.1.0" },
		"catalog traversal":  func(m *installedManifest) { m.CatalogVersion = "../1" },
		"old major":          func(m *installedManifest) { m.CatalogVersion = "0.9.0" },
		"unknown recipe":     func(m *installedManifest) { m.Recipes[0].Name = "unknown" },
		"duplicate recipe":   func(m *installedManifest) { m.Recipes = append(m.Recipes, m.Recipes[0]) },
		"future recipe":      func(m *installedManifest) { m.Recipes[0].Version = "1.0.1" },
		"invalid recipe":     func(m *installedManifest) { m.Recipes[0].Version = "1" },
		"unknown file":       func(m *installedManifest) { m.Recipes[0].Files[0].Path = "app/ui/unowned.gsx" },
		"other recipe file":  func(m *installedManifest) { m.Recipes[0].Files[0].Path = "public/ui/tokens.css" },
		"duplicate file":     func(m *installedManifest) { m.Recipes[0].Files[1] = m.Recipes[0].Files[0] },
		"missing file":       func(m *installedManifest) { m.Recipes[0].Files = m.Recipes[0].Files[:1] },
		"invalid hash":       func(m *installedManifest) { m.Recipes[0].Files[0].SHA256 = strings.Repeat("g", 64) },
		"wrong hash":         func(m *installedManifest) { m.Recipes[0].Files[0].SHA256 = strings.Repeat("0", 64) },
		"missing dependency": func(m *installedManifest) { m.Recipes = m.Recipes[:1] },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			data, _ := json.Marshal(base)
			var value installedManifest
			json.Unmarshal(data, &value)
			mutate(&value)
			if err := catalog.validateInstalledManifest(value); err == nil {
				t.Fatal("accepted invalid manifest")
			}
		})
	}
	root := testAppRoot(t)
	if _, err := catalog.Add(root, "button", AddOptions{}); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(root, filepath.FromSlash(installedManifestPath))
	data := append(mustRead(t, manifestPath), []byte("{}")...)
	if err := os.WriteFile(manifestPath, data, 0644); err != nil {
		t.Fatal(err)
	}
	before := sourceSnapshot(t, root)
	if _, err := catalog.Add(root, "card", AddOptions{}); err == nil {
		t.Fatal("Add accepted trailing manifest document")
	}
	if !reflect.DeepEqual(before, sourceSnapshot(t, root)) {
		t.Fatal("invalid manifest caused a partial install")
	}
}

func TestInstallRollsBackEveryWriteFailure(t *testing.T) {
	for _, update := range []bool{false, true} {
		for _, phase := range []string{"stage", "install"} {
			for failAt := 0; failAt < 4; failAt++ {
				t.Run(fmt.Sprintf("update=%v/%s/%d", update, phase, failAt), func(t *testing.T) {
					catalog := mustCatalog(t)
					root := testAppRoot(t)
					if update {
						if _, err := catalog.Add(root, "button", AddOptions{}); err != nil {
							t.Fatal(err)
						}
						catalog = advancedCatalog(catalog)
					}
					before := sourceSnapshot(t, root)
					triggered := false
					_, err := catalog.add(root, "button", AddOptions{Update: update}, func(p string, i int) error {
						if p == phase && i == failAt {
							triggered = true
							return errors.New("injected write failure")
						}
						return nil
					})
					if err == nil || !triggered {
						t.Fatalf("failure did not cause error: %v, triggered=%v", err, triggered)
					}
					if !reflect.DeepEqual(before, sourceSnapshot(t, root)) {
						t.Fatalf("partial source after %v", err)
					}
					assertNoRecoveryFiles(t, root)
				})
			}
		}
	}
}

func TestUnwritableLaterParentDoesNotLeaveSource(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions")
	}
	root := testAppRoot(t)
	public := filepath.Join(root, "public")
	if err := os.Mkdir(public, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(public, 0755) })
	probe, err := os.CreateTemp(public, "probe")
	if err == nil {
		probe.Close()
		os.Remove(probe.Name())
		t.Skip("current user bypasses write permissions")
	}
	before := sourceSnapshot(t, root)
	if _, err := mustCatalog(t).Add(root, "button", AddOptions{}); err == nil {
		t.Fatal("unwritable destination succeeded")
	}
	if !reflect.DeepEqual(before, sourceSnapshot(t, root)) {
		t.Fatal("unwritable later destination left source")
	}
}

func TestRollbackPreservesAnEditorChange(t *testing.T) {
	for _, update := range []bool{false, true} {
		t.Run(fmt.Sprint(update), func(t *testing.T) {
			root := testAppRoot(t)
			catalog := mustCatalog(t)
			if update {
				if _, err := catalog.Add(root, "button", AddOptions{}); err != nil {
					t.Fatal(err)
				}
				catalog = advancedCatalog(catalog)
			}
			changedPath := filepath.Join(root, "app/ui/button.gsx")
			_, err := catalog.add(root, "button", AddOptions{Update: update}, func(phase string, i int) error {
				if phase == "install" && i == 1 {
					if err := os.WriteFile(changedPath, []byte("editor change\n"), 0644); err != nil {
						return err
					}
					return errors.New("later write failed")
				}
				return nil
			})
			if err == nil || !strings.Contains(err.Error(), "changed after installation") {
				t.Fatalf("error=%v", err)
			}
			if got := string(mustRead(t, changedPath)); got != "editor change\n" {
				t.Fatalf("editor content lost: %q", got)
			}
			if _, err := os.Stat(filepath.Join(root, ".gosx/ui/transaction.json")); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPublicationPreservesNewFileAfterValidation(t *testing.T) {
	for _, update := range []bool{false, true} {
		t.Run(fmt.Sprint(update), func(t *testing.T) {
			root := testAppRoot(t)
			catalog := mustCatalog(t)
			if update {
				if _, err := catalog.Add(root, "button", AddOptions{}); err != nil {
					t.Fatal(err)
				}
				catalog = advancedCatalog(catalog)
			}
			target := filepath.Join(root, "app/ui/button.gsx")
			_, err := catalog.add(root, "button", AddOptions{Update: update}, func(phase string, i int) error {
				if phase == "publish" && i == 0 {
					return os.WriteFile(target, []byte("created by editor\n"), 0644)
				}
				return nil
			})
			if err == nil {
				t.Fatal("publish overwrote a newly created target")
			}
			if got := string(mustRead(t, target)); got != "created by editor\n" {
				t.Fatalf("editor content lost: %q", got)
			}
		})
	}
}

func TestRollbackFailureRetainsRecoverableOriginalsAndBlocksNextAdd(t *testing.T) {
	root := testAppRoot(t)
	catalog := mustCatalog(t)
	if _, err := catalog.Add(root, "button", AddOptions{}); err != nil {
		t.Fatal(err)
	}
	original := mustRead(t, filepath.Join(root, "app/ui/button.gsx"))
	future := advancedCatalog(catalog)
	_, err := future.add(root, "button", AddOptions{Update: true}, func(phase string, i int) error {
		if phase == "install" && i == 3 || phase == "rollback" && i == 0 {
			return errors.New("injected failure")
		}
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "manual recovery") {
		t.Fatalf("error=%v", err)
	}
	var journal struct {
		Files []recoveryFile `json:"files"`
	}
	if err := json.Unmarshal(mustRead(t, filepath.Join(root, ".gosx/ui/transaction.json")), &journal); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range journal.Files {
		if entry.Path == "app/ui/button.gsx" {
			found = true
			if !bytes.Equal(mustRead(t, filepath.Join(root, "app/ui", entry.Backup)), original) {
				t.Fatal("original backup was lost")
			}
		}
	}
	if !found {
		t.Fatal("missing recovery row")
	}
	before := sourceSnapshot(t, root)
	if _, err := future.Add(root, "card", AddOptions{}); err == nil || !strings.Contains(err.Error(), "unfinished UI transaction") {
		t.Fatalf("error=%v", err)
	}
	if !reflect.DeepEqual(before, sourceSnapshot(t, root)) {
		t.Fatal("new add changed interrupted transaction")
	}
}

func TestParentSwapCannotRedirectStagedWrites(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privilege depends on Windows developer mode")
	}
	for _, phase := range []string{"stage", "staged", "install"} {
		t.Run(phase, func(t *testing.T) {
			root := testAppRoot(t)
			outside := t.TempDir()
			catalog := mustCatalog(t)
			if _, err := catalog.Add(root, "button", AddOptions{}); err != nil {
				t.Fatal(err)
			}
			outsideFile := filepath.Join(outside, "button.css")
			os.WriteFile(outsideFile, []byte("outside sentinel\n"), 0644)
			swapped := false
			_, err := advancedCatalog(catalog).add(root, "button", AddOptions{Update: true}, func(p string, i int) error {
				if !swapped && p == phase {
					swapped = true
					if err := os.Rename(filepath.Join(root, "public/ui"), filepath.Join(root, "public/saved-ui")); err != nil {
						return err
					}
					return os.Symlink(outside, filepath.Join(root, "public/ui"))
				}
				return nil
			})
			if !swapped || err == nil {
				t.Fatalf("swap not rejected: %v, swapped=%v", err, swapped)
			}
			if got := string(mustRead(t, outsideFile)); got != "outside sentinel\n" {
				t.Fatalf("outside changed: %q", got)
			}
			entries, _ := os.ReadDir(outside)
			if len(entries) != 1 {
				t.Fatalf("outside received stage or backup files: %v", entries)
			}
			if got := mustRead(t, filepath.Join(root, "public/saved-ui/button.css")); !bytes.Equal(got, recipeContent(t, catalog, "button", ".css")) {
				t.Fatal("pinned original did not roll back")
			}
		})
	}
}

func TestTerminalDiffEscapesControlsAndBoundsWork(t *testing.T) {
	current := []byte("before\x1b[2J\x1b]52;c;copied\a\rspoof\u202eRTL\u2066iso\u009b2J\tend\n")
	patch := unifiedDiff(current, []byte("after\n"), "app/ui/button.gsx")
	for _, r := range patch {
		if r != '\n' && (unicode.IsControl(r) || unicode.Is(unicode.Cf, r)) {
			t.Fatalf("unsafe rune %U emitted", r)
		}
	}
	for _, want := range []string{`\u001b`, `\u0007`, `\u000d`, `\u202e`, `\u2066`, `\u009b`, `\u0009`} {
		if !strings.Contains(patch, want) {
			t.Fatalf("missing escaped control %q", want)
		}
	}
	for _, data := range [][]byte{[]byte("binary\x00secret"), []byte{0xff, 0xfe}, bytes.Repeat([]byte("a\n"), 5000), bytes.Repeat([]byte("x"), 100000)} {
		got := unifiedDiff(data, []byte("after\n"), "public/ui/x.css")
		if len(got) > 512 || !strings.Contains(got, "omitted") || !strings.Contains(got, "sha256:") {
			t.Fatalf("unbounded/noninformative summary: %d bytes", len(got))
		}
	}
	root := testAppRoot(t)
	catalog := mustCatalog(t)
	if _, err := catalog.Add(root, "tokens", AddOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "public/ui/tokens.css"), bytes.Repeat([]byte("x"), maxSourceBytes+1), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.Diff(root, "tokens"); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("oversized file was not bounded: %v", err)
	}
}

func TestConcurrentInstallProcess(t *testing.T) {
	root := os.Getenv("GOSX_UI_TEST_ROOT")
	if root == "" {
		return
	}
	catalog := mustCatalog(t)
	if _, err := catalog.Add(root, os.Getenv("GOSX_UI_TEST_RECIPE"), AddOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentProcessesPreserveAllManifestRows(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for round := 0; round < 20; round++ {
		root := testAppRoot(t)
		var commands []*exec.Cmd
		var outputs []*bytes.Buffer
		for _, name := range []string{"button", "card", "input"} {
			cmd := exec.Command(executable, "-test.run=^TestConcurrentInstallProcess$")
			cmd.Env = append(os.Environ(), "GOSX_UI_TEST_ROOT="+root, "GOSX_UI_TEST_RECIPE="+name)
			output := new(bytes.Buffer)
			cmd.Stdout = output
			cmd.Stderr = output
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			commands = append(commands, cmd)
			outputs = append(outputs, output)
		}
		for i, cmd := range commands {
			if err := cmd.Wait(); err != nil {
				t.Fatalf("round %d: %v: %s", round, err, outputs[i].String())
			}
		}
		app, err := openAppRoot(root)
		if err != nil {
			t.Fatal(err)
		}
		manifest, _, err := mustCatalog(t).readInstalledManifest(app)
		app.Close()
		if err != nil || len(manifest.Recipes) != 4 {
			t.Fatalf("round %d lost manifest rows: %+v, %v", round, manifest, err)
		}
	}
}

func advancedCatalog(catalog *Catalog) *Catalog {
	next := cloneCatalog(catalog)
	next.version = "1.1.0"
	for name, item := range next.recipes {
		item.Version = "1.1.0"
		for i := range item.files {
			item.files[i].Content = append(item.files[i].Content, []byte("/* catalog update */\n")...)
			item.files[i].SHA256 = contentHash(item.files[i].Content)
		}
		next.recipes[name] = item
	}
	return next
}

func sourceSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		rel, _ := filepath.Rel(root, name)
		if strings.HasPrefix(entry.Name(), ".gosx-ui-") || rel == filepath.FromSlash(".gosx/ui/install.lock") {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		out[rel] = fmt.Sprintf("%s/%o", contentHash(mustRead(t, name)), info.Mode().Perm())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func assertNoRecoveryFiles(t *testing.T, root string) {
	t.Helper()
	filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			t.Error(err)
			return nil
		}
		if strings.HasPrefix(entry.Name(), ".gosx-ui-") || entry.Name() == "transaction.json" {
			t.Errorf("left recovery file %s", name)
		}
		return nil
	})
}
