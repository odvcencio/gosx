package playground

import (
	"fmt"
	"strings"

	clientvm "m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/island/program"
)

// PlaygroundPreviewTree is an inert, structured initial DOM snapshot. The
// editor builds it with DOM construction APIs; it is never parsed as HTML.
type PlaygroundPreviewTree struct {
	Nodes []PlaygroundPreviewNode `json:"nodes"`
}

type PlaygroundPreviewNode struct {
	Tag      string                  `json:"tag,omitempty"`
	Text     string                  `json:"text,omitempty"`
	Attrs    []PlaygroundPreviewAttr `json:"attrs,omitempty"`
	Children []int                   `json:"children,omitempty"`
}

type PlaygroundPreviewAttr struct {
	Name  string `json:"name"`
	Value string `json:"value,omitempty"`
	Bool  bool   `json:"bool,omitempty"`
}

// The public playground intentionally supports a conservative HTML subset.
// Excluding active/resource-bearing elements means both the initial snapshot
// and later VM create/replace operations remain inert even without a page CSP.
var playgroundAllowedTags = map[string]struct{}{
	"a": {}, "article": {}, "aside": {}, "b": {}, "blockquote": {}, "br": {},
	"button": {}, "caption": {}, "code": {}, "dd": {}, "details": {},
	"div": {}, "dl": {}, "dt": {}, "em": {}, "fieldset": {},
	"figcaption": {}, "figure": {}, "footer": {}, "form": {},
	"h1": {}, "h2": {}, "h3": {}, "h4": {}, "h5": {}, "h6": {},
	"header": {}, "hr": {}, "i": {}, "input": {}, "label": {},
	"legend": {}, "li": {}, "main": {}, "mark": {}, "meter": {},
	"nav": {}, "ol": {}, "option": {}, "output": {}, "p": {},
	"pre": {}, "progress": {}, "s": {}, "section": {}, "select": {},
	"small": {}, "span": {}, "strong": {}, "sub": {}, "summary": {},
	"sup": {}, "table": {}, "tbody": {}, "td": {}, "textarea": {},
	"tfoot": {}, "th": {}, "thead": {}, "tr": {}, "u": {}, "ul": {},
}

var playgroundAllowedAttrs = map[string]struct{}{
	"aria-atomic": {}, "aria-busy": {}, "aria-controls": {},
	"aria-current": {}, "aria-describedby": {}, "aria-details": {},
	"aria-disabled": {}, "aria-expanded": {}, "aria-haspopup": {},
	"aria-hidden": {}, "aria-label": {}, "aria-labelledby": {},
	"aria-live": {}, "aria-pressed": {}, "aria-selected": {},
	"aria-valuemax": {}, "aria-valuemin": {}, "aria-valuenow": {},
	"aria-valuetext": {}, "autocomplete": {}, "checked": {},
	"class": {}, "cols": {}, "colspan": {}, "dir": {}, "disabled": {},
	"for": {}, "hidden": {}, "inputmode": {}, "lang": {}, "max": {},
	"maxlength": {}, "min": {}, "minlength": {}, "multiple": {},
	"name": {}, "open": {}, "pattern": {}, "placeholder": {},
	"readonly": {}, "required": {}, "role": {}, "rows": {},
	"rowspan": {}, "selected": {}, "size": {}, "step": {}, "style": {},
	"tabindex": {}, "title": {}, "type": {}, "value": {}, "wrap": {},
}

var playgroundURLAttrs = map[string]struct{}{
	"action": {}, "cite": {}, "formaction": {}, "href": {}, "poster": {},
	"src": {}, "xlink:href": {},
}

var playgroundAllowedEvents = map[string]struct{}{
	"blur": {}, "change": {}, "click": {}, "dragend": {},
	"dragleave": {}, "dragover": {}, "dragstart": {}, "drop": {},
	"document-keydown": {}, "document-keyup": {}, "focus": {},
	"input": {}, "keydown": {}, "keyup": {},
	"pointercancel": {}, "pointerdown": {}, "pointermove": {},
	"pointerup": {}, "submit": {}, "window-resize": {},
}

// validatePlaygroundProgram is the authority for every DOM mutation the VM can
// later request. Tags and attribute names are static program metadata even
// when their values are reactive, so checking the encoded program once covers
// initial construction plus create/replace/set-attribute patches.
func validatePlaygroundProgram(prog *program.Program) error {
	if prog == nil {
		return fmt.Errorf("playground: missing island program")
	}
	if int(prog.Root) >= len(prog.Nodes) || prog.Nodes[prog.Root].Kind != program.NodeElement {
		return fmt.Errorf("playground: island root must be one HTML element")
	}
	for _, node := range prog.Nodes {
		if node.Kind != program.NodeElement {
			continue
		}
		tag := strings.ToLower(strings.TrimSpace(node.Tag))
		if _, ok := playgroundAllowedTags[tag]; !ok {
			return fmt.Errorf("playground: element <%s> is not allowed", node.Tag)
		}
		for _, attr := range node.Attrs {
			if attr.Kind == program.AttrEvent {
				eventType := playgroundEventType(attr.Name)
				if _, ok := playgroundAllowedEvents[eventType]; !ok {
					return fmt.Errorf("playground: event %q is not allowed on <%s>", attr.Name, node.Tag)
				}
				continue
			}
			name := strings.ToLower(strings.TrimSpace(attr.Name))
			if strings.HasPrefix(name, "on") || name == "srcdoc" || name == "is" {
				return fmt.Errorf("playground: attribute %q is not allowed on <%s>", attr.Name, node.Tag)
			}
			if _, isURL := playgroundURLAttrs[name]; isURL {
				if attr.Kind != program.AttrStatic || !playgroundStaticURLAllowed(attr.Value) {
					return fmt.Errorf("playground: attribute %q requires a safe static URL", attr.Name)
				}
				continue
			}
			if !playgroundAttributeAllowed(name) {
				return fmt.Errorf("playground: attribute %q is not allowed on <%s>", attr.Name, node.Tag)
			}
		}
	}
	return nil
}

func playgroundStaticURLAllowed(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "./") || strings.HasPrefix(trimmed, "../") || strings.HasPrefix(trimmed, "?") {
		return true
	}
	lower := strings.ToLower(trimmed)
	return strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "mailto:") || strings.HasPrefix(lower, "tel:")
}

func playgroundAttributeAllowed(name string) bool {
	if _, ok := playgroundAllowedAttrs[name]; ok {
		return true
	}
	// Application data is inert and is forwarded in event payloads. Framework
	// data-gosx-* metadata is reserved so authored nodes cannot create nested
	// island boundaries or forge handler/path markers.
	return strings.HasPrefix(name, "data-") && !strings.HasPrefix(name, "data-gosx-")
}

func playgroundEventType(name string) string {
	switch name {
	case "onClick":
		return "click"
	case "onInput":
		return "input"
	case "onChange":
		return "change"
	case "onSubmit":
		return "submit"
	case "onKeyDown":
		return "keydown"
	case "onKeyUp":
		return "keyup"
	case "onFocus":
		return "focus"
	case "onBlur":
		return "blur"
	case "onDragStart":
		return "dragstart"
	case "onDragEnd":
		return "dragend"
	case "onDragOver":
		return "dragover"
	case "onDragLeave":
		return "dragleave"
	case "onDrop":
		return "drop"
	case "onPointerDown":
		return "pointerdown"
	case "onPointerMove":
		return "pointermove"
	case "onPointerUp":
		return "pointerup"
	case "onPointerCancel":
		return "pointercancel"
	case "onDocumentKeyDown":
		return "document-keydown"
	case "onDocumentKeyUp":
		return "document-keyup"
	case "onWindowResize":
		return "window-resize"
	default:
		return strings.ToLower(strings.TrimSpace(name))
	}
}

func makePlaygroundPreview(resolved *clientvm.ResolvedTree) PlaygroundPreviewTree {
	if resolved == nil || len(resolved.Nodes) == 0 {
		return PlaygroundPreviewTree{}
	}
	preview := PlaygroundPreviewTree{Nodes: make([]PlaygroundPreviewNode, len(resolved.Nodes))}
	visited := make(map[int]struct{}, len(resolved.Nodes))
	var copyNode func(int, string)
	copyNode = func(index int, path string) {
		if index < 0 || index >= len(resolved.Nodes) {
			return
		}
		if _, ok := visited[index]; ok {
			return
		}
		visited[index] = struct{}{}
		source := &resolved.Nodes[index]
		target := &preview.Nodes[index]
		target.Tag = source.Tag
		target.Text = source.Text
		target.Children = append(target.Children, source.Children...)

		for _, attr := range source.Attrs {
			target.Attrs = append(target.Attrs, PlaygroundPreviewAttr{
				Name:  attr.Name,
				Value: attr.Value,
				Bool:  attr.Bool,
			})
		}
		if len(source.Events) > 0 {
			for _, event := range source.Events {
				eventType := playgroundEventType(event.Name)
				target.Attrs = append(target.Attrs, PlaygroundPreviewAttr{
					Name:  "data-gosx-on-" + eventType,
					Value: event.Handler,
				})
				if eventType == "click" {
					target.Attrs = append(target.Attrs, PlaygroundPreviewAttr{
						Name:  "data-gosx-handler",
						Value: event.Handler,
					})
				}
			}
			target.Attrs = append(target.Attrs, PlaygroundPreviewAttr{
				Name:  "data-gosx-path",
				Value: path,
			})
		}
		for childIndex, child := range source.Children {
			copyNode(child, path+"/"+fmt.Sprint(childIndex))
		}
	}
	copyNode(0, "0")
	return preview
}
