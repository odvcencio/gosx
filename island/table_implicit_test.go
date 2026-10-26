package island

import (
	"strings"
	"testing"

	"m31labs.dev/gosx/client/vm"
)

// tableTree builds a resolved tree: node 0 is the table, the rest are its
// children in order. A child is "tr", "thead", "tbody", "tfoot" or "#text".
func tableTree(kinds ...string) *vm.ResolvedTree {
	table := vm.ResolvedNode{Tag: "table"}
	tree := &vm.ResolvedTree{Nodes: []vm.ResolvedNode{table}}
	for _, kind := range kinds {
		idx := len(tree.Nodes)
		node := vm.ResolvedNode{Tag: kind}
		if kind == "#text" {
			node = vm.ResolvedNode{Text: " "}
		}
		tree.Nodes = append(tree.Nodes, node)
		tree.Nodes[0].Children = append(tree.Nodes[0].Children, idx)
	}
	return tree
}

// TestResolvedHTMLWrapsRowsUnderTableInMarkedTbody pins the server half of the
// table patch contract. The browser's HTML parser puts a <tr> that sits
// directly under <table> inside an implicit <tbody> it creates, so the live
// DOM no longer matches the island's tree and patch paths aim at the wrong
// nodes. The renderer writes that <tbody> itself and marks it, so the client
// can skip it when it resolves a path.
func TestResolvedHTMLWrapsRowsUnderTableInMarkedTbody(t *testing.T) {
	const open = `<tbody data-gosx-implicit="">`
	cases := []struct {
		name  string
		kinds []string
		want  string
	}{
		{"rows only", []string{"tr", "tr"}, `<table>` + open + `<tr></tr><tr></tr></tbody></table>`},
		{"head then rows", []string{"thead", "tr", "tr"}, `<table><thead></thead>` + open + `<tr></tr><tr></tr></tbody></table>`},
		{"rows then foot", []string{"tr", "tfoot"}, `<table>` + open + `<tr></tr></tbody><tfoot></tfoot></table>`},
		{"head rows foot", []string{"thead", "tr", "tfoot"}, `<table><thead></thead>` + open + `<tr></tr></tbody><tfoot></tfoot></table>`},
		{"text between rows stays in the run", []string{"tr", "#text", "tr"}, `<table>` + open + `<tr></tr> <tr></tr></tbody></table>`},
		{"trailing text leaves the run", []string{"tr", "#text"}, `<table>` + open + `<tr></tr></tbody> </table>`},
		{"explicit tbody is left alone", []string{"thead", "tbody"}, `<table><thead></thead><tbody></tbody></table>`},
		{"empty table", nil, `<table></table>`},
		{"separate runs", []string{"tr", "tfoot", "tr"}, `<table>` + open + `<tr></tr></tbody><tfoot></tfoot>` + open + `<tr></tr></tbody></table>`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := RenderResolvedHTML(nil, tableTree(tc.kinds...)); got != tc.want {
				t.Fatalf("html =\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

// TestResolvedHTMLKeepsEventPathsAcrossImplicitTbody pins that the added
// <tbody> takes no index: a row's data-gosx-path stays its position in the
// island's own tree.
func TestResolvedHTMLKeepsEventPathsAcrossImplicitTbody(t *testing.T) {
	tree := tableTree("thead", "tr", "tr")
	tree.Nodes[3].Events = []vm.ResolvedEvent{{Name: "onClick", Handler: "pick"}}
	got := RenderResolvedHTML(nil, tree)
	want := `<tr data-gosx-on-click="pick" data-gosx-handler="pick" data-gosx-path="0/2"></tr>`
	if !strings.Contains(got, want) {
		t.Fatalf("html = %s, want it to contain %s", got, want)
	}
}
