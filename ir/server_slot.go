package ir

import (
	"fmt"
	"m31labs.dev/gosx/island/program"
)

func (l *islandLowerer) lowerServerSlot(src *Node, context *islandInlineContext) (program.Node, error) {
	n := program.Node{Kind: program.NodeServerSlot, Tag: "div"}
	if len(src.Children) != 0 || len(src.Slots) != 0 {
		return n, fmt.Errorf("ServerSlot must be self-closing without children")
	}
	for _, a := range src.Attrs {
		if a.Name == "name" {
			if a.Kind != AttrStatic || n.Text != "" {
				return n, fmt.Errorf("ServerSlot requires exactly one literal name")
			}
			n.Text = a.Value
			continue
		}
		lowered, err := l.lowerAttr(a, context)
		if err != nil {
			return n, err
		}
		n.Attrs = append(n.Attrs, lowered)
	}
	if !program.ValidServerSlotName(n.Text) {
		return n, fmt.Errorf("ServerSlot requires a literal name matching [A-Z][A-Za-z0-9_]{0,63}")
	}
	return n, nil
}
