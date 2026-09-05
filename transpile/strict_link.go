package transpile

import (
	"fmt"
	gotreesitter "github.com/odvcencio/gotreesitter"
	"strings"
)

func (t *transpiler) isStrictManagedLinkTag(tag string) bool {
	if t.strict == 0 || !t.strictServer || tag != "Link" {
		return false
	}
	if _, ok := t.strictNames[tag]; ok {
		return false
	}
	if _, ok := t.typedPropsType(tag); ok {
		return false
	}
	if _, ok := t.sharedComponent(tag); ok {
		return false
	}
	return true
}

func (t *transpiler) emitStrictManagedLink(open *gotreesitter.Node, children []string) string {
	href := ""
	var attrs []string
	for i := 0; i < int(open.NamedChildCount()); i++ {
		attr := open.NamedChild(i)
		if t.nodeType(attr) != "jsx_attribute" {
			continue
		}
		if t.suppressSlotAttr && t.isSlotRoutingAttribute(attr) {
			continue
		}
		name := t.childByField(attr, "name")
		if name == nil {
			continue
		}
		if t.text(name) == "href" {
			href, _, _ = t.emitAttrValue(attr)
			continue
		}
		if value := t.emitAttrWithMode(attr, false, true); value != "" {
			attrs = append(attrs, value)
		}
	}
	args := []string{href, fmt.Sprintf("%s(%s)", t.gosxRef("Attrs"), strings.Join(attrs, ", "))}
	args = append(args, children...)
	return fmt.Sprintf("%s(%s)", t.gosxRef("ManagedLink"), strings.Join(args, ", "))
}
