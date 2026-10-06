package basepath

import (
	"strings"

	"golang.org/x/net/html"
)

// HTML rewrites URL attributes without inspecting scripts, text, or arbitrary
// JSON. Unchanged tokens retain their original bytes, including script nonces.
func HTML(prefix, markup string) string {
	if prefix == "" {
		return markup
	}
	z := html.NewTokenizer(strings.NewReader(markup))
	var out strings.Builder
	for {
		kind := z.Next()
		if kind == html.ErrorToken {
			break
		}
		raw := append([]byte(nil), z.Raw()...)
		if kind != html.StartTagToken && kind != html.SelfClosingTagToken {
			out.Write(raw)
			continue
		}
		token := z.Token()
		changed := false
		for i := range token.Attr {
			a := &token.Attr[i]
			value := a.Val
			switch a.Key {
			case "href", "src", "action", "formaction", "poster", "data-gosx-engine-bytecode", "data-gosx-region-src", "data-gosx-revalidate-src", "data-gosx-live-src":
				a.Val = URL(prefix, value)
			case "data-gosx-action", "data-gosx-reorder-action", "data-gosx-transfer-action":
				method, target, ok := strings.Cut(value, " ")
				if ok {
					a.Val = method + " " + URL(prefix, target)
				} else {
					a.Val = URL(prefix, value)
				}
			case "srcset":
				// A data URL can contain commas; leave such lists to their author.
				if !strings.Contains(value, "data:") {
					parts := strings.Split(value, ",")
					for j, part := range parts {
						fields := strings.Fields(part)
						if len(fields) > 0 {
							fields[0] = URL(prefix, fields[0])
							parts[j] = strings.Join(fields, " ")
						}
					}
					a.Val = strings.Join(parts, ", ")
				}
			}
			if strings.HasPrefix(a.Key, "data-gosx-scene3d-") && strings.HasSuffix(a.Key, "-url") {
				a.Val = URL(prefix, value)
			}
			changed = changed || value != a.Val
		}
		if changed {
			out.WriteString(token.String())
		} else {
			out.Write(raw)
		}
	}
	return out.String()
}
