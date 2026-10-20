//go:build linux

package budgetci

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// These use the existing synthetic compiler, real child processes, and HTTP
// readiness. Absolute compiler scratch isolates the relative source regression
// so a successful build must also launch the docs server and collect a response.
func TestProductionRelativeSourceBuildsAndServes(t *testing.T) {
	for _, spelling := range []string{"dot", "subdirectory"} {
		t.Run(spelling, func(t *testing.T) {
			fixture, sha := productionFixture(t)
			cwd, root := fixture.SourceRoot, "."
			if spelling == "subdirectory" {
				cwd, root = filepath.Dir(cwd), filepath.Base(cwd)
			}
			t.Chdir(cwd)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			compiler, err := BuildCompiler(ctx, root, sha, t.TempDir())
			if err != nil {
				t.Fatal("relative source compiler failed", err)
			}
			defer compiler.Close()
			app, err := compiler.BuildApplication(ctx, "docs", t.TempDir())
			if err != nil {
				t.Fatal("relative source application failed", err)
			}
			defer app.Close()
			assertProductionPathsAndReadiness(t, ctx, compiler, app)
		})
	}
}

func TestProductionRelativeScratchBuildsAndServes(t *testing.T) {
	for _, appName := range []string{"docs", "scaffold"} {
		t.Run(appName, func(t *testing.T) {
			fixture, sha := productionFixture(t)
			t.Chdir(filepath.Dir(fixture.SourceRoot))
			cwd, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			scratch, err := filepath.Rel(cwd, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			// Include a redundant component to test cleaning at the boundary.
			scratch += string(filepath.Separator) + "."
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			compiler, err := BuildCompiler(ctx, fixture.SourceRoot, sha, scratch)
			if err != nil {
				t.Fatal("relative compiler scratch failed", err)
			}
			defer compiler.Close()
			app, err := compiler.BuildApplication(ctx, appName, scratch)
			if err != nil {
				t.Fatal("relative application scratch failed", err)
			}
			defer app.Close()
			assertProductionPathsAndReadiness(t, ctx, compiler, app)
		})
	}
}

// Test application scratch independently of compiler scratch; otherwise a
// compiler failure can hide the second API's incorrect relative path handling.
func TestProductionRelativeApplicationScratch(t *testing.T) {
	compiler, _ := productionFixture(t)
	t.Chdir(filepath.Dir(compiler.SourceRoot))
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	scratch, err := filepath.Rel(cwd, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	app, err := compiler.BuildApplication(ctx, "scaffold", scratch)
	if err != nil {
		t.Fatal("relative application scratch failed", err)
	}
	defer app.Close()
	assertProductionPathsAndReadiness(t, ctx, compiler, app)
}

func assertProductionPathsAndReadiness(t *testing.T, ctx context.Context, c *Compiler, a *Application) {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	log, err := filepath.Rel(cwd, filepath.Join(t.TempDir(), "server.log"))
	if err != nil {
		t.Fatal(err)
	}
	server, err := a.Serve(ctx, log)
	if err != nil {
		t.Fatal("built application did not launch and become ready", err)
	}
	defer server.Close()
	for name, path := range map[string]string{
		"source": c.SourceRoot, "compiler": c.Path, "compiler scratch": c.owned,
		"app": a.Root, "dist": a.DistDir, "application output": a.owned,
		"server executable": server.cmd.Path, "server argv": server.cmd.Args[0],
		"server directory": server.cmd.Dir, "server log": server.log.Name(),
	} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			t.Errorf("%s path was not made absolute and clean", name)
		}
	}
	response, err := server.Client.Get(server.BaseURL + "/environment")
	if err != nil {
		t.Fatal("ready server did not serve a fixture response", err)
	}
	data, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	var got struct{ Root string }
	if readErr != nil || closeErr != nil || json.Unmarshal(data, &got) != nil || !filepath.IsAbs(got.Root) || got.Root != a.DistDir || server.cmd.Dir != a.DistDir {
		t.Fatal("server resolved its application root differently")
	}
}
