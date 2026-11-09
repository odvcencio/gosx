package teamai

import "fmt"

// Predicate is a yes-or-no question about the world from one agent's point of
// view: "is the nearest opponent within 3 units", "has the flag been dropped".
// Decisions, step ends and interrupts name predicates. Test must not change
// the Query or keep it, and must give the same answer for the same world and
// agent state on every platform: no clock, no global random source, no map
// iteration. The executor calls Test from one goroutine at a time.
type Predicate interface {
	Test(q *Query) bool
}

// PredicateFunc adapts a function to Predicate.
type PredicateFunc func(q *Query) bool

// Test calls f(q).
func (f PredicateFunc) Test(q *Query) bool { return f(q) }

// Chooser picks a branch of a decision in place of its branch rules. Choose
// returns a branch index, or -1 to keep waiting. Use it for utility scoring or
// a rules engine that ranks options.
type Chooser interface {
	Choose(q *Query, d *Decision) int
}

// ChooserFunc adapts a function to Chooser.
type ChooserFunc func(q *Query, d *Decision) int

// Choose calls f(q, d).
func (f ChooserFunc) Choose(q *Query, d *Decision) int { return f(q, d) }

// Binder is implemented by predicates and choosers that need the Book, for
// example to compile fact names with Book.ParseFact. Compile calls Bind once,
// after the Book's tables are built and before it returns.
type Binder interface {
	Bind(b *Book) error
}

// Registry holds the predicates and choosers a game provides in Go, by name.
// Data rules (Spec.Rules) and registry entries share one namespace.
type Registry struct {
	predNames []string
	preds     []Predicate
	chNames   []string
	choosers  []Chooser
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{} }

// Predicate registers p under name.
func (r *Registry) Predicate(name string, p Predicate) error {
	if !validName(name) {
		return fmt.Errorf("teamai: predicate name %q: use 1 to %d characters from A-Z a-z 0-9 _ -", name, maxNameLen)
	}
	if p == nil {
		return fmt.Errorf("teamai: predicate %q is nil", name)
	}
	if indexOf(r.predNames, name) >= 0 {
		return fmt.Errorf("teamai: predicate %q registered twice", name)
	}
	r.predNames = append(r.predNames, name)
	r.preds = append(r.preds, p)
	return nil
}

// Chooser registers c under name.
func (r *Registry) Chooser(name string, c Chooser) error {
	if !validName(name) {
		return fmt.Errorf("teamai: chooser name %q: use 1 to %d characters from A-Z a-z 0-9 _ -", name, maxNameLen)
	}
	if c == nil {
		return fmt.Errorf("teamai: chooser %q is nil", name)
	}
	if indexOf(r.chNames, name) >= 0 {
		return fmt.Errorf("teamai: chooser %q registered twice", name)
	}
	r.chNames = append(r.chNames, name)
	r.choosers = append(r.choosers, c)
	return nil
}

// RuleSpec is a rule written in the data file. It has exactly one form:
//
//	fact, op and value     compare a fact with a number
//	fact, op and than      compare two facts
//	all                    every named rule holds
//	any                    at least one named rule holds
//	not                    the named rule does not hold
//
// op is one of < <= > >= == !=. A comparison with a missing fact (NaN) is
// false whatever the op.
type RuleSpec struct {
	Name  string   `json:"name"`
	Fact  string   `json:"fact,omitempty"`
	Op    string   `json:"op,omitempty"`
	Value *float64 `json:"value,omitempty"`
	Than  string   `json:"than,omitempty"`
	All   []string `json:"all,omitempty"`
	Any   []string `json:"any,omitempty"`
	Not   string   `json:"not,omitempty"`
}

type cmpOp uint8

const (
	opLT cmpOp = iota + 1
	opLE
	opGT
	opGE
	opEQ
	opNE
)

func parseOp(s string) cmpOp {
	switch s {
	case "<":
		return opLT
	case "<=":
		return opLE
	case ">":
		return opGT
	case ">=":
		return opGE
	case "==":
		return opEQ
	case "!=":
		return opNE
	}
	return 0
}

// compare applies op; NaN on either side is false.
func compare(op cmpOp, a, b float64) bool {
	if a != a || b != b {
		return false
	}
	switch op {
	case opLT:
		return a < b
	case opLE:
		return a <= b
	case opGT:
		return a > b
	case opGE:
		return a >= b
	case opEQ:
		return a == b
	case opNE:
		return a != b
	}
	return false
}

type cmpRule struct {
	fact  Fact
	op    cmpOp
	value float64
	than  Fact
	two   bool
}

func (r *cmpRule) Test(q *Query) bool {
	b := r.value
	if r.two {
		b = q.Fact(r.than)
	}
	return compare(r.op, q.Fact(r.fact), b)
}

type allRule struct{ parts []Predicate }

func (r *allRule) Test(q *Query) bool {
	for _, p := range r.parts {
		if !p.Test(q) {
			return false
		}
	}
	return true
}

type anyRule struct{ parts []Predicate }

func (r *anyRule) Test(q *Query) bool {
	for _, p := range r.parts {
		if p.Test(q) {
			return true
		}
	}
	return false
}

type notRule struct{ part Predicate }

func (r *notRule) Test(q *Query) bool { return !r.part.Test(q) }

// ruleBinder gives every rule and chooser name a Book index while the Spec
// compiles, and remembers where each name was used so an unknown name is
// reported at the entry that used it.
type ruleBinder struct {
	decisions uint32 // decisions compiled so far
	preds     []string
	predRefs  []ruleUse
	choosers  []string
	chRefs    []ruleUse
}

type ruleUse struct {
	path string
	name string
}

func (rb *ruleBinder) predicate(is *issues, path, name string) int16 {
	rb.predRefs = append(rb.predRefs, ruleUse{path: path, name: name})
	if i := indexOf(rb.preds, name); i >= 0 {
		return int16(i)
	}
	rb.preds = append(rb.preds, name)
	return int16(len(rb.preds) - 1)
}

func (rb *ruleBinder) chooser(is *issues, path, name string) int16 {
	rb.chRefs = append(rb.chRefs, ruleUse{path: path, name: name})
	if i := indexOf(rb.choosers, name); i >= 0 {
		return int16(i)
	}
	rb.choosers = append(rb.choosers, name)
	return int16(len(rb.choosers) - 1)
}

// ruleCompiler turns Spec.Rules into predicates, resolving names against the
// data rules first and the registry second.
type ruleCompiler struct {
	b     *Book
	is    *issues
	specs []RuleSpec
	reg   *Registry
	built []Predicate
	state []uint8 // 0 new, 1 building, 2 done
}

func (rc *ruleCompiler) path(i int) string { return at("", "rules", i, rc.specs[i].Name) }

// lookup resolves a rule name used at path.
func (rc *ruleCompiler) lookup(path, name string) Predicate {
	for i := range rc.specs {
		if rc.specs[i].Name == name {
			return rc.build(i)
		}
	}
	if rc.reg != nil {
		if j := indexOf(rc.reg.predNames, name); j >= 0 {
			return rc.reg.preds[j]
		}
	}
	rc.is.addf(path, "unknown rule %q: not in rules and not registered", name)
	return nil
}

func (rc *ruleCompiler) build(i int) Predicate {
	switch rc.state[i] {
	case 1:
		rc.is.addf(rc.path(i), "rule refers to itself through all, any or not")
		return nil
	case 2:
		return rc.built[i]
	}
	rc.state[i] = 1
	rs := &rc.specs[i]
	p := rc.path(i)
	forms := 0
	if rs.Fact != "" || rs.Op != "" || rs.Value != nil || rs.Than != "" {
		forms++
	}
	if len(rs.All) > 0 {
		forms++
	}
	if len(rs.Any) > 0 {
		forms++
	}
	if rs.Not != "" {
		forms++
	}
	var out Predicate
	switch {
	case forms != 1:
		rc.is.addf(p, "a rule needs exactly one of fact/op/value (or than), all, any or not")
	case len(rs.All) > 0 || len(rs.Any) > 0:
		names, field0 := rs.All, "all"
		if len(rs.Any) > 0 {
			names, field0 = rs.Any, "any"
		}
		parts := make([]Predicate, 0, len(names))
		for k, n := range names {
			if q := rc.lookup(field(p, field0+"["+itoa(k)+"]"), n); q != nil {
				parts = append(parts, q)
			}
		}
		if field0 == "all" {
			out = &allRule{parts: parts}
		} else {
			out = &anyRule{parts: parts}
		}
	case rs.Not != "":
		if q := rc.lookup(field(p, "not"), rs.Not); q != nil {
			out = &notRule{part: q}
		}
	default:
		r := &cmpRule{op: parseOp(rs.Op)}
		if r.op == 0 {
			rc.is.addf(field(p, "op"), "op %q: want one of < <= > >= == !=", rs.Op)
		}
		f, err := rc.b.ParseFact(rs.Fact)
		if err != nil {
			rc.is.addf(field(p, "fact"), "%v", err)
		}
		r.fact = f
		switch {
		case rs.Value != nil && rs.Than != "":
			rc.is.addf(p, "a comparison takes value or than, not both")
		case rs.Value != nil:
			r.value = *rs.Value
			if !finite(r.value) {
				rc.is.addf(field(p, "value"), "must be a finite number")
			}
		case rs.Than != "":
			t, err := rc.b.ParseFact(rs.Than)
			if err != nil {
				rc.is.addf(field(p, "than"), "%v", err)
			}
			r.than, r.two = t, true
		default:
			rc.is.addf(p, "a comparison needs value or than")
		}
		out = r
	}
	rc.state[i] = 2
	rc.built[i] = out
	return out
}

// compileRules builds the Book's rule tables. It runs after every other table,
// because facts can name marks, objects and signals.
func compileRules(is *issues, b *Book, specs []RuleSpec, reg *Registry, rb *ruleBinder) {
	rc := &ruleCompiler{b: b, is: is, specs: specs, reg: reg,
		built: make([]Predicate, len(specs)), state: make([]uint8, len(specs))}
	for i := range specs {
		p := rc.path(i)
		checkEntryName(is, p, specs[i].Name, i, func(j int) string { return specs[j].Name })
		if reg != nil && indexOf(reg.predNames, specs[i].Name) >= 0 {
			is.addf(field(p, "name"), "rule %q is also a registered predicate", specs[i].Name)
		}
	}
	for i := range specs {
		rc.build(i)
	}
	b.preds = make([]Predicate, len(rb.preds))
	for _, use := range rb.predRefs {
		i := indexOf(rb.preds, use.name)
		if b.preds[i] == nil {
			b.preds[i] = rc.lookup(use.path, use.name)
		}
	}
	b.choosers = make([]Chooser, len(rb.choosers))
	for _, use := range rb.chRefs {
		i := indexOf(rb.choosers, use.name)
		if b.choosers[i] != nil {
			continue
		}
		if reg != nil {
			if j := indexOf(reg.chNames, use.name); j >= 0 {
				b.choosers[i] = reg.choosers[j]
				continue
			}
		}
		is.addf(use.path, "unknown chooser %q: not registered", use.name)
	}
	if len(is.list) > 0 || reg == nil {
		return
	}
	// Bind every registry entry, in registration order.
	for j, p := range reg.preds {
		if bd, ok := p.(Binder); ok {
			if err := bd.Bind(b); err != nil {
				is.addf("registry", "predicate %q: %v", reg.predNames[j], err)
			}
		}
	}
	for j, c := range reg.choosers {
		if bd, ok := c.(Binder); ok {
			if err := bd.Bind(b); err != nil {
				is.addf("registry", "chooser %q: %v", reg.chNames[j], err)
			}
		}
	}
}
