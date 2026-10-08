package aot

import (
	"fmt"
	"sort"
	"strconv"

	"m31labs.dev/gosx/island/program"
)

// NoBindingName identifies text bindings, which have neither a tag nor an
// attribute. All other name IDs index the corresponding byte-sorted table.
const NoBindingName = ^uint32(0)

// BindingSet describes fixed physical DOM nodes. SourceNodes preserve the
// source text group; Path addresses the one browser node that group resolves.
// Attributes retain declaration order. Events refer to program-local handlers.
type BindingSet struct {
	Tags       []string  `json:"tags"`
	Attributes []string  `json:"attributes"`
	Bindings   []Binding `json:"bindings"`
}

type Binding struct {
	ID          uint32           `json:"id"`
	Path        string           `json:"path"`
	Kind        program.NodeKind `json:"kind"`
	TagID       uint32           `json:"tagId"`
	SourceNodes []program.NodeID `json:"sourceNodes"`
	Attributes  []uint32         `json:"attributes"`
	Events      []BindingEvent   `json:"events"`
}

type BindingEvent struct {
	Type    string `json:"type"`
	Handler uint32 `json:"handler"`
}

// BuildBindings admits the complete proved unit before deriving paths. It
// uses physical preorder and the same adjacent-text grouping as the VM.
func BuildBindings(u Unit) (BindingSet, error) {
	if r := Classify(u, ScalarDOMV1); !r.Eligible {
		return BindingSet{}, fmt.Errorf("binding profile: %s[%d]: %s", r.Table, r.Index, r.Reason)
	}
	out := BindingSet{Bindings: []Binding{}}
	tags, attrs := map[string]bool{}, map[string]bool{}
	for _, binding := range u.Contract.Bindings {
		if binding.Kind == program.NodeElement {
			tags[binding.Tag] = true
		}
		for _, name := range binding.Attributes {
			attrs[name] = true
		}
	}
	out.Tags, out.Attributes = sortedBindingNames(tags), sortedBindingNames(attrs)
	tagIDs, attrIDs := bindingNameIDs(out.Tags), bindingNameIDs(out.Attributes)
	handlerIDs := map[string]uint32{}
	for i, handler := range u.Program.Handlers {
		handlerIDs[handler.Name] = uint32(i)
	}
	owners := make([]uint32, len(u.Program.Nodes))
	for _, source := range u.Contract.Bindings {
		binding := Binding{ID: source.ID, Kind: source.Kind, TagID: NoBindingName,
			SourceNodes: append([]program.NodeID(nil), source.Nodes...), Attributes: []uint32{}, Events: []BindingEvent{}}
		for _, id := range source.Nodes {
			owners[id] = source.ID
		}
		if source.Kind == program.NodeElement {
			binding.TagID = tagIDs[source.Tag]
			for _, name := range source.Attributes {
				binding.Attributes = append(binding.Attributes, attrIDs[name])
			}
			for _, attr := range u.Program.Nodes[source.Nodes[0]].Attrs {
				if attr.Kind == program.AttrEvent {
					binding.Events = append(binding.Events, BindingEvent{Type: bindingEventType(attr.Name), Handler: handlerIDs[attr.Event]})
				}
			}
		}
		out.Bindings = append(out.Bindings, binding)
	}
	var walk func(program.NodeID, string)
	walk = func(id program.NodeID, path string) {
		out.Bindings[owners[id]].Path = path
		children := u.Program.Nodes[id].Children
		for i, physical := 0, 0; i < len(children); physical++ {
			child := children[i]
			childPath := strconv.Itoa(physical)
			if path != "" {
				childPath = path + "/" + childPath
			}
			walk(child, childPath)
			owner := owners[child]
			for i < len(children) && owners[children[i]] == owner {
				i++
			}
		}
	}
	walk(u.Program.Root, "")
	return out, nil
}

func sortedBindingNames(names map[string]bool) []string {
	out := make([]string, 0, len(names))
	for name := range names {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func bindingNameIDs(names []string) map[string]uint32 {
	ids := make(map[string]uint32, len(names))
	for i, name := range names {
		ids[name] = uint32(i)
	}
	return ids
}

func bindingEventType(name string) string {
	switch name {
	case "onClick":
		return "click"
	case "onInput":
		return "input"
	case "onChange":
		return "change"
	case "onKeyDown":
		return "keydown"
	case "onKeyUp":
		return "keyup"
	case "onFocus":
		return "focus"
	case "onBlur":
		return "blur"
	default:
		return name
	}
}
