package vm

import (
	"fmt"
	"testing"

	"m31labs.dev/gosx/island/program"
	"m31labs.dev/gosx/signal"
)

// benchDockProgram is a small stand-in for a console island: one composite
// signal "s" read through field paths by a heading, a list of 200 rows with
// four nodes each, and a footer.
//
//	<section>
//	  <h1>{s.title}</h1>
//	  <ul><ForEach s.rows as row><li><span>{row.name}</span><em>{row.state}</em></li></ForEach></ul>
//	  <footer>{s.footer}</footer>
//	</section>
func benchDockProgram() *program.Program {
	prog := &program.Program{Name: "BenchDock"}
	expr := func(e program.Expr) program.ExprID {
		prog.Exprs = append(prog.Exprs, e)
		return program.ExprID(len(prog.Exprs) - 1)
	}
	node := func(n program.Node) program.NodeID {
		prog.Nodes = append(prog.Nodes, n)
		return program.NodeID(len(prog.Nodes) - 1)
	}
	field := func(base program.ExprID, name string) program.ExprID {
		key := expr(program.Expr{Op: program.OpLitString, Value: name, Type: program.TypeString})
		return expr(program.Expr{Op: program.OpIndex, Operands: []program.ExprID{base, key}, Type: program.TypeAny})
	}
	sig := expr(program.Expr{Op: program.OpSignalGet, Value: "s", Type: program.TypeAny})
	row := expr(program.Expr{Op: program.OpPropGet, Value: "row", Type: program.TypeAny})
	zero := expr(program.Expr{Op: program.OpLitInt, Value: "0", Type: program.TypeInt})
	prog.Signals = []program.SignalDef{{Name: "s", Type: program.TypeAny, Init: zero}}
	root := node(program.Node{Kind: program.NodeElement, Tag: "section"})
	h1t := node(program.Node{Kind: program.NodeExpr, Expr: field(sig, "title")})
	h1 := node(program.Node{Kind: program.NodeElement, Tag: "h1", Children: []program.NodeID{h1t}})
	ul := node(program.Node{Kind: program.NodeElement, Tag: "ul"})
	each := node(program.Node{Kind: program.NodeForEach, Expr: field(sig, "rows"),
		Attrs: []program.Attr{{Kind: program.AttrStatic, Name: "as", Value: "row"}}})
	nameT := node(program.Node{Kind: program.NodeExpr, Expr: field(row, "name")})
	stateT := node(program.Node{Kind: program.NodeExpr, Expr: field(row, "state")})
	span := node(program.Node{Kind: program.NodeElement, Tag: "span", Children: []program.NodeID{nameT},
		Attrs: []program.Attr{{Kind: program.AttrExpr, Name: "title", Expr: field(row, "name")}}})
	em := node(program.Node{Kind: program.NodeElement, Tag: "em", Children: []program.NodeID{stateT}})
	li := node(program.Node{Kind: program.NodeElement, Tag: "li", Children: []program.NodeID{span, em}})
	prog.Nodes[each].Children = []program.NodeID{li}
	prog.Nodes[ul].Children = []program.NodeID{each}
	ft := node(program.Node{Kind: program.NodeExpr, Expr: field(sig, "footer")})
	footer := node(program.Node{Kind: program.NodeElement, Tag: "footer", Children: []program.NodeID{ft}})
	prog.Nodes[root].Children = []program.NodeID{h1, ul, footer}
	return prog
}

func benchDockState(title, footer string, unread int, rows []Value) Value {
	return ObjectVal(map[string]Value{
		"title":  StringVal(title),
		"footer": StringVal(footer),
		"unread": IntVal(unread),
		"rows":   ArrayVal(rows),
	})
}

func benchDockRows(n int, changed int, stamp int) []Value {
	rows := make([]Value, n)
	for i := range rows {
		state := "idle"
		if i == changed {
			state = fmt.Sprintf("busy %d", stamp)
		}
		rows[i] = ObjectVal(map[string]Value{"name": StringVal(fmt.Sprintf("row %d", i)), "state": StringVal(state)})
	}
	return rows
}

// BenchmarkCompositeWrite measures one reconcile after a write to a composite
// signal: "unread" changes a field no node reads, "oneRow" changes one list
// row, and "title" changes the heading only.
func BenchmarkCompositeWrite(b *testing.B) {
	const rowCount = 200
	for _, kind := range []string{"unread", "oneRow", "title"} {
		b.Run(kind, func(b *testing.B) {
			island := NewIsland(benchDockProgram(), `{}`)
			sig := signal.New(benchDockState("t", "f", 0, benchDockRows(rowCount, -1, 0)))
			island.vm.SetSignal("s", sig)
			island.Reconcile()
			island.Reconcile()
			// The states are built before the clock starts: the benchmark
			// measures the reconcile, not the cost of decoding a batch.
			states := make([]Value, 64)
			for i := range states {
				switch kind {
				case "unread":
					states[i] = benchDockState("t", "f", i+1, benchDockRows(rowCount, -1, 0))
				case "oneRow":
					states[i] = benchDockState("t", "f", 0, benchDockRows(rowCount, 100, i))
				case "title":
					states[i] = benchDockState(fmt.Sprintf("t%d", i), "f", 0, benchDockRows(rowCount, -1, 0))
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				sig.Set(states[i%len(states)])
				island.Reconcile()
			}
		})
	}
}
