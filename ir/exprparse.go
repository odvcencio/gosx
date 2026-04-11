package ir

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/odvcencio/gosx/island/program"
)

// ExprScope holds the names known at the island level for resolving identifiers.
type ExprScope struct {
	Signals       map[string]bool   // runtime signal names
	SignalAliases map[string]string // local variable -> runtime signal name
	Props         map[string]bool   // prop names
	Handlers      map[string]bool   // handler names
	EventFields   map[string]bool   // current event payload fields available inside handlers
}

// ParseExpr parses a GoSX island expression into VM opcodes.
func ParseExpr(source string, scope *ExprScope) ([]program.Expr, program.ExprID, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return nil, 0, fmt.Errorf("empty expression")
	}
	if err := islandExprRestrictionError(source); err != nil {
		return nil, 0, err
	}

	tokens, err := lexExpr(source)
	if err != nil {
		return nil, 0, err
	}

	p := &exprParser{
		source: source,
		scope:  scope,
		tokens: tokens,
	}

	rootID, err := p.parseConditional()
	if err != nil {
		return nil, 0, err
	}
	if p.peek().kind != tokenEOF {
		return nil, 0, fmt.Errorf("unexpected token %q", p.peek().text)
	}
	return p.exprs, rootID, nil
}

type exprParser struct {
	source string
	scope  *ExprScope
	exprs  []program.Expr
	tokens []exprToken
	pos    int
}

func (p *exprParser) addExpr(e program.Expr) program.ExprID {
	id := program.ExprID(len(p.exprs))
	p.exprs = append(p.exprs, e)
	return id
}

func (p *exprParser) parseConditional() (program.ExprID, error) {
	condID, err := p.parseOr()
	if err != nil {
		return 0, err
	}
	if !p.match(tokenQuestion) {
		return condID, nil
	}

	trueID, err := p.parseConditional()
	if err != nil {
		return 0, err
	}
	if _, err := p.expect(tokenColon); err != nil {
		return 0, err
	}
	falseID, err := p.parseConditional()
	if err != nil {
		return 0, err
	}

	return p.addExpr(program.Expr{
		Op:       program.OpCond,
		Operands: []program.ExprID{condID, trueID, falseID},
		Type:     program.TypeAny,
	}), nil
}

func (p *exprParser) parseOr() (program.ExprID, error) {
	leftID, err := p.parseAnd()
	if err != nil {
		return 0, err
	}
	for p.match(tokenOrOr) {
		rightID, err := p.parseAnd()
		if err != nil {
			return 0, err
		}
		leftID = p.addExpr(program.Expr{
			Op:       program.OpOr,
			Operands: []program.ExprID{leftID, rightID},
			Type:     program.TypeBool,
		})
	}
	return leftID, nil
}

func (p *exprParser) parseAnd() (program.ExprID, error) {
	leftID, err := p.parseEquality()
	if err != nil {
		return 0, err
	}
	for p.match(tokenAndAnd) {
		rightID, err := p.parseEquality()
		if err != nil {
			return 0, err
		}
		leftID = p.addExpr(program.Expr{
			Op:       program.OpAnd,
			Operands: []program.ExprID{leftID, rightID},
			Type:     program.TypeBool,
		})
	}
	return leftID, nil
}

func (p *exprParser) parseEquality() (program.ExprID, error) {
	leftID, err := p.parseComparison()
	if err != nil {
		return 0, err
	}
	for {
		var op program.OpCode
		switch {
		case p.match(tokenEqEq):
			op = program.OpEq
		case p.match(tokenNotEq):
			op = program.OpNeq
		default:
			return leftID, nil
		}

		rightID, err := p.parseComparison()
		if err != nil {
			return 0, err
		}
		leftID = p.addExpr(program.Expr{
			Op:       op,
			Operands: []program.ExprID{leftID, rightID},
			Type:     program.TypeBool,
		})
	}
}

func (p *exprParser) parseComparison() (program.ExprID, error) {
	leftID, err := p.parseAdditive()
	if err != nil {
		return 0, err
	}
	for {
		var op program.OpCode
		switch {
		case p.match(tokenLt):
			op = program.OpLt
		case p.match(tokenGt):
			op = program.OpGt
		case p.match(tokenLte):
			op = program.OpLte
		case p.match(tokenGte):
			op = program.OpGte
		default:
			return leftID, nil
		}

		rightID, err := p.parseAdditive()
		if err != nil {
			return 0, err
		}
		leftID = p.addExpr(program.Expr{
			Op:       op,
			Operands: []program.ExprID{leftID, rightID},
			Type:     program.TypeBool,
		})
	}
}

func (p *exprParser) parseAdditive() (program.ExprID, error) {
	leftID, err := p.parseMultiplicative()
	if err != nil {
		return 0, err
	}
	for {
		var op program.OpCode
		switch {
		case p.match(tokenPlus):
			op = program.OpAdd
		case p.match(tokenMinus):
			op = program.OpSub
		default:
			return leftID, nil
		}

		rightID, err := p.parseMultiplicative()
		if err != nil {
			return 0, err
		}
		leftID = p.addExpr(program.Expr{
			Op:       op,
			Operands: []program.ExprID{leftID, rightID},
			Type:     program.TypeAny,
		})
	}
}

func (p *exprParser) parseMultiplicative() (program.ExprID, error) {
	leftID, err := p.parseUnary()
	if err != nil {
		return 0, err
	}
	for {
		var op program.OpCode
		switch {
		case p.match(tokenStar):
			op = program.OpMul
		case p.match(tokenSlash):
			op = program.OpDiv
		case p.match(tokenPercent):
			op = program.OpMod
		default:
			return leftID, nil
		}

		rightID, err := p.parseUnary()
		if err != nil {
			return 0, err
		}
		leftID = p.addExpr(program.Expr{
			Op:       op,
			Operands: []program.ExprID{leftID, rightID},
			Type:     program.TypeAny,
		})
	}
}

func (p *exprParser) parseUnary() (program.ExprID, error) {
	if p.match(tokenBang) {
		operandID, err := p.parseUnary()
		if err != nil {
			return 0, err
		}
		return p.addExpr(program.Expr{
			Op:       program.OpNot,
			Operands: []program.ExprID{operandID},
			Type:     program.TypeBool,
		}), nil
	}

	if p.match(tokenMinus) {
		if p.peek().kind == tokenNumber {
			tok := p.next()
			return p.literalNumber("-" + tok.text)
		}

		operandID, err := p.parseUnary()
		if err != nil {
			return 0, err
		}
		return p.addExpr(program.Expr{
			Op:       program.OpNeg,
			Operands: []program.ExprID{operandID},
			Type:     program.TypeAny,
		}), nil
	}

	return p.parsePostfix()
}

func (p *exprParser) parsePostfix() (program.ExprID, error) {
	baseID, err := p.parsePrimary()
	if err != nil {
		return 0, err
	}

	for {
		switch {
		case p.match(tokenDot):
			nameTok, err := p.expect(tokenIdent)
			if err != nil {
				return 0, err
			}
			if p.match(tokenLParen) {
				args, err := p.parseArgs()
				if err != nil {
					return 0, err
				}
				baseID, err = p.buildMethodCall(baseID, nameTok.text, args)
				if err != nil {
					return 0, err
				}
				continue
			}

			baseID, err = p.buildFieldAccess(baseID, nameTok.text)
			if err != nil {
				return 0, err
			}

		case p.match(tokenLBracket):
			indexID, err := p.parseConditional()
			if err != nil {
				return 0, err
			}
			if _, err := p.expect(tokenRBracket); err != nil {
				return 0, err
			}
			baseID = p.addExpr(program.Expr{
				Op:       program.OpIndex,
				Operands: []program.ExprID{baseID, indexID},
				Type:     program.TypeAny,
			})

		default:
			return baseID, nil
		}
	}
}

func (p *exprParser) parsePrimary() (program.ExprID, error) {
	switch tok := p.peek(); tok.kind {
	case tokenLParen:
		p.next()
		exprID, err := p.parseConditional()
		if err != nil {
			return 0, err
		}
		if _, err := p.expect(tokenRParen); err != nil {
			return 0, err
		}
		return exprID, nil

	case tokenString:
		p.next()
		val, err := strconv.Unquote(tok.text)
		if err != nil {
			return 0, fmt.Errorf("invalid string literal: %s", tok.text)
		}
		return p.addExpr(program.Expr{
			Op:    program.OpLitString,
			Value: val,
			Type:  program.TypeString,
		}), nil

	case tokenNumber:
		p.next()
		return p.literalNumber(tok.text)

	case tokenIdent:
		p.next()
		switch tok.text {
		case "true", "false":
			return p.addExpr(program.Expr{
				Op:    program.OpLitBool,
				Value: tok.text,
				Type:  program.TypeBool,
			}), nil
		}

		if p.match(tokenLParen) {
			args, err := p.parseArgs()
			if err != nil {
				return 0, err
			}
			return p.buildFunctionCall(tok.text, args)
		}
		return p.resolveIdent(tok.text)
	}

	return 0, fmt.Errorf("cannot parse expression: %q", p.source)
}

func (p *exprParser) parseArgs() ([]program.ExprID, error) {
	var args []program.ExprID
	if p.match(tokenRParen) {
		return args, nil
	}
	for {
		argID, err := p.parseConditional()
		if err != nil {
			return nil, err
		}
		args = append(args, argID)

		if p.match(tokenRParen) {
			return args, nil
		}
		if _, err := p.expect(tokenComma); err != nil {
			return nil, err
		}
	}
}

func (p *exprParser) buildFieldAccess(receiverID program.ExprID, field string) (program.ExprID, error) {
	if strings.EqualFold(field, "length") || strings.EqualFold(field, "len") {
		return p.addExpr(program.Expr{
			Op:       program.OpLen,
			Operands: []program.ExprID{receiverID},
			Type:     program.TypeInt,
		}), nil
	}

	fieldID := p.addExpr(program.Expr{
		Op:    program.OpLitString,
		Value: field,
		Type:  program.TypeString,
	})
	return p.addExpr(program.Expr{
		Op:       program.OpIndex,
		Operands: []program.ExprID{receiverID, fieldID},
		Type:     program.TypeAny,
	}), nil
}

func (p *exprParser) buildMethodCall(receiverID program.ExprID, method string, args []program.ExprID) (program.ExprID, error) {
	normalized := strings.ToLower(method)
	if signalName, ok := p.signalReceiver(receiverID); ok {
		if exprID, handled, err := p.buildSignalMethodCall(signalName, normalized, args); handled {
			return exprID, err
		}
	}
	if exprID, handled, err := p.buildLengthMethodCall(receiverID, normalized, method, args); handled {
		return exprID, err
	}
	if exprID, handled, err := p.buildUnaryNamedMethod(receiverID, normalized, method, args); handled {
		return exprID, err
	}
	if exprID, handled, err := p.buildLiteralValueMethod(receiverID, normalized, args); handled {
		return exprID, err
	}
	if exprID, handled, err := p.buildFixedArityMethod(receiverID, normalized, args); handled {
		return exprID, err
	}
	return 0, fmt.Errorf("unknown method %q", method)
}

func (p *exprParser) buildUnaryMethod(receiverID program.ExprID, args []program.ExprID, method string, op program.OpCode, typ program.ExprType) (program.ExprID, error) {
	if len(args) != 0 {
		return 0, fmt.Errorf("%s takes no arguments", method)
	}
	return p.addExpr(program.Expr{
		Op:       op,
		Operands: []program.ExprID{receiverID},
		Type:     typ,
	}), nil
}

func (p *exprParser) buildSignalMethodCall(signalName, method string, args []program.ExprID) (program.ExprID, bool, error) {
	switch method {
	case "get":
		if len(args) != 0 {
			return 0, true, fmt.Errorf("signal Get takes no arguments")
		}
		return p.addExpr(program.Expr{
			Op:    program.OpSignalGet,
			Value: signalName,
			Type:  program.TypeAny,
		}), true, nil
	case "set":
		if len(args) != 1 {
			return 0, true, fmt.Errorf("signal Set requires exactly one argument")
		}
		return p.addExpr(program.Expr{
			Op:       program.OpSignalSet,
			Operands: []program.ExprID{args[0]},
			Value:    signalName,
			Type:     program.TypeAny,
		}), true, nil
	case "update":
		if len(args) != 1 {
			return 0, true, fmt.Errorf("signal Update requires exactly one argument")
		}
		return p.addExpr(program.Expr{
			Op:       program.OpSignalUpdate,
			Operands: []program.ExprID{args[0]},
			Value:    signalName,
			Type:     program.TypeAny,
		}), true, nil
	default:
		return 0, false, nil
	}
}

func (p *exprParser) buildLengthMethodCall(receiverID program.ExprID, normalized, method string, args []program.ExprID) (program.ExprID, bool, error) {
	if normalized != "length" && normalized != "len" {
		return 0, false, nil
	}
	if len(args) != 0 {
		return 0, true, fmt.Errorf("%s takes no arguments", method)
	}
	return p.addExpr(program.Expr{
		Op:       program.OpLen,
		Operands: []program.ExprID{receiverID},
		Type:     program.TypeInt,
	}), true, nil
}

func (p *exprParser) buildUnaryNamedMethod(receiverID program.ExprID, normalized, method string, args []program.ExprID) (program.ExprID, bool, error) {
	spec, ok := unaryMethodSpecs[normalized]
	if !ok {
		return 0, false, nil
	}
	exprID, err := p.buildUnaryMethod(receiverID, args, method, spec.op, spec.typ)
	return exprID, true, err
}

func (p *exprParser) buildLiteralValueMethod(receiverID program.ExprID, normalized string, args []program.ExprID) (program.ExprID, bool, error) {
	spec, ok := literalValueMethodSpecs[normalized]
	if !ok {
		return 0, false, nil
	}
	if len(args) != 1 {
		return 0, true, fmt.Errorf("%s requires exactly one argument", spec.display)
	}
	expr := program.Expr{
		Op:       spec.op,
		Operands: []program.ExprID{receiverID},
		Type:     spec.typ,
	}
	if literal, ok := p.stringLiteralValue(args[0]); ok {
		expr.Value = literal
	} else {
		expr.Operands = append(expr.Operands, args[0])
	}
	return p.addExpr(expr), true, nil
}

func (p *exprParser) buildFixedArityMethod(receiverID program.ExprID, normalized string, args []program.ExprID) (program.ExprID, bool, error) {
	spec, ok := fixedArityMethodSpecs[normalized]
	if !ok {
		return 0, false, nil
	}
	if len(args) != spec.arity {
		switch spec.arity {
		case 1:
			return 0, true, fmt.Errorf("%s requires exactly one argument", spec.display)
		case 2:
			return 0, true, fmt.Errorf("%s requires exactly two arguments", spec.display)
		default:
			return 0, true, fmt.Errorf("%s requires exactly %d arguments", spec.display, spec.arity)
		}
	}
	operands := make([]program.ExprID, 0, 1+len(args))
	operands = append(operands, receiverID)
	operands = append(operands, args...)
	return p.addExpr(program.Expr{
		Op:       spec.op,
		Operands: operands,
		Type:     spec.typ,
	}), true, nil
}

func (p *exprParser) buildFunctionCall(name string, args []program.ExprID) (program.ExprID, error) {
	switch strings.ToLower(name) {
	case "len":
		if len(args) != 1 {
			return 0, fmt.Errorf("len requires exactly one argument")
		}
		return p.addExpr(program.Expr{
			Op:       program.OpLen,
			Operands: []program.ExprID{args[0]},
			Type:     program.TypeInt,
		}), nil
	}
	if p.scope != nil && p.scope.Handlers != nil && p.scope.Handlers[name] {
		return p.addExpr(program.Expr{
			Op:       program.OpCall,
			Operands: args,
			Value:    name,
			Type:     program.TypeAny,
		}), nil
	}
	return 0, fmt.Errorf("unknown function %q", name)
}

func (p *exprParser) resolveIdent(name string) (program.ExprID, error) {
	if p.scope != nil && p.scope.EventFields != nil && p.scope.EventFields[name] {
		return p.addExpr(program.Expr{
			Op:    program.OpEventGet,
			Value: name,
			Type:  program.TypeAny,
		}), nil
	}
	if p.scope != nil && p.scope.SignalAliases != nil {
		if signalName, ok := p.scope.SignalAliases[name]; ok && signalName != "" {
			return p.addExpr(program.Expr{
				Op:    program.OpSignalGet,
				Value: signalName,
				Type:  program.TypeAny,
			}), nil
		}
	}
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
	return p.addExpr(program.Expr{
		Op:    program.OpPropGet,
		Value: name,
		Type:  program.TypeAny,
	}), nil
}

func (p *exprParser) literalNumber(text string) (program.ExprID, error) {
	if strings.Contains(text, ".") {
		return p.addExpr(program.Expr{
			Op:    program.OpLitFloat,
			Value: text,
			Type:  program.TypeFloat,
		}), nil
	}
	return p.addExpr(program.Expr{
		Op:    program.OpLitInt,
		Value: text,
		Type:  program.TypeInt,
	}), nil
}

func (p *exprParser) signalReceiver(id program.ExprID) (string, bool) {
	if int(id) >= len(p.exprs) {
		return "", false
	}
	expr := p.exprs[id]
	if expr.Op != program.OpSignalGet || expr.Value == "" {
		return "", false
	}
	return expr.Value, true
}

func (p *exprParser) stringLiteralValue(id program.ExprID) (string, bool) {
	if int(id) >= len(p.exprs) {
		return "", false
	}
	expr := p.exprs[id]
	if expr.Op != program.OpLitString {
		return "", false
	}
	return expr.Value, true
}

func (p *exprParser) peek() exprToken {
	if p.pos >= len(p.tokens) {
		return exprToken{kind: tokenEOF}
	}
	return p.tokens[p.pos]
}

func (p *exprParser) next() exprToken {
	tok := p.peek()
	if p.pos < len(p.tokens) {
		p.pos++
	}
	return tok
}

func (p *exprParser) match(kind exprTokenKind) bool {
	if p.peek().kind != kind {
		return false
	}
	p.pos++
	return true
}

func (p *exprParser) expect(kind exprTokenKind) (exprToken, error) {
	tok := p.peek()
	if tok.kind != kind {
		return exprToken{}, fmt.Errorf("expected %s, found %q", kind, tok.text)
	}
	p.pos++
	return tok, nil
}

// rejectDisallowed rejects expressions that are outside the island subset.
func islandExprRestrictionError(source string) error {
	source = strings.TrimSpace(source)
	switch {
	case source == "":
		return nil
	case strings.HasPrefix(source, "go "):
		return fmt.Errorf("goroutine launch is not allowed in island expressions: %q", source)
	case disallowedGoroutineExpr(source):
		return fmt.Errorf("goroutine launch is not allowed in island expressions: %q", source)
	case strings.Contains(source, "<-"):
		return fmt.Errorf("channel operations are not allowed in island expressions: %q", source)
	case strings.Contains(source, "make(chan"):
		return fmt.Errorf("channel creation is not allowed in island expressions: %q", source)
	default:
		return nil
	}
}

func disallowedGoroutineExpr(source string) bool {
	if idx := strings.Index(source, "go "); idx >= 0 {
		return strings.Contains(source[idx:], "func")
	}
	return false
}

type unaryMethodSpec struct {
	op  program.OpCode
	typ program.ExprType
}

var unaryMethodSpecs = map[string]unaryMethodSpec{
	"toupper":  {op: program.OpToUpper, typ: program.TypeString},
	"tolower":  {op: program.OpToLower, typ: program.TypeString},
	"trim":     {op: program.OpTrim, typ: program.TypeString},
	"tostring": {op: program.OpToString, typ: program.TypeString},
	"toint":    {op: program.OpToInt, typ: program.TypeInt},
	"tofloat":  {op: program.OpToFloat, typ: program.TypeFloat},
}

type literalValueMethodSpec struct {
	display string
	op      program.OpCode
	typ     program.ExprType
}

var literalValueMethodSpecs = map[string]literalValueMethodSpec{
	"split": {display: "Split", op: program.OpSplit, typ: program.TypeAny},
	"join":  {display: "Join", op: program.OpJoin, typ: program.TypeString},
}

type fixedArityMethodSpec struct {
	display string
	arity   int
	op      program.OpCode
	typ     program.ExprType
}

var fixedArityMethodSpecs = map[string]fixedArityMethodSpec{
	"replace":    {display: "Replace", arity: 2, op: program.OpReplace, typ: program.TypeString},
	"substring":  {display: "Substring", arity: 2, op: program.OpSubstring, typ: program.TypeString},
	"startswith": {display: "StartsWith", arity: 1, op: program.OpStartsWith, typ: program.TypeBool},
	"endswith":   {display: "EndsWith", arity: 1, op: program.OpEndsWith, typ: program.TypeBool},
	"contains":   {display: "Contains", arity: 1, op: program.OpContains, typ: program.TypeBool},
	"append":     {display: "Append", arity: 1, op: program.OpAppend, typ: program.TypeAny},
	"slice":      {display: "Slice", arity: 2, op: program.OpSlice, typ: program.TypeAny},
	"map":        {display: "map", arity: 1, op: program.OpMap, typ: program.TypeAny},
	"filter":     {display: "filter", arity: 1, op: program.OpFilter, typ: program.TypeAny},
	"find":       {display: "find", arity: 1, op: program.OpFind, typ: program.TypeAny},
}

type exprTokenKind uint8

const (
	tokenEOF exprTokenKind = iota
	tokenIdent
	tokenNumber
	tokenString
	tokenLParen
	tokenRParen
	tokenLBracket
	tokenRBracket
	tokenDot
	tokenComma
	tokenQuestion
	tokenColon
	tokenPlus
	tokenMinus
	tokenStar
	tokenSlash
	tokenPercent
	tokenBang
	tokenAndAnd
	tokenOrOr
	tokenEqEq
	tokenNotEq
	tokenLt
	tokenGt
	tokenLte
	tokenGte
)

func (k exprTokenKind) String() string {
	switch k {
	case tokenEOF:
		return "end of expression"
	case tokenIdent:
		return "identifier"
	case tokenNumber:
		return "number"
	case tokenString:
		return "string"
	case tokenLParen:
		return "("
	case tokenRParen:
		return ")"
	case tokenLBracket:
		return "["
	case tokenRBracket:
		return "]"
	case tokenDot:
		return "."
	case tokenComma:
		return ","
	case tokenQuestion:
		return "?"
	case tokenColon:
		return ":"
	case tokenPlus:
		return "+"
	case tokenMinus:
		return "-"
	case tokenStar:
		return "*"
	case tokenSlash:
		return "/"
	case tokenPercent:
		return "%"
	case tokenBang:
		return "!"
	case tokenAndAnd:
		return "&&"
	case tokenOrOr:
		return "||"
	case tokenEqEq:
		return "=="
	case tokenNotEq:
		return "!="
	case tokenLt:
		return "<"
	case tokenGt:
		return ">"
	case tokenLte:
		return "<="
	case tokenGte:
		return ">="
	default:
		return "token"
	}
}

type exprToken struct {
	kind exprTokenKind
	text string
}

// singleCharTokenText interns the one-byte string for each ASCII char that
// can appear as a single-character token. Storing the table at package scope
// lets the lexer reuse the same string headers across calls instead of
// allocating a fresh `string(ch)` per single-char token (which the previous
// implementation did 8+ times for a typical complex expression).
var singleCharTokenText = func() *[256]string {
	var t [256]string
	for i := range t {
		t[i] = string(byte(i))
	}
	return &t
}()

func lexExpr(source string) ([]exprToken, error) {
	// Most expressions tokenize to ~one token per 3 chars; pre-sizing
	// tokens here keeps the slice from doubling 3-4 times for the common
	// expression sizes we see in real islands.
	tokens := make([]exprToken, 0, len(source)/3+4)

	for i := 0; i < len(source); {
		ch := source[i]

		if isSpace(ch) {
			i++
			continue
		}

		if ch == '"' {
			start := i
			i++
			for i < len(source) {
				if source[i] == '\\' {
					i += 2
					continue
				}
				if source[i] == '"' {
					i++
					tokens = append(tokens, exprToken{kind: tokenString, text: source[start:i]})
					goto nextToken
				}
				i++
			}
			return nil, fmt.Errorf("unterminated string literal")
		}

		if isDigit(ch) {
			start := i
			hasDot := false
			for i < len(source) {
				switch {
				case isDigit(source[i]):
					i++
				case source[i] == '.' && !hasDot:
					hasDot = true
					i++
				default:
					tokens = append(tokens, exprToken{kind: tokenNumber, text: source[start:i]})
					goto nextToken
				}
			}
			tokens = append(tokens, exprToken{kind: tokenNumber, text: source[start:i]})
			goto nextToken
		}

		if isIdentStart(ch) {
			start := i
			i++
			for i < len(source) && isIdentPart(source[i]) {
				i++
			}
			tokens = append(tokens, exprToken{kind: tokenIdent, text: source[start:i]})
			goto nextToken
		}

		if i+1 < len(source) {
			switch source[i : i+2] {
			case "&&":
				tokens = append(tokens, exprToken{kind: tokenAndAnd, text: "&&"})
				i += 2
				goto nextToken
			case "||":
				tokens = append(tokens, exprToken{kind: tokenOrOr, text: "||"})
				i += 2
				goto nextToken
			case "==":
				tokens = append(tokens, exprToken{kind: tokenEqEq, text: "=="})
				i += 2
				goto nextToken
			case "!=":
				tokens = append(tokens, exprToken{kind: tokenNotEq, text: "!="})
				i += 2
				goto nextToken
			case "<=":
				tokens = append(tokens, exprToken{kind: tokenLte, text: "<="})
				i += 2
				goto nextToken
			case ">=":
				tokens = append(tokens, exprToken{kind: tokenGte, text: ">="})
				i += 2
				goto nextToken
			}
		}

		// Single-char tokens reuse the package-level text table so each
		// case is a 16-byte token-struct write with no allocation.
		switch ch {
		case '(':
			tokens = append(tokens, exprToken{kind: tokenLParen, text: singleCharTokenText[ch]})
		case ')':
			tokens = append(tokens, exprToken{kind: tokenRParen, text: singleCharTokenText[ch]})
		case '[':
			tokens = append(tokens, exprToken{kind: tokenLBracket, text: singleCharTokenText[ch]})
		case ']':
			tokens = append(tokens, exprToken{kind: tokenRBracket, text: singleCharTokenText[ch]})
		case '.':
			tokens = append(tokens, exprToken{kind: tokenDot, text: singleCharTokenText[ch]})
		case ',':
			tokens = append(tokens, exprToken{kind: tokenComma, text: singleCharTokenText[ch]})
		case '?':
			tokens = append(tokens, exprToken{kind: tokenQuestion, text: singleCharTokenText[ch]})
		case ':':
			tokens = append(tokens, exprToken{kind: tokenColon, text: singleCharTokenText[ch]})
		case '+':
			tokens = append(tokens, exprToken{kind: tokenPlus, text: singleCharTokenText[ch]})
		case '-':
			tokens = append(tokens, exprToken{kind: tokenMinus, text: singleCharTokenText[ch]})
		case '*':
			tokens = append(tokens, exprToken{kind: tokenStar, text: singleCharTokenText[ch]})
		case '/':
			tokens = append(tokens, exprToken{kind: tokenSlash, text: singleCharTokenText[ch]})
		case '%':
			tokens = append(tokens, exprToken{kind: tokenPercent, text: singleCharTokenText[ch]})
		case '!':
			tokens = append(tokens, exprToken{kind: tokenBang, text: singleCharTokenText[ch]})
		case '<':
			tokens = append(tokens, exprToken{kind: tokenLt, text: singleCharTokenText[ch]})
		case '>':
			tokens = append(tokens, exprToken{kind: tokenGt, text: singleCharTokenText[ch]})
		default:
			return nil, fmt.Errorf("unexpected character %q", ch)
		}
		i++

	nextToken:
	}

	tokens = append(tokens, exprToken{kind: tokenEOF})
	return tokens, nil
}

func isSpace(ch byte) bool {
	return ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r'
}

func isDigit(ch byte) bool {
	return ch >= '0' && ch <= '9'
}

func isLetter(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')
}

func isIdentStart(ch byte) bool {
	return isLetter(ch) || ch == '_' || ch == '$'
}

func isIdentPart(ch byte) bool {
	return isLetter(ch) || isDigit(ch) || ch == '_' || ch == '$'
}
