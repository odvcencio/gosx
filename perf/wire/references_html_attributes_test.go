package wire

import (
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestReferencesResponsiveImagePreloads(t *testing.T) {
	for _, href := range []string{"", ` href="/fallback.png"`} {
		body := `<link rel="preload" as="image" imagesrcset="/small.png 1x, /large.png 2x"` + href + `>`
		set, err := ScanReferences([]byte(body), KindDocument)
		if err != nil || set.Complete {
			t.Errorf("responsive preload claimed complete coverage: %+v %v", set, err)
		}
	}
}

func TestReferencesUnderstoodHTMLAttributesStayComplete(t *testing.T) {
	for _, tc := range []struct {
		body string
		want []Reference
	}{
		{`<p id="message" class="text" title="A URL /is/text">Plain content</p>`, []Reference{}},
		{`<img src="/opaque">`, []Reference{{"/opaque", KindImage, false}}},
		{`<input type="image" src="/opaque">`, []Reference{{"/opaque", KindImage, false}}},
		{`<iframe src="/child/"></iframe>`, []Reference{{"/child/", KindDocument, false}}},
		{`<video src="/opaque.png" poster="/opaque"></video>`, []Reference{{"/opaque", KindImage, false}, {"/opaque.png", KindOther, false}}},
		{`<link rel="preload" as="image" imagesrcset="" href="/opaque">`, []Reference{{"/opaque", KindImage, false}}},
		{`<img src="/opaque" srcset="">`, []Reference{{"/opaque", KindImage, false}}},
		{`<div style="background:url('/image.png');width:calc(100% - 1px)"></div>`, []Reference{{"/image.png", KindImage, false}}},
	} {
		set, err := ScanReferences([]byte(tc.body), KindDocument)
		if err != nil || !set.Complete || !reflect.DeepEqual(set.Resources, tc.want) {
			t.Errorf("understood syntax lost coverage or kind: %+v %v", set, err)
		}
	}
}

type htmlURLAttributeCase struct {
	attribute, elements, kind string
	list, incomplete          bool
}

// WHATWG HTML attribute index, plus URL-bearing obsolete features and SVG 2:
// https://html.spec.whatwg.org/multipage/indices.html#attributes-3
// https://html.spec.whatwg.org/multipage/obsolete.html
// https://www.w3.org/TR/SVG2/linking.html#XLinkHrefAttribute
// This test table is independent of the scanner's completeness allowlist.
var htmlURLAttributeCases = []htmlURLAttributeCase{
	{"src", "img input", KindImage, false, false},
	{"src", "script", KindScript, false, false},
	{"src", "iframe", KindDocument, false, false},
	{"src", "audio source track video", KindOther, false, false},
	{"src", "embed frame bgsound", KindOther, false, true},
	{"href", "link", KindStyle, false, false},
	{"href", "a area base", KindDocument, false, true},
	{"srcset", "img source", KindImage, true, true},
	{"imagesrcset", "link", KindImage, true, true},
	{"poster", "video", KindImage, false, false},
	{"data", "object", KindOther, false, true},
	{"action", "form", KindDocument, false, true},
	{"formaction", "button input", KindDocument, false, true},
	{"cite", "blockquote del ins q", KindDocument, false, true},
	{"ping", "a area", KindOther, true, true},
	{"itemid", "*", KindOther, false, true},
	{"itemprop", "*", KindOther, true, true},
	{"itemtype", "*", KindOther, true, true},
	{"usemap", "img input object", KindOther, false, true},
	{"srcdoc", "iframe", KindDocument, false, true},
	{"style", "*", KindImage, false, false},
	{"content", "meta", KindDocument, false, true},
	{"background", "body table thead tbody tfoot tr td th", KindImage, false, true},
	{"longdesc", "img iframe frame", KindDocument, false, true},
	{"manifest", "html", KindOther, false, true},
	{"profile", "head", KindOther, true, true},
	{"archive", "object applet", KindOther, true, true},
	{"codebase", "object applet", KindOther, false, true},
	{"classid", "object", KindOther, false, true},
	{"code", "object applet", KindOther, false, true},
	{"object", "applet", KindOther, false, true},
	{"lowsrc", "img", KindImage, false, true},
	{"dynsrc", "img", KindOther, false, true},
	{"datasrc", "div", KindOther, false, true},
	{"value", "param", KindOther, false, true},
	{"href", "svg:image svg:use svg:script", KindOther, false, true},
	{"xlink:href", "svg:image svg:use svg:script", KindOther, false, true},
}

const htmlAttributeIndexElements = "a abbr address area article aside audio b base bdi bdo blockquote body br button canvas caption cite code col colgroup data datalist dd del details dfn dialog div dl dt em embed fieldset figcaption figure footer form h1 h2 h3 h4 h5 h6 head header hgroup hr html i iframe img input ins kbd label legend li link main map mark menu meta meter nav noscript object ol optgroup option output p picture pre progress q rp rt ruby s samp script search section select selectedcontent slot small source span strong style sub summary sup table tbody td template textarea tfoot th thead time title tr track u ul var video wbr"

func htmlAttributeDocument(element, attributes string) string {
	if strings.HasPrefix(element, "svg:") {
		tag := strings.TrimPrefix(element, "svg:")
		return "<svg><" + tag + " " + attributes + "></" + tag + "></svg>"
	}
	if element == "source" {
		if strings.Contains(attributes, "srcset") {
			return "<picture><source " + attributes + "><img></picture>"
		}
		return "<video><source " + attributes + "></video>"
	}
	if element == "track" {
		return "<video><track " + attributes + "></video>"
	}
	if element == "area" {
		return "<map><area " + attributes + "></map>"
	}
	if element == "param" {
		return `<object><param name="movie" valuetype="ref" ` + attributes + "></object>"
	}
	tag := "<" + element + " " + attributes + "></" + element + ">"
	switch element {
	case "td", "th":
		return "<table><tbody><tr>" + tag + "</tr></tbody></table>"
	case "tr":
		return "<table><tbody>" + tag + "</tbody></table>"
	case "caption", "colgroup", "thead", "tbody", "tfoot":
		return "<table>" + tag + "</table>"
	case "col":
		return "<table><colgroup>" + tag + "</colgroup></table>"
	case "option", "optgroup":
		return "<select>" + tag + "</select>"
	case "frameset", "frame":
		return "<frameset>" + tag + "</frameset>"
	}
	return tag
}

func TestReferencesHTMLURLAttributeCorpus(t *testing.T) {
	rng := rand.New(rand.NewSource(53905))
	count, complete, incomplete, disagreements := 0, 0, 0, 0
	for _, row := range htmlURLAttributeCases {
		elements := strings.Fields(row.elements)
		if row.elements == "*" {
			elements = strings.Fields(htmlAttributeIndexElements)
		}
		for _, element := range elements {
			for variant := 0; variant < 6; variant++ {
				name := fmt.Sprintf("%s/%s/%d", row.attribute, element, variant)
				t.Run(name, func(t *testing.T) {
					count++
					urls := []string{fmt.Sprintf("/resource-%08x.png?q=a&b=c#x", rng.Uint32())}
					value := urls[0]
					if row.list {
						urls = append(urls, fmt.Sprintf("/alternate-%08x.png#large", rng.Uint32()))
						value += " " + urls[1]
						if row.attribute == "srcset" || row.attribute == "imagesrcset" {
							value = urls[0] + " 1x, " + urls[1] + " 2x"
						}
					}
					if row.attribute == "srcdoc" {
						value = `<iframe src="` + value + `"></iframe>`
					} else if row.attribute == "style" {
						value = `background:url("` + value + `")`
					} else if row.attribute == "content" {
						value = "0; URL=" + value
					}
					attribute := row.attribute
					if variant >= 3 {
						attribute = strings.ToUpper(attribute)
					}
					encoded := html.EscapeString(value)
					if variant%3 == 1 {
						encoded = strings.ReplaceAll(encoded, "/", "&#47;")
					}
					attributes := attribute + `="` + encoded + `"`
					switch {
					case element == "input" && row.attribute == "src":
						attributes += ` type="image"`
					case element == "link" && row.attribute == "imagesrcset":
						attributes += ` rel="preload" as="image"`
						if variant%2 != 0 {
							attributes += ` href="/fallback.png"`
							urls = append(urls, "/fallback.png")
						}
					case element == "link":
						attributes += ` rel="stylesheet"`
					case row.attribute == "content":
						attributes += ` http-equiv="refresh"`
					}
					set, err := ScanReferences([]byte(htmlAttributeDocument(element, attributes)), KindDocument)
					if err != nil {
						t.Fatal(err)
					}
					if !set.Complete {
						incomplete++
						return
					}
					complete++
					var want []Reference
					if element != "template" {
						for _, u := range urls {
							want = append(want, Reference{u, row.kind, false})
						}
					}
					sort.Slice(want, func(i, j int) bool { return want[i].URL < want[j].URL })
					if row.incomplete && element != "template" || !reflect.DeepEqual(set.Resources, append([]Reference{}, want...)) {
						disagreements++
						t.Errorf("unhandled or wrong complete URL set: %+v want %+v", set.Resources, want)
					}
				})
			}
		}
	}
	t.Logf("seed=53905 rows=%d documents=%d complete=%d incomplete=%d disagreements=%d", len(htmlURLAttributeCases), count, complete, incomplete, disagreements)
}

func TestReferencesUnknownHTMLConstructsAreIncomplete(t *testing.T) {
	for _, body := range []string{
		`<img data-future-source="/image.png">`, `<div future-url="/image.png"></div>`,
		`<resource-fixture></resource-fixture>`, `<img is="resource-fixture" src="/image.png">`,
		`<button onclick="fetch('/data')"></button>`, `<div onpointerenter=""></div>`,
		`<link rel="icon" href="/icon.png">`, `<link rel="future-loader" href="/target">`,
		`<link rel="preload" as="fetch" href="/data.png">`, `<link rel="preload" as="document" href="/child.png">`,
		`<link rel="preload" as="future-destination" href="/target">`,
		`<script type="speculationrules">{"prerender":[{"source":"list","urls":["/next/"]}]}</script>`,
		`<script type="text/javascript1.5" src="/legacy.js"></script>`,
		`<iframe src="data:text/html,&lt;script src='/nested.js'&gt;&lt;/script&gt;"></iframe>`,
		`<iframe src="javascript:fetch('/nested')"></iframe>`,
		`<svg><template><image href="/foreign.png"/></template></svg>`,
		`<div><template shadowrootmode="open"><img src="/shadow.png"></template></div>`,
		`<div><template shadowrootmode="closed"><script src="/shadow.js"></script></template></div>`,
		`<div style="background:image('/future.png')"></div>`,
	} {
		set, err := ScanReferences([]byte(body), KindDocument)
		if err == nil && set.Complete {
			t.Errorf("unknown loading behavior claimed complete coverage: %s", body)
		}
	}
}
