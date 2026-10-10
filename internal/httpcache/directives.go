package httpcache

import "strings"

// Directives preserves repeated Cache-Control arguments. Names are folded;
// argument values are not. In particular, extension arguments are opaque.
type Directives map[string][]string

// ParseDirectives parses the combined Cache-Control field (RFC 9111 §5.2).
// Commas inside quoted strings are data, and quoted-pairs remove one escape.
// Invalid syntax is reported without exposing a header value.
func ParseDirectives(header string) (Directives, bool) {
	out := Directives{}
	start, quoted, escaped := 0, false, false
	for i := 0; i <= len(header); i++ {
		if i < len(header) {
			c := header[i]
			if escaped {
				escaped = false
				continue
			}
			if quoted && c == '\\' {
				escaped = true
				continue
			}
			if c == '"' {
				quoted = !quoted
				continue
			}
			if quoted || c != ',' {
				continue
			}
		}
		if quoted || escaped {
			return nil, false
		}
		part := strings.Trim(header[start:i], " \t")
		start = i + 1
		if part == "" { // RFC 9110 §5.6.1.2: ignore empty list members.
			continue
		}
		name, value, hasValue := strings.Cut(part, "=")
		name = strings.Trim(name, " \t")
		if !cacheToken(name) {
			return nil, false
		}
		if hasValue {
			value = strings.Trim(value, " \t")
			if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
				var unquoted strings.Builder
				for j := 1; j < len(value)-1; j++ {
					if value[j] == '\\' {
						j++
						if j >= len(value)-1 {
							return nil, false
						}
					} else if value[j] == '"' {
						return nil, false
					}
					if value[j] < 32 && value[j] != '\t' || value[j] == 127 {
						return nil, false
					}
					unquoted.WriteByte(value[j])
				}
				value = unquoted.String()
			} else if !cacheToken(value) {
				return nil, false
			}
		}
		name = strings.ToLower(name)
		out[name] = append(out[name], value)
	}
	return out, true
}

func cacheToken(s string) bool {
	if s == "" {
		return false
	}
	for i := range s {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", rune(c)) {
			continue
		}
		return false
	}
	return true
}

func (d Directives) Has(name string) bool { return len(d[name]) > 0 }

// UniqueValue rejects repeated freshness directives as stale (RFC 9111
// §4.2.1), including repeats whose arguments happen to be identical.
func (d Directives) UniqueValue(name string) (string, bool) {
	values := d[name]
	if len(values) != 1 {
		return "", false
	}
	return values[0], true
}
