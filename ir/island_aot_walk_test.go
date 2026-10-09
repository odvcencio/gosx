//go:build !tinygo

package ir_test

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"m31labs.dev/gosx/ir"
)

type oraclePosition struct {
	name, field string
}

// These are source recipes, not a list of IR types. Type discovery below
// starts at Program and follows every field, pointer, slice, array and map.
var oraclePositions = []oraclePosition{
	{"text", "Node.Text"}, {"attribute", "Attr.Expr"},
	{"signal", "SignalInfo.InitExpr"}, {"shared", "SignalInfo.InitExpr"},
	{"computed", "ComputedInfo.BodyExpr"}, {"argument", "HandlerInfo.Statements"},
	{"inline_handler", "Attr.Value"},
	{"statement", "HandlerInfo.Statements"}, {"predicate", "Attr.Expr"},
	{"iteration", "Attr.Expr"}, {"fallback", "Attr.Expr"},
	{"loop_key", "Attr.Expr"}, {"spread", "Attr.Expr"}, {"event_attribute", "Attr.Expr"},
	{"conditional_body", "Node.Children"}, {"loop_body", "Node.Children"},
	{"default_child", "Node.Children"}, {"slot_text", "Node.Slots"},
	{"slot_attribute", "Node.Slots"}, {"slot_prop", "Node.Slots"},
	{"nested_slot", "Node.Slots"}, {"prop", "Attr.Expr"},
	{"callee_text", "Component.Root"}, {"callee_attribute", "Component.Root"},
}

// A reason is required even for metadata: string-typed fields cannot be
// classified by reflection alone. No whole struct is excluded from discovery.
func oracleFieldKey(field string) string {
	return reflect.TypeFor[ir.Program]().PkgPath() + "." + field
}

func oracleFieldPolicies() map[string]string {
	groups := []struct{ fields, reason string }{
		{"Program.Package Program.PackagePath Program.Dir", "compilation identity and source directory, not expression source"},
		{"Program.Imports Import.Alias Import.Path", "import binding names and paths"},
		{"Program.aotBindings aotSourceBindings.source aotSourceBindings.project aotSourceBindings.lower", "source name-resolution evidence, not node or expression payloads"},
		{"Component.Name Component.PropsType Component.PropsName", "component and parameter binding names"},
		{"Component.PropsFields Component.PropsPaths Component.PropsSlices SlicePropSchema.Elem SlicePropSchema.Reads", "declared type evidence; scalar kinds are checked separately"},
		{"Component.PropsFormActions", "form path metadata; actions are outside the scalar profile"},
		{"Component.AcceptsChildren Component.AcceptsSlots Component.Syntax Component.PropsTyped", "component boundary shape and slot names"},
		{"Component.IsIsland Component.IsEngine Component.EngineKind Component.EngineCapabilities Component.ServerOnly Component.EngineSurface Component.SurfaceHandlers SurfaceHandlerRef.EventName SurfaceHandlerRef.FunctionName", "execution category and surface capability metadata"},
		{"ComponentScope.Locals SignalInfo.Name SignalInfo.Local SignalInfo.TypeHint SignalInfo.SourceType ComputedInfo.Name ComputedInfo.ReturnType HandlerInfo.Name", "declaration names and type/constructor evidence"},
		{"Node.Kind Node.syntheticConditional Node.Tag Node.IsStatic Node.IsIslandRoot", "node discriminants, component binding name and hydration metadata"},
		{"Attr.Kind Attr.Name Attr.IsEvent", "attribute discriminant/name and handler classification"},
		{"Component.Span Node.Span Attr.Span Span.File Span.StartLine Span.StartCol Span.EndLine Span.EndCol", "diagnostic source coordinates"},
	}
	policies := map[string]string{}
	for _, group := range groups {
		for _, field := range strings.Fields(group.fields) {
			policies[oracleFieldKey(field)] = group.reason
		}
	}
	for _, field := range strings.Fields("Program.Components Program.Nodes Component.Root Component.Scope ComponentScope.Signals ComponentScope.Computeds ComponentScope.Handlers Node.Attrs Node.Children Node.Slots Node.Text Attr.Expr Attr.Value SignalInfo.InitExpr ComputedInfo.BodyExpr HandlerInfo.Statements") {
		policies[oracleFieldKey(field)] = "visited"
	}
	return policies
}

func oracleIRFields() map[string]reflect.Type {
	fields := map[string]reflect.Type{}
	seen := map[reflect.Type]bool{}
	var discover func(reflect.Type)
	discover = func(typ reflect.Type) {
		if seen[typ] {
			return
		}
		seen[typ] = true
		switch typ.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array:
			discover(typ.Elem())
		case reflect.Map:
			discover(typ.Key())
			discover(typ.Elem())
		case reflect.Interface:
			// No expression-bearing interface is currently reachable. An
			// interface addition requires an explicit closed implementation policy.
			fields[typ.PkgPath()+"."+typ.Name()+".<implementations>"] = typ
		case reflect.Struct:
			for i := 0; i < typ.NumField(); i++ {
				field := typ.Field(i)
				fields[typ.PkgPath()+"."+typ.Name()+"."+field.Name] = field.Type
				discover(field.Type)
			}
		}
	}
	discover(reflect.TypeFor[ir.Program]())
	return fields
}

func TestIslandAOTSourceTraversalFieldCoverage(t *testing.T) {
	fields, policies := oracleIRFields(), oracleFieldPolicies()
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	slices.Sort(names)
	// Every discovered field must have a behavioral probe or an explicit
	// non-expression reason. A new struct nested in a slice/map is discovered
	// automatically, and each of its fields starts without a policy.
	for _, name := range names {
		if policies[name] == "" {
			t.Errorf("unclassified reachable IR field %s (%v)", name, fields[name])
		}
	}
	for name := range policies {
		if _, exists := fields[name]; !exists {
			t.Errorf("stale field policy %s", name)
		}
	}
	positions := map[string]bool{}
	for _, position := range oraclePositions {
		if policies[oracleFieldKey(position.field)] != "visited" {
			t.Errorf("source position %s has no visited field policy: %s", position.name, position.field)
		}
		positions[oracleFieldKey(position.field)] = true
	}
	// Containers are measured below. Every direct source payload must also
	// have its own poison probe, rather than borrowing container coverage.
	for _, name := range names {
		typ := fields[name]
		if typ.Kind() == reflect.Slice {
			typ = typ.Elem()
		}
		if policies[name] == "visited" && typ.Kind() == reflect.String && !positions[name] {
			t.Errorf("expression-bearing field has no direct source probe: %s", name)
		}
	}
	covered := map[string]bool{}
	for _, position := range oraclePositions {
		t.Run(position.name, func(t *testing.T) {
			c := scalarOracleCase{root: position.name, typ: "bool", expr: "true", goExpr: "true", packageDecl: `const true = "yes"`}
			source, _ := c.sources()
			p, err := parseAOT(t, []byte(source))
			if err != nil {
				t.Fatalf("coverage recipe does not reach admission: %v\n%s", err, source)
			}
			p.PackagePath = "example/components"
			_, err = ir.LowerIslandAOT(p, 0)
			if err == nil {
				t.Fatalf("source walker skipped %s in %s: %v", position.field, position.name, err)
			}
			// Derive container coverage from populated reflected values, rather
			// than trusting a hand-written list of parent container fields.
			oracleMarkSourceContainers(reflect.ValueOf(p), policies, covered)
		})
	}
	for _, name := range names {
		if policies[name] == "visited" && !covered[name] {
			t.Errorf("visited field has no populated behavioral coverage: %s", name)
		}
	}
	t.Logf("reachable_fields=%d source_or_node_fields=%d positions=%d", len(fields), len(covered), len(oraclePositions))
}

func oracleMarkSourceContainers(value reflect.Value, policies map[string]string, covered map[string]bool) {
	if !value.IsValid() {
		return
	}
	switch value.Kind() {
	case reflect.Pointer:
		if !value.IsNil() {
			oracleMarkSourceContainers(value.Elem(), policies, covered)
		}
	case reflect.Map:
		for _, key := range value.MapKeys() {
			oracleMarkSourceContainers(value.MapIndex(key), policies, covered)
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < value.Len(); i++ {
			oracleMarkSourceContainers(value.Index(i), policies, covered)
		}
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			field := value.Type().Field(i)
			name, child := value.Type().PkgPath()+"."+value.Type().Name()+"."+field.Name, value.Field(i)
			if policies[name] == "visited" && !child.IsZero() {
				covered[name] = true
			}
			if field.IsExported() {
				oracleMarkSourceContainers(child, policies, covered)
			}
		}
	}
}

func TestIslandAOTNamedSlotGraphGuards(t *testing.T) {
	for _, mode := range []string{"cycle", "index", "depth", "order"} {
		t.Run(mode, func(t *testing.T) {
			c := scalarOracleCase{root: "slot_text", typ: "int", expr: "1", goExpr: "1"}
			source, _ := c.sources()
			p, err := parseAOT(t, []byte(source))
			if err != nil {
				t.Fatal(err)
			}
			p.PackagePath = "example/components"
			var call ir.NodeID
			for id, node := range p.Nodes {
				if len(node.Slots) > 0 {
					call = ir.NodeID(id)
					break
				}
			}
			child := call
			switch mode {
			case "index":
				child = ir.NodeID(len(p.Nodes))
			case "depth":
				child = p.AddNode(ir.Node{Kind: ir.NodeText, Text: "ready"})
				for i := 0; i < 65; i++ {
					child = p.AddNode(ir.Node{Kind: ir.NodeFragment, Children: []ir.NodeID{child}})
				}
			case "order":
				child = ir.NodeID(len(p.Nodes) + 100)
				p.Nodes[call].Slots["A"] = child
				child = p.AddNode(ir.Node{Kind: ir.NodeFragment, Children: []ir.NodeID{call}})
			}
			p.Nodes[call].Slots["Title"] = child
			_, err = ir.LowerIslandAOT(p, 0)
			want := "invalid source node graph"
			if mode == "order" {
				want = fmt.Sprintf("invalid source node graph at %d", p.Nodes[call].Slots["A"])
			}
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatal(fmt.Sprintf("slot %s guard: %v; want %s", mode, err, want))
			}
		})
	}
}
