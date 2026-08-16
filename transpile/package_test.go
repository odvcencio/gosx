package transpile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

func packageTestFile(t *testing.T, name, source string) PackageFile {
	t.Helper()
	program, err := gosx.Compile([]byte(source))
	if err != nil {
		t.Fatalf("Compile fixture: %v", err)
	}
	return PackageFile{Path: name, Source: []byte(source), Program: program}
}

func TestStrictProjectionKeepsStrictFileCoherentAndPrunesLegacyImports(t *testing.T) {
	file := packageTestFile(t, "/project/page.gsx", `package app
import (
	legacy "example.test/legacy"
	clock "time"
	. "strings"
	gx "m31labs.dev/gosx"
)

type BadgeProps struct {
	Label string
	When clock.Time
}

const BadgeLimit = 3

func Legacy() Node {
	return <p>{legacy.Route(data.missing)}{ToUpper(request.name)}</p>
}

component Badge(props: BadgeProps) {
	return <span>{props.Label}</span>
}

component Page() {
	return <Badge label="ready" />
}
`)
	got, strict, err := StrictProjection(file)
	if err != nil {
		t.Fatalf("StrictProjection: %v", err)
	}
	if !strict {
		t.Fatal("strict declaration not detected")
	}
	for _, want := range []string{
		`clock "time"`,
		`gx "m31labs.dev/gosx"`,
		`func Badge(props BadgeProps) gx.Node`,
		`Badge(BadgeProps{Label: "ready"})`,
		`const BadgeLimit = 3`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("projection missing %q:\n%s", want, got)
		}
	}
	for _, excluded := range []string{`example.test/legacy`, `. "strings"`, `func Legacy`, `data.missing`, `request.name`} {
		if strings.Contains(got, excluded) {
			t.Fatalf("projection retained legacy-only %q:\n%s", excluded, got)
		}
	}
}

func TestStrictProjectionRetainsUsedDotGoSXImport(t *testing.T) {
	file := packageTestFile(t, "/project/page.gsx", `package app
import . "m31labs.dev/gosx"
component Page() {
	return <main>ok</main>
}
`)
	got, _, err := StrictProjection(file)
	if err != nil {
		t.Fatalf("StrictProjection: %v", err)
	}
	if !strings.Contains(got, `. "m31labs.dev/gosx"`) || !strings.Contains(got, `func Page() Node`) {
		t.Fatalf("dot import style lost:\n%s", got)
	}
}

func TestLoadPackageUsesTargetPackageBeforeAlphabeticalSibling(t *testing.T) {
	dir := t.TempDir()
	other := filepath.Join(dir, "a.gsx")
	target := filepath.Join(dir, "z.gsx")
	if err := os.WriteFile(other, []byte("package other\ncomponent ???"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("package target\ncomponent Page() {\nreturn <main />\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	files, err := LoadPackage(target)
	if err != nil {
		t.Fatalf("LoadPackage: %v", err)
	}
	if len(files) != 1 || files[0].Program.Package != "target" || files[0].Path != target {
		t.Fatalf("files = %#v", files)
	}
}

func TestCompileRejectsCrossFileStrictCallBeforePackageProjection(t *testing.T) {
	_, err := gosx.Compile([]byte(`package app
component Page() {
	return <Badge />
}
`))
	if err == nil || !strings.Contains(err.Error(), "may call only same-file strict components") {
		t.Fatalf("error = %v", err)
	}
}

func TestTranspilePackageStrictOwnerCannotBeHiddenByLegacyPeer(t *testing.T) {
	files := []PackageFile{
		packageTestFile(t, "/project/a_strict.gsx", `package app
component Badge() {
	return <strong>strict</strong>
}
`),
		packageTestFile(t, "/project/b_legacy.gsx", `package app
func Badge() Node {
	return <span>legacy</span>
}
`),
		packageTestFile(t, "/project/c_page.gsx", `package app
func Page() Node {
	return <Badge />
}
`),
	}
	_, err := TranspilePackage(files)
	if err == nil || (!strings.Contains(err.Error(), "cross-file strict component call") && !strings.Contains(err.Error(), "may call only same-file strict components")) {
		t.Fatalf("error = %v", err)
	}
}

func TestTranspilePackageLocalLegacyComponentShadowsStrictPeer(t *testing.T) {
	files := []PackageFile{
		packageTestFile(t, "/project/a_strict.gsx", `package app
component Badge() {
	return <strong>strict</strong>
}
`),
		packageTestFile(t, "/project/b_page.gsx", `package app
func Badge() Node {
	return <span>legacy</span>
}
func Page() Node {
	return <Badge />
}
`),
	}
	if _, err := TranspilePackage(files); err != nil {
		t.Fatalf("TranspilePackage: %v", err)
	}
}
