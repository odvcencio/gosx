// Package arbiterrules lets Arbiter rules (m31labs.dev/arbiter) answer
// game/teamai decisions: each Arbiter rule becomes a teamai.Predicate, and an
// ordered list of rules becomes a teamai.Chooser.
//
// It is its own Go module so that the core GoSX module does not depend on
// Arbiter. Import it only in games that author their reads in Arbiter.
//
// A rule reads teamai facts by name. Every variable path in a rule is a fact
// name for Book.ParseFact, such as nearest_opponent.dist or signal.clock:
//
//	rule LaneOpen {
//	    when { nearest_opponent.dist > 6 and self.y < 30 }
//	    then Read { branch: "deep" }
//	}
//
// A fact whose subject is missing reads as null, so a comparison with it is
// false. Rules are evaluated with the Arbiter VM against a data context that
// reads the teamai.Query directly: no maps or JSON are built per tick, and a
// predicate call does not allocate once each fact name has been seen.
//
// Determinism: the adapter evaluates rule conditions only. It refuses rules
// that use governance features that depend on the clock or on rollout state
// (active_from, active_until, rollout, segments, prerequisites, excludes and
// kill switches), so the answer depends only on the facts.
package arbiterrules

import (
	"errors"
	"fmt"
	"sync"

	"m31labs.dev/arbiter"
	"m31labs.dev/arbiter/compiler"
	"m31labs.dev/arbiter/ir"
	"m31labs.dev/arbiter/vm"

	"m31labs.dev/gosx/game/teamai"
)

// Rules is a compiled Arbiter ruleset bound to at most one teamai.Book.
// Its predicates and choosers share one VM behind a mutex, so they are safe
// for concurrent use; for many rooms stepping in parallel, build one Rules per
// room to avoid contention.
type Rules struct {
	rs     *compiler.CompiledRuleset
	vars   []string // variable paths from the IR; nil for a precompiled ruleset
	names  []string // rule names, by header index
	mu     sync.Mutex
	eval   *vm.Evaluator
	book   *teamai.Book
	facts  map[string]factEntry
	dc     dataContext
	branch []string // per header: the "branch" action param, for choosers
}

type factEntry struct {
	f  teamai.Fact
	ok bool
}

// dataContext implements vm.DataContext over the current teamai.Query.
type dataContext struct {
	r *Rules
	q *teamai.Query
}

// Compile compiles Arbiter source.
func Compile(src []byte) (*Rules, error) {
	prog, err := arbiter.Compile(src)
	if err != nil {
		return nil, fmt.Errorf("arbiterrules: %w", err)
	}
	r, err := New(prog.Ruleset)
	if err != nil {
		return nil, err
	}
	if prog.IR != nil {
		r.vars = []string{}
		for i := range prog.IR.Exprs {
			if e := &prog.IR.Exprs[i]; e.Kind == ir.ExprVarRef {
				name := e.Path
				if name == "" {
					name = e.Name
				}
				r.vars = append(r.vars, name)
			}
		}
	}
	return r, nil
}

// New wraps a compiled ruleset, for example one loaded from a precompiled
// Arbiter bundle. Without the IR the fact names are checked the first time a
// rule reads them; an unknown name reads as null.
func New(rs *compiler.CompiledRuleset) (*Rules, error) {
	if rs == nil {
		return nil, errors.New("arbiterrules: nil ruleset")
	}
	r := &Rules{rs: rs, facts: map[string]factEntry{}}
	r.eval = vm.NewEvaluator(rs, vm.NewStringPool(rs.Constants.Strings()))
	r.dc.r = r
	r.names = make([]string, len(rs.Rules))
	for i, h := range rs.Rules {
		name := rs.Constants.GetString(h.NameIdx)
		r.names[i] = name
		if err := deterministic(name, h); err != nil {
			return nil, err
		}
	}
	return r, nil
}

func deterministic(name string, h compiler.RuleHeader) error {
	var uses string
	switch {
	case h.HasActiveFrom || h.HasActiveUntil:
		uses = "an active window"
	case h.HasRollout:
		uses = "a rollout"
	case h.HasSegment:
		uses = "a segment"
	case h.PrereqLen > 0:
		uses = "requires"
	case h.ExcludeLen > 0:
		uses = "excludes"
	case h.KillSwitch.IsSet():
		uses = "a kill switch"
	default:
		return nil
	}
	return fmt.Errorf("arbiterrules: rule %s uses %s; team AI rules must depend only on facts", name, uses)
}

// Names returns the rule names in the ruleset.
func (r *Rules) Names() []string { return append([]string(nil), r.names...) }

// Register adds every rule to reg as a predicate named prefix + rule name.
func (r *Rules) Register(reg *teamai.Registry, prefix string) error {
	for i, n := range r.names {
		if err := reg.Predicate(prefix+n, &predicate{r: r, header: i}); err != nil {
			return err
		}
	}
	return nil
}

// Predicate returns the named rule as a predicate.
func (r *Rules) Predicate(name string) (teamai.Predicate, error) {
	i := r.index(name)
	if i < 0 {
		return nil, fmt.Errorf("arbiterrules: no rule %q", name)
	}
	return &predicate{r: r, header: i}, nil
}

// Chooser returns a chooser that tests the named rules in order. The first
// rule whose condition holds picks the decision branch named by the string
// param "branch" of its action (then Pick { branch: "deep" }). When no rule
// holds, or the branch names no label of the decision, the chooser keeps
// waiting, and the decision takes its default when its window ends.
func (r *Rules) Chooser(names ...string) (teamai.Chooser, error) {
	c := &chooser{r: r}
	for _, n := range names {
		i := r.index(n)
		if i < 0 {
			return nil, fmt.Errorf("arbiterrules: no rule %q", n)
		}
		c.headers = append(c.headers, i)
	}
	if len(c.headers) == 0 {
		return nil, errors.New("arbiterrules: a chooser needs at least one rule")
	}
	return c, nil
}

func (r *Rules) index(name string) int {
	for i, n := range r.names {
		if n == name {
			return i
		}
	}
	return -1
}

// bind attaches the Book: it checks every variable path the source uses and
// reads each rule's branch param.
func (r *Rules) bind(b *teamai.Book) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.book == b {
		return nil
	}
	if r.book != nil {
		return errors.New("arbiterrules: these Rules are already bound to another Book; build one Rules per Book")
	}
	for _, v := range r.vars {
		f, err := b.ParseFact(v)
		if err != nil {
			return fmt.Errorf("arbiterrules: %w", err)
		}
		r.facts[v] = factEntry{f: f, ok: true}
	}
	r.branch = make([]string, len(r.rs.Rules))
	for i, h := range r.rs.Rules {
		r.eval.ResetBudget()
		m, err := r.eval.BuildMatch(h, nullContext{})
		if err != nil {
			continue
		}
		if s, ok := m.Params["branch"].(string); ok {
			r.branch[i] = s
		}
	}
	r.book = b
	return nil
}

type nullContext struct{}

func (nullContext) Get(string) vm.Value { return vm.NullVal() }

// Get reads a fact for the VM.
func (c *dataContext) Get(key string) vm.Value {
	e, seen := c.r.facts[key]
	if !seen {
		f, err := c.r.book.ParseFact(key)
		e = factEntry{f: f, ok: err == nil}
		c.r.facts[key] = e
	}
	if !e.ok {
		return vm.NullVal()
	}
	v := c.q.Fact(e.f)
	if v != v {
		return vm.NullVal()
	}
	return vm.NumVal(v)
}

// test evaluates rule header i for q. An evaluation error counts as false.
func (r *Rules) test(i int, q *teamai.Query) bool {
	r.dc.q = q
	r.eval.ResetBudget()
	ok, err := r.eval.EvalRuleCondition(r.rs.Rules[i], &r.dc)
	r.dc.q = nil
	return err == nil && ok
}

type predicate struct {
	r      *Rules
	header int
}

func (p *predicate) Bind(b *teamai.Book) error { return p.r.bind(b) }

func (p *predicate) Test(q *teamai.Query) bool {
	p.r.mu.Lock()
	defer p.r.mu.Unlock()
	return p.r.test(p.header, q)
}

type chooser struct {
	r       *Rules
	headers []int
}

func (c *chooser) Bind(b *teamai.Book) error { return c.r.bind(b) }

func (c *chooser) Choose(q *teamai.Query, d *teamai.Decision) int {
	c.r.mu.Lock()
	defer c.r.mu.Unlock()
	for _, h := range c.headers {
		if !c.r.test(h, q) {
			continue
		}
		label := c.r.branch[h]
		for i := 0; i < d.NumBranches(); i++ {
			if d.BranchLabel(i) == label {
				return i
			}
		}
		return -1
	}
	return -1
}
