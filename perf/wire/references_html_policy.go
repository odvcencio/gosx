package wire

import (
	"strings"

	"golang.org/x/net/html"
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

// Every present attribute reaches this dispatcher. An allowlisted metadata
// attribute is explicitly inert; a new or unsupported field fails closed.
func scanHTMLReferenceAttribute(n *html.Node, a html.Attribute, out *referenceScanner) error {
	if handled, err := scanExecutableReferenceAttribute(n, a, out); handled {
		return err
	}
	if n.Namespace != "" || a.Namespace != "" {
		out.drop(dropUnresolved)
		return nil
	}
	if strings.HasPrefix(a.Key, "data-gosx-") && strings.HasSuffix(a.Key, "-url") {
		if out.scriptsBlocked {
			out.drop(dropSandboxedExecutable)
			return nil
		}
		addFetchReference(out, a.Val, "", true)
		return nil
	}
	switch a.Key {
	case "srcdoc":
		if n.Data != "iframe" {
			out.drop(dropUnresolved)
			return nil
		}
		return scanSrcdocReferences(n, a.Val, out)
	case "style":
		if n.Data == "template" {
			out.drop(dropTemplateContent)
			return nil
		}
		if strings.TrimSpace(a.Val) == "" {
			out.drop(dropEmptySyntax)
			return nil
		}
		return scanSyntaxReferences([]byte(".inline{"+a.Val+"}"), KindStyle, out)
	case "src":
		kind := ""
		switch n.Data {
		case "script":
			if out.scriptsBlocked {
				out.drop(dropSandboxedExecutable)
				return nil
			}
			if !executableType(attr(n, "type")) {
				if knownHTMLDataScript(attr(n, "type")) {
					out.drop(dropInertDataScript)
				} else {
					out.drop(dropUnresolved)
				}
				return nil
			}
			kind = KindScript
		case "img":
			kind = KindImage
		case "input":
			if !strings.EqualFold(attr(n, "type"), "image") {
				out.drop(dropUnresolved)
				return nil
			}
			kind = KindImage
		case "iframe":
			kind = KindDocument
		case "audio", "source", "track", "video", "embed":
			kind = KindOther
			if n.Data == "embed" {
				out.drop(dropUnresolved)
			}
		default:
			out.drop(dropUnresolved)
			return nil
		}
		addFetchReference(out, a.Val, kind, false)
	case "href":
		if n.Data != "link" {
			out.drop(dropUnresolved)
			return nil
		}
		if !understoodHTMLLink(n) {
			out.drop(dropUnresolved)
		}
		for _, rel := range strings.Fields(strings.ToLower(attr(n, "rel"))) {
			kind := ""
			switch rel {
			case "stylesheet":
				kind = KindStyle
			case "modulepreload":
				kind = KindScript
			case "preload", "prefetch":
				switch strings.ToLower(attr(n, "as")) {
				case "script":
					kind = KindScript
				case "style":
					kind = KindStyle
				case "font":
					kind = KindFont
				case "image":
					kind = KindImage
				}
			default:
				out.drop(dropUnresolved)
				continue
			}
			addFetchReference(out, a.Val, kind, false)
		}
	case "poster":
		if n.Data == "video" {
			addFetchReference(out, a.Val, KindImage, false)
		} else {
			out.drop(dropUnresolved)
		}
	case "data":
		out.drop(dropUnresolved)
		if n.Data == "object" {
			addFetchReference(out, a.Val, "", false)
		}
	case "srcset", "imagesrcset":
		if strings.TrimSpace(a.Val) == "" && (a.Key == "srcset" && (n.Data == "img" || n.Data == "source") || a.Key == "imagesrcset" && n.Data == "link") {
			out.drop(dropInertHTML)
		} else {
			out.drop(dropUnresolved)
		}
	case "content":
		if n.Data == "meta" {
			switch strings.ToLower(strings.TrimSpace(attr(n, "http-equiv"))) {
			case "", "content-type", "default-style", "x-ua-compatible":
				out.drop(dropInertHTML)
				return nil
			}
		}
		out.drop(dropUnresolved)
	case "http-equiv":
		if n.Data == "meta" {
			switch strings.ToLower(strings.TrimSpace(a.Val)) {
			case "content-type", "default-style", "x-ua-compatible":
				out.drop(dropInertHTML)
				return nil
			}
		}
		out.drop(dropUnresolved)
	default:
		if htmlReferenceGlobalAttributes[a.Key] || htmlReferenceElementAttributes[n.Data][a.Key] {
			out.drop(dropInertHTML)
		} else {
			out.drop(dropUnresolved)
		}
	}
	return nil
}

func hasHTMLReferenceAttribute(n *html.Node, key string) bool {
	for _, a := range n.Attr {
		if a.Key == key {
			return true
		}
	}
	return false
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
