package transpile

import (
	"github.com/odvcencio/gotreesitter"
	"m31labs.dev/gosx/internal/strictcomponent"
	"strconv"
	"strings"
)

func (t *transpiler) emitServerSlot(n *gotreesitter.Node) string {
	var name string
	var attrs []string
	for i := 0; i < int(n.NamedChildCount()); i++ {
		a := n.NamedChild(i)
		if t.nodeType(a) != "jsx_attribute" {
			continue
		}
		key := t.text(t.childByField(a, "name"))
		if key == "name" {
			value, _, _ := t.emitAttrValue(a)
			name, _ = strconv.Unquote(value)
			continue
		}
		if key == "hidden" {
			continue
		} // visible SSR fallback until adoption
		attrs = append(attrs, t.emitAttrWithMode(a, false, true))
	}
	attrs = append(attrs, t.gosxRef("Attr")+`("data-gosx-server-slot", `+strconv.Quote(name)+`)`)
	return t.emitElementCall("div", attrs, []string{strings.TrimSpace(strictcomponent.SlotBindingName(name))})
}
