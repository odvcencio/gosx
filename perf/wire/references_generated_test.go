package wire

import (
	"encoding/json"
	"fmt"
	"html"
	"math/rand"
	"path"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestReferencesCSSLiteralEscapes(t *testing.T) {
	for _, tc := range []struct{ encoded, target string }{
		{`\u0061.png`, "u0061.png"}, {`\u0061.css`, "u0061.css"},
		{`\x61.png`, "x61.png"}, {`\61.png`, "a.png"},
		{`\000061 .png`, "a.png"}, {`\f.png`, "\x0f.png"},
		{`a\.png`, "a.png"}, {"a\\\n.png", "a.png"},
	} {
		for _, template := range []string{
			`.a{background:url("TARGET")}`, `.a{background:url('TARGET')}`,
			`.a{background:url(TARGET)}`, `@import "TARGET";`, `@import url("TARGET");`,
		} {
			// CSS line continuation is only valid inside quoted strings.
			if strings.Contains(tc.encoded, "\n") && template == `.a{background:url(TARGET)}` {
				continue
			}
			body := strings.ReplaceAll(template, "TARGET", tc.encoded)
			set, err := ScanReferences([]byte(body), KindStyle)
			if set.Complete && (err != nil || len(set.Resources) != 1 || set.Resources[0].URL != tc.target) {
				t.Errorf("CSS escape changed its target with complete coverage: source=%q got=%+v err=%v want=%q", body, set, err, tc.target)
			}
		}
	}
}

type referenceEncoding struct {
	name, text string
	// Expressions and line continuations are not allowed in every position.
	expression, continuation bool
}

type referencePosition struct {
	name, language, template, kind string
	potential, literal, unquoted   bool
}

func generatedReferenceTargets() []string {
	targets := []string{
		"./base.css", "./theme-dark.v2.css?mode=night#palette",
		"../images/logo-2.png#mark", "/images/photo.v3.webp?w=320&format=webp#crop",
		"./entry.mjs", "./module-name.v2.js?lang=en#run", "./worker-task.js?channel=1#ready",
		"/api/mesh-data.glb?quality=2&view=top#part", "../fonts/body-text.woff2#font",
		"./u0061.css", "./u0061.png", "./x61.png", "./n0061.png",
		"../unicode-café/scene-😀.glb?lang=fr#vue",
		// These nonzero code points exercise one-digit CSS hex escapes.
		"./control-\x07.bin", "\x0f.png",
	}
	rng := rand.New(rand.NewSource(53903))
	const letters = "abcdefnrtuxyz0123456789.-"
	for i := 0; i < 8; i++ {
		var name strings.Builder
		for j := 0; j < 14; j++ {
			name.WriteByte(letters[rng.Intn(len(letters))])
		}
		targets = append(targets, "./asset-"+name.String()+[]string{".css", ".png", ".js", ".mjs"}[i%4]+"?v=2&mode=fast#part-1")
	}
	return targets
}

func generatedReferencePositions() []referencePosition {
	positions := []referencePosition{
		{"css-url-double", KindStyle, `.a{background:url("TARGET")}`, "", false, false, false},
		{"css-url-single", KindStyle, `.a{background:url('TARGET')}`, "", false, false, false},
		{"css-url-unquoted", KindStyle, `.a{background:url(TARGET)}`, "", false, false, true},
		{"css-import-double", KindStyle, `@import "TARGET";`, KindStyle, false, false, false},
		{"css-import-single", KindStyle, `@import 'TARGET';`, KindStyle, false, false, false},
		{"css-import-url", KindStyle, `@import url("TARGET");`, KindStyle, false, false, false},
		{"css-import-url-unquoted", KindStyle, `@import url(TARGET);`, KindStyle, false, false, true},
		{"css-image-set", KindStyle, `.a{background:image-set("TARGET" 1x)}`, KindImage, false, false, false},
		{"css-webkit-image-set", KindStyle, `.a{background:-webkit-image-set("TARGET" 1x)}`, KindImage, false, false, false},
		{"css-image-set-url", KindStyle, `.a{background:image-set(url("TARGET") 1x)}`, "", false, false, false},
		{"css-font-url", KindStyle, `@font-face{src:url("TARGET")}`, "", false, false, false},
		{"html-inline-style", KindDocument, `<div style="TARGET"></div>`, "", false, false, false},
		{"html-style-element", KindDocument, `<style>.a{background:url("TARGET")}</style>`, "", false, false, false},
	}
	for _, tc := range []struct {
		name, template, kind string
		literal              bool
	}{
		{"js-import", `import TARGET;`, KindScript, true},
		{"js-import-from", `import value from TARGET;`, KindScript, true},
		{"js-export-from", `export {value} from TARGET;`, KindScript, true},
		{"js-export-all", `export * from TARGET;`, KindScript, true},
		{"js-import-call", `import(TARGET);`, KindScript, false},
		{"js-fetch", `fetch(TARGET);`, "", false},
		{"js-url", `new URL(TARGET,import.meta.url);`, "", false},
		{"js-worker", `new Worker(TARGET);`, KindScript, false},
		{"js-shared-worker", `new SharedWorker(TARGET);`, KindScript, false},
		{"js-worker-url", `new Worker(new URL(TARGET,import.meta.url));`, KindScript, false},
		{"js-shared-worker-url", `new SharedWorker(new URL(TARGET,import.meta.url));`, KindScript, false},
		{"js-import-scripts", `importScripts(TARGET);`, KindScript, false},
		{"js-service-worker", `navigator.serviceWorker.register(TARGET);`, "", false},
		{"js-event-source", `new EventSource(TARGET);`, "", false},
		{"js-web-socket", `new WebSocket(TARGET);`, "", false},
		{"js-xhr", `const request=new XMLHttpRequest();request.open("GET",TARGET);`, "", false},
	} {
		positions = append(positions, referencePosition{tc.name, KindScript, tc.template, tc.kind, false, tc.literal, false})
	}
	for _, global := range []string{"globalThis", "window", "self"} {
		for _, loader := range []string{"fetch", "Worker", "SharedWorker", "URL"} {
			expression, kind := global+"."+loader+"(TARGET);", ""
			if loader != "fetch" {
				expression = "new " + expression
			}
			if loader == "Worker" || loader == "SharedWorker" {
				kind = KindScript
			} else if loader == "URL" {
				expression = "new " + global + ".URL(TARGET,import.meta.url);"
			}
			positions = append(positions, referencePosition{global + "-" + loader, KindScript, expression, kind, false, false, false})
		}
	}
	for _, tc := range []struct{ name, template, kind string }{
		{"html-script", `<script src="TARGET"></script>`, KindScript},
		{"html-stylesheet", `<link rel="stylesheet" href="TARGET">`, KindStyle},
		{"html-modulepreload", `<link rel="modulepreload" href="TARGET">`, KindScript},
		{"html-preload-script", `<link rel="preload" as="script" href="TARGET">`, KindScript},
		{"html-preload-style", `<link rel="preload" as="style" href="TARGET">`, KindStyle},
		{"html-preload-font", `<link rel="preload" as="font" href="TARGET">`, KindFont},
		{"html-preload-image", `<link rel="preload" as="image" href="TARGET">`, KindImage},
		{"html-preload", `<link rel="preload" href="TARGET">`, ""},
		{"html-prefetch", `<link rel="prefetch" href="TARGET">`, ""},
		{"html-object", `<object data="TARGET"></object>`, ""},
		{"html-potential", `<div data-gosx-feature-url="TARGET"></div>`, ""},
	} {
		positions = append(positions, referencePosition{tc.name, KindDocument, tc.template, tc.kind, tc.name == "html-potential", false, false})
	}
	for _, tag := range []string{"img", "source", "video", "audio", "track", "iframe", "embed"} {
		positions = append(positions,
			referencePosition{"html-" + tag + "-src", KindDocument, "<" + tag + ` src="TARGET"></` + tag + ">", "", false, false, false},
			referencePosition{"html-" + tag + "-poster", KindDocument, "<" + tag + ` poster="TARGET"></` + tag + ">", KindImage, false, false, false})
	}
	return positions
}

func generatedCSSEncodings(target string) []referenceEncoding {
	var plain, escaped strings.Builder
	for _, r := range target {
		// Literal escapes of hex digits have different CSS semantics.
		if r < 0x20 || r == 0x7f || strings.ContainsRune(`"'\()`, r) {
			fmt.Fprintf(&plain, "\\%x ", r)
		} else {
			plain.WriteRune(r)
		}
		if r < 0x20 || r == 0x7f || strings.ContainsRune("0123456789abcdefABCDEF", r) {
			fmt.Fprintf(&escaped, "\\%x ", r)
		} else {
			escaped.WriteByte('\\')
			escaped.WriteRune(r)
		}
	}
	encodings := []referenceEncoding{{name: "plain", text: plain.String()}, {name: "literal-escapes", text: escaped.String()}}
	// Escape one character as well as whole strings. Whole-string encodings can
	// conceal a wrong decoder by failing on an unrelated escape first.
	for index, r := range []rune(target) {
		if r < 0x20 || strings.ContainsRune(`0123456789abcdefABCDEF"'\()`, r) {
			continue
		}
		var encoded strings.Builder
		for j, c := range []rune(target) {
			if j == index {
				encoded.WriteByte('\\')
				encoded.WriteRune(c)
			} else if c < 0x20 || c == 0x7f || strings.ContainsRune(`"'\()`, c) {
				fmt.Fprintf(&encoded, "\\%x ", c)
			} else {
				encoded.WriteRune(c)
			}
		}
		encodings = append(encodings, referenceEncoding{name: fmt.Sprintf("literal-at-%d", index), text: encoded.String()})
	}
	for digits := 1; digits <= 6; digits++ {
		for _, whitespace := range []string{"", " ", "\t", "\n", "\r\n", "\f"} {
			var encoded strings.Builder
			for _, r := range target {
				hex := strconv.FormatInt(int64(r), 16)
				if len(hex) <= digits {
					encoded.WriteByte('\\')
					encoded.WriteString(strings.Repeat("0", digits-len(hex)) + hex + whitespace)
				} else {
					encoded.WriteRune(r)
				}
			}
			encodings = append(encodings, referenceEncoding{name: fmt.Sprintf("hex-%d/%q", digits, whitespace), text: encoded.String()})
		}
	}
	// Backslash-newline is a string continuation, not an unquoted URL escape.
	for _, newline := range []string{"\n", "\r", "\r\n", "\f"} {
		encodings = append(encodings, referenceEncoding{name: fmt.Sprintf("continuation/%q", newline), text: "\\" + newline + plain.String(), continuation: true})
	}
	return encodings
}

func generatedJSEncodings(target string) []referenceEncoding {
	raw, _ := json.Marshal(target)
	encodings := []referenceEncoding{{name: "double", text: string(raw)}}
	var single, hex, unicode, codePoint, literal strings.Builder
	for _, r := range target {
		if r < 0x20 || r == 0x7f || r == '\\' || r == '\'' {
			fmt.Fprintf(&single, "\\u%04x", r)
		} else {
			single.WriteRune(r)
		}
		if r <= 0xff {
			fmt.Fprintf(&hex, "\\x%02x", r)
		} else {
			fmt.Fprintf(&hex, "\\u{%x}", r)
		}
		if r <= 0xffff {
			fmt.Fprintf(&unicode, "\\u%04x", r)
		} else {
			cp := r - 0x10000
			fmt.Fprintf(&unicode, "\\u%04x\\u%04x", 0xd800+(cp>>10), 0xdc00+(cp&0x3ff))
		}
		fmt.Fprintf(&codePoint, "\\u{%x}", r)
		if strings.ContainsRune("./-?#&", r) {
			literal.WriteByte('\\')
			literal.WriteRune(r)
		} else if r < 0x20 || r == 0x7f {
			fmt.Fprintf(&literal, "\\x%02x", r)
		} else {
			literal.WriteRune(r)
		}
	}
	for _, tc := range []struct{ name, value string }{
		{"single", "'" + single.String() + "'"}, {"hex", `"` + hex.String() + `"`},
		{"unicode", `"` + unicode.String() + `"`}, {"code-point", `"` + codePoint.String() + `"`},
		{"literal-escapes", `"` + literal.String() + `"`},
	} {
		encodings = append(encodings, referenceEncoding{name: tc.name, text: tc.value})
	}
	for _, tc := range []struct{ name, value string }{
		{"hex", hex.String()}, {"unicode", unicode.String()}, {"code-point", codePoint.String()}, {"literal-escapes", literal.String()},
	} {
		encodings = append(encodings,
			referenceEncoding{name: "single-" + tc.name, text: "'" + tc.value + "'"},
			referenceEncoding{name: "template-" + tc.name, text: "`" + tc.value + "`", expression: true})
	}
	for _, newline := range []string{"\n", "\r", "\r\n", "\u2028", "\u2029"} {
		encodings = append(encodings, referenceEncoding{name: fmt.Sprintf("continuation/%q", newline), text: `"\` + newline + string(raw[1:]), continuation: true})
		encodings = append(encodings,
			referenceEncoding{name: fmt.Sprintf("single-continuation/%q", newline), text: "'\\" + newline + single.String() + "'", continuation: true},
			referenceEncoding{name: fmt.Sprintf("template-continuation/%q", newline), text: "`\\" + newline + string(raw[1:len(raw)-1]) + "`", expression: true, continuation: true})
	}
	_, firstWidth := utf8.DecodeRuneInString(target)
	left, _ := json.Marshal(target[:firstWidth])
	right, _ := json.Marshal(target[firstWidth:])
	encodings = append(encodings,
		referenceEncoding{name: "concatenation", text: string(left) + "+" + string(right), expression: true},
		referenceEncoding{name: "template", text: "`" + target + "`", expression: true},
		referenceEncoding{name: "template-substitution", text: "`${" + string(left) + "}" + target[firstWidth:] + "`", expression: true})
	return encodings
}

func generatedHTMLEncodings(target string) []referenceEncoding {
	encodings := []referenceEncoding{{name: "named", text: html.EscapeString(target)}}
	for _, radix := range []int{10, 16} {
		for _, terminator := range []string{";", ""} {
			var encoded strings.Builder
			for _, r := range target {
				encoded.WriteString("&#")
				if radix == 16 {
					encoded.WriteByte('x')
				}
				encoded.WriteString(strconv.FormatInt(int64(r), radix) + terminator)
			}
			encodings = append(encodings, referenceEncoding{name: fmt.Sprintf("numeric-%d/%q", radix, terminator), text: encoded.String()})
		}
	}
	return encodings
}

func generatedReferenceKind(target string) string {
	name := strings.FieldsFunc(target, func(r rune) bool { return r == '?' || r == '#' })[0]
	switch strings.ToLower(path.Ext(name)) {
	case ".css":
		return KindStyle
	case ".js", ".mjs":
		return KindScript
	case ".woff2":
		return KindFont
	case ".png", ".webp":
		return KindImage
	default:
		return KindOther
	}
}

func TestReferencesGeneratedEncodingCorpus(t *testing.T) {
	seen := map[string]bool{}
	failures := map[string]int{}
	count, complete := 0, 0
	for _, target := range generatedReferenceTargets() {
		for _, position := range generatedReferencePositions() {
			var encodings []referenceEncoding
			switch {
			case position.language == KindStyle || strings.HasPrefix(position.name, "html-inline-style") || position.name == "html-style-element":
				encodings = generatedCSSEncodings(target)
			case position.language == KindScript:
				encodings = generatedJSEncodings(target)
			default:
				encodings = generatedHTMLEncodings(target)
			}
			for _, encoding := range encodings {
				if position.literal && encoding.expression || position.unquoted && encoding.continuation {
					continue
				}
				value := encoding.text
				if position.name == "html-inline-style" {
					value = html.EscapeString(`background:url("` + value + `")`)
				}
				body := strings.ReplaceAll(position.template, "TARGET", value)
				key := position.language + "\x00" + body
				if seen[key] {
					continue
				}
				seen[key] = true
				count++
				set, err := ScanReferences([]byte(body), position.language)
				if !set.Complete {
					continue
				}
				complete++
				kind := position.kind
				if kind == "" {
					kind = generatedReferenceKind(target)
				}
				want := Reference{URL: target, Kind: kind, Potential: position.potential}
				got := referenceValues(set.Resources)
				if err != nil || len(got) != 1 || got[0] != want {
					category := position.name + "/" + encoding.name
					if failures[category] == 0 {
						t.Logf("wrong complete result: %s target=%q source=%q got=%+v err=%v", category, target, body, set, err)
					}
					failures[category]++
				}
			}
		}
	}
	t.Logf("fixed seed 53903: %d unique sources, %d complete scans", count, complete)
	if count != 40853 {
		t.Fatalf("generated corpus size changed: %d", count)
	}
	if complete == 0 {
		t.Fatal("generated corpus produced no complete scans")
	}
	if len(failures) > 0 {
		categories := []string{}
		for category, n := range failures {
			categories = append(categories, fmt.Sprintf("%s=%d", category, n))
		}
		sort.Strings(categories)
		t.Fatalf("generated corpus found wrong complete results: %s", strings.Join(categories, ", "))
	}
}
