package main

import (
	"errors"
	"strings"
)

type previewModeQuery struct {
	prefix     string
	origin     string
	enabled    bool
	diagnostic string
}

// parsePreviewModeQuery extracts the preview-mode prefix + origin from a URL
// query string. Returns (prefix, origin, true) when the page should activate
// the cross-frame relay. The prefix defaults to "$preview.". The peer origin
// is mandatory so URL-driven preview mode never silently enables a wildcard.
//
// Query parameter contract — see ADR 0009 + plan section C of
// plans/2026-05-26-iframe-cross-frame-signal-transport.md:
//
//   - gosx-preview=1               → activate the relay
//   - gosx-preview-origin=<origin> → pin the expected peer origin
//   - gosx-preview-prefix=<prefix> → override the default $preview. prefix
//
// Lives in its own non-build-tagged file so unit tests can exercise the
// parser without needing a js+wasm runtime. cross_frame.go's runtime
// integration consumes the result.
func parsePreviewModeQuery(search string) (string, string, bool) {
	query := inspectPreviewModeQuery(search)
	return query.prefix, query.origin, query.enabled
}

func inspectPreviewModeQuery(search string) previewModeQuery {
	search = strings.TrimPrefix(search, "?")
	if search == "" {
		return previewModeQuery{}
	}
	active := false
	prefix := "$preview."
	origin := ""
	originDiagnostic := ""
	for _, part := range strings.Split(search, "&") {
		key, value := splitQueryPair(part)
		switch key {
		case "gosx-preview":
			if value == "1" || value == "true" {
				active = true
			}
		case "gosx-preview-origin":
			origin = ""
			originDiagnostic = ""
			if value == "" {
				continue
			}
			decoded, err := decodeQueryComponent(value)
			if err != nil {
				originDiagnostic = "gosx-preview-origin is malformed"
				continue
			}
			origin = decoded
		case "gosx-preview-prefix":
			if value != "" {
				decoded, err := decodeQueryComponent(value)
				if err == nil && decoded != "" {
					prefix = decoded
				}
			}
		}
	}
	if !active {
		return previewModeQuery{}
	}
	if originDiagnostic != "" {
		return previewModeQuery{diagnostic: originDiagnostic}
	}
	if strings.TrimSpace(origin) == "" {
		return previewModeQuery{diagnostic: "gosx-preview-origin is required"}
	}
	if origin == "*" {
		return previewModeQuery{diagnostic: "gosx-preview-origin must not use the wildcard origin"}
	}
	if !validPreviewOrigin(origin) {
		return previewModeQuery{diagnostic: "gosx-preview-origin must be an absolute http(s) origin without a path"}
	}
	return previewModeQuery{prefix: prefix, origin: origin, enabled: true}
}

func validPreviewOrigin(origin string) bool {
	if origin == "" || origin != strings.TrimSpace(origin) {
		return false
	}
	schemeEnd := strings.Index(origin, "://")
	if schemeEnd < 0 {
		return false
	}
	scheme := origin[:schemeEnd]
	if scheme != "http" && scheme != "https" {
		return false
	}
	authority := origin[schemeEnd+3:]
	if authority == "" || strings.ContainsAny(authority, "/?#@ \t\r\n") {
		return false
	}
	if authority[0] == '[' {
		closeBracket := strings.IndexByte(authority, ']')
		if closeBracket <= 1 {
			return false
		}
		return closeBracket == len(authority)-1 || validPreviewPort(authority[closeBracket+1:])
	}
	if strings.Count(authority, ":") > 1 {
		return false
	}
	if portAt := strings.LastIndexByte(authority, ':'); portAt >= 0 {
		return portAt > 0 && validPreviewPort(authority[portAt:])
	}
	return true
}

func validPreviewPort(s string) bool {
	if len(s) < 2 || s[0] != ':' {
		return false
	}
	for i := 1; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func splitQueryPair(part string) (string, string) {
	idx := strings.IndexByte(part, '=')
	if idx < 0 {
		return part, ""
	}
	return part[:idx], part[idx+1:]
}

// decodeQueryComponent is a tiny URL-component decoder. The full net/url
// package pulls in too much surface for the tiny WASM build; preview-mode
// origins are well-formed URLs (no exotic encoding) so a pared-down decoder
// suffices. Returns an error for malformed %xx escapes.
func decodeQueryComponent(s string) (string, error) {
	if !strings.Contains(s, "%") && !strings.Contains(s, "+") {
		return s, nil
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '+':
			b.WriteByte(' ')
		case '%':
			if i+2 >= len(s) {
				return "", errors.New("truncated percent escape")
			}
			hi, err := unhex(s[i+1])
			if err != nil {
				return "", err
			}
			lo, err := unhex(s[i+2])
			if err != nil {
				return "", err
			}
			b.WriteByte(byte(hi<<4 | lo))
			i += 2
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String(), nil
}

func unhex(c byte) (int, error) {
	switch {
	case '0' <= c && c <= '9':
		return int(c - '0'), nil
	case 'a' <= c && c <= 'f':
		return int(c-'a') + 10, nil
	case 'A' <= c && c <= 'F':
		return int(c-'A') + 10, nil
	}
	return 0, errors.New("invalid hex digit")
}
