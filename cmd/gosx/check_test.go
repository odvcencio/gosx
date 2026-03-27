package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunCheckAcceptsModernGSXShapes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "page.gsx")
	writeTempFile(t, dir, "page.gsx", `package main

func Page(item Item, ok bool) Node {
	return <article>
		<div class="card">
			<Link href={item.EditHref} class="btn btn-sm">Edit</Link>
			<Link href={item.ViewHref} class="btn btn-sm">View</Link>
		</div>
		<If when={ok}>
			<div class="empty">foo=bar</div>
		</If>
		<p>alpha & beta</p>
		<Demo.ThemeSwitcher></Demo.ThemeSwitcher>
		<Avatar userId={item.AuthorID} />
	</article>
}
`)

	var stderr bytes.Buffer
	if err := runCheck(path, &stderr); err != nil {
		t.Fatalf("runCheck failed: %v", err)
	}

	output := stderr.String()
	if !strings.Contains(output, "ok: 1 components") {
		t.Fatalf("unexpected check output: %q", output)
	}
	if !strings.Contains(output, "Page(Item)") {
		t.Fatalf("expected component signature in output: %q", output)
	}
}

func TestRunCheckReportsReadError(t *testing.T) {
	var stderr bytes.Buffer
	err := runCheck(filepath.Join(t.TempDir(), "missing.gsx"), &stderr)
	if err == nil {
		t.Fatal("expected missing file error")
	}
	if !strings.Contains(err.Error(), "read") {
		t.Fatalf("unexpected error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected no stderr output on failure, got %q", stderr.String())
	}
}

func TestRunCheckAcceptsDocsAppPages(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "examples", "gosx-docs", "app"))
	if err != nil {
		t.Fatal(err)
	}

	var checked int
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || filepath.Ext(path) != ".gsx" {
			return nil
		}

		var stderr bytes.Buffer
		if err := runCheck(path, &stderr); err != nil {
			t.Fatalf("runCheck(%s) failed: %v", path, err)
		}
		if !strings.Contains(stderr.String(), "ok: ") {
			t.Fatalf("unexpected check output for %s: %q", path, stderr.String())
		}
		checked++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked == 0 {
		t.Fatal("expected to check docs app GSX files")
	}
}
