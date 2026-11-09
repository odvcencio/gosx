// Package arbiterrules lets Arbiter rules (m31labs.dev/arbiter) answer
// game/teamai decisions. Each rule becomes a teamai.Predicate, a list of rules
// becomes a first-match teamai.Chooser, and a tagged Arbiter decision
// (gameai.Decider with First, Best or Weighted selection) becomes a Chooser
// too.
//
// It is its own Go module so that the core GoSX module does not depend on
// Arbiter. Import it only in games that author their reads in Arbiter.
//
// A rule reads teamai facts by name. Every variable a rule reads is a fact
// name for Book.ParseFact, such as nearest_opponent.dist or signal.clock:
//
//	rule LaneOpen tag "read" {
//	    when { nearest_opponent.dist > 6 and self.y < 30 }
//	    then Read { branch: "deep", score: 10 - self.y / 5 }
//	}
//
// A fact whose subject is missing reads as null, so a comparison with it is
// false.
//
// # Bundles
//
// The game ships a precompiled bundle and loads it with Load, which uses
// Arbiter's gameai runtime: no .arb parser and no gotreesitter, so a js/wasm
// client stays small. Compile, CompileFile and Bundle (in build.go, left out
// of js/wasm builds) compile .arb source at build time or on a development
// server. They keep rule names, which Register and Predicate use; a bundle
// built elsewhere must keep them too (build.Options.KeepNames).
//
// # Determinism and cost
//
// Rules are evaluated by the Arbiter VM against a data context that reads the
// teamai.Query directly and caches each name's compiled fact by its string
// pool index (vm.IndexedDataContext). No maps or JSON are built per tick, and
// a predicate or decision does not allocate once each name has been seen.
// gameai.Load refuses rules that depend on more than their inputs (active
// windows, segments, requires, excludes, rollouts); rules with kill_switch on
// never hold. Weighted decisions draw from gameai.Seed(world seed, tick,
// agent, gameai.Key(tag)). Record Digest with your snapshots and swap bundles
// only at a tick every peer agrees on.
package arbiterrules

import (
	"errors"
	"fmt"
	"sync"

	"m31labs.dev/arbiter/bytecode"
	"m31labs.dev/arbiter/gameai"
	"m31labs.dev/arbiter/vm"

	"m31labs.dev/gosx/game/teamai"
)

// Rules is a loaded Arbiter program bound to at most one teamai.Book. Its
// predicates and choosers share one VM behind a mutex, so they are safe for
// concurrent use; for many rooms stepping in parallel, load one Rules per room
// (they can share the bundle bytes) to avoid contention.
type Rules struct {
	prog   *gameai.Program
	rs     *bytecode.CompiledRuleset
	names  []string // rule names, by header index
	mu     sync.Mutex
	eval   *vm.Evaluator
	book   *teamai.Book
	byIdx  []factEntry // by string pool index
	byName map[string]factEntry
	dc     dataContext
	branch []string // per header: the "branch" action param
}

type factEntry struct {
	f     teamai.Fact
	state uint8 // 0 unseen, 1 known, 2 unknown
}

// dataContext implements vm.IndexedDataContext over the current Query.
type dataContext struct {
	r *Rules
	q *teamai.Query
}

// Load loads a bundle written by Bundle, build.Compile or
// `arbiter bundle --game`.
func Load(bundle []byte) (*Rules, error) {
	p, err := gameai.Load(bundle)
	if err != nil {
		return nil, fmt.Errorf("arbiterrules: %w", err)
	}
	return New(p)
}

// New wraps a loaded gameai Program.
func New(p *gameai.Program) (*Rules, error) {
	if p == nil {
		return nil, errors.New("arbiterrules: nil program")
	}
	rs := p.Ruleset()
	r := &Rules{
		prog:   p,
		rs:     rs,
		eval:   vm.NewEvaluator(rs, p.Strings()),
		byIdx:  make([]factEntry, rs.Constants.StringCount()),
		byName: map[string]factEntry{},
	}
	r.dc.r = r
	r.names = make([]string, len(rs.Rules))
	for i, h := range rs.Rules {
		r.names[i] = rs.Constants.GetString(h.NameIdx)
	}
	return r, nil
}

// Program returns the loaded program.
func (r *Rules) Program() *gameai.Program { return r.prog }

// Digest returns the SHA-256 of the bundle. Record it with snapshots and
// replays, so a restore can tell which rules made the decisions.
func (r *Rules) Digest() [32]byte { return r.prog.Digest() }

// Names returns the rule names.
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

// Decision returns a chooser that runs the rules tagged tag as one Arbiter
// decision with selection sel: gameai.First(), gameai.Best("score") for
// utility scoring, or gameai.Weighted("weight") for a seeded weighted draw.
// The chosen rule's "branch" param names the teamai decision branch. Weighted
// draws use gameai.Seed(World.Seed, World.Tick, agent, gameai.Key(tag)), so
// they are the same on every peer.
func (r *Rules) Decision(tag string, sel gameai.Selection) (teamai.Chooser, error) {
	d, err := gameai.NewDecider(r.prog, tag)
	if err != nil {
		return nil, fmt.Errorf("arbiterrules: %w", err)
	}
	return &decision{r: r, d: d, sel: sel, key: gameai.Key(tag)}, nil
}

func (r *Rules) index(name string) int {
	for i, n := range r.names {
		if n == name {
			return i
		}
	}
	return -1
}

// bind attaches the Book: it checks every name the program reads and reads
// each rule's branch param.
func (r *Rules) bind(b *teamai.Book) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.book == b {
		return nil
	}
	if r.book != nil {
		return errors.New("arbiterrules: these Rules are already bound to another Book; load one Rules per Book")
	}
	all, err := gameai.NewDecider(r.prog, "")
	if err != nil {
		return fmt.Errorf("arbiterrules: %w", err)
	}
	for _, name := range all.Reads() {
		f, err := b.ParseFact(name)
		if err != nil {
			return fmt.Errorf("arbiterrules: %w", err)
		}
		r.byName[name] = factEntry{f: f, state: 1}
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
	e, seen := c.r.byName[key]
	if !seen {
		e = c.r.parse(key)
		c.r.byName[key] = e
	}
	return c.value(e)
}

// GetIndexed reads a fact by its string pool index, without hashing the name.
func (c *dataContext) GetIndexed(idx uint16, key string) vm.Value {
	if int(idx) >= len(c.r.byIdx) {
		return c.Get(key)
	}
	e := &c.r.byIdx[idx]
	if e.state == 0 {
		if known, seen := c.r.byName[key]; seen {
			*e = known
		} else {
			*e = c.r.parse(key)
		}
	}
	return c.value(*e)
}

func (r *Rules) parse(key string) factEntry {
	f, err := r.book.ParseFact(key)
	if err != nil {
		return factEntry{state: 2}
	}
	return factEntry{f: f, state: 1}
}

func (c *dataContext) value(e factEntry) vm.Value {
	if e.state != 1 {
		return vm.NullVal()
	}
	v := c.q.Fact(e.f)
	if v != v {
		return vm.NullVal()
	}
	return vm.NumVal(v)
}

// test evaluates rule header i for q. An evaluation error, or a rule with
// kill_switch on, counts as false.
func (r *Rules) test(i int, q *teamai.Query) bool {
	h := r.rs.Rules[i]
	if h.KillSwitch.Enabled() {
		return false
	}
	r.dc.q = q
	r.eval.ResetBudget()
	ok, err := r.eval.EvalRuleCondition(h, &r.dc)
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
		if c.r.test(h, q) {
			return branchIndex(d, c.r.branch[h])
		}
	}
	return -1
}

type decision struct {
	r   *Rules
	d   *gameai.Decider
	sel gameai.Selection
	key uint64
}

func (c *decision) Bind(b *teamai.Book) error { return c.r.bind(b) }

func (c *decision) Choose(q *teamai.Query, d *teamai.Decision) int {
	c.r.mu.Lock()
	defer c.r.mu.Unlock()
	w := q.World()
	seed := gameai.Seed(w.Seed, w.Tick, uint64(q.Self()), c.key)
	c.r.dc.q = q
	choice, err := c.d.Decide(&c.r.dc, c.sel, seed, nil)
	c.r.dc.q = nil
	if err != nil || !choice.OK() {
		return -1
	}
	return branchIndex(d, choice.Text("branch"))
}

func branchIndex(d *teamai.Decision, label string) int {
	for i := 0; i < d.NumBranches(); i++ {
		if d.BranchLabel(i) == label {
			return i
		}
	}
	return -1
}
