package lsp

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

const lspSelenaSource = `material Glow {
    param tint : color = rgb(1.0, 0.5, 0.2)
    context {
        time : float
    }
    surface(geo) -> color {
        let pulse = sin(time) * 0.5 + 0.5
        return tint * pulse
    }
}
`

func TestServerRoutesSelenaDocuments(t *testing.T) {
	input := bytes.NewBuffer(nil)
	output := bytes.NewBuffer(nil)
	uri := "file:///tmp/glow.sel"
	hoverPos := testPositionIn(lspSelenaSource, "time) *", 1)
	completePos := testPositionIn(lspSelenaSource, "        return", 8)
	defPos := testPositionIn(lspSelenaSource, "tint * pulse", 1)
	broken := strings.Replace(lspSelenaSource, "return tint * pulse", "return missing", 1)

	writeFramedJSON(input, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	writeFramedJSON(input, fmt.Sprintf(`{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":%s,"version":1,"text":%s}}}`, quoteJSON(uri), quoteJSON(lspSelenaSource)))
	writeFramedJSON(input, fmt.Sprintf(`{"jsonrpc":"2.0","id":2,"method":"textDocument/hover","params":{"textDocument":{"uri":%s},"position":{"line":%d,"character":%d}}}`, quoteJSON(uri), hoverPos.Line, hoverPos.Character))
	writeFramedJSON(input, fmt.Sprintf(`{"jsonrpc":"2.0","id":3,"method":"textDocument/completion","params":{"textDocument":{"uri":%s},"position":{"line":%d,"character":%d}}}`, quoteJSON(uri), completePos.Line, completePos.Character))
	writeFramedJSON(input, fmt.Sprintf(`{"jsonrpc":"2.0","id":4,"method":"textDocument/definition","params":{"textDocument":{"uri":%s},"position":{"line":%d,"character":%d}}}`, quoteJSON(uri), defPos.Line, defPos.Character))
	writeFramedJSON(input, fmt.Sprintf(`{"jsonrpc":"2.0","method":"textDocument/didChange","params":{"textDocument":{"uri":%s,"version":2},"contentChanges":[{"text":%s}]}}`, quoteJSON(uri), quoteJSON(broken)))
	writeFramedJSON(input, `{"jsonrpc":"2.0","id":5,"method":"shutdown","params":{}}`)

	if err := Serve(input, output); err != nil {
		t.Fatalf("serve: %v", err)
	}
	wire := output.String()
	for _, want := range []string{
		`"completionProvider"`,
		`"diagnostics":[]`,             // the clean file
		`(context) time : float`,       // hover on a context field
		`Scene clock.`,                 // with its engine input registry entry
		`"label":"sin"`,                // completion offers builtins
		`"label":"tint"`,               // and the material's params
		`"code":"SEL2001"`,             // the edit introduces an unknown name
		`"uri":"file:///tmp/glow.sel"`, // definition stays in the document
	} {
		if !strings.Contains(wire, want) {
			t.Fatalf("expected %s in wire output, got %s", want, wire)
		}
	}
}
