//go:build !tinygo

package gosx

import (
	"os"
	"testing"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

// Parser benchmarks.
//
// The IR benchmarks in ir/bench_danmuji_test.go parse once and then time only
// the lowering. These benchmarks time the parse itself, so a gotreesitter
// version bump shows up as a number.
//
// Run with: go test -run '^$' -bench Parse -benchmem .

const benchSmallSource = `package main

func App() Node {
	return <div class="counter">hello</div>
}
`

const benchMediumSource = `package main

//gosx:island
func Counter(start int) Node {
	count := Signal(start)
	doubled := Derive(func() int { return count.Get() * 2 })
	return <div class="counter">
		<h1>Count: {count.Get()}</h1>
		<p>Doubled: {doubled.Get()}</p>
		<button onClick={func() { count.Set(count.Get() + 1) }}>increment</button>
		<button onClick={func() { count.Set(0) }}>reset</button>
		<span>{count.Get() > 10 ? "high" : "low"}</span>
	</div>
}
`

const benchRawTextSource = `package main

func Page() Node {
	return <html>
		<head>
			<style>.a { color: red; } .b > .c { margin: 0 }</style>
			<script>if (a < b) { go(); } else { stop(); }</script>
		</head>
		<body>
			<script>{ClientScript()}</script>
		</body>
	</html>
}
`

// benchParse times full parses of source. It builds one parser per iteration,
// which is the shape of gosx.Parse, so the number includes parser setup.
func benchParse(b *testing.B, source string) {
	lang, err := Language()
	if err != nil {
		b.Fatalf("Language(): %v", err)
	}
	src := []byte(source)
	b.SetBytes(int64(len(src)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		parser := gotreesitter.NewParser(lang)
		tree, err := parser.Parse(src)
		if err != nil {
			b.Fatalf("parse: %v", err)
		}
		if tree.RootNode().HasError() {
			b.Fatal("parse produced an error node")
		}
	}
}

// benchParseReusedParser times parses through one parser. Compare it with
// benchParse to separate parser setup cost from parse cost.
func benchParseReusedParser(b *testing.B, source string) {
	lang, err := Language()
	if err != nil {
		b.Fatalf("Language(): %v", err)
	}
	src := []byte(source)
	parser := gotreesitter.NewParser(lang)
	b.SetBytes(int64(len(src)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tree, err := parser.Parse(src)
		if err != nil {
			b.Fatalf("parse: %v", err)
		}
		if tree.RootNode().HasError() {
			b.Fatal("parse produced an error node")
		}
	}
}

func BenchmarkParseSmall(b *testing.B)   { benchParse(b, benchSmallSource) }
func BenchmarkParseMedium(b *testing.B)  { benchParse(b, benchMediumSource) }
func BenchmarkParseRawText(b *testing.B) { benchParse(b, benchRawTextSource) }

func BenchmarkParseReuseSmall(b *testing.B)  { benchParseReusedParser(b, benchSmallSource) }
func BenchmarkParseReuseMedium(b *testing.B) { benchParseReusedParser(b, benchMediumSource) }

// BenchmarkParseDocsPage parses a real page from the docs example. It is the
// largest .gsx file that ships, so it shows throughput on realistic input.
func BenchmarkParseDocsPage(b *testing.B) {
	src, err := os.ReadFile("examples/gosx-docs/app/page.gsx")
	if err != nil {
		b.Skipf("read corpus file: %v", err)
	}
	benchParse(b, string(src))
}

// BenchmarkLanguageFromBlob times the blob decode that Language() performs on
// the first call. Startup cost matters: the CLI pays it on every invocation.
func BenchmarkLanguageFromBlob(b *testing.B) {
	if len(embeddedGrammarBlob) == 0 {
		b.Skip("no embedded grammar blob")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lang, err := LoadLanguageBlob(embeddedGrammarBlob)
		if err != nil {
			b.Fatalf("load blob: %v", err)
		}
		if lang == nil {
			b.Fatal("nil language")
		}
	}
}
