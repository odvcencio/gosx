package main

import (
	"encoding/json"
	"regexp"
	"strings"
)

var shaderWholeFloat = regexp.MustCompile(`\b([0-9]+)\.0\b`)
var shaderCommaSpaces = regexp.MustCompile(`[ \t]*,[ \t]*`)
var shaderEqualsSpaces = regexp.MustCompile(`[ \t]*=[ \t]*`)
var shaderDivisionSpaces = regexp.MustCompile(`[ \t]+/[ \t]+`)
var shaderArrayStart = regexp.MustCompile(`^\s*(const|var|let) SCENE_[A-Z0-9_]+_(SOURCE|GLSL) = \[$`)

// compactShaderIndentation keeps authored shader text and line origins intact
// while dropping presentation-only indentation from shader array literals.
func compactShaderIndentation(code string) string {
	lines := strings.Split(code, "\n")
	shader := false
	for i, line := range lines {
		if shaderArrayStart.MatchString(line) {
			shader = true
			continue
		}
		if !shader {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "]") {
			shader = false
			continue
		}
		quoted := strings.TrimSuffix(trimmed, ",")
		var text string
		if json.Unmarshal([]byte(quoted), &text) != nil {
			continue
		}
		if strings.Contains(text, "gosxApplyCustom") {
			continue
		}
		compact := strings.TrimLeft(text, " \t")
		compact = shaderCommaSpaces.ReplaceAllString(compact, ",")
		compact = shaderEqualsSpaces.ReplaceAllString(compact, "=")
		compact = shaderDivisionSpaces.ReplaceAllString(compact, "/")
		compact = shaderWholeFloat.ReplaceAllString(compact, "${1}.")
		if compact == text {
			continue
		}
		lines[i] = strings.Replace(line, quoted, jsQuote(compact), 1)
	}
	return strings.Join(lines, "\n")
}

func compactBrowserSource(src source, code string) string {
	if src.rel == "../runtime/scene3d/webgl.ts" || src.rel == "../runtime/scene3d/webgl-ocean.ts" {
		return compactShaderIndentation(code)
	}
	return code
}
