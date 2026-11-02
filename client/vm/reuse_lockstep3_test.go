package vm

import (
	"math/rand"
	"testing"

	"m31labs.dev/gosx/island/program"
)

// movedKeyedRowProgram: like movedRowProgram, but the row root carries an
// explicit key. keyMode "empty" uses key="" on every row; "dup" uses
// key={row.id} with duplicate ids and renders the index.
func movedKeyedRowProgram(keyMode string) *program.Program {
	b := newRV("MovedKeyedRows")
	var keyAttr program.Attr
	if keyMode == "empty" {
		keyAttr = program.Attr{Kind: program.AttrStatic, Name: "key", Value: ""}
	} else {
		keyAttr = attrExpr("key", b.path(b.prop("row"), "id"))
	}
	list := b.each(b.path(b.sig("s"), "rows"), "row", nil,
		b.el("li", []program.Attr{keyAttr}, b.txt(b.prop("_index")), b.txt(b.lit(":")), b.txt(b.path(b.prop("row"), "name"))))
	head := b.cond(b.path(b.sig("s"), "show"), b.el("li", nil, b.txt(b.path(b.sig("s"), "head"))))
	return b.finish(b.el("ul", nil, head, list), "s")
}

func runMovedKeyed(t *testing.T, mode string) {
	p := rvNewPair(t, movedKeyedRowProgram(mode), `{}`, "s")
	rows := ArrayVal([]Value{obj2("id", StringVal("k"), "name", StringVal("A")), obj2("id", StringVal("k"), "name", StringVal("A"))})
	for i, show := range []bool{true, false, true, false} {
		p.set("s", obj2("show", BoolVal(show), "head", StringVal("HEAD"), "rows", rows))
		if !p.step(map[bool]string{true: "show head", false: "hide head: rows move"}[show]) {
			return
		}
		_ = i
	}
}

func TestLockstepMovedRowEmptyExplicitKey(t *testing.T)     { runMovedKeyed(t, "empty") }
func TestLockstepMovedRowDuplicateExplicitKey(t *testing.T) { runMovedKeyed(t, "dup") }

// Computed signals: a list and a field read through computeds over s.
func TestLockstepComputedComposites(t *testing.T) {
	b := newRV("Computed")
	cRows := b.path(b.sig("s"), "rows")
	cTitle := b.path(b.sig("s"), "title")
	list := b.each(b.sig("crows"), "row", nil, b.el("li", nil, b.txt(b.path(b.prop("row"), "name"))))
	root := b.el("div", nil, b.el("h1", nil, b.txt(b.sig("ctitle"))), b.el("ul", nil, list),
		b.el("p", nil, b.txt(b.path(b.sig("cwhole"), "title"))))
	prog := b.finish(root, "s")
	prog.Computeds = []program.ComputedDef{
		{Name: "crows", Type: program.TypeAny, Expr: cRows},
		{Name: "ctitle", Type: program.TypeAny, Expr: cTitle},
		{Name: "cwhole", Type: program.TypeAny, Expr: b.sig("s")},
	}
	for seed := 0; seed < 100; seed++ {
		r := rvRand(seed)
		p := rvNewPair(t, prog, `{}`, "s")
		cur := obj2("title", StringVal("T0"), "rows", rvRows(r))
		p.set("s", cur)
		if !p.step("init") {
			return
		}
		for i := 0; i < 60; i++ {
			what := "set root"
			if r.Intn(3) == 0 {
				what = rvMutate(r, cur)
			} else {
				cur = rvRoot(r, cur)
				p.set("s", cur)
			}
			if !p.step(what) {
				return
			}
		}
	}
}

func rvRand(seed int) *rand.Rand { return rand.New(rand.NewSource(int64(seed))) }

// Two distinct paths whose joined names collide: s["a\x1fb"] and s.a.b.
func TestLockstepPathSeparatorCollision(t *testing.T) {
	b := newRV("PathSep")
	one := b.path(b.sig("s"), "a\x1fb")
	two := b.path(b.sig("s"), "a", "b")
	prog := b.finish(b.el("div", nil, b.el("p", nil, b.txt(one)), b.el("p", nil, b.txt(two))), "s")
	p := rvNewPair(t, prog, `{}`, "s")
	p.set("s", obj2("a\x1fb", StringVal("X"), "a", obj2("b", StringVal("1"))))
	p.step("init")
	p.set("s", obj2("a\x1fb", StringVal("X"), "a", obj2("b", StringVal("2"))))
	p.step("s.a.b changes")
}
