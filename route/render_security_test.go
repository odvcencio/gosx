package route

import (
	"fmt"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/internal/htmlattr"
	"m31labs.dev/gosx/ir"
	"math"
	"strings"
	"testing"
)

func TestFileRenderFiltersURLsAndDataDrivenNames(t *testing.T) {
	for _, name := range []string{"href", "src", "action", "formaction", "xlink:href", "poster"} {
		for _, attr := range []ir.Attr{{Name: name, Kind: ir.AttrStatic, Value: "javascript:example"}, {Name: name, Kind: ir.AttrExpr, Expr: "value"}} {
			var b strings.Builder
			renderFileAttr(&b, attr, fileRenderEnv{values: map[string]any{"value": "javascript:example"}}, "")
			if b.String() != " "+name+`="#"` {
				t.Fatalf("unsafe URL: %s", b.String())
			}
		}
	}
	attrs := map[string]any{"onload": "example", "STYLE": "color:red", "x y": "value", "data-ok": "ok", "href": "javascript:example"}
	var b strings.Builder
	renderFileSpreadAttrs(&b, attrs, "")
	if b.String() != ` data-ok="ok" href="#"` {
		t.Fatalf("unsafe spread: %s", b.String())
	}
	got := defaultRenderedComponent("Unknown", fileComponentHTMLAttrs{values: attrs}, "")
	if got != `<div data-gosx-component="Unknown" data-ok="ok" href="#"></div>` {
		t.Fatalf("unsafe fallback: %s", got)
	}
	r := &fileProgramRenderer{}
	b.Reset()
	r.renderResolvedAttrs(&b, "a", []RenderAttr{{Name: "href", Value: "javascript:example"}})
	if b.String() != ` href="#"` {
		t.Fatalf("unsafe profile: %s", b.String())
	}
	resolved := resolveFileAttrs([]ir.Attr{{Kind: ir.AttrSpread, Expr: "attrs"}}, fileRenderEnv{values: map[string]any{"attrs": attrs}}, "")
	if len(resolved) != 2 {
		t.Fatalf("profile received unsafe spread: %#v", resolved)
	}
}

func TestEqualityMatchesFractionalFloatStrings(t *testing.T) {
	type price float64
	for _, tc := range []struct {
		value any
		text  string
		want  bool
	}{
		{19.99, "19.99", true},
		{0.1, "0.1", true},
		{-19.99, "-19.99", true},
		{19.99, "1.999e1", true},
		{float32(19.99), "19.99", true},
		{price(19.99), "19.99", true},
		{19.99, "19.98", false},
		{19.99, "19.99suffix", false},
		{0.0, "abc", false},
		{0.0, "", false},
		{math.Inf(1), "Inf", false},
		{math.NaN(), "NaN", false},
	} {
		t.Run(fmt.Sprintf("%T/%s", tc.value, tc.text), func(t *testing.T) {
			for _, operands := range [][2]any{{tc.value, tc.text}, {tc.text, tc.value}} {
				if got := equalValues(operands[0], operands[1]); got != tc.want {
					t.Fatalf("%v == %q: %v want %v", tc.value, tc.text, got, tc.want)
				}
			}
		})
	}
	if got := evalFileExpr(`19.99 == "19.99"`, fileRenderEnv{}); got != true {
		t.Fatalf("fractional literal equality: %v", got)
	}
}

func TestEqualityBoundsDecimalAndHexadecimalExponents(t *testing.T) {
	for _, text := range []string{
		"1e-4097", "1E-10000000", "0x1p-4097", "0x1p-10000000",
		"0X1P-10000000", "-0x1p-10000000", "0x0p+10000000",
	} {
		t.Run(text, func(t *testing.T) {
			if number, ok := equalityNumber(text); ok || number != nil {
				// Avoid formatting a rejected rational with a huge denominator.
				t.Fatal("out-of-bounds exponent was accepted")
			}
			if equalValues(0.0, text) || equalValues(text, 0.0) {
				t.Fatal("float equality accepted an out-of-bounds exponent")
			}
		})
	}
	for _, text := range []string{"1e-4096", "0x1p-4096", "0xdep-3", "0X1AP+2"} {
		if number, ok := equalityNumber(text); !ok || number == nil {
			t.Fatalf("bounded numeric string rejected: %s", text)
		}
	}
}

func TestComponentFallbackDistinguishesAuthoredAndSpreadStyle(t *testing.T) {
	for _, tc := range []struct {
		name, markup string
		wantStyle    bool
	}{
		{"authored", `<Unknown style="color:red" />`, true},
		{"expression", `<Unknown style={style} />`, true},
		{"uppercase", `<Unknown STYLE="color:red" />`, true},
		{"spread", `<Unknown {...attrs} />`, false},
		{"authored before spread", `<Unknown style="color:red" {...attrs} />`, true},
		{"authored after spread", `<Unknown {...attrs} style="color:red" />`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prog, err := gosx.Compile([]byte("package main\nfunc Page() Node {\nreturn " + tc.markup + "\n}\n"))
			if err != nil {
				t.Fatal(err)
			}
			got, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{Values: map[string]any{
				"style": "color:red",
				"attrs": map[string]any{"style": "color:blue", "STYLE": "color:green", "onload": "example", "href": "javascript:example"},
			}})
			if err != nil {
				t.Fatal(err)
			}
			if hasStyle := strings.Contains(strings.ToLower(got), `style="color:red"`); hasStyle != tc.wantStyle {
				t.Fatalf("authored style present=%v want=%v: %s", hasStyle, tc.wantStyle, got)
			}
			for _, forbidden := range []string{"color:blue", "color:green", "onload", "javascript:example"} {
				if strings.Contains(strings.ToLower(got), forbidden) {
					t.Fatalf("unsafe spread attribute rendered: %s", got)
				}
			}
		})
	}
}

func TestEqualityPreservesIdentifiersAndRequiresExactNumericStrings(t *testing.T) {
	type identifier int64
	for _, tc := range []struct {
		left, right any
		want        bool
	}{
		{int64(0), "abc", false}, {int64(0), "", false}, {0, false, false},
		{int64(9007199254740993), int64(9007199254740992), false},
		{int64(9007199254740993), "9007199254740993", true},
		{int64(9007199254740993), "9007199254740992", false},
		{uint64(math.MaxUint64), "18446744073709551615", true},
		{int64(-1), uint64(math.MaxUint64), false}, {identifier(42), uint64(42), true},
		{float64(1.5), "1.5", true}, {42, "42oops", false}, {0, "1e-99999999", false},
		{math.NaN(), math.NaN(), false},
	} {
		if got := equalValues(tc.left, tc.right); got != tc.want {
			t.Errorf("%v == %v: %v want %v", tc.left, tc.right, got, tc.want)
		}
	}
	if got := evalFileExpr("9007199254740993 == 9007199254740992", fileRenderEnv{}); got != false {
		t.Fatalf("rounded integer literals: %v", got)
	}
}

func TestServerRenderDropsReservedDispatchAttrs(t *testing.T) {
	for _, name := range []string{"data-gosx-handler", "data-gosx-path", "data-gosx-on-click", "DATA-GOSX-ON-INPUT", "data-gosx-on-"} {
		prog, err := gosx.Compile([]byte("package main\n\nfunc Page() Node {\n return <button " + name + "=\"evil\" {...extra} data-gosx-link=\"true\">ok</button>\n}\n"))
		if err != nil {
			t.Fatal(err)
		}
		out, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{Values: map[string]any{"extra": map[string]any{name: "evil"}}})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out, "evil") || !strings.Contains(out, `data-gosx-link="true"`) {
			t.Fatalf("%s: %s", name, out)
		}
		var b strings.Builder
		r := &fileProgramRenderer{}
		r.renderAttrsWithProfile(&b, "button", []ir.Attr{
			{Kind: ir.AttrStatic, Name: name, Value: "evil"},
			{Kind: ir.AttrExpr, Name: name, Expr: "evil"},
			{Kind: ir.AttrBool, Name: name},
			{Kind: ir.AttrStatic, Name: "data-gosx-link", Value: "true"},
		}, fileRenderEnv{}, "", func(tag string, attrs []RenderAttr) []RenderAttr {
			for _, attr := range attrs {
				if htmlattr.ReservedDispatchName(attr.Name) {
					t.Errorf("authored reserved name reached hook: %s", attr.Name)
				}
			}
			return attrs
		})
		if r.err != nil {
			t.Fatalf("authored reserved name failed render: %v", r.err)
		}
		if b.String() != ` data-gosx-link="true"` {
			t.Fatalf("profile output: %s", b.String())
		}
		fallback := defaultRenderedComponent("Unknown", fileComponentHTMLAttrs{values: map[string]any{name: "evil"}}, "")
		if strings.Contains(fallback, "evil") {
			t.Fatalf("fallback output: %s", fallback)
		}
	}
}

func TestRenderProfileStillRejectsIntroducedDispatchAttrs(t *testing.T) {
	var b strings.Builder
	r := &fileProgramRenderer{}
	r.renderAttrsWithProfile(&b, "button", nil, fileRenderEnv{}, "", func(string, []RenderAttr) []RenderAttr {
		return []RenderAttr{{Name: "data-gosx-on-click", Value: "evil"}}
	})
	if r.err == nil || strings.Contains(b.String(), "evil") {
		t.Fatalf("profile-introduced marker accepted: %q, %v", b.String(), r.err)
	}
}

func TestFileSpreadCannotInstallFrameworkBindings(t *testing.T) {
	attrs := map[string]any{"data-gosx-bind-source": "#source", "DATA-GOSX-BIND-ATTR": "onclick:data-code", "data-gosx-action": "POST /delete", "data-gosx-action-target": "#target", "data-app-id": "safe"}
	var b strings.Builder
	renderFileSpreadAttrs(&b, attrs, "")
	if b.String() != ` data-app-id="safe"` {
		t.Fatalf("spread installed framework behavior: %s", b.String())
	}
	resolved := resolveFileAttrs([]ir.Attr{{Kind: ir.AttrSpread, Expr: "attrs"}}, fileRenderEnv{values: map[string]any{"attrs": attrs}}, "")
	if len(resolved) != 1 || resolved[0].Name != "data-app-id" {
		t.Fatalf("profile received framework attrs: %+v", resolved)
	}
	got := defaultRenderedComponent("Unknown", fileComponentHTMLAttrs{values: attrs}, "")
	if strings.Contains(got, "bind-source") || strings.Contains(got, "BIND-ATTR") || strings.Contains(got, "action-target") {
		t.Fatalf("fallback installed behavior: %s", got)
	}
}
