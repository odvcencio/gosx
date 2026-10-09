package httpcompress

import "strings"

// ResponseEncoding combines an ordered Content-Encoding list across field
// lines. RFC 9110 §§5.6.1.2 and 8.4 require ignoring empty list members and
// comparing coding names case-insensitively. Multiple codings are preserved
// so a decoder can explicitly reject unsupported stacks.
func ResponseEncoding(values []string) string {
	var codings []string
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			if part = strings.Trim(part, " \t"); part != "" {
				codings = append(codings, strings.ToLower(part))
			}
		}
	}
	return strings.Join(codings, ", ")
}
