//go:build !js || !wasm

package highlight

import (
	"testing"
)

const benchGo = "func main() {\n\tapp := server.New()\n\tapp.Route(\"/\", home)\n\tlog.Fatal(app.ListenAndServe(\":8080\"))\n}"
const benchGSX = "func Counter() Node {\n\tcount := signal.New(0)\n\treturn <button onClick={func() { count.Set(count.Get()+1) }}>{count.Get()}</button>\n}"
const benchBash = "go install m31labs.dev/gosx/cmd/gosx@latest\ngosx init myapp\ncd myapp && gosx dev"
const benchJS = "const el = document.querySelector('#app');\nel.addEventListener('click', () => console.log('hi'));"
const benchJSON = `{"name": "gosx", "version": "1.0.0", "items": [1, 2, 3]}`

func BenchmarkHighlightGo(b *testing.B)   { benchHighlight(b, LangGo, benchGo) }
func BenchmarkHighlightGoSX(b *testing.B) { benchHighlight(b, LangGoSX, benchGSX) }
func BenchmarkHighlightBash(b *testing.B) { benchHighlight(b, LangBash, benchBash) }
func BenchmarkHighlightJS(b *testing.B)   { benchHighlight(b, LangJavaScript, benchJS) }
func BenchmarkHighlightJSON(b *testing.B) { benchHighlight(b, LangJSON, benchJSON) }

func benchHighlight(b *testing.B, lang, src string) {
	HTML(lang, src)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		HTML(lang, src)
	}
}

// Render without the HTML memo to measure parser-pool misses.
func BenchmarkHighlightUncached(b *testing.B) {
	for _, sample := range []struct{ lang, source string }{{LangGo, benchGo}, {LangGoSX, benchGSX}, {LangBash, benchBash}, {LangJavaScript, benchJS}, {LangJSON, benchJSON}} {
		b.Run(sample.lang, func(b *testing.B) {
			renderHTML(sample.lang, sample.source)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				renderHTML(sample.lang, sample.source)
			}
		})
	}
}
