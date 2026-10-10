package budget

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
	"m31labs.dev/gosx/internal/assetmeasure"
)

func testMeasureHash(body []byte) string {
	hash := sha256.Sum256(body)
	return hex.EncodeToString(hash[:])
}

// Algorithm tests inject encoders without claiming a certified toolchain. The
// production entry points enforce the exact compressor build identity.
func testBodyNormalizer(body []byte) (assetmeasure.Sizes, error) {
	gzipBody, brotliBody := testMeasureEncodings(body)
	return assetmeasure.Sizes{Raw: int64(len(body)), Gzip: int64(len(gzipBody)), Brotli: int64(len(brotliBody)), SHA256: testMeasureHash(body)}, nil
}
func testMeasureEncodings(body []byte) ([]byte, []byte) {
	var gz, br bytes.Buffer
	g, _ := gzip.NewWriterLevel(&gz, 9)
	g.Header.OS = 255
	g.Write(body)
	g.Close()
	b := brotli.NewWriterOptions(&br, brotli.WriterOptions{Quality: 11, LGWin: 0})
	b.Write(body)
	b.Close()
	return gz.Bytes(), br.Bytes()
}
func testInlineOptions() HTMLMeasureOptions {
	return HTMLMeasureOptions{Fields: []HTMLField{{Element: "script", Attribute: "nonce"}, {Element: "body", Attribute: "data-gosx-session"}}, FrameworkScriptSHA256: []string{testMeasureHash([]byte("window.fixtureRuntime = function(){ return 7; };"))}}
}

func TestInlineOwnershipUsesWholeDocumentMarginal(t *testing.T) {
	body, err := os.ReadFile("testdata/inline.html")
	if err != nil {
		t.Fatal(err)
	}
	result, err := measureHTML(body, testInlineOptions(), testBodyNormalizer)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := os.ReadFile("testdata/inline-normalized.html")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(result.withoutFramework, expected) {
		t.Fatal("non-framework document bytes changed")
	}
	if result.ExecutableScripts != 2 || result.InlineAppScriptMax != int64(len("window.fixtureReady = 1;")) {
		t.Fatal("script accounting includes data/inert script or excludes app code")
	}
	full, _ := testBodyNormalizer(result.full)
	remainder, _ := testBodyNormalizer(expected)
	if result.Framework.Brotli != max(full.Brotli-remainder.Brotli, 0) || result.Framework.Gzip != max(full.Gzip-remainder.Gzip, 0) || result.App.Brotli+result.Framework.Brotli != full.Brotli || result.App.Raw+result.Framework.Raw != full.Raw {
		t.Fatal("whole-document ownership does not reconcile")
	}
	scriptSizes, _ := testBodyNormalizer([]byte("window.fixtureRuntime = function(){ return 7; };"))
	if result.Framework.Brotli == scriptSizes.Brotli {
		t.Fatal("standalone script compression substituted for marginal document cost")
	}
}

func TestInlineOnlyDeclaredTransientFieldsAreNormalized(t *testing.T) {
	body, _ := os.ReadFile("testdata/inline.html")
	opts := testInlineOptions()
	first, err := measureHTML(body, opts, testBodyNormalizer)
	if err != nil {
		t.Fatal(err)
	}
	changed := bytes.ReplaceAll(body, []byte("nonce-first"), []byte("nonce-second"))
	changed = bytes.ReplaceAll(changed, []byte("session-first"), []byte("session-second"))
	second, err := measureHTML(changed, opts, testBodyNormalizer)
	if err != nil || VerifyHTMLRenders(first, second) != nil {
		t.Fatal("declared transient fields changed identity", err)
	}
	undeclared, err := measureHTML(changed, HTMLMeasureOptions{FrameworkScriptSHA256: opts.FrameworkScriptSHA256}, testBodyNormalizer)
	if err != nil {
		t.Fatal(err)
	}
	if VerifyHTMLRenders(first, undeclared) == nil {
		t.Fatal("undeclared transient difference ignored")
	}
	changed = bytes.Replace(body, []byte("Stable content"), []byte("Changed content"), 1)
	second, err = measureHTML(changed, opts, testBodyNormalizer)
	if err != nil || VerifyHTMLRenders(first, second) == nil {
		t.Fatal("content drift ignored")
	}
	for _, raw := range []string{`<script nonce='a' data-note=" nonce=private">x</script>`, `<SCRIPT NONCE=a>x</SCRIPT>`, `<script nonce = "a">x</script>`} {
		result, err := measureHTML([]byte(raw), HTMLMeasureOptions{Fields: []HTMLField{{"script", "nonce"}}}, testBodyNormalizer)
		if err != nil || bytes.Contains(result.full, []byte("nonce=private")) != strings.Contains(raw, "nonce=private") || !bytes.Contains(result.full, []byte("gosx-normalized")) {
			t.Fatal("structural attribute rewrite failed", err)
		}
	}
}

func TestInlineOwnershipCannotBeDeclaredByPathOrAttribute(t *testing.T) {
	for _, body := range []string{`<script data-gosx-navigation="true">privateApp()</script>`, `<script>privateApp()</script>`, `<script type="application/json">privateApp()</script>`} {
		result, err := measureHTML([]byte(body), HTMLMeasureOptions{}, testBodyNormalizer)
		if err != nil || result.Framework != (SizeTriple{}) || !bytes.Equal(result.full, result.withoutFramework) {
			t.Fatal("unverified framework attribution", err)
		}
	}
	result, err := measureHTML([]byte(`<script src="/gosx/app.js"></script><script type="text/ecmascript">app()</script>`), HTMLMeasureOptions{}, testBodyNormalizer)
	if err != nil || result.ExecutableScripts != 2 {
		t.Fatal("external/legacy executable script ignored", err)
	}
	// Clipping can increase the app share when a tiny body compresses better whole.
	inverse := func(body []byte) (assetmeasure.Sizes, error) {
		return assetmeasure.Sizes{Raw: int64(len(body)), Gzip: 1000 - int64(len(body)), Brotli: 1000 - int64(len(body)), SHA256: testMeasureHash(body)}, nil
	}
	result, err = measureHTML([]byte(`<script>x</script>`), HTMLMeasureOptions{FrameworkScriptSHA256: []string{testMeasureHash([]byte("x"))}}, inverse)
	if err != nil || result.Framework.Gzip != 0 || result.Framework.Brotli != 0 || result.App.Brotli != result.Sizes.Brotli {
		t.Fatal("negative compression marginal was not clipped", err)
	}
}

func TestInlineRejectsInvalidInputsAndNoncanonicalPin(t *testing.T) {
	for _, body := range [][]byte{[]byte(`<script>x`), []byte(`<script nonce="a" nonce="b">x</script>`), {0xff}, bytes.Repeat([]byte("x"), (16<<20)+1)} {
		if _, err := measureHTML(body, HTMLMeasureOptions{}, testBodyNormalizer); err == nil {
			t.Fatal("invalid HTML accepted")
		}
	}
	for _, opts := range []HTMLMeasureOptions{{Fields: []HTMLField{{"script", "src"}}}, {Fields: []HTMLField{{"script", "nonce"}, {"script", "nonce"}}}, {FrameworkScriptSHA256: []string{"private"}}} {
		if _, err := measureHTML([]byte("<p>ok</p>"), opts, testBodyNormalizer); err == nil {
			t.Fatal("invalid normalization declaration accepted")
		}
	}
	_, err := MeasureHTML([]byte("<p>ok</p>"), HTMLMeasureOptions{})
	var typed *InputError
	if !errors.As(err, &typed) || typed.Code != "noncanonical" {
		t.Fatal("production pin not enforced", err)
	}
}

func TestInlineNonceCSPBinding(t *testing.T) {
	body := []byte(`<script nonce="fixture">app()</script><style nonce="fixture">p{color:red}</style>`)
	for _, csp := range []string{"script-src 'nonce-fixture'; style-src 'nonce-fixture'", "default-src 'nonce-fixture'", "script-src-elem 'nonce-fixture'; style-src-elem 'nonce-fixture'"} {
		if err := VerifyHTMLNonces(body, csp); err != nil {
			t.Fatal(err)
		}
	}
	for _, csp := range []string{"", "script-src 'nonce-private'", "script-src 'nonce-fixture'; style-src 'nonce-private'", "script-src 'nonce-fixture'; script-src-elem 'none'; style-src 'nonce-fixture'"} {
		if err := VerifyHTMLNonces(body, csp); err == nil || strings.Contains(err.Error(), "private") {
			t.Fatal("nonce mismatch accepted or leaked", err)
		}
	}
}
