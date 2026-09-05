package transpile

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/odvcencio/gotreesitter"
	"m31labs.dev/gosx/internal/strictcomponent"
	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/island/program"
)

func (t *transpiler) prepareRuntimeIslands(p *ir.Program) error {
	t.runtimePrefix = "__gosxRuntime"
	for strings.Contains(string(t.src), t.runtimePrefix) {
		t.runtimePrefix += "_"
	}
	t.runtimeNames = make(map[string]string)
	t.runtimePrograms = make(map[string]string)
	for i, c := range p.Components {
		if c.Syntax != ir.ComponentSyntaxStrict {
			continue
		}
		if c.IsEngine {
			return fmt.Errorf("RuntimeIslands: engine entries are not supported")
		}
		t.runtimeNames[c.Name] = t.runtimePrefix + c.Name
		if !c.IsIsland {
			continue
		}
		lowered, err := ir.LowerIsland(p, i)
		if err != nil {
			return err
		}
		data, err := program.EncodeJSON(lowered)
		if err != nil {
			return err
		}
		t.runtimePrograms[c.Name] = string(data)
	}
	if len(t.runtimeNames) == 0 {
		t.runtimeNames = nil
	}
	return nil
}

func (t *transpiler) emitRuntimeSourceFile(n *gotreesitter.Node) string {
	var pkg string
	var decls []string
	for i := 0; i < int(n.NamedChildCount()); i++ {
		child := n.NamedChild(i)
		switch t.nodeType(child) {
		case "package_clause":
			pkg = t.text(child)
		case "import_declaration":
		default:
			decls = append(decls, t.emit(child))
		}
	}
	body := strings.Join(decls, "\n")
	imports := t.strictProjectionImports(n, body)
	if t.injectGosx {
		imports = append(imports, projectionImport{alias: t.gosxAlias, path: "m31labs.dev/gosx"})
	}
	imports = append(imports, projectionImport{alias: t.runtimePrefix + "server", path: "m31labs.dev/gosx/server"})
	if len(t.runtimePrograms) > 0 {
		imports = append(imports, projectionImport{alias: t.runtimePrefix + "island", path: "m31labs.dev/gosx/island"})
	}
	sortProjectionImports(imports)
	var b strings.Builder
	b.WriteString(pkg + "\nimport (\n")
	for _, imp := range imports {
		fmt.Fprintf(&b, "%s %q\n", imp.alias, imp.path)
	}
	b.WriteString(")\n" + body)
	return b.String()
}

func (t *transpiler) emitRuntimeComponent(n *gotreesitter.Node, params, body string) string {
	name := t.text(t.childByField(n, "name"))
	ctx := t.runtimePrefix + "ctx"
	rt := t.runtimePrefix + "rt"
	server := t.runtimePrefix + "server"
	args := []string{ctx}
	props := "struct{}{}"
	if pn := t.childByField(n, "parameters"); pn != nil {
		for i := 0; i < int(pn.NamedChildCount()); i++ {
			if param := pn.NamedChild(i); t.nodeType(param) == "gosx_component_parameter" {
				if v := t.childByField(param, "name"); v != nil {
					props = t.text(v)
					args = append(args, props)
				}
			}
		}
	}
	slots := append(t.componentDeclaredSlots(t.childByField(n, "body")), t.serverSlotNames[name]...)
	var bindings []string
	for _, slot := range slots {
		binding := strictcomponent.SlotBindingName(slot)
		args = append(args, binding)
		bindings = append(bindings, strconv.Quote(slot)+":"+binding)
	}
	args = append(args, "children...")
	if data, ok := t.runtimePrograms[name]; ok {
		body = "{ return " + ctx + ".IslandJSON(" + strconv.Quote(data) + ", " + props + ", " + t.runtimePrefix + "island.ServerSlots{" + strings.Join(bindings, ",") + "}) }"
	}
	private := "func " + t.runtimeNames[name] + "(" + ctx + " *" + server + ".IslandRenderContext, " + params + ") " + t.gosxRef("Node") + " " + body
	public := "func " + name + "(" + rt + " *" + server + ".PageRuntime, " + params + ") (" + t.gosxRef("Node") + ", error) { return " + rt + ".RenderIslandEntry(func(" + ctx + " *" + server + ".IslandRenderContext) " + t.gosxRef("Node") + " { return " + t.runtimeNames[name] + "(" + strings.Join(args, ", ") + ") }) }"
	return private + "\n" + public
}
