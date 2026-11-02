package vm

// Lockstep tests for subtree and row reuse over composite signals.
// Lockstep oracle: island A runs with every optimisation on (subtree reuse,
// row reuse, diff skip, buffer recycle); island B runs with all of them off.
// After every step A's tree must equal B's tree and a fresh full walk, and
// A's patch ops must equal B's. A stale node fails one of the three checks.

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"m31labs.dev/gosx/island/program"
	"m31labs.dev/gosx/signal"
)

// --- program builder -------------------------------------------------------

type rvProg struct{ p *program.Program }

func newRV(name string) *rvProg { return &rvProg{p: &program.Program{Name: name}} }

func (b *rvProg) e(op program.OpCode, value string, typ program.ExprType, ops ...program.ExprID) program.ExprID {
	b.p.Exprs = append(b.p.Exprs, program.Expr{Op: op, Value: value, Type: typ, Operands: ops})
	return program.ExprID(len(b.p.Exprs) - 1)
}
func (b *rvProg) lit(s string) program.ExprID  { return b.e(program.OpLitString, s, program.TypeString) }
func (b *rvProg) sig(n string) program.ExprID  { return b.e(program.OpSignalGet, n, program.TypeAny) }
func (b *rvProg) prop(n string) program.ExprID { return b.e(program.OpPropGet, n, program.TypeAny) }
func (b *rvProg) path(root program.ExprID, fields ...string) program.ExprID {
	for _, f := range fields {
		root = b.e(program.OpIndex, "", program.TypeAny, root, b.lit(f))
	}
	return root
}
func (b *rvProg) node(n program.Node) program.NodeID {
	b.p.Nodes = append(b.p.Nodes, n)
	return program.NodeID(len(b.p.Nodes) - 1)
}
func (b *rvProg) el(tag string, attrs []program.Attr, kids ...program.NodeID) program.NodeID {
	return b.node(program.Node{Kind: program.NodeElement, Tag: tag, Attrs: attrs, Children: kids})
}
func (b *rvProg) txt(e program.ExprID) program.NodeID {
	return b.node(program.Node{Kind: program.NodeExpr, Expr: e})
}
func (b *rvProg) each(coll program.ExprID, as string, fallback *program.ExprID, kids ...program.NodeID) program.NodeID {
	attrs := []program.Attr{{Kind: program.AttrStatic, Name: "as", Value: as}}
	if fallback != nil {
		attrs = append(attrs, program.Attr{Kind: program.AttrExpr, Name: "fallback", Expr: *fallback})
	}
	return b.node(program.Node{Kind: program.NodeForEach, Expr: coll, Attrs: attrs, Children: kids})
}
func (b *rvProg) cond(c program.ExprID, kids ...program.NodeID) program.NodeID {
	return b.node(program.Node{Kind: program.NodeConditional, Expr: c, Children: kids})
}
func (b *rvProg) finish(root program.NodeID, signals ...string) *program.Program {
	b.p.Root = root
	zero := b.e(program.OpLitInt, "0", program.TypeInt)
	for _, s := range signals {
		b.p.Signals = append(b.p.Signals, program.SignalDef{Name: s, Type: program.TypeAny, Init: zero})
	}
	return b.p
}
func attrExpr(name string, e program.ExprID) program.Attr {
	return program.Attr{Kind: program.AttrExpr, Name: name, Expr: e}
}

// --- lockstep harness ------------------------------------------------------

type rvPair struct {
	t      *testing.T
	a, b   *Island
	sa, sb map[string]*signal.Signal[Value]
	log    []string
}

func rvNewPair(t *testing.T, prog *program.Program, props string, names ...string) *rvPair {
	t.Helper()
	p := &rvPair{t: t, a: NewIsland(prog, props), b: NewIsland(prog, props),
		sa: map[string]*signal.Signal[Value]{}, sb: map[string]*signal.Signal[Value]{}}
	p.b.reuseOff, p.b.skipOff, p.b.recycleOff = true, true, true
	for _, n := range names {
		p.sa[n] = signal.New(ZeroValue(program.TypeAny))
		p.sb[n] = signal.New(ZeroValue(program.TypeAny))
		p.a.vm.SetSignal(n, p.sa[n])
		p.b.vm.SetSignal(n, p.sb[n])
	}
	p.a.Reconcile()
	p.b.Reconcile()
	return p
}

func (p *rvPair) set(name string, v Value) {
	p.sa[name].Set(v)
	p.sb[name].Set(v)
}

func rvTexts(tree *ResolvedTree) string {
	if tree == nil {
		return "<nil>"
	}
	var walk func(i, depth int) string
	walk = func(i, depth int) string {
		if depth > 64 || i < 0 || i >= len(tree.Nodes) {
			return "?"
		}
		n := tree.Nodes[i]
		if n.Tag == "" {
			return n.Text
		}
		var b strings.Builder
		b.WriteString("<" + n.Tag)
		for _, a := range n.Attrs {
			fmt.Fprintf(&b, " %s=%q", a.Name, a.Value)
		}
		b.WriteString(">")
		for _, c := range n.Children {
			b.WriteString(walk(c, depth+1))
		}
		b.WriteString("</" + n.Tag + ">")
		return b.String()
	}
	if len(tree.Nodes) == 0 {
		return ""
	}
	return walk(0, 0)
}

// step reconciles both islands and reports the first divergence.
func (p *rvPair) step(what string) bool {
	p.t.Helper()
	pa := p.a.Reconcile()
	pb := p.b.Reconcile()
	return p.compare(what, pa, pb)
}

// compare checks the trees and ops both islands just produced.
func (p *rvPair) compare(what string, pa, pb []PatchOp) bool {
	p.t.Helper()
	p.log = append(p.log, what)
	full := fullEvalTree(p.a)
	switch {
	case !treesEqual(p.a.prev, p.b.prev):
		p.t.Errorf("STALE TREE after %q (step %d)\n  reuse: %s\n  full:  %s\n  history: %v", what, len(p.log), rvTexts(p.a.prev), rvTexts(p.b.prev), tail(p.log, 6))
		return false
	case !treesEqual(p.a.prev, full):
		p.t.Errorf("STALE TREE vs fresh walk after %q (step %d)\n  reuse: %s\n  fresh: %s", what, len(p.log), rvTexts(p.a.prev), rvTexts(full))
		return false
	case fmt.Sprintf("%+v", pa) != fmt.Sprintf("%+v", pb):
		p.t.Errorf("PATCH DIVERGENCE after %q (step %d)\n  reuse ops: %+v\n  full ops:  %+v\n  history: %v", what, len(p.log), pa, pb, tail(p.log, 6))
		return false
	}
	return true
}

func tail(s []string, n int) []string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}

func obj2(kv ...any) Value {
	m := map[string]Value{}
	for i := 0; i < len(kv); i += 2 {
		m[kv[i].(string)] = kv[i+1].(Value)
	}
	return ObjectVal(m)
}

var null = ZeroValue(program.TypeAny)

func rvDict(v Value) map[string]Value { return v.dict() }

// --- random values ---------------------------------------------------------

func rvItem(r *rand.Rand) Value {
	m := map[string]Value{"name": StringVal([]string{"a", "b", "c"}[r.Intn(3)])}
	switch r.Intn(4) {
	case 1:
		m["cls"] = StringVal("x")
	case 2:
		m["cls"] = null
	case 3:
		m["cls"] = StringVal("y")
	}
	switch r.Intn(5) {
	case 1:
		m["tags"] = ArrayVal(nil)
	case 2:
		m["tags"] = ArrayVal([]Value{})
	case 3:
		m["tags"] = ArrayVal([]Value{StringVal("t1")})
	case 4:
		m["tags"] = ArrayVal([]Value{StringVal("t1"), StringVal("t2")})
	}
	if r.Intn(4) == 0 {
		m["n"] = IntVal(r.Intn(2))
	}
	return ObjectVal(m)
}

func rvRows(r *rand.Rand) Value {
	switch r.Intn(12) {
	case 0:
		return null
	case 1:
		return StringVal("ab")
	case 2:
		m := map[string]Value{}
		for i := 0; i < r.Intn(3); i++ {
			m[[]string{"k1", "k2", "k3"}[r.Intn(3)]] = rvItem(r)
		}
		return ObjectVal(m)
	case 3:
		return ArrayVal(nil)
	}
	items := make([]Value, r.Intn(5))
	for i := range items {
		items[i] = rvItem(r)
	}
	return ArrayVal(items)
}

// rvMutate edits the composite v IN PLACE, the way OpFieldSet/OpIndexSet do,
// and reports what it did. It never calls Set.
func rvMutate(r *rand.Rand, v Value) string {
	if !v.isMap() {
		return ""
	}
	root := v.dict()
	switch r.Intn(5) {
	case 0:
		root["title"] = StringVal(fmt.Sprintf("T%d", r.Intn(3)))
		return "inplace title"
	case 1:
		root["unread"] = IntVal(r.Intn(100))
		return "inplace unread"
	}
	rows, ok := root["rows"]
	if !ok || !rows.isList() || len(rows.list()) == 0 {
		return ""
	}
	items := rows.list()
	i := r.Intn(len(items))
	switch r.Intn(3) {
	case 0:
		items[i] = rvItem(r)
		return fmt.Sprintf("inplace rows[%d]=new", i)
	case 1:
		if items[i].isMap() {
			items[i].dict()["name"] = StringVal([]string{"a", "b", "c", "z"}[r.Intn(4)])
			return fmt.Sprintf("inplace rows[%d].name", i)
		}
	case 2:
		if items[i].isMap() {
			delete(items[i].dict(), "cls")
			return fmt.Sprintf("inplace delete rows[%d].cls", i)
		}
	}
	return ""
}

func rvRoot(r *rand.Rand, prev Value) Value {
	m := map[string]Value{}
	if prev.isMap() {
		for k, v := range prev.dict() {
			m[k] = v // shares the composite children of the previous root
		}
	}
	switch r.Intn(6) {
	case 0:
		m["title"] = StringVal(fmt.Sprintf("T%d", r.Intn(3)))
	case 1:
		m["unread"] = IntVal(r.Intn(100))
	case 2:
		m["meta"] = obj2("a", StringVal(fmt.Sprintf("m%d", r.Intn(2))))
	case 3:
		delete(m, "title")
	case 4:
		m["list2"] = ArrayVal([]Value{StringVal("p"), StringVal([]string{"q", "r"}[r.Intn(2)])})
	default:
		m["rows"] = rvRows(r)
	}
	return ObjectVal(m)
}

// --- programs --------------------------------------------------------------

// rowsProgram is a Bon Voyage-shaped list island:
//
//	<div>
//	  <h1>{s.title}</h1>
//	  <ul><Each of={s.rows} as="row" fallback={"empty " + row}>
//	        <li class={row.cls}>{row.name}
//	          <Cond row.tags><Each of={row.tags} as="t"><i>{t}</i></Each></Cond>
//	          <span>{_index}</span>{bodyOuter? <b>{s.title}</b>}
//	        </li></Each></ul>
//	  <p>{len(s.rows)}</p>
//	  <p>{row}</p>                           island prop "row", outside the loop
//	  <section><b>{s.meta.a}</b><ul><Each of={s.list2} as="x"><li>{x}</li></Each></ul></section>
//	</div>
func rowsProgram(bodyReadsOuter bool) *program.Program {
	b := newRV("Rows")
	s := b.sig("s")
	row := b.prop("row")
	fallback := b.e(program.OpConcat, "", program.TypeString, b.lit("empty "), b.prop("row"))
	li := []program.NodeID{
		b.txt(b.path(row, "name")),
		b.cond(b.path(b.prop("row"), "tags"),
			b.each(b.path(b.prop("row"), "tags"), "t", nil, b.el("i", nil, b.txt(b.prop("t"))))),
		b.el("span", nil, b.txt(b.prop("_index"))),
	}
	if bodyReadsOuter {
		li = append(li, b.el("b", nil, b.txt(b.path(b.sig("s"), "title"))))
	}
	list := b.each(b.path(s, "rows"), "row", &fallback,
		b.el("li", []program.Attr{attrExpr("class", b.path(b.prop("row"), "cls"))}, li...))
	section := b.el("section", nil,
		b.el("b", nil, b.txt(b.path(b.sig("s"), "meta", "a"))),
		b.el("ul", nil, b.each(b.path(b.sig("s"), "list2"), "x", nil, b.el("li", nil, b.txt(b.prop("x"))))))
	root := b.el("div", nil,
		b.el("h1", nil, b.txt(b.path(b.sig("s"), "title"))),
		b.el("ul", nil, list),
		b.el("p", nil, b.txt(b.e(program.OpLen, "", program.TypeInt, b.path(b.sig("s"), "rows")))),
		b.el("p", nil, b.txt(b.prop("row"))),
		section,
	)
	return b.finish(root, "s")
}

func runRandomRows(t *testing.T, bodyReadsOuter bool, seeds, steps int) {
	for seed := 0; seed < seeds; seed++ {
		r := rand.New(rand.NewSource(int64(seed)))
		p := rvNewPair(t, rowsProgram(bodyReadsOuter), `{"row":"outer"}`, "s")
		cur := obj2("title", StringVal("T0"), "rows", rvRows(r))
		var history []Value
		p.set("s", cur)
		if !p.step("init") {
			return
		}
		for i := 0; i < steps; i++ {
			what := ""
			switch k := r.Intn(10); {
			case k < 4:
				history = append(history, cur)
				cur = rvRoot(r, cur)
				p.set("s", cur)
				what = "set root"
			case k < 7:
				what = rvMutate(r, cur)
				if what == "" {
					what = "noop"
				}
			case k < 8:
				p.set("s", cur) // same value again
				what = "set same"
			case k < 9 && len(history) > 0:
				cur = history[r.Intn(len(history))]
				p.set("s", cur)
				what = "restore earlier root"
			default:
				v := StringVal(fmt.Sprintf("outer%d", r.Intn(2)))
				p.a.vm.SetProp("row", v)
				p.b.vm.SetProp("row", v)
				what = "SetProp row"
			}
			if !p.step(fmt.Sprintf("seed %d: %s", seed, what)) {
				return
			}
		}
	}
}

func TestLockstepRandomRowsBodyPure(t *testing.T)       { runRandomRows(t, false, 200, 120) }
func TestLockstepRandomRowsBodyReadsOuter(t *testing.T) { runRandomRows(t, true, 200, 120) }

// carryProgram: a reusable <section> holding a list, beside a sibling that
// changes on its own, so the section is copied and its row records carried.
func carryProgram() *program.Program {
	b := newRV("Carry")
	sec := b.el("section", nil,
		b.el("h2", nil, b.txt(b.path(b.sig("s"), "a"))),
		b.cond(b.path(b.sig("s"), "show"),
			b.el("ul", nil, b.each(b.path(b.sig("s"), "rows"), "row", nil,
				b.el("li", nil, b.txt(b.path(b.prop("row"), "name")), b.el("em", nil, b.txt(b.prop("rowKey"))))))))
	root := b.el("div", nil, b.el("p", nil, b.txt(b.path(b.sig("s"), "b"))), sec,
		b.el("p", nil, b.txt(b.path(b.sig("s"), "b"))))
	return b.finish(root, "s")
}

func TestLockstepCarriedRows(t *testing.T) {
	for seed := 0; seed < 300; seed++ {
		r := rand.New(rand.NewSource(int64(seed)))
		p := rvNewPair(t, carryProgram(), `{}`, "s")
		fields := map[string]Value{"a": StringVal("A"), "b": IntVal(0), "show": BoolVal(true), "rows": rvRows(r)}
		snapshot := func() Value {
			m := map[string]Value{}
			for k, v := range fields {
				m[k] = v
			}
			return ObjectVal(m)
		}
		p.set("s", snapshot())
		p.step("init")
		for i := 0; i < 80; i++ {
			what := ""
			switch r.Intn(6) {
			case 0, 1:
				fields["b"] = IntVal(r.Intn(1000))
				what = "b"
			case 2:
				fields["rows"] = rvRows(r)
				what = "rows"
			case 3:
				fields["show"] = BoolVal(r.Intn(2) == 0)
				what = "show"
			case 4:
				fields["a"] = StringVal([]string{"A", "B"}[r.Intn(2)])
				what = "a"
			case 5:
				if rows := fields["rows"]; rows.isList() && len(rows.list()) > 0 {
					l := rows.list()
					l[r.Intn(len(l))] = rvItem(r) // in place, then Set a new root
				}
				what = "rows in place"
			}
			p.set("s", snapshot())
			if !p.step(fmt.Sprintf("seed %d: %s", seed, what)) {
				return
			}
		}
	}
}

// Many paths force the plan to merge deep paths into ancestors.
func TestLockstepPathOverflow(t *testing.T) {
	b := newRV("Overflow")
	var kids []program.NodeID
	for i := 0; i < 70; i++ {
		kids = append(kids, b.el("p", nil, b.txt(b.path(b.sig("s"), fmt.Sprintf("f%d", i), "x", "y", "z", "w"))))
	}
	prog := b.finish(b.el("div", nil, kids...), "s")
	r := rand.New(rand.NewSource(7))
	p := rvNewPair(t, prog, `{}`, "s")
	root := map[string]Value{}
	deep := func(v string) Value {
		return obj2("x", obj2("y", obj2("z", obj2("w", StringVal(v), "o", StringVal(v)))))
	}
	for i := 0; i < 70; i++ {
		root[fmt.Sprintf("f%d", i)] = deep("0")
	}
	p.set("s", ObjectVal(root))
	p.step("init")
	for i := 0; i < 400; i++ {
		f := fmt.Sprintf("f%d", r.Intn(70))
		v := fmt.Sprintf("%d", r.Intn(3))
		switch r.Intn(3) {
		case 0: // in place, deep
			rvDict(rvDict(rvDict(rvDict(root[f])["x"])["y"])["z"])["w"] = StringVal(v)
		case 1: // unread sibling at depth 4
			rvDict(rvDict(rvDict(rvDict(root[f])["x"])["y"])["z"])["o"] = StringVal(v)
		case 2:
			nm := map[string]Value{}
			for k, x := range root {
				nm[k] = x
			}
			nm[f] = deep(v)
			root = nm
		}
		p.set("s", ObjectVal(root))
		if !p.step(fmt.Sprintf("write %s=%s", f, v)) {
			return
		}
	}
}

// Absent inputs: an island prop and a signal that appear and vanish.
func TestLockstepAbsentInputsAppearAndVanish(t *testing.T) {
	b := newRV("Absent")
	root := b.el("div", nil,
		b.el("p", nil, b.txt(b.prop("maybe"))),
		b.el("p", nil, b.txt(b.path(b.sig("late"), "v"))),
		b.el("p", nil, b.txt(b.sig("late2"))),
		b.el("p", nil, b.txt(b.prop("nil"))),
	)
	prog := b.finish(root) // no signal defs: "late" and "late2" start absent
	p := rvNewPair(t, prog, `{}`)
	p.step("prime")
	p.step("nothing")
	p.a.vm.SetProp("maybe", StringVal("here"))
	p.b.vm.SetProp("maybe", StringVal("here"))
	p.step("prop appears")
	p.a.vm.DeleteProp("maybe")
	p.b.vm.DeleteProp("maybe")
	p.step("prop vanishes")
	la, lb := signal.New(obj2("v", StringVal("L1"))), signal.New(obj2("v", StringVal("L1")))
	p.a.vm.SetSignal("late", la)
	p.b.vm.SetSignal("late", lb)
	p.step("signal installed")
	la.Set(obj2("v", StringVal("L2")))
	lb.Set(obj2("v", StringVal("L2")))
	p.step("signal path changes")
	l2a, l2b := signal.New(IntVal(5)), signal.New(IntVal(5))
	p.a.vm.SetSignal("late2", l2a)
	p.b.vm.SetSignal("late2", l2b)
	p.step("scalar signal installed")
}

// Map collections: the row key is the map key, read through rowKey.
func TestLockstepMapCollectionRows(t *testing.T) {
	b := newRV("MapRows")
	list := b.each(b.path(b.sig("s"), "byId"), "row", nil,
		b.el("li", nil, b.txt(b.prop("rowKey")), b.txt(b.lit(":")), b.txt(b.path(b.prop("row"), "name"))))
	prog := b.finish(b.el("ul", nil, list), "s")
	r := rand.New(rand.NewSource(3))
	p := rvNewPair(t, prog, `{}`, "s")
	for i := 0; i < 600; i++ {
		m := map[string]Value{}
		for j := 0; j < r.Intn(4); j++ {
			m[[]string{"k1", "k2", "k3", "k4"}[r.Intn(4)]] = obj2("name", StringVal([]string{"a", "b"}[r.Intn(2)]))
		}
		p.set("s", obj2("byId", ObjectVal(m)))
		if !p.step(fmt.Sprintf("byId=%v", ObjectVal(m))) {
			return
		}
	}
}

// SetSharedSignal and InstallProgram in the middle of a stream.
func TestLockstepSharedSignalAndSwapMidStream(t *testing.T) {
	r := rand.New(rand.NewSource(11))
	prog := rowsProgram(false)
	p := rvNewPair(t, prog, `{"row":"outer"}`, "s")
	cur := obj2("title", StringVal("T0"), "rows", rvRows(r))
	p.set("s", cur)
	p.step("init")
	for i := 0; i < 300; i++ {
		switch r.Intn(8) {
		case 0:
			v := rvRoot(r, cur)
			na, nb := signal.New(v), signal.New(v)
			p.sa["s"], p.sb["s"] = na, nb
			p.a.SetSharedSignal("s", na)
			p.b.SetSharedSignal("s", nb)
			cur = v
			if !p.step("SetSharedSignal") {
				return
			}
		case 1:
			pa, pb := p.a.SwapProgram(rowsProgram(false)), p.b.SwapProgram(rowsProgram(false))
			if fmt.Sprintf("%+v", pa) != fmt.Sprintf("%+v", pb) {
				t.Fatalf("swap ops differ: %+v vs %+v", pa, pb)
			}
			// SwapProgram keeps signal state by name; rebind ours.
			p.a.vm.SetSignal("s", p.sa["s"])
			p.b.vm.SetSignal("s", p.sb["s"])
			if !p.step("SwapProgram") {
				return
			}
		default:
			cur = rvRoot(r, cur)
			p.set("s", cur)
			if !p.step("set root") {
				return
			}
		}
	}
}

// Latent: a signal path through an OpIndex marked IndexTypedMap. The lowerer
// emits that mark only on props.Field[k] today, never on a signal root.
func TestLockstepTypedMapSignalPathNullVersusMissing(t *testing.T) {
	b := newRV("TypedMap")
	m := b.path(b.sig("s"), "m")
	leaf := b.e(program.OpIndex, program.IndexTypedMap, program.TypeString, m, b.lit("k"))
	prog := b.finish(b.el("div", nil, b.el("p", nil, b.txt(leaf))), "s")
	p := rvNewPair(t, prog, `{}`, "s")
	withNull, err := ValueFromJSON(`{"m":{"k":null}}`)
	if err != nil {
		t.Fatal(err)
	}
	missing, _ := ValueFromJSON(`{"m":{}}`)
	p.set("s", withNull)
	p.step(`{"m":{"k":null}}`)
	p.set("s", missing)
	p.step(`{"m":{}}`)
	p.set("s", withNull)
	p.step(`{"m":{"k":null}} again`)
}
