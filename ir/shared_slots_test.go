package ir_test

import (
	"strings"
	"testing"

	"m31labs.dev/gosx/ir"
)

func TestSharedComponentSlotNameProofDefersToPackageProjection(t *testing.T) {
	_, err := parse(t, []byte(`package app

import ui "./ui"

component Page() {
	return <ui.Frame><h1 slot="Title">Title</h1></ui.Frame>
}
`))
	if err != nil {
		t.Fatalf("shared slot call shape should lower before package projection resolves target metadata: %v", err)
	}
}

func TestExpressionWrappedJSXSlotChildLowersAsNamedSlot(t *testing.T) {
	_, err := parse(t, []byte(`package app

component Layout() {
	return <section>{slotTitle}<main>{children}</main></section>
}

component Page() {
	return <Layout>{<h1 slot="Title">Title</h1>}<p>body</p></Layout>
}
`))
	if err != nil {
		t.Fatalf("expression-wrapped JSX slot child should lower as a named slot: %v", err)
	}
}

func TestSameFileLeafIgnoresWhitespaceAndCommentOnlyChildren(t *testing.T) {
	for name, body := range map[string]string{
		"whitespace":  "\n\t\t\n",
		"commentOnly": "\n\t\t// staging note\n\t\t\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parse(t, []byte(`package app

component Badge() {
	return <span>badge</span>
}

component Page() {
	return <Badge>`+body+`</Badge>
}
`))
			if err != nil {
				t.Fatalf("same-file leaf rejected formatting-only child body: %v", err)
			}
		})
	}
}

func TestSameFileLeafStillRejectsMeaningfulChildren(t *testing.T) {
	_, err := parse(t, []byte(`package app

component Badge() {
	return <span>badge</span>
}

component Page() {
	return <Badge><strong>lost</strong></Badge>
}
`))
	if err == nil || !strings.Contains(err.Error(), "renders no children") {
		t.Fatalf("Compile error = %v, want meaningful child rejection", err)
	}
}

func TestSameFileLeafStillRejectsEmptyFragmentChild(t *testing.T) {
	_, err := parse(t, []byte(`package app

component Badge() {
	return <span>badge</span>
}

component Page() {
	return <Badge><></></Badge>
}
`))
	if err == nil || !strings.Contains(err.Error(), "renders no children") {
		t.Fatalf("Compile error = %v, want empty fragment child rejection", err)
	}
}

func TestSameFileLeafStillRejectsEncodedWhitespaceChildren(t *testing.T) {
	for name, body := range map[string]string{
		"nbsp":    "&nbsp;",
		"space":   "&#32;",
		"tab":     "&#9;",
		"newline": "&#10;",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parse(t, []byte(`package app

component Badge() {
	return <span>badge</span>
}

component Page() {
	return <Badge>`+body+`</Badge>
}
`))
			if err == nil || !strings.Contains(err.Error(), "renders no children") {
				t.Fatalf("Compile error = %v, want encoded whitespace child rejection", err)
			}
		})
	}
}

func TestSameFileComponentSlotNameProofStillFailsClosed(t *testing.T) {
	_, err := parse(t, []byte(`package app

component Frame() {
	return <section>{children}</section>
}

component Page() {
	return <Frame><h1 slot="Title">Title</h1></Frame>
}
`))
	if err == nil || !strings.Contains(err.Error(), `declares no slot named "Title"`) {
		t.Fatalf("Compile error = %v, want same-file declares-no-slot diagnostic", err)
	}
}

func TestIslandCallerStillRejectsImportedPureViewComposition(t *testing.T) {
	prog, err := parse(t, []byte(`package app

import ui "./ui"

//gosx:island
component Counter() {
	return <ui.Frame />
}
`))
	if err != nil {
		t.Fatalf("Lower: %v", err)
	}
	diags := ir.Validate(prog)
	if len(diags) == 0 {
		t.Fatal("Validate returned no diagnostics for imported component inside island")
	}
	if !strings.Contains(diags[0].Message, "imported component <ui.Frame> cannot be composed inside island Counter in v1") {
		t.Fatalf("diagnostic = %#v, want imported component inside island diagnostic", diags[0])
	}
}
