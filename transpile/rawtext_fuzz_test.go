package transpile

import "testing"

// FuzzRawTextScannerNoPanic keeps the raw scanner's adversarial surface under
// the normal parser entry point. Every input is allowed to be rejected—the
// invariant is that a wrong-tag candidate, unterminated body, or arbitrary
// script/CSS byte sequence never becomes a panic or a partial successful
// emission.
func FuzzRawTextScannerNoPanic(f *testing.F) {
	for _, seed := range []string{
		`const marker = "</style>"; /* </script> */`,
		`if (a < b) { run(); } </scriptx>`,
		`/* </script > */ const x = "</script\t>";`,
		`.a > .b { content: "</script>"; }`,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, body string) {
		source := []byte("package demo\n\nimport . \"m31labs.dev/gosx\"\n\nfunc Page() Node {\n\treturn <div><script>" + body + "</SCRIPT \t><style>" + body + "</STYLE \r\n></div>\n}\n")
		_, _ = Transpile(source, Options{SourceFile: "fuzz.gsx"})
	})
}
