package route

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProductionFileCachesFreezeAndDevelopmentChecksFiles(t *testing.T) {
	t.Setenv("GOSX_ENV", "production")
	root := t.TempDir()
	path := filepath.Join(root, "page.gsx")
	if err := os.WriteFile(path, []byte("package p\nfunc Page() Node {\n return <p>first</p>\n}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	first, err := loadCachedGSXProgram(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if got, err := loadCachedGSXProgram(path); err != nil || got != first {
		t.Fatal("production template was read again")
	}
	htmlPath := filepath.Join(root, "layout.html")
	if err := os.WriteFile(htmlPath, []byte("<section>first</section>"), 0600); err != nil {
		t.Fatal(err)
	}
	source, err := loadCachedHTMLSource(htmlPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(htmlPath); err != nil {
		t.Fatal(err)
	}
	if got, err := loadCachedHTMLSource(htmlPath); err != nil || got != source {
		t.Fatal("production HTML was read again")
	}
	cssPath := filepath.Join(root, "page.css")
	if _, ok := statCSSFile(cssPath); ok {
		t.Fatal("missing CSS exists")
	}
	if err := os.WriteFile(cssPath, []byte("p {color:blue}"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, ok := statCSSFile(cssPath); ok {
		t.Fatal("production negative CSS lookup changed")
	}
	if got := sidecarMetadataPath(path); got != "" {
		t.Fatal("missing metadata exists")
	}
	if err := os.WriteFile(filepath.Join(root, "page.meta.json"), []byte(`{"title":{"absolute":"New"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if got := sidecarMetadataPath(path); got != "" {
		t.Fatal("production negative metadata lookup changed")
	}
	t.Setenv("GOSX_ENV", "development")
	if _, err := loadCachedGSXProgram(path); err == nil {
		t.Fatal("development template removal ignored")
	}
	if _, err := loadCachedHTMLSource(htmlPath); err == nil {
		t.Fatal("development HTML removal ignored")
	}
	if _, ok := statCSSFile(cssPath); !ok {
		t.Fatal("development CSS addition ignored")
	}
	if got := sidecarMetadataPath(path); got == "" {
		t.Fatal("development metadata addition ignored")
	}
}
