// Command check-repo-hygiene-manifest verifies the generated client bundle
// artifacts promised by client/js/bootstrap-src/chunks.json.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

type chunkManifest struct {
	Chunks []chunk `json:"chunks"`
}

type chunk struct {
	Name string `json:"name"`
}

var chunkNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*\.js$`)

func main() {
	root := flag.String("root", ".", "repository root")
	flag.Parse()

	repoRoot, err := gitOutput(*root, "rev-parse", "--show-toplevel")
	if err != nil {
		fmt.Fprintf(os.Stderr, "check-repo-hygiene-manifest: resolve repo root: %v\n", err)
		os.Exit(1)
	}
	repoRoot = strings.TrimSpace(repoRoot)

	var failures int
	fail := func(format string, args ...any) {
		fmt.Fprintf(os.Stderr, "check-repo-hygiene-manifest: "+format+"\n", args...)
		failures++
	}

	manifestPath := filepath.Join(repoRoot, filepath.FromSlash("client/js/bootstrap-src/chunks.json"))
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		fail("read client/js/bootstrap-src/chunks.json: %v", err)
		finish(failures)
	}

	var manifest chunkManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		fail("parse client/js/bootstrap-src/chunks.json as JSON: %v", err)
		finish(failures)
	}
	if len(manifest.Chunks) == 0 {
		fail("client/js/bootstrap-src/chunks.json declares no chunks")
		finish(failures)
	}

	if mode, ok := requireTrackedMode(repoRoot, "client/js/bootstrap-src/chunks.json", fail); ok && mode != "100644" {
		fail("client/js/bootstrap-src/chunks.json has mode %s; want 100644", mode)
	}

	seen := map[string]bool{}
	required := 0
	for i, entry := range manifest.Chunks {
		name := entry.Name
		switch {
		case name == "":
			fail("chunk %d has empty name", i)
			continue
		case !strings.HasSuffix(name, ".js"):
			fail("chunk %q must end in .js", name)
			continue
		case strings.Contains(name, "/") || strings.Contains(name, "\\") || strings.Contains(name, "..") || strings.HasPrefix(name, "."):
			fail("chunk %q is not a safe top-level generated JS entrypoint name", name)
			continue
		case !chunkNamePattern.MatchString(name):
			fail("chunk %q is not an allowed generated JS entrypoint name; want ^[A-Za-z0-9][A-Za-z0-9._-]*\\.js$", name)
			continue
		case seen[name]:
			fail("duplicate generated JS entrypoint in client/js/bootstrap-src/chunks.json: %s", name)
			continue
		}
		seen[name] = true
		for _, suffix := range []string{"", ".br", ".gz", ".map"} {
			artifact := "client/js/" + name + suffix
			required++
			if mode, ok := requireTrackedMode(repoRoot, artifact, fail); ok && mode != "100644" {
				fail("%s has mode %s; want 100644", artifact, mode)
			}
		}
	}

	if failures == 0 {
		fmt.Printf("check-repo-hygiene-manifest: %d chunks, %d generated artifacts verified\n", len(manifest.Chunks), required)
	}
	finish(failures)
}

func finish(failures int) {
	if failures != 0 {
		os.Exit(1)
	}
}

func requireTrackedMode(root, path string, fail func(string, ...any)) (string, bool) {
	mode, ok, err := trackedMode(root, path)
	if err != nil {
		fail("%v", err)
		return "", false
	}
	if !ok {
		fail("required tracked file missing: %s", path)
		return "", false
	}
	return mode, true
}

func trackedMode(root, path string) (string, bool, error) {
	out, err := gitOutput(root, "ls-files", "-s", "--", ":(top,literal)"+path)
	if err != nil {
		return "", false, fmt.Errorf("inspect index for %s: %v", path, err)
	}
	if strings.TrimSpace(out) == "" {
		return "", false, nil
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != 1 {
		return "", false, fmt.Errorf("%s has %d index entries; want exactly 1", path, len(lines))
	}
	meta, trackedPath, ok := strings.Cut(lines[0], "\t")
	if !ok || trackedPath != path {
		return "", false, fmt.Errorf("%s has unexpected git ls-files output: %q", path, lines[0])
	}
	fields := strings.Fields(meta)
	if len(fields) != 3 {
		return "", false, fmt.Errorf("%s has unexpected git ls-files metadata: %q", path, meta)
	}
	if fields[2] != "0" {
		return "", false, fmt.Errorf("%s has index stage %s; want stage 0 exactly", path, fields[2])
	}
	return fields[0], true, nil
}

func gitOutput(root string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
