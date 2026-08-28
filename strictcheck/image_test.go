package strictcheck

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// imageFixture wraps tag in a LEGACY (func, not strict `component`) .gsx
// page -- gosx#201's whole reason for placing validateImageContract before
// the packageHasStrict early return: the real consumer surface
// (gridiron-2000) compiles as legacy syntax, so every test here proves the
// check runs without any strict syntax present anywhere in the package.
func imageFixture(tag string) string {
	return "package docs\n\nfunc Page() Node {\n\treturn " + tag + "\n}\n"
}

// writeTestPNG writes a real, decodable w x h PNG to path so
// imagepipe.Probe (rule 2) succeeds against it, mirroring how a real
// public/ asset would look.
func writeTestPNG(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 0, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode test png: %v", err)
	}
	mustWrite(t, path, buf.String())
}

func checkImageFixture(t *testing.T, dir, tag string) error {
	t.Helper()
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, path, imageFixture(tag))
	return CheckFile(context.Background(), path)
}

// --- Accepts ---------------------------------------------------------

func TestImageContractAcceptsLocalSrcWithNoWidthOrHeight(t *testing.T) {
	dir := newTestModule(t)
	writeTestPNG(t, filepath.Join(dir, "public", "hero.png"), 4, 4)

	if err := checkImageFixture(t, dir, `<Image src="/hero.png" alt="Hero" />`); err != nil {
		t.Fatalf("expected a local src to need no width/height, got %v", err)
	}
}

func TestImageContractAcceptsLocalSVGWithNoWidthOrHeight(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "public", "mark.svg"), `<svg xmlns="http://www.w3.org/2000/svg"></svg>`)

	if err := checkImageFixture(t, dir, `<Image src="/mark.svg" alt="Mark" />`); err != nil {
		t.Fatalf("expected a local SVG src to be accepted on existence alone, got %v", err)
	}
}

func TestImageContractAcceptsExternalSrcWithExplicitWidthAndHeight(t *testing.T) {
	dir := newTestModule(t)

	if err := checkImageFixture(t, dir, `<Image src="https://cdn.example.com/hero.jpg" alt="Hero" width={800} height={600} />`); err != nil {
		t.Fatalf("expected an external src with explicit dimensions to be accepted, got %v", err)
	}
}

func TestImageContractAcceptsDynamicSrcWithExplicitWidthAndHeight(t *testing.T) {
	dir := newTestModule(t)

	if err := checkImageFixture(t, dir, `<Image src={data.heroURL} alt="Hero" width={800} height={600} />`); err != nil {
		t.Fatalf("expected a dynamic src with explicit dimensions to be accepted, got %v", err)
	}
}

func TestImageContractAcceptsDataURISrcWithNoWidthOrHeight(t *testing.T) {
	dir := newTestModule(t)

	if err := checkImageFixture(t, dir, `<Image src="data:image/png;base64,aaaa" alt="Inline" />`); err != nil {
		t.Fatalf("expected a data: URI src to need no width/height, got %v", err)
	}
}

func TestImageContractAcceptsExplicitProducibleFormat(t *testing.T) {
	dir := newTestModule(t)
	writeTestPNG(t, filepath.Join(dir, "public", "hero.png"), 4, 4)

	for _, format := range []string{"jpeg", "jpg", "png", "gif"} {
		t.Run(format, func(t *testing.T) {
			tag := `<Image src="/hero.png" alt="Hero" format="` + format + `" />`
			if err := checkImageFixture(t, dir, tag); err != nil {
				t.Fatalf("expected format %q to be accepted, got %v", format, err)
			}
		})
	}
}

func TestImageContractAcceptsDynamicFormat(t *testing.T) {
	dir := newTestModule(t)
	writeTestPNG(t, filepath.Join(dir, "public", "hero.png"), 4, 4)

	if err := checkImageFixture(t, dir, `<Image src="/hero.png" alt="Hero" format={data.format} />`); err != nil {
		t.Fatalf("expected a dynamic format value to be exempt at check time, got %v", err)
	}
}

// TestImageContractAcceptsSpreadSuppliedFieldsWithoutNamedAttrs proves the
// real, already-tested fixture shape
// (route/filesystem_test.go's TestDefaultFileRendererSupportsImageBuiltin:
// `{...data.image}` alone supplying src/width/height/widths, no named src
// or dimension attributes at all) keeps passing: a spread might supply
// alt, width, and height in a way this check cannot see, so none of them
// is required by name when a spread is present.
func TestImageContractAcceptsSpreadSuppliedFieldsWithoutNamedAttrs(t *testing.T) {
	dir := newTestModule(t)

	if err := checkImageFixture(t, dir, `<Image sizes="100vw" {...data.image} />`); err != nil {
		t.Fatalf("expected a spread-only <Image> to be accepted, got %v", err)
	}
}

// --- Rejects: exact messages ------------------------------------------

func TestImageContractRejectsMissingAlt(t *testing.T) {
	dir := newTestModule(t)
	writeTestPNG(t, filepath.Join(dir, "public", "hero.png"), 4, 4)

	err := checkImageFixture(t, dir, `<Image src="/hero.png" />`)
	requireImageDiagnostic(t, err, `gosx: Image requires a non-empty "alt" attribute`)
}

func TestImageContractRejectsEmptyAlt(t *testing.T) {
	dir := newTestModule(t)
	writeTestPNG(t, filepath.Join(dir, "public", "hero.png"), 4, 4)

	err := checkImageFixture(t, dir, `<Image src="/hero.png" alt="" />`)
	requireImageDiagnostic(t, err, `gosx: Image requires a non-empty "alt" attribute`)
}

func TestImageContractRejectsLocalSrcNamingNoFile(t *testing.T) {
	dir := newTestModule(t)
	// public/ exists (so the rule is active) but hero.png does not.
	mustMkdir(t, filepath.Join(dir, "public"))

	err := checkImageFixture(t, dir, `<Image src="/hero.png" alt="Hero" />`)
	requireImageDiagnostic(t, err, `gosx: Image src "/hero.png" does not name a readable image file under public/`)
}

func TestImageContractRejectsLocalSrcThatIsNotADecodableImage(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "public", "hero.png"), "not a real png")

	err := checkImageFixture(t, dir, `<Image src="/hero.png" alt="Hero" />`)
	requireImageDiagnostic(t, err, `gosx: Image src "/hero.png" does not name a readable image file under public/`)
}

func TestImageContractRejectsExternalSrcMissingWidth(t *testing.T) {
	dir := newTestModule(t)

	err := checkImageFixture(t, dir, `<Image src="https://cdn.example.com/hero.jpg" alt="Hero" height={600} />`)
	requireImageDiagnostic(t, err, `gosx: Image src "https://cdn.example.com/hero.jpg" is external and requires explicit width and height`)
}

func TestImageContractRejectsExternalSrcMissingHeight(t *testing.T) {
	dir := newTestModule(t)

	err := checkImageFixture(t, dir, `<Image src="https://cdn.example.com/hero.jpg" alt="Hero" width={800} />`)
	requireImageDiagnostic(t, err, `gosx: Image src "https://cdn.example.com/hero.jpg" is external and requires explicit width and height`)
}

func TestImageContractRejectsProtocolRelativeSrcMissingDimensions(t *testing.T) {
	dir := newTestModule(t)

	err := checkImageFixture(t, dir, `<Image src="//cdn.example.com/hero.jpg" alt="Hero" />`)
	requireImageDiagnostic(t, err, `is external and requires explicit width and height`)
}

func TestImageContractRejectsDynamicSrcMissingDimensions(t *testing.T) {
	dir := newTestModule(t)

	err := checkImageFixture(t, dir, `<Image src={data.heroURL} alt="Hero" />`)
	requireImageDiagnostic(t, err, `gosx: Image src is dynamic and requires explicit width and height`)
}

func TestImageContractRejectsMissingSrcEntirely(t *testing.T) {
	dir := newTestModule(t)

	err := checkImageFixture(t, dir, `<Image alt="Hero" />`)
	requireImageDiagnostic(t, err, `gosx: Image src is dynamic and requires explicit width and height`)
}

func TestImageContractRejectsUnproducibleFormat(t *testing.T) {
	dir := newTestModule(t)
	writeTestPNG(t, filepath.Join(dir, "public", "hero.png"), 4, 4)

	err := checkImageFixture(t, dir, `<Image src="/hero.png" alt="Hero" format="avif" />`)
	requireImageDiagnostic(t, err, `gosx: Image format "avif" is not a producible output format (want jpeg, png, or gif)`)
}

// TestImageContractRejectsUnproducibleFormatWebP covers gosx's excision of
// its WebP encoder dependency: format="webp" still fails check-time, but
// the message now names the real situation honestly -- gosx ships no
// built-in WebP encoder, and points at the registered-Encoder extension
// point (imagepipe.RegisterEncoder), rather than treating "webp" as just
// another unrecognized format value like TestImageContractRejectsUnproducibleFormat's
// "avif".
func TestImageContractRejectsUnproducibleFormatWebP(t *testing.T) {
	dir := newTestModule(t)
	writeTestPNG(t, filepath.Join(dir, "public", "hero.png"), 4, 4)

	err := checkImageFixture(t, dir, `<Image src="/hero.png" alt="Hero" format="webp" />`)
	requireImageDiagnostic(t, err, `gosx: Image format "webp" is not producible: gosx ships no built-in WebP encoder (want jpeg, png, or gif); register an imagepipe.Encoder for build-time WebP variants, or omit format to use the source's own format`)
}

// TestImageContractAccumulatesMultipleViolationsOnOneNode proves every
// rule a single <Image> node fails is reported in one check run, not just
// the first.
func TestImageContractAccumulatesMultipleViolationsOnOneNode(t *testing.T) {
	dir := newTestModule(t)

	err := checkImageFixture(t, dir, `<Image src="https://cdn.example.com/hero.jpg" format="avif" />`)
	if err == nil {
		t.Fatal("expected findings")
	}
	message := err.Error()
	for _, want := range []string{
		`gosx: Image requires a non-empty "alt" attribute`,
		`gosx: Image src "https://cdn.example.com/hero.jpg" is external and requires explicit width and height`,
		`gosx: Image format "avif" is not a producible output format`,
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("expected %q in accumulated message:\n%s", want, message)
		}
	}
}

// TestImageContractRunsWithoutAnyStrictSyntaxInThePackage is the
// placement proof gosx#201 explains: the check must run for a package
// with no `component` declaration anywhere, mirroring the real consumer
// surface. imageFixture already builds a legacy `func` component; this
// test only makes the intent explicit and pins it against regression if a
// future change moves the call site back after packageHasStrict.
func TestImageContractRunsWithoutAnyStrictSyntaxInThePackage(t *testing.T) {
	dir := newTestModule(t)
	src, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil || len(src) == 0 {
		t.Fatalf("test module fixture missing go.mod: %v", err)
	}

	err = checkImageFixture(t, dir, `<Image src="https://cdn.example.com/hero.jpg" alt="" />`)
	requireImageDiagnostic(t, err, `gosx: Image requires a non-empty "alt" attribute`)
}

// TestImageContractCheckTreeResolvesPublicDirFromTreeRoot proves the
// public/ resolution also works through CheckTree (the path
// cmd/gosx's checkStrictProject uses), not only CheckFile's best-effort
// go.mod search.
func TestImageContractCheckTreeResolvesPublicDirFromTreeRoot(t *testing.T) {
	dir := newTestModule(t)
	writeTestPNG(t, filepath.Join(dir, "public", "hero.png"), 4, 4)
	mustWrite(t, filepath.Join(dir, "page.gsx"), imageFixture(`<Image src="/hero.png" alt="Hero" />`))

	if err := CheckTree(context.Background(), dir); err != nil {
		t.Fatalf("expected a real local file to be accepted through CheckTree, got %v", err)
	}

	mustWrite(t, filepath.Join(dir, "page.gsx"), imageFixture(`<Image src="/missing.png" alt="Hero" />`))
	err := CheckTree(context.Background(), dir)
	requireImageDiagnostic(t, err, `gosx: Image src "/missing.png" does not name a readable image file under public/`)
}

// TestImageContractRejectsImageInsideIsland proves the check-time island
// rejection (gosx#201, ir/validate.go's unsupportedIslandComponentDiagnostic)
// surfaces through strictcheck's own entry point: CheckFile loads the
// package via transpile.LoadPackage, which compiles every file through
// gosx.Compile -- the same call that runs ir.Validate and fails closed on
// an island-nested <Image> before validateImageContract, or any other
// strictcheck stage, ever sees the program.
func TestImageContractRejectsImageInsideIsland(t *testing.T) {
	dir := newTestModule(t)
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, path, `package docs

//gosx:island
func Gallery() Node {
	return <Image src="/hero.png" alt="Hero" width={800} height={600} />
}
`)

	err := CheckFile(context.Background(), path)
	if err == nil {
		t.Fatal("expected <Image> inside an island to fail check")
	}
	for _, snippet := range []string{"<Image>", "not supported inside island components", "plain <img>"} {
		if !strings.Contains(err.Error(), snippet) {
			t.Fatalf("expected %q in error, got %v", snippet, err)
		}
	}
}

func requireImageDiagnostic(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error containing %q, got nil", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("expected %q in error, got %v", want, err)
	}
}
