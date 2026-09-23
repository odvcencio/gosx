//go:build !tinygo

package gosx

import (
	"bytes"
	"fmt"
	"os"
	"sync"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"m31labs.dev/gosx/ir"
)

// grammarRegenEnvVar opts into regenerating the GoSX grammar at runtime
// (~40s) when the blob loadGSXLanguageBlob was given fails to load or
// fails its smoke parse. Unset (the default), a bad blob fails loudly
// instead of silently paying that cost — see loadGSXLanguageBlob.
const grammarRegenEnvVar = "GOSX_ALLOW_GRAMMAR_REGEN"

// gosxGrammarCache holds the process-wide cached GoSX tree-sitter
// language, computed at most once. It is its own type — rather than
// three bare package vars — so tests can exercise SetGrammarBlob's
// already-initialized and idempotent-reload paths against a private
// instance instead of the shared gosxLang singleton every other test in
// this package also depends on staying warm.
type gosxGrammarCache struct {
	once sync.Once
	lang *gotreesitter.Language
	err  error
	// blob records the exact bytes that initialized lang via setBlob (or
	// via language's embedded-blob path). It is nil when lang was
	// produced by generating from grammar source instead of loading a
	// blob. A later setBlob call with byte-identical data is treated as
	// an idempotent no-op rather than an error.
	blob []byte
}

// gosxLang is the shared cache SetGrammarBlob and Language operate on.
var gosxLang = &gosxGrammarCache{}

// setBlob preloads the cache's language from data, matching
// SetGrammarBlob's documented contract (see that function). embedded is
// the build's embedded grammar blob, injected as a parameter so tests
// can exercise this against a private cache instance without depending
// on the real embedded blob.
func (c *gosxGrammarCache) setBlob(data, embedded []byte) error {
	if len(embedded) > 0 && !bytes.Equal(data, embedded) {
		return fmt.Errorf(
			"grammar blob override (%d bytes) does not match the blob embedded in this gosx version (%d bytes); ignoring the override — regenerate it with `go run ./cmd/gosx-grammar-blob` or delete it to use the embedded grammar",
			len(data), len(embedded))
	}
	var ranNow bool
	c.once.Do(func() {
		ranNow = true
		c.blob = data
		c.lang, c.err = loadGSXLanguageBlob(data)
	})
	if !ranNow {
		if c.blob != nil && bytes.Equal(data, c.blob) {
			// Idempotent: this exact blob already initialized the
			// cache (via an earlier setBlob call, or via language's own
			// embedded-blob load), so this call changes nothing.
			return nil
		}
		return fmt.Errorf(
			"SetGrammarBlob called after the gosx grammar was already initialized (by an earlier SetGrammarBlob or Language() call) with a different blob; the active language cannot be replaced — call SetGrammarBlob before the first Language()/Compile()/Parse() call")
	}
	return c.err
}

// language returns the cache's language, generating or loading it on
// first call. embedded is the build's embedded grammar blob.
func (c *gosxGrammarCache) language(embedded []byte) (*gotreesitter.Language, error) {
	c.once.Do(func() {
		if len(embedded) > 0 {
			c.blob = embedded
			c.lang, c.err = loadGSXLanguageBlob(embedded)
		} else {
			c.lang, _, c.err = GenerateLanguageAndBlob(GosxGrammar())
		}
		if c.err == nil && c.lang == nil {
			c.lang, _, c.err = GenerateLanguageAndBlob(GosxGrammar())
		}
		if c.err == nil && c.lang != nil && c.lang.ExternalScanner == nil {
			c.lang.ExternalScanner = newGSXScanner(c.lang)
		}
	})
	return c.lang, c.err
}

// SetGrammarBlob preloads the GoSX grammar from a pre-compiled binary blob.
// Call this before any Compile/Parse calls to skip the 40s grammar generation.
// The gosx library embeds a default blob, and callers may provide an override.
//
// An override that does not match the blob embedded in this gosx version is
// refused with an error and nothing is loaded, so a later Language() call
// falls back to the embedded blob. Parse tables from a different grammar
// version can mis-parse real source with no error node to point at
// (gosx#139: an app-staged dist/gosx-grammar.blob from an older checkout
// failed context-dependently on a multi-line self-closing svg path). The
// trivial smoke parse cannot catch that class, so the byte fingerprint is
// the gate. A rejected override costs nothing: the embedded blob carries the
// same grammar this module compiles against.
//
// Calling SetGrammarBlob after the grammar is already initialized (by an
// earlier SetGrammarBlob or Language() call) returns an error unless data
// is byte-identical to whatever already initialized it, in which case the
// call is a harmless no-op. The active language is never replaced.
func SetGrammarBlob(data []byte) error {
	return gosxLang.setBlob(data, embeddedGrammarBlob)
}

// Language returns the GoSX tree-sitter language, generating it on first call.
// If SetGrammarBlob was called first, returns the preloaded language instantly.
// Otherwise it loads the embedded library blob before falling back to generation.
func Language() (*gotreesitter.Language, error) {
	return gosxLang.language(embeddedGrammarBlob)
}

// loadGSXLanguageBlob loads a GoSX tree-sitter language from a
// pre-compiled blob and smoke-parses it to catch a stale or corrupt
// blob (gosx#139). A blob that fails to load or fails that smoke parse
// used to be silently discarded in favor of regenerating the grammar
// from source at runtime — a ~40s stall with no indication anything was
// wrong. Regeneration now requires the explicit opt-in
// GOSX_ALLOW_GRAMMAR_REGEN=1; without it, a bad blob fails loudly.
func loadGSXLanguageBlob(data []byte) (*gotreesitter.Language, error) {
	lang, loadErr := LoadLanguageBlob(data)
	switch {
	case loadErr == nil && lang != nil:
		lang.ExternalScanner = newGSXScanner(lang)
		if err := validateGSXLanguage(lang); err == nil {
			return lang, nil
		} else {
			loadErr = err
		}
	case loadErr == nil:
		loadErr = fmt.Errorf("LoadLanguageBlob returned no language and no error")
	}

	if os.Getenv(grammarRegenEnvVar) != "1" {
		return nil, fmt.Errorf(
			"gosx grammar blob is invalid: %w; regenerating it at runtime costs about 40s, so this fails instead of doing that silently — set %s=1 to allow runtime regeneration, or run `go run ./cmd/gosx-grammar-blob` to refresh the embedded blob ahead of time",
			loadErr, grammarRegenEnvVar)
	}

	lang, _, genErr := GenerateLanguageAndBlob(GosxGrammar())
	if genErr != nil {
		return nil, genErr
	}
	if lang != nil {
		lang.ExternalScanner = newGSXScanner(lang)
	}
	return lang, nil
}

func validateGSXLanguage(lang *gotreesitter.Language) error {
	const smoke = "package app\nfunc Page() Node {\n\treturn <div>ok</div>\n}\n"
	parser := gotreesitter.NewParser(lang)
	tree, err := parser.Parse([]byte(smoke))
	if err != nil {
		return err
	}
	if root := tree.RootNode(); root == nil || root.HasError() {
		return fmt.Errorf("embedded gosx grammar blob failed smoke parse")
	}
	return nil
}

// Parse parses GoSX source into a tree-sitter tree.
func Parse(source []byte) (*gotreesitter.Tree, *gotreesitter.Language, error) {
	lang, err := Language()
	if err != nil {
		return nil, nil, fmt.Errorf("generate gosx language: %w", err)
	}

	parser := gotreesitter.NewParser(lang)
	tree, err := parser.Parse(source)
	if err != nil {
		return nil, nil, fmt.Errorf("parse: %w", err)
	}

	return tree, lang, nil
}

// Compile parses GoSX source and produces the component IR.
func Compile(source []byte) (*ir.Program, error) {
	tree, lang, err := Parse(source)
	if err != nil {
		return nil, err
	}

	root := tree.RootNode()
	if root.HasError() {
		return nil, DescribeParseError(root, source, lang)
	}
	if err := requirePackageClause(root, lang); err != nil {
		return nil, err
	}

	prog, err := ir.Lower(root, source, lang)
	if err != nil {
		return nil, err
	}

	// Run validation
	diags := ir.Validate(prog)
	if len(diags) > 0 {
		return nil, ir.NewDiagnosticsError("validation", diags)
	}

	return prog, nil
}
