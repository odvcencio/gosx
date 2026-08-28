package route

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

// gosx#226: a hand-written Go http.Handler in the same package as a page.gsx
// file must be able to render that page's own component with typed props
// and get the exact markup the page itself produces — the pattern
// data-gosx-region needs for a server-rendered, polled fragment. These tests
// prove RenderProgramComponent's strict-entry support and LoadFileProgram
// together close that gap, through the same strict-props boundary a nested
// <Component/> call already goes through, never around it.

const signalCardFixtureSource = `package app
type SignalCardProps struct {
	Label string
	Value string
}
component SignalCard(props: SignalCardProps) {
	return <li class="signal-card"><span class="signal-card__label">{props.Label}</span><strong class="signal-card__value">{props.Value}</strong></li>
}
component Page() {
	return <SignalCard label="Passing Yards" value="317" />
}
`

// SignalCardProps mirrors signalCardFixtureSource's declared props type for
// the named entry test below. The strict render-entry boundary used by
// ProgramRenderEnv.Props proves rendered fields structurally; it does not
// require a Go caller's struct to reuse the .gsx type name.
type SignalCardProps struct {
	Label string
	Value string
}

// TestRenderProgramComponentStrictEntryMatchesNestedRenderByteForByte is the
// required byte-for-byte proof: SignalCard renders identically whether it is
// reached as Page's own nested call (named string attrs, the only shape a
// .gsx caller can spell) or as the render entry itself (a typed Go value
// through ProgramRenderEnv.Props).
func TestRenderProgramComponentStrictEntryMatchesNestedRenderByteForByte(t *testing.T) {
	prog, err := gosx.Compile([]byte(signalCardFixtureSource))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	viaPage, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{})
	if err != nil {
		t.Fatalf("render Page: %v", err)
	}

	viaEntry, err := RenderProgramComponent(prog, "SignalCard", ProgramRenderEnv{
		Props: SignalCardProps{Label: "Passing Yards", Value: "317"},
	})
	if err != nil {
		t.Fatalf("render SignalCard entry: %v", err)
	}

	const want = `<li class="signal-card"><span class="signal-card__label">Passing Yards</span><strong class="signal-card__value">317</strong></li>`
	if viaPage != want {
		t.Fatalf("Page render = %q, want %q", viaPage, want)
	}
	if viaEntry != viaPage {
		t.Fatalf("strict entry render = %q, does not byte-match the page's own nested render %q", viaEntry, viaPage)
	}
}

// TestRenderProgramComponentStrictEntryAcceptsStructurallyMatchingProps is a
// success proof for a production fragment handler's normal shape: its Go
// loader type has a different name from the .gsx props type, but supplies every
// rendered field with the declared leaf type.
func TestRenderProgramComponentStrictEntryAcceptsStructurallyMatchingProps(t *testing.T) {
	type fragmentSignalCardProps struct {
		Label string
		Value string
	}
	prog, err := gosx.Compile([]byte(signalCardFixtureSource))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	html, err := RenderProgramComponent(prog, "SignalCard", ProgramRenderEnv{
		Props: fragmentSignalCardProps{Label: "Passing Yards", Value: "317"},
	})
	if err != nil {
		t.Fatalf("render structurally matching props: %v", err)
	}
	const want = `<li class="signal-card"><span class="signal-card__label">Passing Yards</span><strong class="signal-card__value">317</strong></li>`
	if html != want {
		t.Fatalf("rendered HTML = %q, want %q", html, want)
	}
}

// TestRenderProgramComponentStrictEntryRejectsMapProps proves the strict
// boundary still rejects a map at the entry point, the same way it rejects
// one at a nested {...props} call site: a map cannot prove field coverage,
// no matter how the render was reached.
//
// gosx#248 gives this specific case (a map at the render entry, the shape
// every existing file-routed Load hook returns) its own diagnostic, ahead
// of strictSpreadProps' generic struct-kind message: it names the
// component, its declared props type, and what to return instead of a map.
func TestRenderProgramComponentStrictEntryRejectsMapProps(t *testing.T) {
	prog, err := gosx.Compile([]byte(signalCardFixtureSource))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	_, err = RenderProgramComponent(prog, "SignalCard", ProgramRenderEnv{
		Props: map[string]any{"Label": "Passing Yards", "Value": "317"},
	})
	if err == nil {
		t.Fatal("RenderProgramComponent unexpectedly accepted a map for strict typed props")
	}
	for _, want := range []string{"strict render entry SignalCard", "props SignalCardProps", "return a SignalCardProps value", "instead of a map"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, want it to contain %q", err, want)
		}
	}
}

// TestRenderProgramComponentStrictEntryRejectsIncompleteStruct proves the
// entry boundary proves every rendered field's presence and declared leaf
// type, the same way a nested {...props} spread call's boundary does
// (strictSpreadProps, see TestStrictSpreadProps): a struct missing a
// rendered field fails closed instead of silently zero-filling it. (A
// same-shaped struct under a different Go type name is accepted here, by
// design — see ProgramRenderEnv.Props; strictSpreadProps proves field
// coverage, not the source struct's own type identity.)
func TestRenderProgramComponentStrictEntryRejectsIncompleteStruct(t *testing.T) {
	type partialSignalCardProps struct {
		Label string
	}
	prog, err := gosx.Compile([]byte(signalCardFixtureSource))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	_, err = RenderProgramComponent(prog, "SignalCard", ProgramRenderEnv{
		Props: partialSignalCardProps{Label: "Passing Yards"},
	})
	if err == nil {
		t.Fatal("RenderProgramComponent unexpectedly accepted a struct missing a rendered field")
	}
	if !strings.Contains(err.Error(), "no field Value") {
		t.Fatalf("error = %v, want the missing-field boundary message", err)
	}
}

// TestRenderProgramComponentStrictEntryRejectsNilProps proves the original,
// unconditional refusal (gosx#185/#226: "the file renderer has no root
// props binding") still fires byte-for-byte when a caller renders a strict
// entry without supplying Props — the pre-existing
// TestRenderProgramComponentRejectsStrictRootProps covers the same
// contract; this one exercises it against a component with a real,
// non-empty PropsFields set instead of a hand-built program.
func TestRenderProgramComponentStrictEntryRejectsNilProps(t *testing.T) {
	prog, err := gosx.Compile([]byte(signalCardFixtureSource))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	_, err = RenderProgramComponent(prog, "SignalCard", ProgramRenderEnv{})
	if err == nil || !strings.Contains(err.Error(), "no root props binding") {
		t.Fatalf("error = %v, want the no-props-supplied refusal", err)
	}
}

// TestFragmentHandlerServesPageOwnComponentForLiveRegion is the gosx#226 end
// to end proof. fragmentHandler stands in for a hand-written page.server.go
// handler living in the same package as page.gsx: it loads the page's own
// compiled program with LoadFileProgram and renders one of its strict
// components directly, typed props and all, through RenderProgramComponent.
// It never re-derives SignalCard's markup with the low-level
// El/Attrs/Text API — the two-copies-that-drift workaround gosx#226
// reports — it renders the SAME component the page itself declares. The
// fragment it serves is byte-for-byte the same markup the page renders that
// component as inside its own <ul data-gosx-region> list: the exact
// swap-in fragment data-gosx-region-url polls for.
func TestFragmentHandlerServesPageOwnComponentForLiveRegion(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "page.gsx", `package app
type SignalCardProps struct {
	Label string
	Value string
}
component SignalCard(props: SignalCardProps) {
	return <li class="signal-card">{props.Label}: {props.Value}</li>
}
component Page() {
	return <ul data-gosx-region data-gosx-region-url="/wire/signal" data-gosx-region-interval="20s">
		<SignalCard label="Passing Yards" value="317" />
	</ul>
}
`)
	gsxPath := filepath.Join(root, "page.gsx")

	fragmentHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		prog, err := LoadFileProgram(gsxPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		html, err := RenderProgramComponent(prog, "SignalCard", ProgramRenderEnv{
			Props: SignalCardProps{Label: "Passing Yards", Value: "317"},
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(html))
	})

	router := NewRouter()
	router.Handle("/wire/signal", fragmentHandler)
	if err := router.AddDir(root, FileRoutesOptions{}); err != nil {
		t.Fatalf("AddDir: %v", err)
	}
	handler := router.Build()

	pageReq := httptest.NewRequest(http.MethodGet, "/", nil)
	pageRec := httptest.NewRecorder()
	handler.ServeHTTP(pageRec, pageReq)
	if pageRec.Code != http.StatusOK {
		t.Fatalf("GET / = %d, body: %s", pageRec.Code, pageRec.Body.String())
	}
	pageBody := pageRec.Body.String()
	if !strings.Contains(pageBody, `data-gosx-region-url="/wire/signal"`) {
		t.Fatalf("page is missing its data-gosx-region wiring: %s", pageBody)
	}

	fragmentReq := httptest.NewRequest(http.MethodGet, "/wire/signal", nil)
	fragmentRec := httptest.NewRecorder()
	handler.ServeHTTP(fragmentRec, fragmentReq)
	if fragmentRec.Code != http.StatusOK {
		t.Fatalf("GET /wire/signal = %d, body: %s", fragmentRec.Code, fragmentRec.Body.String())
	}
	fragmentBody := fragmentRec.Body.String()

	const wantFragment = `<li class="signal-card">Passing Yards: 317</li>`
	if fragmentBody != wantFragment {
		t.Fatalf("fragment body = %q, want %q", fragmentBody, wantFragment)
	}
	if !strings.Contains(pageBody, wantFragment) {
		t.Fatalf("page body does not embed the exact fragment markup %q: %s", wantFragment, pageBody)
	}
}

// TestLoadFileProgramSharesFileRouterCache proves LoadFileProgram is not a
// second, independently-stale compile path: it returns the identical
// *ir.Program a file-routed page renders through (gsxCompileCache is keyed
// by path plus mtime plus size), not a fresh parse of the same bytes.
func TestLoadFileProgramSharesFileRouterCache(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "page.gsx", `package app
component Page() {
	return <main>hello</main>
}
`)
	gsxPath := filepath.Join(root, "page.gsx")

	viaHelper, err := LoadFileProgram(gsxPath)
	if err != nil {
		t.Fatalf("LoadFileProgram: %v", err)
	}
	viaFileRoute, err := loadCachedGSXProgram(gsxPath)
	if err != nil {
		t.Fatalf("loadCachedGSXProgram: %v", err)
	}
	if viaHelper != viaFileRoute {
		t.Fatalf("LoadFileProgram returned a different *ir.Program than the file router's own cache")
	}
}
