package vm

import (
	"fmt"
	"math"
	"math/rand"
	"testing"

	"m31labs.dev/gosx/island/program"
	"m31labs.dev/gosx/signal"
)

// handlerProgram renders a list and a few fields of s, and has handlers that
// edit s IN PLACE through OpFieldSet and OpIndexSet, including an aliasing
// write rows[1] = rows[0].
func handlerProgram() *program.Program {
	b := newRV("Handlers")
	rowsOf := func() program.ExprID { return b.path(b.sig("s"), "rows") }
	at := func(i string) program.ExprID {
		return b.e(program.OpIndex, "", program.TypeAny, rowsOf(), b.e(program.OpLitInt, i, program.TypeInt))
	}
	list := b.each(rowsOf(), "row", nil,
		b.el("li", []program.Attr{attrExpr("class", b.path(b.prop("row"), "cls"))}, b.txt(b.path(b.prop("row"), "name"))))
	root := b.el("div", nil,
		b.el("h1", nil, b.txt(b.path(b.sig("s"), "title"))),
		b.el("ul", nil, list),
		b.el("p", nil, b.txt(b.path(b.sig("s"), "meta", "a"))),
		b.el("p", nil, b.txt(b.e(program.OpLen, "", program.TypeInt, rowsOf()))),
	)
	set := func(name string, body ...program.ExprID) {
		b.p.Handlers = append(b.p.Handlers, program.Handler{Name: name, Body: body})
	}
	for i, v := range []string{"x", "y", "z"} {
		set(fmt.Sprintf("title%d", i), b.e(program.OpFieldSet, "title", program.TypeAny, b.sig("s"), b.lit("T"+v)))
		set(fmt.Sprintf("name0_%d", i), b.e(program.OpFieldSet, "name", program.TypeAny, at("0"), b.lit("n"+v)))
		set(fmt.Sprintf("name1_%d", i), b.e(program.OpFieldSet, "name", program.TypeAny, at("1"), b.lit("n"+v)))
		set(fmt.Sprintf("meta_%d", i), b.e(program.OpIndexSet, "", program.TypeAny, b.path(b.sig("s"), "meta"), b.lit("a"), b.lit("m"+v)))
		set(fmt.Sprintf("unread_%d", i), b.e(program.OpFieldSet, "unread", program.TypeAny, b.sig("s"), b.lit(v)))
	}
	set("alias", b.e(program.OpIndexSet, "", program.TypeAny, rowsOf(), b.e(program.OpLitInt, "1", program.TypeInt), at("0")))
	set("replace2", b.e(program.OpIndexSet, "", program.TypeAny, rowsOf(), b.e(program.OpLitInt, "2", program.TypeInt), b.lit("plain")))
	set("cls0", b.e(program.OpFieldSet, "cls", program.TypeAny, at("0"), b.lit("hot")))
	return b.finish(root, "s")
}

func TestLockstepInPlaceEditsThroughHandlers(t *testing.T) {
	prog := handlerProgram()
	var names []string
	for _, h := range prog.Handlers {
		names = append(names, h.Name)
	}
	roots := []string{
		`{"title":"T0","rows":[{"name":"a"},{"name":"b","cls":"c"},{"name":"c"}],"meta":{"a":"m0"}}`,
		`{"title":"T0","rows":[{"name":"a"},{"name":"b"}],"meta":{"a":"m0"}}`,
		`{"title":"T1","rows":[{"name":"a"},{"name":"a"},{"name":"a"},{"name":"d"}],"meta":{}}`,
	}
	for seed := 0; seed < 300; seed++ {
		r := rand.New(rand.NewSource(int64(seed)))
		p := rvNewPair(t, prog, `{}`, "s")
		load := func() string {
			text := roots[r.Intn(len(roots))]
			va, _ := ValueFromJSON(text) // separate storage per island
			vb, _ := ValueFromJSON(text)
			p.sa["s"].Set(va)
			p.sb["s"].Set(vb)
			return text
		}
		load()
		if !p.step("init") {
			return
		}
		for i := 0; i < 40; i++ {
			if r.Intn(8) == 0 {
				if !p.step("load " + load()) {
					return
				}
				continue
			}
			h := names[r.Intn(len(names))]
			if !p.compare(fmt.Sprintf("seed %d dispatch %s", seed, h), p.a.Dispatch(h, "{}"), p.b.Dispatch(h, "{}")) {
				return
			}
		}
	}
}

// movedRowProgram: a conditional element ahead of a list in the same parent.
// When it disappears, every unchanged row is copied to a lower index, which
// is the index another node held in the previous tree.
func movedRowProgram() *program.Program {
	b := newRV("MovedRows")
	list := b.each(b.path(b.sig("s"), "rows"), "row", nil, b.el("li", nil, b.txt(b.path(b.prop("row"), "name"))))
	head := b.cond(b.path(b.sig("s"), "show"), b.el("li", nil, b.txt(b.path(b.sig("s"), "head"))))
	return b.finish(b.el("ul", nil, head, list), "s")
}

func TestLockstepMovedRowOntoAnotherNodesIndex(t *testing.T) {
	p := rvNewPair(t, movedRowProgram(), `{}`, "s")
	rows := ArrayVal([]Value{obj2("name", StringVal("A")), obj2("name", StringVal("B"))})
	p.set("s", obj2("show", BoolVal(true), "head", StringVal("HEAD"), "rows", rows))
	p.step("show head")
	p.set("s", obj2("show", BoolVal(false), "head", StringVal("HEAD"), "rows", rows))
	p.step("hide head: rows move down")
	p.set("s", obj2("show", BoolVal(true), "head", StringVal("HEAD"), "rows", rows))
	p.step("show head: rows move up")
	two := ArrayVal([]Value{obj2("name", StringVal("A")), obj2("name", StringVal("A"))})
	p.set("s", obj2("show", BoolVal(false), "head", StringVal("A"), "rows", two))
	p.step("hide head, equal rows")
}

// Paths that overflow 64 keys at depth 3 and 2 but fit at depth 1.
func TestLockstepPathMergeAtIntermediateDepth(t *testing.T) {
	b := newRV("MergeDepth")
	var kids []program.NodeID
	for i := 0; i < 70; i++ {
		kids = append(kids, b.el("p", nil, b.txt(b.path(b.sig("s"), fmt.Sprintf("g%d", i%5), fmt.Sprintf("h%d", i), "x", "y"))))
	}
	kids = append(kids, b.el("b", nil, b.txt(b.path(b.sig("s"), "solo"))))
	prog := b.finish(b.el("div", nil, kids...), "s")
	p := rvNewPair(t, prog, `{}`, "s")
	groups := map[string]Value{}
	leaf := func(v string) Value { return obj2("x", obj2("y", StringVal(v), "o", StringVal(v))) }
	for g := 0; g < 5; g++ {
		m := map[string]Value{}
		for i := g; i < 70; i += 5 {
			m[fmt.Sprintf("h%d", i)] = leaf("0")
		}
		groups[fmt.Sprintf("g%d", g)] = ObjectVal(m)
	}
	groups["solo"] = StringVal("s0")
	root := ObjectVal(groups)
	p.set("s", root)
	p.step("init")
	r := rand.New(rand.NewSource(5))
	for i := 0; i < 500; i++ {
		n := r.Intn(70)
		g := rvDict(root)[fmt.Sprintf("g%d", n%5)]
		h := rvDict(g)[fmt.Sprintf("h%d", n)]
		v := fmt.Sprintf("%d", r.Intn(3))
		what := ""
		switch r.Intn(4) {
		case 0:
			rvDict(rvDict(h)["x"])["y"] = StringVal(v)
			what = fmt.Sprintf("in place g%d.h%d.x.y=%s", n%5, n, v)
		case 1:
			rvDict(rvDict(h)["x"])["o"] = StringVal(v)
			what = "in place unread sibling"
		case 2:
			rvDict(root)["solo"] = StringVal("s" + v)
			what = "in place solo"
		case 3:
			rvDict(g)[fmt.Sprintf("h%d", n)] = leaf(v)
			what = "in place replace leaf"
		}
		p.set("s", root)
		if !p.step(what) {
			return
		}
	}
}

// Float zero and negative zero render differently ("0" and "-0").
func TestLockstepNegativeZeroScalarSignal(t *testing.T) {
	b := newRV("NegZero")
	prog := b.finish(b.el("div", nil, b.el("p", nil, b.txt(b.sig("f"))), b.el("p", nil, b.txt(b.prop("pf")))), "f")
	p := rvNewPair(t, prog, `{}`, "f")
	p.a.vm.SetProp("pf", FloatVal(0))
	p.b.vm.SetProp("pf", FloatVal(0))
	p.set("f", FloatVal(0))
	p.step("f=0, pf=0")
	p.set("f", FloatVal(math.Copysign(0, -1)))
	p.step("f=-0")
	p.a.vm.SetProp("pf", FloatVal(math.Copysign(0, -1)))
	p.b.vm.SetProp("pf", FloatVal(math.Copysign(0, -1)))
	p.step("pf=-0")
}

// The same value inside a composite: the digest compares float bits.
func TestLockstepNegativeZeroInsideComposite(t *testing.T) {
	b := newRV("NegZeroPath")
	prog := b.finish(b.el("div", nil, b.el("p", nil, b.txt(b.path(b.sig("s"), "v")))), "s")
	p := rvNewPair(t, prog, `{}`, "s")
	p.set("s", obj2("v", FloatVal(0)))
	p.step("s.v=0")
	p.set("s", obj2("v", FloatVal(math.Copysign(0, -1))))
	p.step("s.v=-0")
	p.set("s", obj2("v", FloatVal(math.NaN())))
	p.step("s.v=NaN")
	p.set("s", obj2("v", FloatVal(math.NaN())))
	p.step("s.v=NaN again")
}

var _ = signal.New[int]
