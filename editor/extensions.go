package editor

import (
	"strings"

	"m31labs.dev/gosx"
)

// Extension contributes browser assets and optional toolbar commands to an
// editor instance. Extension behavior stays in the browser asset and listens
// for GoSX editor lifecycle/command events; the editor package only owns the
// stable contribution contract.
type Extension struct {
	ID            string
	StylesheetURL string
	ScriptURL     string
	Toolbar       Toolbar
}

func cloneExtensions(src []Extension) []Extension {
	if src == nil {
		return nil
	}
	dst := make([]Extension, 0, len(src))
	for _, extension := range src {
		extension.Toolbar = cloneToolbar(extension.Toolbar)
		dst = append(dst, extension)
	}
	return dst
}

func (e *Editor) extensionIDs() string {
	ids := make([]string, 0, len(e.Options.Extensions))
	for _, extension := range e.Options.Extensions {
		if id := strings.TrimSpace(extension.ID); id != "" {
			ids = append(ids, id)
		}
	}
	return strings.Join(ids, ",")
}

type extensionToolbarGroup struct {
	id    string
	items []ToolbarItem
}

func (e *Editor) extensionToolbarGroups() []extensionToolbarGroup {
	groups := make([]extensionToolbarGroup, 0, len(e.Options.Extensions))
	for _, extension := range e.Options.Extensions {
		id := strings.TrimSpace(extension.ID)
		if id == "" || len(extension.Toolbar.Items) == 0 {
			continue
		}
		items := make([]ToolbarItem, 0, len(extension.Toolbar.Items))
		for _, item := range extension.Toolbar.Items {
			if strings.TrimSpace(string(item.Command)) == "" || strings.TrimSpace(item.Label) == "" {
				continue
			}
			items = append(items, item)
		}
		if len(items) > 0 {
			groups = append(groups, extensionToolbarGroup{id: id, items: items})
		}
	}
	return groups
}

func (e *Editor) renderToolbarButtonsForExtension(items []ToolbarItem, extensionID string) []gosx.Node {
	nodes := make([]gosx.Node, 0, len(items))
	for _, item := range items {
		attrs := gosx.Attrs(
			gosx.Attr("type", "button"),
			gosx.Attr("data-command", string(item.Command)),
			gosx.Attr("title", item.Label),
			gosx.Attr("aria-label", item.Label),
		)
		if extensionID != "" {
			attrs = append(attrs, gosx.Attr("data-gosx-extension", extensionID))
		}
		if e.Options.ReadOnly {
			attrs = append(attrs, gosx.BoolAttr("disabled"))
		}
		nodes = append(nodes, gosx.El("button", attrs, gosx.Text(toolbarButtonLabel(item.Command, item.Label))))
	}
	return nodes
}
