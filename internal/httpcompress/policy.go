// Package httpcompress defines the shared policy for text response compression.
package httpcompress

import (
	"mime"
	"strconv"
	"strings"
)

const MinimumSize = 1024

// Accepts reports whether an encoding is allowed, including wildcard support.
// An explicit entry takes precedence over a wildcard, even when its q is zero.
func Accepts(header, encoding string) bool {
	wildcard := false
	for _, part := range strings.Split(header, ",") {
		fields := strings.Split(part, ";")
		token := strings.TrimSpace(fields[0])
		if !strings.EqualFold(token, encoding) && token != "*" {
			continue
		}
		allowed := true
		for _, param := range fields[1:] {
			key, value, ok := strings.Cut(strings.TrimSpace(param), "=")
			if !strings.EqualFold(strings.TrimSpace(key), "q") {
				continue
			}
			q, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			allowed = ok && err == nil && q > 0 && q <= 1
		}
		if strings.EqualFold(token, encoding) {
			return allowed
		}
		wildcard = allowed
	}
	return wildcard
}

// Compressible reports whether a media type represents text. Binary formats
// are excluded even if their bytes happen to compress well.
func Compressible(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	mediaType = strings.ToLower(mediaType)
	if strings.HasPrefix(mediaType, "text/") {
		return true
	}
	if !strings.HasPrefix(mediaType, "application/") {
		return false
	}
	switch mediaType {
	case "application/javascript", "application/x-javascript", "application/json",
		"application/xml", "application/graphql", "application/x-www-form-urlencoded":
		return true
	}
	return strings.HasSuffix(mediaType, "+json") || strings.HasSuffix(mediaType, "+xml")
}
