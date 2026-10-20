package pagecaps

import "testing"

func TestExecutableSourceCodeAndKinds(t *testing.T) {
	for _, tc := range []struct {
		name, markup, code string
		kind               ExecutableSourceKind
	}{
		{"handler-decoded", `<button onclick="&quot;é&quot;; &#32;">Run</button>`, `"é";  `, EventHandlerSource},
		{"handler-label", `<button onclick="javascript:run()">Run</button>`, "javascript:run()", EventHandlerSource},
		{"url-decoded", `<a href="  JaVaScRiPt:'é'; &#32;">Run</a>`, "'é';  ", JavascriptURLSource},
		{"refresh-quoted", `<meta http-equiv="refresh" content="5; URL = 'JaVaScRiPt:run(); '">`, "run(); ", RefreshURLSource},
		{"refresh-code-quote", `<meta http-equiv="refresh" content="0; URL=javascript:'é'">`, "'é'", RefreshURLSource},
		{"refresh-comma", `<meta http-equiv="refresh" content="0,javascript:run()">`, "run()", RefreshURLSource},
		{"script-raw", "<script type=module>run();\r\n</script>", "run();\r\n", ScriptSource},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var sources []ExecutableSource
			_, err := InspectHTML([]byte(tc.markup), func(source ExecutableSource) { sources = append(sources, source) })
			if err != nil || len(sources) != 1 {
				t.Fatalf("source classification: sources=%d err=%v", len(sources), err)
			}
			if source := sources[0]; source.Kind != tc.kind || string(source.Body) != tc.code {
				t.Errorf("source code: kind=%d code=%q want kind=%d code=%q", source.Kind, source.Body, tc.kind, tc.code)
			}
		})
	}
}
