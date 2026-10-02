package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf16"

	"github.com/tdewolff/parse/v2"
	jsparse "github.com/tdewolff/parse/v2/js"
)

type compactFunction struct{ line, column, parameters, endLine, endColumn, parent, bodyDepth int }

// compactFunctions reads function tokens rather than searching shader strings
// or comments. Parameter counts also guard the order used by the second pass.
func compactFunctions(code string) ([]compactFunction, error) {
	input := parse.NewInputString(code)
	lexer := jsparse.NewLexer(input)
	var functions []compactFunction
	line, column, params, depth := 0, 0, -1, 0
	previous := jsparse.SemicolonToken
	braces, pendingBody := 0, -1
	var owners []int
	for {
		token, data := lexer.Next()
		if token == jsparse.ErrorToken {
			if lexer.Err() == io.EOF {
				break
			}
			return nil, lexer.Err()
		}
		if token == jsparse.DivToken || token == jsparse.DivEqToken {
			switch previous {
			case jsparse.EqToken, jsparse.OpenParenToken, jsparse.OpenBracketToken,
				jsparse.CommaToken, jsparse.ColonToken, jsparse.ReturnToken,
				jsparse.SemicolonToken, jsparse.NotToken, jsparse.AndToken, jsparse.OrToken,
				jsparse.QuestionToken, jsparse.ArrowToken:
				token, data = lexer.RegExp()
				if token == jsparse.ErrorToken {
					return nil, lexer.Err()
				}
			}
		}
		if token != jsparse.WhitespaceToken && token != jsparse.LineTerminatorToken && token != jsparse.CommentToken && token != jsparse.CommentLineTerminatorToken {
			previous = token
		}
		if token == jsparse.FunctionToken {
			parent := -1
			if len(owners) > 0 {
				parent = owners[len(owners)-1]
			}
			if params >= 0 || pendingBody >= 0 {
				return nil, fmt.Errorf("nested function parameter expression is unsupported")
			}
			functions = append(functions, compactFunction{line: line, column: column, parent: parent})
			params = len(functions) - 1
		} else if params >= 0 {
			switch token {
			case jsparse.OpenParenToken:
				depth++
				if depth == 1 {
					functions[params].parameters = 0
				}
			case jsparse.CloseParenToken:
				depth--
				if depth == 0 {
					pendingBody = params
					params = -1
				}
			case jsparse.IdentifierToken:
				if depth == 1 {
					functions[params].parameters++
				}
			}
		}
		ending := -1
		if token == jsparse.OpenBraceToken {
			braces++
			if pendingBody >= 0 {
				functions[pendingBody].bodyDepth = braces
				owners = append(owners, pendingBody)
				pendingBody = -1
			}
		} else if token == jsparse.CloseBraceToken {
			if len(owners) > 0 && functions[owners[len(owners)-1]].bodyDepth == braces {
				ending = owners[len(owners)-1]
				owners = owners[:len(owners)-1]
			}
			braces--
		}
		for _, r := range string(data) {
			if r == '\n' {
				line++
				column = 0
			} else if utf16.IsSurrogate(r) || r <= 0xffff {
				column++
			} else {
				column += 2
			}
		}
		if ending >= 0 {
			functions[ending].endLine = line
			functions[ending].endColumn = column
		}
	}
	if len(owners) > 0 {
		return nil, fmt.Errorf("unterminated function scope")
	}
	return functions, nil
}

type compactOrigin struct{ column, source, line, originalColumn int }

func compactMapRows(mapping string) ([][]compactOrigin, error) {
	var rows [][]compactOrigin
	source, line, originalColumn := 0, 0, 0
	for _, row := range strings.Split(mapping, ";") {
		column := 0
		var origins []compactOrigin
		for _, segment := range strings.Split(row, ",") {
			if segment == "" {
				continue
			}
			fields, err := decodeVLQSegment(segment)
			if err != nil {
				return nil, err
			}
			column += fields[0]
			if len(fields) < 4 {
				continue
			}
			source += fields[1]
			line += fields[2]
			originalColumn += fields[3]
			origins = append(origins, compactOrigin{column, source, line, originalColumn})
		}
		rows = append(rows, origins)
	}
	return rows, nil
}

// minifyCompactBundle preserves accurate function-entry origins after the
// second minifier. Its map deliberately has function granularity; debug builds
// retain esbuild's full token mappings. It fails closed if functions change.
func minifyCompactBundle(entry output, built builtBundle) (builtBundle, error) {
	code, err := minifyTdewolff(built.code)
	if err != nil {
		return builtBundle{}, err
	}
	before, err := compactFunctions(built.code)
	if err != nil {
		return builtBundle{}, err
	}
	after, err := compactFunctions(code)
	if err != nil {
		return builtBundle{}, err
	}
	if len(before) != len(after) {
		return builtBundle{}, fmt.Errorf("compact %s changed function count: %d -> %d", entry.name, len(before), len(after))
	}
	var m esbuildMap
	if err = json.Unmarshal([]byte(built.m), &m); err != nil {
		return builtBundle{}, err
	}
	rows, err := compactMapRows(m.Mappings)
	if err != nil {
		return builtBundle{}, err
	}
	type event struct{ line, column, origin int }
	var events []event
	origins := make([]*compactOrigin, len(before))
	for i, fn := range after {
		if fn.parameters > before[i].parameters || fn.parent != before[i].parent {
			return builtBundle{}, fmt.Errorf("compact %s changed function %d shape", entry.name, i)
		}
		old := before[i]
		if old.line >= len(rows) {
			return builtBundle{}, fmt.Errorf("compact %s has no function origin", entry.name)
		}
		for _, candidate := range rows[old.line] {
			if candidate.column > old.column {
				break
			}
			copy := candidate
			origins[i] = &copy
		}
		events = append(events, event{fn.line, fn.column, i}, event{fn.endLine, fn.endColumn, fn.parent})
	}
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].line != events[j].line {
			return events[i].line < events[j].line
		}
		return events[i].column < events[j].column
	})
	var mapping strings.Builder
	outputLine, previousColumn, previousSource, previousLine, previousOriginalColumn := 0, 0, 0, 0, 0
	hasSegment := false
	for _, e := range events {
		for outputLine < e.line {
			mapping.WriteByte(';')
			outputLine++
			previousColumn = 0
			hasSegment = false
		}
		if hasSegment {
			mapping.WriteByte(',')
		}
		mapping.WriteString(base64VLQEncode(e.column - previousColumn))
		previousColumn = e.column
		hasSegment = true
		if e.origin < 0 || origins[e.origin] == nil {
			continue
		}
		origin := origins[e.origin]
		mapping.WriteString(base64VLQEncode(origin.source - previousSource))
		mapping.WriteString(base64VLQEncode(origin.line - previousLine))
		mapping.WriteString(base64VLQEncode(origin.originalColumn - previousOriginalColumn))
		previousSource, previousLine, previousOriginalColumn = origin.source, origin.line, origin.originalColumn
	}
	m.Mappings = mapping.String()
	m.Names = nil
	raw, err := json.Marshal(m)
	if err != nil {
		return builtBundle{}, err
	}
	finalMap, err := normalizeESBuildMap(raw, entry.name)
	if err != nil {
		return builtBundle{}, err
	}
	return builtBundle{code: code, m: finalMap}, nil
}
