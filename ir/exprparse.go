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
	if err := rejectDisallowed(source); err != nil {
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
	if signalName, ok := p.signalReceiver(receiverID); ok {
		switch strings.ToLower(method) {
		case "get":
			if len(args) != 0 {
				return 0, fmt.Errorf("signal Get takes no arguments")
			}
			return p.addExpr(program.Expr{
				Op:    program.OpSignalGet,
				Value: signalName,
				Type:  program.TypeAny,
			}), nil
		case "set":
			if len(args) != 1 {
				return 0, fmt.Errorf("signal Set requires exactly one argument")
			}
			return p.addExpr(program.Expr{
				Op:       program.OpSignalSet,
				Operands: []program.ExprID{args[0]},
				Value:    signalName,
				Type:     program.TypeAny,
			}), nil
		case "update":
			if len(args) != 1 {
				return 0, fmt.Errorf("signal Update requires exactly one argument")
			}
			return p.addExpr(program.Expr{
				Op:       program.OpSignalUpdate,
				Operands: []program.ExprID{args[0]},
				Value:    signalName,
				Type:     program.TypeAny,
			}), nil
		}
	}

	switch strings.ToLower(method) {
	case "length", "len":
		if len(args) != 0 {
			return 0, fmt.Errorf("%s takes no arguments", method)
		}
		return p.addExpr(program.Expr{
			Op:       program.OpLen,
			Operands: []program.ExprID{receiverID},
			Type:     program.TypeInt,
		}), nil

	case "toupper":
		return p.buildUnaryMethod(receiverID, args, method, program.OpToUpper, program.TypeString)
	case "tolower":
		return p.buildUnaryMethod(receiverID, args, method, program.OpToLower, program.TypeString)
	case "trim":
		return p.buildUnaryMethod(receiverID, args, method, program.OpTrim, program.TypeString)
	case "tostring":
		return p.buildUnaryMethod(receiverID, args, method, program.OpToString, program.TypeString)
	case "toint":
		return p.buildUnaryMethod(receiverID, args, method, program.OpToInt, program.TypeInt)
	case "tofloat":
		return p.buildUnaryMethod(receiverID, args, method, program.OpToFloat, program.TypeFloat)

	case "split":
		if len(args) != 1 {
			return 0, fmt.Errorf("Split requires exactly one argument")
		}
		expr := program.Expr{
			Op:       program.OpSplit,
			Operands: []program.ExprID{receiverID},
			Type:     program.TypeAny,
		}
		if literal, ok := p.stringLiteralValue(args[0]); ok {
			expr.Value = literal
		} else {
			expr.Operands = append(expr.Operands, args[0])
		}
		return p.addExpr(expr), nil

	case "join":
		if len(args) != 1 {
			return 0, fmt.Errorf("Join requires exactly one argument")
		}
		expr := program.Expr{
			Op:       program.OpJoin,
			Operands: []program.ExprID{receiverID},
			Type:     program.TypeString,
		}
		if literal, ok := p.stringLiteralValue(args[0]); ok {
			expr.Value = literal
		} else {
			expr.Operands = append(expr.Operands, args[0])
		}
		return p.addExpr(expr), nil

	case "replace":
		if len(args) != 2 {
			return 0, fmt.Errorf("Replace requires exactly two arguments")
		}
		return p.addExpr(program.Expr{
			Op:       program.OpReplace,
			Operands: []program.ExprID{receiverID, args[0], args[1]},
			Type:     program.TypeString,
		}), nil

	case "substring":
		if len(args) != 2 {
			return 0, fmt.Errorf("Substring requires exactly two arguments")
		}
		return p.addExpr(program.Expr{
			Op:       program.OpSubstring,
			Operands: []program.ExprID{receiverID, args[0], args[1]},
			Type:     program.TypeString,
		}), nil

	case "startswith":
		if len(args) != 1 {
			return 0, fmt.Errorf("StartsWith requires exactly one argument")
		}
		return p.addExpr(program.Expr{
			Op:       program.OpStartsWith,
			Operands: []program.ExprID{receiverID, args[0]},
			Type:     program.TypeBool,
		}), nil

	case "endswith":
		if len(args) != 1 {
			return 0, fmt.Errorf("EndsWith requires exactly one argument")
		}
		return p.addExpr(program.Expr{
			Op:       program.OpEndsWith,
			Operands: []program.ExprID{receiverID, args[0]},
			Type:     program.TypeBool,
		}), nil

	case "contains":
		if len(args) != 1 {
			return 0, fmt.Errorf("Contains requires exactly one argument")
		}
		return p.addExpr(program.Expr{
			Op:       program.OpContains,
			Operands: []program.ExprID{receiverID, args[0]},
			Type:     program.TypeBool,
		}), nil

	case "append":
		if len(args) != 1 {
			return 0, fmt.Errorf("Append requires exactly one argument")
		}
		return p.addExpr(program.Expr{
			Op:       program.OpAppend,
			Operands: []program.ExprID{receiverID, args[0]},
			Type:     program.TypeAny,
		}), nil

	case "slice":
		if len(args) != 2 {
			return 0, fmt.Errorf("Slice requires exactly two arguments")
		}
		return p.addExpr(program.Expr{
			Op:       program.OpSlice,
			Operands: []program.ExprID{receiverID, args[0], args[1]},
			Type:     program.TypeAny,
		}), nil

	case "map":
		if len(args) != 1 {
			return 0, fmt.Errorf("map requires exactly one argument")
		}
		return p.addExpr(program.Expr{
			Op:       program.OpMap,
			Operands: []program.ExprID{receiverID, args[0]},
			Type:     program.TypeAny,
		}), nil

	case "filter":
		if len(args) != 1 {
			return 0, fmt.Errorf("filter requires exactly one argument")
		}
		return p.addExpr(program.Expr{
			Op:       program.OpFilter,
			Operands: []program.ExprID{receiverID, args[0]},
			Type:     program.TypeAny,
		}), nil

	case "find":
		if len(args) != 1 {
			return 0, fmt.Errorf("find requires exactly one argument")
		}
		return p.addExpr(program.Expr{
			Op:       program.OpFind,
			Operands: []program.ExprID{receiverID, args[0]},
			Type:     program.TypeAny,
		}), nil
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
func rejectDisallowed(source string) error {
	if strings.HasPrefix(source, "go ") {
		return fmt.Errorf("goroutine launch is not allowed in island expressions: %q", source)
	}
	if strings.HasPrefix(source, "<-") {
		return fmt.Errorf("channel receive is not allowed in island expressions: %q", source)
	}
	return nil
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

func lexExpr(source string) ([]exprToken, error) {
	var tokens []exprToken

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

		switch ch {
		case '(':
			tokens = append(tokens, exprToken{kind: tokenLParen, text: string(ch)})
		case ')':
			tokens = append(tokens, exprToken{kind: tokenRParen, text: string(ch)})
		case '[':
			tokens = append(tokens, exprToken{kind: tokenLBracket, text: string(ch)})
		case ']':
			tokens = append(tokens, exprToken{kind: tokenRBracket, text: string(ch)})
		case '.':
			tokens = append(tokens, exprToken{kind: tokenDot, text: string(ch)})
		case ',':
			tokens = append(tokens, exprToken{kind: tokenComma, text: string(ch)})
		case '?':
			tokens = append(tokens, exprToken{kind: tokenQuestion, text: string(ch)})
		case ':':
			tokens = append(tokens, exprToken{kind: tokenColon, text: string(ch)})
		case '+':
			tokens = append(tokens, exprToken{kind: tokenPlus, text: string(ch)})
		case '-':
			tokens = append(tokens, exprToken{kind: tokenMinus, text: string(ch)})
		case '*':
			tokens = append(tokens, exprToken{kind: tokenStar, text: string(ch)})
		case '/':
			tokens = append(tokens, exprToken{kind: tokenSlash, text: string(ch)})
		case '%':
			tokens = append(tokens, exprToken{kind: tokenPercent, text: string(ch)})
		case '!':
			tokens = append(tokens, exprToken{kind: tokenBang, text: string(ch)})
		case '<':
			tokens = append(tokens, exprToken{kind: tokenLt, text: string(ch)})
		case '>':
			tokens = append(tokens, exprToken{kind: tokenGt, text: string(ch)})
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
