package route

import (
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

func TestStrictFileLayoutChildren(t *testing.T) {
	for _, tc := range []struct {
		name, body, helpers, want string
		content                   gosx.Node
		fail                      bool
	}{
		{name: "text", body: "<main>{children}</main>", content: gosx.Text("<unsafe>&"), want: "<main>&lt;unsafe&gt;&amp;</main>"},
		{name: "structured", body: "<main>{children}</main>", content: gosx.El("strong", gosx.Text("safe")), want: "<main><strong>safe</strong></main>"},
		{name: "empty", body: "<main>{children}</main>", want: "<main></main>"},
		{name: "twice", body: "<main>{children}{children}</main>", content: gosx.Text("x"), want: "<main>xx</main>"},
		{name: "unused", body: "<main>none</main>", content: gosx.Text("x"), fail: true},
		{name: "forwarded", body: "<Frame>{children}</Frame>", helpers: "component Frame() {\n return <section>{children}</section>\n}\n", content: gosx.Text("x"), want: "<section>x</section>"},
		// FileLayout tracks evaluation, like legacy Slot replacement, not
		// final-output provenance through an eagerly rendered child buffer.
		{name: "forwarded dropped", body: "<Frame Enabled={false}>{children}</Frame>", helpers: "type FrameProps struct { Enabled bool }\ncomponent Frame(props: FrameProps) {\n return <section><If cond={props.Enabled}>{children}</If></section>\n}\n", content: gosx.Text("x"), want: "<section></section>"},
		{name: "shadowed unrelated children", body: "<main><Frame>other</Frame></main>", helpers: "component Frame() {\n return <section>{children}</section>\n}\n", content: gosx.Text("x"), fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeRouteFile(t, root, "layout.gsx", "package app\ncomponent Layout() {\n return "+tc.body+"\n}\n"+tc.helpers)
			path := filepath.Join(root, "layout.gsx")
			node, err := renderFileLayout(path, &RouteContext{}, tc.content, layoutFilePage("", path), FileModule{}, FileLayoutOptions{})
			if tc.fail {
				if err == nil || !strings.Contains(err.Error(), "missing") {
					t.Fatalf("error = %v, want missing placement", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := gosx.RenderHTML(node); got != tc.want {
				t.Fatalf("HTML = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFileLayoutEvaluationContractParity(t *testing.T) {
	for _, strict := range []bool{false, true} {
		for _, drop := range []bool{false, true} {
			root := t.TempDir()
			source := "package app\nfunc Layout() Node {\n return <Frame><Slot /></Frame>\n}\nfunc Frame() Node {\n return <section>{children}</section>\n}\n"
			if strict {
				source = "package app\ncomponent Layout() {\n return <Frame>{children}</Frame>\n}\ncomponent Frame() {\n return <section>{children}</section>\n}\n"
			}
			if drop {
				if strict {
					source = strings.Replace(source, "<Frame>", "<Frame Enabled={false}>", 1)
					source = strings.Replace(source, "component Frame()", "type FrameProps struct { Enabled bool }\ncomponent Frame(props: FrameProps)", 1)
					source = strings.Replace(source, "<section>{children}</section>", "<section><If cond={props.Enabled}>{children}</If></section>", 1)
				} else {
					source = strings.Replace(source, "<section>{children}</section>", "<section><If when={false}>{children}</If></section>", 1)
				}
			}
			writeRouteFile(t, root, "layout.gsx", source)
			path := filepath.Join(root, "layout.gsx")
			node, err := renderFileLayout(path, &RouteContext{}, gosx.Text("x"), layoutFilePage("", path), FileModule{}, FileLayoutOptions{})
			if err != nil {
				t.Fatalf("strict=%v drop=%v: %v", strict, drop, err)
			}
			want := "<section>x</section>"
			if drop {
				want = "<section></section>"
			}
			if got := gosx.RenderHTML(node); got != want {
				t.Fatalf("strict=%v drop=%v: %q != %q", strict, drop, got, want)
			}
		}
	}
}

func TestStrictFileLayoutNestedShared(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "layout.gsx", "package app\nimport ui \"./ui\"\ncomponent Layout() {\n return <ui.Frame>{children}</ui.Frame>\n}\n")
	writeRouteFile(t, root, "ui/frame.gsx", "package ui\ncomponent Frame() {\n return <article>{children}</article>\n}\n")
	writeRouteFile(t, root, "nested/layout.gsx", "package nested\ncomponent Layout() {\n return <section>{children}</section>\n}\n")
	outer, err := FileLayoutWithRegistry(filepath.Join(root, "layout.gsx"), nil)
	if err != nil {
		t.Fatal(err)
	}
	inner, err := FileLayoutWithRegistry(filepath.Join(root, "nested/layout.gsx"), nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := &RouteContext{}
	got := gosx.RenderHTML(outer(ctx, inner(ctx, gosx.Text("<x>"))))
	if want := "<article><section>&lt;x&gt;</section></article>"; got != want {
		t.Fatalf("HTML = %q, want %q", got, want)
	}
}
