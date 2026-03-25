package ir

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/odvcencio/gosx/island/program"
)

// ExprScope holds the names known at the island level for resolving identifiers.
type ExprScope struct {
	Signals  map[string]bool // signal names
	Props    map[string]bool // prop names
	Handlers map[string]bool // handler names
}

// ParseExpr parses a Go expression source string into island opcodes.
// Returns the expression list and the root ExprID, or an error if the
// expression uses features outside the island subset.
func ParseExpr(source string, scope *ExprScope) ([]program.Expr, program.ExprID, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return nil, 0, fmt.Errorf("empty expression")
	}

	// Reject disallowed patterns early.
	if err := rejectDisallowed(source); err != nil {
		return nil, 0, err
	}

	p := &exprParser{
		source: source,
		scope:  scope,
	}

	rootID, err := p.parseExpr(source)
	if err != nil {
		return nil, 0, err
	}

	return p.exprs, rootID, nil
}

type exprParser struct {
	source string
	scope  *ExprScope
	exprs  []program.Expr
}

func (p *exprParser) addExpr(e program.Expr) program.ExprID {
	id := program.ExprID(len(p.exprs))
	p.exprs = append(p.exprs, e)
	return id
}

// rejectDisallowed rejects expressions that are outside the island subset.
func rejectDisallowed(source string) error {
	if strings.HasPrefix(source, "go ") {
		return fmt.Errorf("goroutine launch is not allowed in island expressions: %q", source)
	}
	if strings.HasPrefix(source, "<-") {
		return fmt.Errorf("channel receive is not allowed in island expressions: %q", source)
	}
	return nil
}

// parseExpr is the entry point for recursive-descent parsing.
// It handles operator precedence from lowest to highest.
func (p *exprParser) parseExpr(s string) (program.ExprID, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty expression")
	}

	// Precedence levels (lowest to highest):
	// 1. || (logical or)
	// 2. && (logical and)
	// 3. ==, != (equality)
	// 4. <, >, <=, >= (comparison)
	// 5. +, - (additive)
	// 6. *, /, % (multiplicative)
	// 7. unary (!, -)
	// 8. atoms (literals, identifiers, method calls, function calls)

	return p.parseOr(s)
}

func (p *exprParser) parseOr(s string) (program.ExprID, error) {
	return p.parseBinary(s, "||", program.OpOr, p.parseAnd)
}

func (p *exprParser) parseAnd(s string) (program.ExprID, error) {
	return p.parseBinary(s, "&&", program.OpAnd, p.parseEquality)
}

func (p *exprParser) parseEquality(s string) (program.ExprID, error) {
	// Try != before == to avoid matching the = in != first.
	if left, right, ok := splitBinaryOp(s, "!="); ok {
		return p.buildBinary(left, right, program.OpNeq, p.parseComparison)
	}
	if left, right, ok := splitBinaryOp(s, "=="); ok {
		return p.buildBinary(left, right, program.OpEq, p.parseComparison)
	}
	return p.parseComparison(s)
}

func (p *exprParser) parseComparison(s string) (program.ExprID, error) {
	// Try two-char operators first.
	if left, right, ok := splitBinaryOp(s, "<="); ok {
		return p.buildBinary(left, right, program.OpLte, p.parseAdditive)
	}
	if left, right, ok := splitBinaryOp(s, ">="); ok {
		return p.buildBinary(left, right, program.OpGte, p.parseAdditive)
	}
	// Single-char comparison operators. Must not match <= or >=.
	if left, right, ok := splitBinaryOpSingle(s, '<'); ok {
		return p.buildBinary(left, right, program.OpLt, p.parseAdditive)
	}
	if left, right, ok := splitBinaryOpSingle(s, '>'); ok {
		return p.buildBinary(left, right, program.OpGt, p.parseAdditive)
	}
	return p.parseAdditive(s)
}

func (p *exprParser) parseAdditive(s string) (program.ExprID, error) {
	// Split on + or - at the top level (rightmost to get left-associativity).
	// We need to be careful: - can be a unary minus.
	if left, right, ok := splitAdditiveOp(s, '+'); ok {
		return p.buildBinary(left, right, program.OpAdd, p.parseMultiplicative)
	}
	if left, right, ok := splitAdditiveOp(s, '-'); ok {
		return p.buildBinary(left, right, program.OpSub, p.parseMultiplicative)
	}
	return p.parseMultiplicative(s)
}

func (p *exprParser) parseMultiplicative(s string) (program.ExprID, error) {
	if left, right, ok := splitBinaryOpSingle(s, '*'); ok {
		return p.buildBinary(left, right, program.OpMul, p.parseUnary)
	}
	if left, right, ok := splitBinaryOpSingle(s, '/'); ok {
		return p.buildBinary(left, right, program.OpDiv, p.parseUnary)
	}
	if left, right, ok := splitBinaryOpSingle(s, '%'); ok {
		return p.buildBinary(left, right, program.OpMod, p.parseUnary)
	}
	return p.parseUnary(s)
}

func (p *exprParser) parseUnary(s string) (program.ExprID, error) {
	s = strings.TrimSpace(s)
	if len(s) == 0 {
		return 0, fmt.Errorf("empty expression")
	}

	// Logical not.
	if s[0] == '!' {
		inner := strings.TrimSpace(s[1:])
		operandID, err := p.parseUnary(inner)
		if err != nil {
			return 0, err
		}
		return p.addExpr(program.Expr{
			Op:       program.OpNot,
			Operands: []program.ExprID{operandID},
			Type:     program.TypeBool,
		}), nil
	}

	// Unary minus — but only if it's not a negative number literal.
	// If it starts with - and the rest is NOT a pure number, treat as unary neg.
	if s[0] == '-' {
		rest := strings.TrimSpace(s[1:])
		if rest == "" {
			return 0, fmt.Errorf("incomplete unary minus expression")
		}
		// If the rest looks like a non-numeric expression (identifier, etc.), it's unary neg.
		if !isNumericLiteral(rest) {
			operandID, err := p.parseUnary(rest)
			if err != nil {
				return 0, err
			}
			return p.addExpr(program.Expr{
				Op:       program.OpNeg,
				Operands: []program.ExprID{operandID},
				Type:     program.TypeAny,
			}), nil
		}
		// Otherwise fall through to atom parsing (negative number literal).
	}

	return p.parseAtom(s)
}

func (p *exprParser) parseAtom(s string) (program.ExprID, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty expression")
	}

	// Parenthesized expression.
	if s[0] == '(' && findMatchingParen(s, 0) == len(s)-1 {
		inner := strings.TrimSpace(s[1 : len(s)-1])
		return p.parseExpr(inner)
	}

	// String literal.
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		val, err := strconv.Unquote(s)
		if err != nil {
			return 0, fmt.Errorf("invalid string literal: %s", s)
		}
		return p.addExpr(program.Expr{
			Op:    program.OpLitString,
			Value: val,
			Type:  program.TypeString,
		}), nil
	}

	// Boolean literals.
	if s == "true" || s == "false" {
		return p.addExpr(program.Expr{
			Op:    program.OpLitBool,
			Value: s,
			Type:  program.TypeBool,
		}), nil
	}

	// Numeric literals (int or float), including negatives like "-3".
	if isNumericLiteral(s) {
		if strings.Contains(s, ".") {
			return p.addExpr(program.Expr{
				Op:    program.OpLitFloat,
				Value: s,
				Type:  program.TypeFloat,
			}), nil
		}
		return p.addExpr(program.Expr{
			Op:    program.OpLitInt,
			Value: s,
			Type:  program.TypeInt,
		}), nil
	}

	// Method call: ident.Method(args)
	if dotIdx := strings.Index(s, "."); dotIdx > 0 {
		receiver := s[:dotIdx]
		remainder := s[dotIdx+1:]
		if isIdent(receiver) {
			return p.parseMethodCall(receiver, remainder)
		}
	}

	// Function call: ident(args)
	if parenIdx := strings.Index(s, "("); parenIdx > 0 {
		name := strings.TrimSpace(s[:parenIdx])
		if isIdent(name) && s[len(s)-1] == ')' {
			return p.parseFunctionCall(name, s[parenIdx+1:len(s)-1])
		}
	}

	// Bare identifier.
	if isIdent(s) {
		return p.resolveIdent(s)
	}

	return 0, fmt.Errorf("cannot parse expression: %q", s)
}

// parseMethodCall handles receiver.Method(args) patterns.
func (p *exprParser) parseMethodCall(receiver, remainder string) (program.ExprID, error) {
	// remainder is like "Get()" or "Set(expr)" or "Update(handler)"
	parenIdx := strings.Index(remainder, "(")
	if parenIdx < 0 || remainder[len(remainder)-1] != ')' {
		return 0, fmt.Errorf("invalid method call on %q: %q", receiver, remainder)
	}

	method := remainder[:parenIdx]
	argsStr := strings.TrimSpace(remainder[parenIdx+1 : len(remainder)-1])

	// Check if receiver is a known signal.
	if p.scope != nil && p.scope.Signals != nil && p.scope.Signals[receiver] {
		switch method {
		case "Get":
			return p.addExpr(program.Expr{
				Op:    program.OpSignalGet,
				Value: receiver,
				Type:  program.TypeAny,
			}), nil

		case "Set":
			if argsStr == "" {
				return 0, fmt.Errorf("signal Set requires an argument")
			}
			argID, err := p.parseExpr(argsStr)
			if err != nil {
				return 0, fmt.Errorf("parsing Set argument: %w", err)
			}
			return p.addExpr(program.Expr{
				Op:       program.OpSignalSet,
				Operands: []program.ExprID{argID},
				Value:    receiver,
				Type:     program.TypeAny,
			}), nil

		case "Update":
			if argsStr == "" {
				return 0, fmt.Errorf("signal Update requires a handler argument")
			}
			return p.addExpr(program.Expr{
				Op:    program.OpSignalUpdate,
				Value: receiver,
				Type:  program.TypeAny,
			}), nil

		default:
			return 0, fmt.Errorf("unknown method %q on signal %q", method, receiver)
		}
	}

	return 0, fmt.Errorf("unknown receiver %q in method call", receiver)
}

// parseFunctionCall handles ident(args) patterns.
func (p *exprParser) parseFunctionCall(name, argsStr string) (program.ExprID, error) {
	if p.scope != nil && p.scope.Handlers != nil && p.scope.Handlers[name] {
		return p.addExpr(program.Expr{
			Op:    program.OpCall,
			Value: name,
			Type:  program.TypeAny,
		}), nil
	}
	return 0, fmt.Errorf("unknown function %q", name)
}

// resolveIdent maps a bare identifier to the appropriate opcode.
func (p *exprParser) resolveIdent(name string) (program.ExprID, error) {
	if p.scope != nil && p.scope.Signals != nil && p.scope.Signals[name] {
		return p.addExpr(program.Expr{
			Op:    program.OpSignalGet,
			Value: name,
			Type:  program.TypeAny,
		}), nil
	}
	if p.scope != nil && p.scope.Props != nil && p.scope.Props[name] {
		return p.addExpr(program.Expr{
			Op:    program.OpPropGet,
			Value: name,
			Type:  program.TypeAny,
		}), nil
	}
	return 0, fmt.Errorf("unknown identifier %q (not in signals or props)", name)
}

// parseBinary is a generic binary-op parser for multi-char operators.
// It looks for the rightmost top-level occurrence of the operator.
func (p *exprParser) parseBinary(s, op string, opCode program.OpCode, next func(string) (program.ExprID, error)) (program.ExprID, error) {
	if left, right, ok := splitBinaryOp(s, op); ok {
		return p.buildBinary(left, right, opCode, next)
	}
	return next(s)
}

// buildBinary parses left and right operands and emits a binary operation.
func (p *exprParser) buildBinary(left, right string, opCode program.OpCode, next func(string) (program.ExprID, error)) (program.ExprID, error) {
	leftID, err := p.parseExpr(left)
	if err != nil {
		return 0, err
	}
	rightID, err := next(right)
	if err != nil {
		return 0, err
	}
	return p.addExpr(program.Expr{
		Op:       opCode,
		Operands: []program.ExprID{leftID, rightID},
		Type:     program.TypeAny,
	}), nil
}

// splitBinaryOp finds the rightmost top-level occurrence of a multi-char operator.
// Returns left, right parts and true if found, or zero values and false otherwise.
func splitBinaryOp(s, op string) (string, string, bool) {
	depth := 0
	opLen := len(op)
	inString := false

	// Scan from right to left to get left-associativity.
	for i := len(s) - opLen; i >= 0; i-- {
		ch := s[i]

		// Handle string literals.
		if ch == '"' && (i == 0 || s[i-1] != '\\') {
			inString = !inString
			continue
		}
		if inString {
			continue
		}

		if ch == ')' {
			depth++
		} else if ch == '(' {
			depth--
		}

		if depth != 0 {
			continue
		}

		if s[i:i+opLen] == op {
			left := strings.TrimSpace(s[:i])
			right := strings.TrimSpace(s[i+opLen:])
			if left != "" && right != "" {
				return left, right, true
			}
		}
	}
	return "", "", false
}

// splitBinaryOpSingle finds the rightmost top-level occurrence of a single-char operator.
// It avoids matching the operator when it's part of a two-char operator.
func splitBinaryOpSingle(s string, op byte) (string, string, bool) {
	depth := 0
	inString := false

	for i := len(s) - 1; i >= 0; i-- {
		ch := s[i]

		if ch == '"' && (i == 0 || s[i-1] != '\\') {
			inString = !inString
			continue
		}
		if inString {
			continue
		}

		if ch == ')' {
			depth++
		} else if ch == '(' {
			depth--
		}

		if depth != 0 {
			continue
		}

		if ch == op {
			// Avoid matching two-char operators.
			// Check the char before and after for operator characters.
			if isPartOfMultiCharOp(s, i, op) {
				continue
			}

			left := strings.TrimSpace(s[:i])
			right := strings.TrimSpace(s[i+1:])
			if left != "" && right != "" {
				return left, right, true
			}
		}
	}
	return "", "", false
}

// splitAdditiveOp splits on + or - but only when the operator is a binary operator
// (i.e., the left side is non-empty and the operator isn't part of a multi-char op).
func splitAdditiveOp(s string, op byte) (string, string, bool) {
	depth := 0
	inString := false

	for i := len(s) - 1; i >= 0; i-- {
		ch := s[i]

		if ch == '"' && (i == 0 || s[i-1] != '\\') {
			inString = !inString
			continue
		}
		if inString {
			continue
		}

		if ch == ')' {
			depth++
		} else if ch == '(' {
			depth--
		}

		if depth != 0 {
			continue
		}

		if ch == op {
			if isPartOfMultiCharOp(s, i, op) {
				continue
			}

			left := strings.TrimSpace(s[:i])
			right := strings.TrimSpace(s[i+1:])
			// Must have non-empty left to be a binary op (otherwise it's unary).
			if left != "" && right != "" {
				return left, right, true
			}
		}
	}
	return "", "", false
}

// isPartOfMultiCharOp checks whether the character at position i is part of a
// multi-character operator (like ==, !=, <=, >=, &&, ||).
func isPartOfMultiCharOp(s string, i int, op byte) bool {
	// Check if this is part of ==, !=, <=, >=
	if op == '=' {
		if i > 0 && (s[i-1] == '=' || s[i-1] == '!' || s[i-1] == '<' || s[i-1] == '>') {
			return true
		}
		if i+1 < len(s) && s[i+1] == '=' {
			return true
		}
	}
	if op == '<' {
		if i+1 < len(s) && s[i+1] == '=' {
			return true
		}
		// <-  channel receive (already rejected, but be safe)
		if i+1 < len(s) && s[i+1] == '-' {
			return true
		}
	}
	if op == '>' {
		if i+1 < len(s) && s[i+1] == '=' {
			return true
		}
	}
	if op == '&' {
		if i+1 < len(s) && s[i+1] == '&' {
			return true
		}
		if i > 0 && s[i-1] == '&' {
			return true
		}
	}
	if op == '|' {
		if i+1 < len(s) && s[i+1] == '|' {
			return true
		}
		if i > 0 && s[i-1] == '|' {
			return true
		}
	}
	if op == '!' {
		if i+1 < len(s) && s[i+1] == '=' {
			return true
		}
	}
	return false
}

// isNumericLiteral checks whether s is a valid integer or float literal.
func isNumericLiteral(s string) bool {
	if s == "" {
		return false
	}
	start := 0
	if s[0] == '-' {
		if len(s) == 1 {
			return false
		}
		start = 1
	}
	hasDot := false
	for i := start; i < len(s); i++ {
		if s[i] == '.' {
			if hasDot {
				return false
			}
			hasDot = true
			continue
		}
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// isIdent checks whether s is a valid Go identifier.
func isIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, ch := range s {
		if i == 0 {
			if !isLetter(ch) && ch != '_' {
				return false
			}
		} else {
			if !isLetter(ch) && !isDigit(ch) && ch != '_' {
				return false
			}
		}
	}
	return true
}

func isLetter(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')
}

func isDigit(ch rune) bool {
	return ch >= '0' && ch <= '9'
}

// findMatchingParen returns the index of the closing paren matching the
// opening paren at position start, or -1 if not found.
func findMatchingParen(s string, start int) int {
	depth := 0
	inString := false
	for i := start; i < len(s); i++ {
		ch := s[i]
		if ch == '"' && (i == 0 || s[i-1] != '\\') {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		if ch == '(' {
			depth++
		} else if ch == ')' {
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}
