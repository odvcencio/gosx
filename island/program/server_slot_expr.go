package program

// Re-derive the limited pure shell-expression type; never trust a wire Type
// tag to turn an effectful opcode or a string hidden value into a valid attr.
func (p *Program) serverSlotExprType(id ExprID, stable bool, depth int) ExprType {
	return p.serverSlotExprTypeMemo(id, stable, depth, make(map[serverSlotExprKey]ExprType), make(map[serverSlotExprKey]bool))
}

type serverSlotExprKey struct {
	id     ExprID
	stable bool
}

func (p *Program) serverSlotExprTypeMemo(id ExprID, stable bool, depth int, memo map[serverSlotExprKey]ExprType, visiting map[serverSlotExprKey]bool) (result ExprType) {
	if int(id) >= len(p.Exprs) || depth > 64 {
		return TypeAny
	}
	key := serverSlotExprKey{id, stable}
	if typ, ok := memo[key]; ok {
		return typ
	}
	if visiting[key] {
		return TypeAny
	}
	visiting[key] = true
	defer func() { delete(visiting, key); memo[key] = result }()
	e := p.Exprs[id]
	child := func(i int) ExprType {
		if i >= len(e.Operands) {
			return TypeAny
		}
		return p.serverSlotExprTypeMemo(e.Operands[i], stable, depth+1, memo, visiting)
	}
	switch e.Op {
	case OpLitString:
		return TypeString
	case OpLitInt:
		return TypeInt
	case OpLitBool:
		if e.Value == "true" || e.Value == "false" {
			return TypeBool
		}
	case OpPropGet:
		for _, prop := range p.Props {
			if prop.Name == e.Value {
				return prop.Type
			}
		}
	case OpIndex:
		if len(e.Operands) == 2 && int(e.Operands[0]) < len(p.Exprs) && int(e.Operands[1]) < len(p.Exprs) {
			root, key := p.Exprs[e.Operands[0]], p.Exprs[e.Operands[1]]
			if root.Op == OpPropGet && root.Value == "props" && key.Op == OpLitString {
				for _, prop := range p.Props {
					if prop.Name == key.Value {
						return prop.Type
					}
				}
			}
		}
	case OpSignalGet:
		if !stable {
			for _, sig := range p.Signals {
				if sig.Name == e.Value {
					return p.serverSlotExprTypeMemo(sig.Init, true, depth+1, memo, visiting)
				}
			}
		}
	case OpAdd, OpConcat:
		if len(e.Operands) == 2 && child(0) == TypeString && child(1) == TypeString {
			return TypeString
		}
	case OpEq, OpNeq:
		if len(e.Operands) == 2 && child(0) != TypeAny && child(0) == child(1) {
			return TypeBool
		}
	case OpAnd, OpOr:
		if len(e.Operands) == 2 && child(0) == TypeBool && child(1) == TypeBool {
			return TypeBool
		}
	case OpNot:
		if len(e.Operands) == 1 && child(0) == TypeBool {
			return TypeBool
		}
	}
	return TypeAny
}
