package regularfile

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenRegularFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "body"), []byte("regular body"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	file, err := Open(root, "body")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	body, err := io.ReadAll(file)
	if err != nil || string(body) != "regular body" {
		t.Fatal("regular body did not round trip", err)
	}
}

func TestOpenRejectsRegularFileReplacement(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"body", "replacement"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("same bytes"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	before, err := root.Lstat("body")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, "replacement"), filepath.Join(dir, "body")); err != nil {
		t.Fatal(err)
	}
	file, err := openChecked(root, "body", before)
	if file != nil {
		file.Close()
	}
	if !errors.Is(err, ErrUnsafe) || file != nil {
		t.Fatal("changed inode with identical content was accepted", err)
	}
}
