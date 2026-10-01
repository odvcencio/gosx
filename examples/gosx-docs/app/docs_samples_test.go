package docs

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"

	docsamples "m31labs.dev/gosx/examples/gosx-docs/samples"
)

var docsSampleReference = regexp.MustCompile(`[A-Za-z]+\.DocSample\("([^"]+\.sample)"\)`)

func TestEveryDocumentationSampleIsEmbeddedAndUsed(t *testing.T) {
	t.Parallel()

	root := "docs"
	references := make([]string, 0, 177)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || (entry.Name() != "page.gsx" && entry.Name() != "page.server.go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, match := range docsSampleReference.FindAllSubmatch(body, -1) {
			references = append(references, string(match[1]))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	files := docsamples.Files()
	sort.Strings(references)
	if len(references) != len(files) {
		t.Fatalf("documentation references %d samples, embedded tree contains %d", len(references), len(files))
	}
	for index, path := range files {
		if references[index] != path {
			t.Errorf("sample reference %q does not match embedded file %q", references[index], path)
		}
		body, err := docsamples.Read(path)
		if err != nil {
			t.Errorf("read embedded sample %q: %v", path, err)
			continue
		}
		if body == "" {
			t.Errorf("documentation sample %q is empty", path)
		}
	}
}
