package wire

import (
	"strings"

	"golang.org/x/net/html"
	"m31labs.dev/gosx/internal/pagecaps"
)

// HTML completeness is a positive allowlist, derived from the attribute index:
// https://html.spec.whatwg.org/multipage/indices.html#attributes-3
// URL fields outside the implemented subset remain unresolved, including
// obsolete attributes, microdata URLs, navigation and nested documents.
var htmlReferenceElements = htmlReferenceWords("a abbr address area article aside audio b base bdi bdo blockquote body br button canvas caption cite code col colgroup data datalist dd del details dfn dialog div dl dt em embed fieldset figcaption figure footer form h1 h2 h3 h4 h5 h6 head header hgroup hr html i iframe img input ins kbd label legend li link main map mark menu meta meter nav noscript object ol optgroup option output p picture pre progress q rp rt ruby s samp script search section select selectedcontent slot small source span strong style sub summary sup table tbody td template textarea tfoot th thead time title tr track u ul var video wbr")

var htmlReferenceGlobalAttributes = htmlReferenceWords("id class title lang dir hidden inert tabindex accesskey translate draggable spellcheck contenteditable autocapitalize autocorrect autofocus inputmode enterkeyhint popover slot nonce part exportparts itemscope itemref role aria-label aria-labelledby aria-describedby aria-hidden aria-live aria-atomic aria-busy aria-controls aria-current aria-disabled aria-expanded aria-haspopup aria-pressed aria-selected aria-checked aria-required aria-invalid aria-valuemin aria-valuemax aria-valuenow aria-valuetext")

var htmlReferenceElementAttributes = map[string]map[string]bool{
	"a":        htmlReferenceWords("target download rel hreflang type referrerpolicy"),
	"area":     htmlReferenceWords("alt coords shape target download rel referrerpolicy"),
	"audio":    htmlReferenceWords("crossorigin preload autoplay loading loop muted controls"),
	"base":     htmlReferenceWords("target"),
	"button":   htmlReferenceWords("command commandfor disabled form formenctype formmethod formnovalidate formtarget name popovertarget popovertargetaction type value"),
	"canvas":   htmlReferenceWords("width height"),
	"col":      htmlReferenceWords("span"),
	"colgroup": htmlReferenceWords("span"),
	"data":     htmlReferenceWords("value"),
	"del":      htmlReferenceWords("datetime"),
	"details":  htmlReferenceWords("name open"),
	"dialog":   htmlReferenceWords("open closedby"),
	"embed":    htmlReferenceWords("type width height"),
	"fieldset": htmlReferenceWords("disabled form name"),
	"form":     htmlReferenceWords("accept-charset autocomplete enctype method name novalidate rel target"),
	"iframe":   htmlReferenceWords("name sandbox allowfullscreen width height referrerpolicy loading"),
	"img":      htmlReferenceWords("alt sizes crossorigin ismap width height referrerpolicy decoding loading fetchpriority"),
	"input":    htmlReferenceWords("accept alpha alt autocomplete checked colorspace dirname disabled form formenctype formmethod formnovalidate formtarget height list max maxlength min minlength multiple name pattern placeholder popovertarget popovertargetaction readonly required size step type value width"),
	"ins":      htmlReferenceWords("datetime"),
	"label":    htmlReferenceWords("for"),
	"li":       htmlReferenceWords("value"),
	"link":     htmlReferenceWords("crossorigin rel as media hreflang type sizes imagesizes referrerpolicy integrity blocking color disabled fetchpriority"),
	"map":      htmlReferenceWords("name"),
	"meta":     htmlReferenceWords("name http-equiv charset media"),
	"meter":    htmlReferenceWords("value min max low high optimum"),
	"object":   htmlReferenceWords("type name form width height"),
	"ol":       htmlReferenceWords("reversed start type"),
	"optgroup": htmlReferenceWords("disabled label"),
	"option":   htmlReferenceWords("disabled label selected value"),
	"output":   htmlReferenceWords("for form name"),
	"progress": htmlReferenceWords("value max"),
	"script":   htmlReferenceWords("type async defer crossorigin integrity referrerpolicy blocking fetchpriority"),
	"select":   htmlReferenceWords("autocomplete disabled form multiple name required size"),
	"slot":     htmlReferenceWords("name"),
	"source":   htmlReferenceWords("type media sizes width height"),
	"style":    htmlReferenceWords("media blocking type"),
	"td":       htmlReferenceWords("colspan rowspan headers"),
	"textarea": htmlReferenceWords("autocomplete cols dirname disabled form maxlength minlength name placeholder readonly required rows wrap"),
	"th":       htmlReferenceWords("abbr colspan rowspan headers scope"),
	"time":     htmlReferenceWords("datetime"),
	"track":    htmlReferenceWords("default kind label srclang"),
	"video":    htmlReferenceWords("crossorigin preload autoplay loading playsinline loop muted controls width height"),
}

func htmlReferenceWords(words string) map[string]bool {
	set := map[string]bool{}
	for _, word := range strings.Fields(words) {
		set[word] = true
	}
	return set
}

func understoodHTMLReferenceAttribute(n *html.Node, a html.Attribute) bool {
	if n.Namespace != "" || a.Namespace != "" {
		return false
	}
	if strings.HasPrefix(a.Key, "data-gosx-") && strings.HasSuffix(a.Key, "-url") {
		return htmlReferenceURL(a.Val, "")
	}
	switch a.Key {
	case "srcdoc":
		return n.Data == "iframe"
	case "style":
		// The syntax scanner checks URL literals, substitutions and functions.
		return true
	case "src":
		switch n.Data {
		case "script":
			if !pagecaps.ExecutableScriptType(attr(n, "type")) {
				return knownHTMLDataScript(attr(n, "type"))
			}
		case "img":
			return htmlReferenceURL(a.Val, KindImage)
		case "input":
			return strings.EqualFold(attr(n, "type"), "image") && htmlReferenceURL(a.Val, KindImage)
		case "audio", "iframe", "source", "track", "video":
		default:
			return false
		}
		return htmlReferenceURL(a.Val, "")
	case "href":
		kind := ""
		if strings.EqualFold(attr(n, "rel"), "preload") || strings.EqualFold(attr(n, "rel"), "prefetch") {
			switch strings.ToLower(attr(n, "as")) {
			case "image":
				kind = KindImage
			case "font":
				kind = KindFont
			}
		}
		return n.Data == "base" && (strings.TrimSpace(a.Val) == "" || htmlReferenceURL(a.Val, "")) || n.Data == "link" && htmlReferenceURL(a.Val, kind)
	case "poster":
		return n.Data == "video" && htmlReferenceURL(a.Val, KindImage)
	case "srcset":
		return (n.Data == "img" || n.Data == "source") && strings.TrimSpace(a.Val) == ""
	case "imagesrcset":
		return n.Data == "link" && strings.TrimSpace(a.Val) == ""
	case "content":
		if n.Data != "meta" {
			return false
		}
		switch strings.ToLower(strings.TrimSpace(attr(n, "http-equiv"))) {
		case "", "content-type", "default-style", "x-ua-compatible":
			return true
		default:
			// Refresh and embedded CSP can initiate or change network activity.
			return false
		}
	case "http-equiv":
		if n.Data != "meta" {
			return false
		}
		switch strings.ToLower(strings.TrimSpace(a.Val)) {
		case "content-type", "default-style", "x-ua-compatible":
			return true
		default:
			return false
		}
	}
	return htmlReferenceGlobalAttributes[a.Key] || htmlReferenceElementAttributes[n.Data][a.Key]
}

func htmlReferenceURL(raw, kind string) bool {
	value := strings.TrimSpace(raw)
	if value == "" || strings.HasPrefix(value, "#") {
		return false
	}
	return understoodReferenceURL(value, kind)
}

func knownHTMLDataScript(typ string) bool {
	switch strings.ToLower(strings.TrimSpace(typ)) {
	case "application/json", "application/ld+json", "text/plain":
		return true
	}
	return false
}

func understoodHTMLLink(n *html.Node) bool {
	rels := strings.Fields(strings.ToLower(attr(n, "rel")))
	if len(rels) == 0 {
		return false
	}
	for _, rel := range rels {
		switch rel {
		case "stylesheet", "modulepreload":
		case "preload", "prefetch":
			// Unknown destinations cannot inherit a resource kind from a suffix.
			// Empty destinations keep the compatibility URL-kind inference.
			switch strings.ToLower(attr(n, "as")) {
			case "", "script", "style", "font", "image":
			default:
				return false
			}
		default:
			return false
		}
	}
	return true
}
