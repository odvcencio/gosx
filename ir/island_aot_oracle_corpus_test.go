//go:build !tinygo

package ir_test

import (
	"fmt"
	"strings"
)

type scalarOracleCase struct {
	name, root, typ, expr, goExpr, packageDecl, localDecl string
}

// This Cartesian corpus has no random, map-order, clock or filesystem inputs.
// Recipes describe source expressions, never inferred AOT types or opcodes.
func scalarOracleCorpus() []scalarOracleCase {
	var cases []scalarOracleCase
	add := func(group, root, typ, expr, goExpr, pkg, local string) {
		cases = append(cases, scalarOracleCase{
			fmt.Sprintf("%s/%04d/%s", group, len(cases), root), root, typ, expr, goExpr, pkg, local,
		})
	}
	for _, typ := range []string{"int", "int32", "int64", "float64", "string", "bool"} {
		for _, other := range []string{"props.Other", "props.I32", "props.I64", "1", "-1", "(1 + 2)", "0.5", `"x"`, "true"} {
			for _, op := range []string{"+", "-", "*", "/", "%", "==", "!=", "<", "<=", ">", ">="} {
				for _, pair := range [][2]string{{"props.Value", other}, {other, "props.Value"}} {
					expr := pair[0] + " " + op + " " + pair[1]
					add("operands", "text", typ, expr, expr, "", "")
				}
			}
		}
		for _, other := range []string{"props.Other", "props.I32", "props.I64", "1", "0.5", `"x"`, "true"} {
			for _, condition := range []string{"true", "false", "props.Flag", "props.Value"} {
				for _, pair := range [][2]string{{"props.Value", other}, {other, "props.Value"}} {
					add("conditional", "text", typ,
						condition+" ? "+pair[0]+" : "+pair[1],
						"oracle.Choose("+condition+", "+pair[0]+", "+pair[1]+")", "", "")
				}
			}
		}
	}
	roots := []string{"text", "attribute", "signal", "shared", "computed", "argument", "statement", "predicate", "iteration"}
	for _, boundary := range []struct{ literal, arithmetic string }{
		{"2147483647", "2147483646 + 1"}, {"2147483648", "2147483647 + 1"},
		{"-2147483648", "-2147483647 - 1"}, {"-2147483649", "-2147483648 - 1"},
	} {
		for _, typ := range []string{"int", "int32"} {
			for _, expr := range []string{boundary.literal, boundary.arithmetic} {
				for _, root := range roots {
					add("boundary", root, typ, expr, expr, "", "")
					for _, condition := range []string{"true", "false"} {
						add("boundary_conditional", root, typ,
							condition+" ? "+expr+" : 0", "oracle.Choose("+condition+", "+expr+", 0)", "", "")
					}
				}
			}
		}
	}
	for _, expr := range []string{
		"props", "props.Detail", "props.Detail.Label", "props.Detail.Number",
		"props.List", "props.Map", "props.Pointer", "props.Pointer.Label", `props.Map["x"]`, "props.List[0]",
	} {
		for _, root := range roots {
			add("selector", root, "int32", expr, expr, "", "")
		}
	}
	for _, name := range []string{"int", "int32", "bool", "string"} {
		for _, declaration := range []string{
			"type " + name + " = int64", "type " + name + " int",
			"type CounterAlias = " + name, "type CounterNamed " + name,
		} {
			for _, local := range []bool{false, true} {
				pkg, block := declaration, ""
				if local {
					pkg, block = "", declaration
				}
				for _, root := range roots {
					add("declaration", root, name, "props.Value", "props.Value", pkg, block)
				}
			}
		}
	}
	for _, typ := range []string{"int", "int32", "bool", "string"} {
		for _, root := range roots {
			for _, expr := range []string{"props.Value", oracleZero(typ), "len(props.Label)"} {
				add("root", root, typ, expr, expr, "", "")
			}
		}
	}
	for _, expr := range []string{"010", "-010", "010 + 1", "1 + 010", "010 * 2", "001"} {
		for _, typ := range []string{"int", "int32"} {
			for _, root := range []string{"text", "attribute", "signal", "shared", "computed", "argument"} {
				add("literal_spelling", root, typ, expr, expr, "", "")
			}
		}
	}
	for _, tc := range []struct{ expr, pkg, local string }{
		{"len(props.Label)", "func len(s string) int64 { return 9 }", ""},
		{"len(props.Label)", "", "len := func(s string) int64 { return 9 }; _ = len"},
		{"true", "const true = 1", ""}, {"false", "const false = 1", ""},
		{"true", "", "const true = 1"}, {"false", "", "const false = 1"},
		{"props.Label.length", "", ""}, {"props.Value.ToString()", "", ""},
		{"props.Value", "", "type int32 = int64"},
	} {
		for _, root := range roots {
			add("binding", root, "int32", tc.expr, tc.expr, tc.pkg, tc.local)
		}
	}
	return cases
}

func oracleZero(typ string) string {
	switch typ {
	case "bool":
		return "false"
	case "string":
		return `""`
	default:
		return "0"
	}
}

func (c scalarOracleCase) sources() (string, string) {
	imports := ""
	if c.root == "signal" || c.root == "shared" || c.root == "computed" || c.root == "argument" {
		imports = "import signal \"m31labs.dev/gosx/signal\"\n"
	}
	head := "package example\n" + imports + c.packageDecl + fmt.Sprintf(`
type Detail struct { Label string; Number int32 }
type Props struct {
 Value %s; Other int; I32 int32; I64 int64; Flag bool; Label string
 Detail Detail; List []int32; Map map[string]int32; Pointer *Detail
}
`, c.typ)
	body := func(expr string, goSource bool) string {
		value := "return <div>{" + expr + "}</div>"
		if goSource {
			value = "return oracle.Emit(" + expr + ")"
		}
		switch c.root {
		case "attribute":
			value = "return <div title={" + expr + "}/>"
			if goSource {
				value = "return oracle.Attr(" + expr + ")"
			}
		case "signal", "shared":
			constructor, args := "New", expr
			if c.root == "shared" {
				constructor, args = "NewShared", `"value", `+expr
			}
			value = fmt.Sprintf("value := signal.%s[%s](%s)\nreturn <div>{value.Get()}</div>", constructor, c.typ, args)
			if goSource {
				value = fmt.Sprintf("value := signal.%s[%s](%s)\nreturn oracle.Emit(value.Get())", constructor, c.typ, args)
			}
		case "computed":
			value = fmt.Sprintf("value := signal.Derive(func() %s { return %s })\nreturn <div>{value.Get()}</div>", c.typ, expr)
			if goSource {
				value = fmt.Sprintf("value := signal.Derive(func() %s { return %s })\nreturn oracle.Emit(value.Get())", c.typ, expr)
			}
		case "argument":
			value = fmt.Sprintf("value := signal.New[%s](%s)\nchange := func() { value.Set(%s) }\nreturn <button onClick={change}>{value.Get()}</button>", c.typ, oracleZero(c.typ), expr)
			if goSource {
				value = fmt.Sprintf("value := signal.New[%s](%s)\nchange := func() { value.Set(%s) }; _ = change\nreturn oracle.Emit(value.Get())", c.typ, oracleZero(c.typ), expr)
			}
		case "statement":
			value = "change := func() { " + expr + " }\nreturn <button onClick={change}>ready</button>"
			if goSource {
				value = "change := func() { " + expr + " }; _ = change\nreturn oracle.Emit(0)"
			}
		case "predicate":
			value = "return <If when={" + expr + "}><span>ready</span></If>"
			if goSource {
				value = "return oracle.When(" + expr + ")"
			}
		case "iteration":
			value = "return <Each of={" + expr + "} as=\"item\"><span>ready</span></Each>"
			if goSource {
				value = "return oracle.Each(" + expr + ")"
			}
		}
		return c.localDecl + "\n" + value
	}
	gsx := head + "//gosx:island\nfunc Counter(props Props) Node {\n" + body(c.expr, false) + "\n}\n"
	// The independent Go projection replaces node construction only. Real
	// imports, declaration scopes, generic signal arguments, return contexts
	// and handler statements are retained. Go has no ternary syntax: Choose
	// uses Go's own generic unification and requires a boolean condition.
	goHead := strings.Replace(head, "package example\n", "package example\nimport oracle \"example.test/scalaroracle\"\n", 1)
	goSource := goHead + "func Counter(props Props) any {\n" + body(c.goExpr, true) + "\n}\n"
	return gsx, goSource
}
