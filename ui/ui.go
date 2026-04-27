// Package ui provides GoSX UI, a small Go-native component library.
package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/components"
)

const packageName = "github.com/odvcencio/gosx/ui"

// BaseProps are shared by GoSX UI components.
type BaseProps struct {
	ID    string
	Class string
	Attrs gosx.AttrList
}

// ButtonProps configures Button.
type ButtonProps struct {
	BaseProps
	Type     string
	Href     string
	Variant  string
	Size     string
	Disabled bool
}

// TextProps configures Text and Heading.
type TextProps struct {
	BaseProps
	As     string
	Tone   string
	Size   string
	Weight string
}

// LayoutProps configures layout primitives such as Box, Stack, Inline, and Grid.
type LayoutProps struct {
	BaseProps
	As      string
	Gap     string
	Align   string
	Justify string
}

// CardProps configures Card.
type CardProps struct {
	BaseProps
	Tone string
}

// BadgeProps configures Badge.
type BadgeProps struct {
	BaseProps
	Tone string
}

// FieldProps configures Field.
type FieldProps struct {
	BaseProps
	ID       string
	Label    string
	Help     string
	Error    string
	Required bool
}

// InputProps configures Input.
type InputProps struct {
	BaseProps
	ID          string
	Name        string
	Type        string
	Value       string
	Placeholder string
	Required    bool
	Disabled    bool
}

// TextareaProps configures Textarea.
type TextareaProps struct {
	InputProps
	Rows int
}

// Option configures a Select option.
type Option struct {
	Value    string
	Label    string
	Selected bool
	Disabled bool
}

// SelectProps configures Select.
type SelectProps struct {
	InputProps
	Options []Option
}

// CheckboxProps configures Checkbox.
type CheckboxProps struct {
	BaseProps
	ID       string
	Name     string
	Value    string
	Label    string
	Checked  bool
	Disabled bool
}

// TabsProps configures Tabs.
type TabsProps struct {
	BaseProps
	Active string
	Items  []TabItem
}

// TabItem is one tab trigger.
type TabItem struct {
	ID       string
	Label    string
	Href     string
	Active   bool
	Disabled bool
}

// TableProps configures Table.
type TableProps struct {
	BaseProps
	Columns []string
	Rows    [][]gosx.Node
	Empty   gosx.Node
}

// Box renders a generic layout container.
func Box(props LayoutProps, children ...gosx.Node) gosx.Node {
	tag := elementTag(props.As, "div")
	return gosx.El(tag, nodeArgs(baseAttrs(props.BaseProps, "gosx-ui-box", gapClass(props.Gap), alignClass(props.Align), justifyClass(props.Justify)), children...)...)
}

// Stack renders vertical layout.
func Stack(props LayoutProps, children ...gosx.Node) gosx.Node {
	tag := elementTag(props.As, "div")
	return gosx.El(tag, nodeArgs(baseAttrs(props.BaseProps, "gosx-ui-stack", gapClass(props.Gap), alignClass(props.Align), justifyClass(props.Justify)), children...)...)
}

// Inline renders horizontal wrapping layout.
func Inline(props LayoutProps, children ...gosx.Node) gosx.Node {
	tag := elementTag(props.As, "div")
	return gosx.El(tag, nodeArgs(baseAttrs(props.BaseProps, "gosx-ui-inline", gapClass(props.Gap), alignClass(props.Align), justifyClass(props.Justify)), children...)...)
}

// Grid renders a responsive grid container.
func Grid(props LayoutProps, children ...gosx.Node) gosx.Node {
	tag := elementTag(props.As, "div")
	return gosx.El(tag, nodeArgs(baseAttrs(props.BaseProps, "gosx-ui-grid", gapClass(props.Gap), alignClass(props.Align), justifyClass(props.Justify)), children...)...)
}

// Text renders inline or block text with UI tone classes.
func Text(props TextProps, children ...gosx.Node) gosx.Node {
	tag := elementTag(props.As, "p")
	return gosx.El(tag, nodeArgs(baseAttrs(props.BaseProps, "gosx-ui-text", toneClass(props.Tone), textSizeClass(props.Size), weightClass(props.Weight)), children...)...)
}

// Heading renders an h1-h6 heading.
func Heading(level int, props TextProps, children ...gosx.Node) gosx.Node {
	if level < 1 || level > 6 {
		level = 2
	}
	props.As = fmt.Sprintf("h%d", level)
	return Text(props, children...)
}

// Button renders a button or link-styled button.
func Button(props ButtonProps, children ...gosx.Node) gosx.Node {
	attrs := baseAttrs(props.BaseProps, "gosx-ui-button", variantClass("button", props.Variant), sizeClass("button", props.Size))
	if props.Href != "" {
		attrs = append(attrs, gosx.Attr("href", props.Href))
		if props.Disabled {
			attrs = append(attrs, gosx.Attr("aria-disabled", "true"), gosx.Attr("tabindex", "-1"))
		}
		return gosx.El("a", nodeArgs(attrs, children...)...)
	}
	typ := strings.TrimSpace(props.Type)
	if typ == "" {
		typ = "button"
	}
	attrs = append(attrs, gosx.Attr("type", typ))
	if props.Disabled {
		attrs = append(attrs, gosx.BoolAttr("disabled"))
	}
	return gosx.El("button", nodeArgs(attrs, children...)...)
}

// Card renders a framed content panel.
func Card(props CardProps, children ...gosx.Node) gosx.Node {
	return gosx.El("section", nodeArgs(baseAttrs(props.BaseProps, "gosx-ui-card", toneClass(props.Tone)), children...)...)
}

// CardHeader renders a card header region.
func CardHeader(props BaseProps, children ...gosx.Node) gosx.Node {
	return gosx.El("header", nodeArgs(baseAttrs(props, "gosx-ui-card-header"), children...)...)
}

// CardTitle renders a card title.
func CardTitle(props TextProps, children ...gosx.Node) gosx.Node {
	props.As = elementTag(props.As, "h3")
	props.Weight = firstNonEmpty(props.Weight, "semibold")
	return gosx.El(props.As, nodeArgs(baseAttrs(props.BaseProps, "gosx-ui-card-title", textSizeClass(props.Size), weightClass(props.Weight)), children...)...)
}

// CardContent renders card body content.
func CardContent(props BaseProps, children ...gosx.Node) gosx.Node {
	return gosx.El("div", nodeArgs(baseAttrs(props, "gosx-ui-card-content"), children...)...)
}

// CardFooter renders a card footer region.
func CardFooter(props BaseProps, children ...gosx.Node) gosx.Node {
	return gosx.El("footer", nodeArgs(baseAttrs(props, "gosx-ui-card-footer"), children...)...)
}

// Badge renders a compact status label.
func Badge(props BadgeProps, children ...gosx.Node) gosx.Node {
	return gosx.El("span", nodeArgs(baseAttrs(props.BaseProps, "gosx-ui-badge", variantClass("badge", props.Tone)), children...)...)
}

// Field renders a label, one control, and help/error text.
func Field(props FieldProps, control gosx.Node) gosx.Node {
	id := strings.TrimSpace(props.ID)
	label := strings.TrimSpace(props.Label)
	children := []gosx.Node{}
	if label != "" {
		labelArgs := []any{gosx.Attrs(gosx.Attr("class", "gosx-ui-field-label"))}
		if id != "" {
			labelArgs = append(labelArgs, gosx.Attrs(gosx.Attr("for", id)))
		}
		labelChildren := []gosx.Node{gosx.Text(label)}
		if props.Required {
			labelChildren = append(labelChildren, gosx.El("span", gosx.Attrs(gosx.Attr("aria-hidden", "true")), gosx.Text(" *")))
		}
		children = append(children, gosx.El("label", append(labelArgs, gosx.Fragment(labelChildren...))...))
	}
	children = append(children, control)
	if props.Help != "" {
		children = append(children, gosx.El("p", gosx.Attrs(gosx.Attr("class", "gosx-ui-field-help")), gosx.Text(props.Help)))
	}
	if props.Error != "" {
		children = append(children, gosx.El("p", gosx.Attrs(gosx.Attr("class", "gosx-ui-field-error"), gosx.Attr("role", "alert")), gosx.Text(props.Error)))
	}
	return Stack(LayoutProps{BaseProps: props.BaseProps, Gap: "xs"}, children...)
}

// Input renders an input control.
func Input(props InputProps) gosx.Node {
	typ := strings.TrimSpace(props.Type)
	if typ == "" {
		typ = "text"
	}
	attrs := formAttrs(props, "gosx-ui-input")
	attrs = append(attrs, gosx.Attr("type", typ))
	return gosx.El("input", attrs)
}

// Textarea renders a textarea control.
func Textarea(props TextareaProps) gosx.Node {
	attrs := formAttrs(props.InputProps, "gosx-ui-textarea")
	if props.Rows > 0 {
		attrs = append(attrs, gosx.Attr("rows", props.Rows))
	}
	return gosx.El("textarea", attrs, gosx.Text(props.Value))
}

// Select renders a select control.
func Select(props SelectProps) gosx.Node {
	attrs := formAttrs(props.InputProps, "gosx-ui-select")
	options := make([]gosx.Node, 0, len(props.Options))
	for _, option := range props.Options {
		optionAttrs := gosx.Attrs(gosx.Attr("value", option.Value))
		if option.Selected {
			optionAttrs = append(optionAttrs, gosx.BoolAttr("selected"))
		}
		if option.Disabled {
			optionAttrs = append(optionAttrs, gosx.BoolAttr("disabled"))
		}
		options = append(options, gosx.El("option", optionAttrs, gosx.Text(firstNonEmpty(option.Label, option.Value))))
	}
	return gosx.El("select", append([]any{attrs}, nodesToAny(options)...)...)
}

// Checkbox renders a checkbox with an optional inline label.
func Checkbox(props CheckboxProps) gosx.Node {
	inputAttrs := baseAttrs(props.BaseProps, "gosx-ui-checkbox-input")
	appendAttrIf(&inputAttrs, "id", props.ID)
	appendAttrIf(&inputAttrs, "name", props.Name)
	appendAttrIf(&inputAttrs, "value", props.Value)
	inputAttrs = append(inputAttrs, gosx.Attr("type", "checkbox"))
	if props.Checked {
		inputAttrs = append(inputAttrs, gosx.BoolAttr("checked"))
	}
	if props.Disabled {
		inputAttrs = append(inputAttrs, gosx.BoolAttr("disabled"))
	}
	box := gosx.El("input", inputAttrs)
	if strings.TrimSpace(props.Label) == "" {
		return box
	}
	return gosx.El("label",
		gosx.Attrs(gosx.Attr("class", "gosx-ui-checkbox")),
		box,
		gosx.El("span", gosx.Text(props.Label)),
	)
}

// Tabs renders a static tab list.
func Tabs(props TabsProps) gosx.Node {
	items := make([]gosx.Node, 0, len(props.Items))
	for _, item := range props.Items {
		active := item.Active || (props.Active != "" && props.Active == item.ID)
		attrs := gosx.Attrs(
			gosx.Attr("class", gosx.Classes("gosx-ui-tab", activeClass(active))),
			gosx.Attr("role", "tab"),
			gosx.Attr("aria-selected", fmt.Sprint(active)),
		)
		if item.Disabled {
			attrs = append(attrs, gosx.Attr("aria-disabled", "true"), gosx.Attr("tabindex", "-1"))
		}
		if item.Href != "" {
			attrs = append(attrs, gosx.Attr("href", item.Href))
			items = append(items, gosx.El("a", attrs, gosx.Text(item.Label)))
			continue
		}
		attrs = append(attrs, gosx.Attr("type", "button"))
		items = append(items, gosx.El("button", attrs, gosx.Text(item.Label)))
	}
	return gosx.El("div", nodeArgs(baseAttrs(props.BaseProps, "gosx-ui-tabs"), items...)...)
}

// Table renders a simple data table.
func Table(props TableProps) gosx.Node {
	headCells := make([]gosx.Node, 0, len(props.Columns))
	for _, column := range props.Columns {
		headCells = append(headCells, gosx.El("th", gosx.Attrs(gosx.Attr("scope", "col")), gosx.Text(column)))
	}
	bodyRows := make([]gosx.Node, 0, len(props.Rows))
	for _, row := range props.Rows {
		cells := make([]gosx.Node, 0, len(row))
		for _, cell := range row {
			cells = append(cells, gosx.El("td", cell))
		}
		bodyRows = append(bodyRows, gosx.El("tr", gosx.Fragment(cells...)))
	}
	if len(bodyRows) == 0 && !props.Empty.IsZero() {
		bodyRows = append(bodyRows, gosx.El("tr", gosx.El("td", gosx.Attrs(gosx.Attr("colspan", max(1, len(props.Columns)))), props.Empty)))
	}
	return gosx.El("table",
		nodeArgs(baseAttrs(props.BaseProps, "gosx-ui-table"),
			gosx.El("thead", gosx.El("tr", gosx.Fragment(headCells...))),
			gosx.El("tbody", gosx.Fragment(bodyRows...)),
		)...,
	)
}

// Styles returns the default GoSX UI stylesheet.
func Styles() gosx.Node {
	return gosx.RawHTML(`<style data-gosx-ui>` + stylesheet + `</style>`)
}

// Registry returns a component registry populated with GoSX UI components.
func Registry() *components.Registry {
	registry := components.NewRegistry()
	for _, def := range definitions() {
		registry.MustRegister(def)
	}
	return registry
}

func definitions() []components.Definition {
	meta := func(description string, tags ...string) components.Metadata {
		return components.Metadata{Package: packageName, Description: description, Tags: tags}
	}
	return []components.Definition{
		definition("Box", meta("Generic layout container", "layout"), func(props components.Props, children ...gosx.Node) gosx.Node {
			return Box(layoutProps(props), children...)
		}),
		definition("Stack", meta("Vertical layout primitive", "layout"), func(props components.Props, children ...gosx.Node) gosx.Node {
			return Stack(layoutProps(props), children...)
		}),
		definition("Inline", meta("Horizontal wrapping layout primitive", "layout"), func(props components.Props, children ...gosx.Node) gosx.Node {
			return Inline(layoutProps(props), children...)
		}),
		definition("Grid", meta("Responsive grid layout primitive", "layout"), func(props components.Props, children ...gosx.Node) gosx.Node {
			return Grid(layoutProps(props), children...)
		}),
		definition("Text", meta("Text primitive", "typography"), func(props components.Props, children ...gosx.Node) gosx.Node {
			return Text(textProps(props), children...)
		}),
		definition("Heading", meta("Heading primitive", "typography"), func(props components.Props, children ...gosx.Node) gosx.Node {
			return Heading(intProp(props, "level", "Level"), textProps(props), children...)
		}),
		definition("Button", meta("Button or link button control", "control"), func(props components.Props, children ...gosx.Node) gosx.Node {
			return Button(buttonProps(props), children...)
		}),
		definition("Card", meta("Framed content panel", "surface"), func(props components.Props, children ...gosx.Node) gosx.Node {
			return Card(cardProps(props), children...)
		}),
		definition("CardHeader", meta("Card header region", "surface"), func(props components.Props, children ...gosx.Node) gosx.Node {
			return CardHeader(baseProps(props), children...)
		}),
		definition("CardTitle", meta("Card title", "surface", "typography"), func(props components.Props, children ...gosx.Node) gosx.Node {
			return CardTitle(textProps(props), children...)
		}),
		definition("CardContent", meta("Card body region", "surface"), func(props components.Props, children ...gosx.Node) gosx.Node {
			return CardContent(baseProps(props), children...)
		}),
		definition("CardFooter", meta("Card footer region", "surface"), func(props components.Props, children ...gosx.Node) gosx.Node {
			return CardFooter(baseProps(props), children...)
		}),
		definition("Badge", meta("Status badge", "status"), func(props components.Props, children ...gosx.Node) gosx.Node {
			return Badge(badgeProps(props), children...)
		}),
		definition("Field", meta("Form field wrapper", "form"), func(props components.Props, children ...gosx.Node) gosx.Node {
			return Field(fieldProps(props), gosx.Fragment(children...))
		}),
		definition("Input", meta("Text input control", "form"), func(props components.Props, children ...gosx.Node) gosx.Node {
			return Input(inputProps(props))
		}),
		definition("Textarea", meta("Textarea control", "form"), func(props components.Props, children ...gosx.Node) gosx.Node {
			return Textarea(textareaProps(props))
		}),
		definition("Select", meta("Select control", "form"), func(props components.Props, children ...gosx.Node) gosx.Node {
			return Select(selectProps(props))
		}),
		definition("Checkbox", meta("Checkbox control", "form"), func(props components.Props, children ...gosx.Node) gosx.Node {
			return Checkbox(checkboxProps(props))
		}),
		definition("Tabs", meta("Static tab list", "navigation"), func(props components.Props, children ...gosx.Node) gosx.Node {
			return Tabs(tabsProps(props))
		}),
		definition("Table", meta("Data table", "data"), func(props components.Props, children ...gosx.Node) gosx.Node {
			table := tableProps(props)
			if len(table.Rows) == 0 && len(children) > 0 {
				table.Rows = [][]gosx.Node{children}
			}
			return Table(table)
		}),
		definition("Styles", meta("Default GoSX UI stylesheet", "style"), func(props components.Props, children ...gosx.Node) gosx.Node {
			return Styles()
		}),
	}
}

func definition(name string, meta components.Metadata, render components.RenderFunc) components.Definition {
	return components.Definition{Name: name, Metadata: meta, Render: render}
}

func nodeArgs(attrs gosx.AttrList, children ...gosx.Node) []any {
	args := []any{attrs}
	for _, child := range children {
		if child.IsZero() {
			continue
		}
		args = append(args, child)
	}
	return args
}

func nodesToAny(nodes []gosx.Node) []any {
	out := make([]any, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, node)
	}
	return out
}

func baseAttrs(props BaseProps, classes ...string) gosx.AttrList {
	allClasses := append([]string{"gosx-ui"}, classes...)
	allClasses = append(allClasses, props.Class)
	attrs := gosx.Attrs(gosx.Attr("class", gosx.Classes(allClasses...)))
	appendAttrIf(&attrs, "id", props.ID)
	attrs = append(attrs, props.Attrs...)
	return attrs
}

func formAttrs(props InputProps, className string) gosx.AttrList {
	attrs := baseAttrs(props.BaseProps, className)
	appendAttrIf(&attrs, "id", props.ID)
	appendAttrIf(&attrs, "name", props.Name)
	appendAttrIf(&attrs, "value", props.Value)
	appendAttrIf(&attrs, "placeholder", props.Placeholder)
	if props.Required {
		attrs = append(attrs, gosx.BoolAttr("required"))
	}
	if props.Disabled {
		attrs = append(attrs, gosx.BoolAttr("disabled"))
	}
	return attrs
}

func appendAttrIf(attrs *gosx.AttrList, name, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	*attrs = append(*attrs, gosx.Attr(name, value))
}

func elementTag(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func activeClass(active bool) string {
	if active {
		return "is-active"
	}
	return ""
}

func variantClass(scope, value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		value = "default"
	}
	return "gosx-ui-" + scope + "-" + value
}

func sizeClass(scope, value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		value = "md"
	}
	return "gosx-ui-" + scope + "-" + value
}

func toneClass(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	return "gosx-ui-tone-" + value
}

func textSizeClass(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	return "gosx-ui-text-" + value
}

func weightClass(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	return "gosx-ui-weight-" + value
}

func gapClass(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		value = "md"
	}
	return "gosx-ui-gap-" + value
}

func alignClass(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	return "gosx-ui-align-" + value
}

func justifyClass(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	return "gosx-ui-justify-" + value
}

func baseProps(props components.Props) BaseProps {
	return BaseProps{
		ID:    stringProp(props, "id", "ID"),
		Class: stringProp(props, "class", "Class"),
	}
}

func layoutProps(props components.Props) LayoutProps {
	return LayoutProps{
		BaseProps: baseProps(props),
		As:        stringProp(props, "as", "As"),
		Gap:       stringProp(props, "gap", "Gap"),
		Align:     stringProp(props, "align", "Align"),
		Justify:   stringProp(props, "justify", "Justify"),
	}
}

func textProps(props components.Props) TextProps {
	return TextProps{
		BaseProps: baseProps(props),
		As:        stringProp(props, "as", "As"),
		Tone:      stringProp(props, "tone", "Tone"),
		Size:      stringProp(props, "size", "Size"),
		Weight:    stringProp(props, "weight", "Weight"),
	}
}

func buttonProps(props components.Props) ButtonProps {
	return ButtonProps{
		BaseProps: baseProps(props),
		Type:      stringProp(props, "type", "Type"),
		Href:      stringProp(props, "href", "Href"),
		Variant:   stringProp(props, "variant", "Variant"),
		Size:      stringProp(props, "size", "Size"),
		Disabled:  boolProp(props, "disabled", "Disabled"),
	}
}

func cardProps(props components.Props) CardProps {
	return CardProps{BaseProps: baseProps(props), Tone: stringProp(props, "tone", "Tone")}
}

func badgeProps(props components.Props) BadgeProps {
	return BadgeProps{BaseProps: baseProps(props), Tone: stringProp(props, "tone", "Tone")}
}

func fieldProps(props components.Props) FieldProps {
	return FieldProps{
		BaseProps: controlBaseProps(props),
		ID:        stringProp(props, "id", "ID"),
		Label:     stringProp(props, "label", "Label"),
		Help:      stringProp(props, "help", "Help"),
		Error:     stringProp(props, "error", "Error"),
		Required:  boolProp(props, "required", "Required"),
	}
}

func inputProps(props components.Props) InputProps {
	return InputProps{
		BaseProps:   controlBaseProps(props),
		ID:          stringProp(props, "id", "ID"),
		Name:        stringProp(props, "name", "Name"),
		Type:        stringProp(props, "type", "Type"),
		Value:       stringProp(props, "value", "Value"),
		Placeholder: stringProp(props, "placeholder", "Placeholder"),
		Required:    boolProp(props, "required", "Required"),
		Disabled:    boolProp(props, "disabled", "Disabled"),
	}
}

func textareaProps(props components.Props) TextareaProps {
	return TextareaProps{InputProps: inputProps(props), Rows: intProp(props, "rows", "Rows")}
}

func selectProps(props components.Props) SelectProps {
	return SelectProps{InputProps: inputProps(props), Options: optionProps(props)}
}

func checkboxProps(props components.Props) CheckboxProps {
	return CheckboxProps{
		BaseProps: controlBaseProps(props),
		ID:        stringProp(props, "id", "ID"),
		Name:      stringProp(props, "name", "Name"),
		Value:     stringProp(props, "value", "Value"),
		Label:     stringProp(props, "label", "Label"),
		Checked:   boolProp(props, "checked", "Checked"),
		Disabled:  boolProp(props, "disabled", "Disabled"),
	}
}

func tabsProps(props components.Props) TabsProps {
	return TabsProps{
		BaseProps: baseProps(props),
		Active:    stringProp(props, "active", "Active"),
		Items:     tabItemProps(props),
	}
}

func tableProps(props components.Props) TableProps {
	return TableProps{
		BaseProps: baseProps(props),
		Columns:   stringSliceProp(props, "columns", "Columns"),
		Rows:      nodeRowsProp(props, "rows", "Rows"),
		Empty:     nodeProp(props, "empty", "Empty"),
	}
}

func optionProps(props components.Props) []Option {
	value := propValue(props, "options", "Options")
	switch options := value.(type) {
	case []Option:
		return append([]Option(nil), options...)
	case []string:
		out := make([]Option, 0, len(options))
		for _, option := range options {
			out = append(out, Option{Value: option, Label: option})
		}
		return out
	case []map[string]any:
		out := make([]Option, 0, len(options))
		for _, option := range options {
			out = append(out, optionFromMap(option))
		}
		return out
	case []any:
		out := make([]Option, 0, len(options))
		for _, option := range options {
			switch option := option.(type) {
			case Option:
				out = append(out, option)
			case string:
				out = append(out, Option{Value: option, Label: option})
			case map[string]any:
				out = append(out, optionFromMap(option))
			case map[string]string:
				out = append(out, optionFromStringMap(option))
			}
		}
		return out
	default:
		return nil
	}
}

func optionFromMap(values map[string]any) Option {
	return Option{
		Value:    stringValue(values["value"]),
		Label:    stringValue(values["label"]),
		Selected: boolValue(values["selected"]),
		Disabled: boolValue(values["disabled"]),
	}
}

func optionFromStringMap(values map[string]string) Option {
	return Option{Value: values["value"], Label: values["label"]}
}

func tabItemProps(props components.Props) []TabItem {
	value := propValue(props, "items", "Items")
	switch items := value.(type) {
	case []TabItem:
		return append([]TabItem(nil), items...)
	case []map[string]any:
		out := make([]TabItem, 0, len(items))
		for _, item := range items {
			out = append(out, tabItemFromMap(item))
		}
		return out
	case []any:
		out := make([]TabItem, 0, len(items))
		for _, item := range items {
			switch item := item.(type) {
			case TabItem:
				out = append(out, item)
			case map[string]any:
				out = append(out, tabItemFromMap(item))
			}
		}
		return out
	default:
		return nil
	}
}

func tabItemFromMap(values map[string]any) TabItem {
	return TabItem{
		ID:       stringValue(values["id"]),
		Label:    stringValue(values["label"]),
		Href:     stringValue(values["href"]),
		Active:   boolValue(values["active"]),
		Disabled: boolValue(values["disabled"]),
	}
}

func stringProp(props components.Props, names ...string) string {
	value := propValue(props, names...)
	return stringValue(value)
}

func stringValue(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return fmt.Sprint(v)
	}
}

func boolProp(props components.Props, names ...string) bool {
	value := propValue(props, names...)
	return boolValue(value)
}

func boolValue(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(v, "true") || v == "1" || strings.EqualFold(v, "yes")
	default:
		return false
	}
}

func stringSliceProp(props components.Props, names ...string) []string {
	value := propValue(props, names...)
	switch values := value.(type) {
	case []string:
		return append([]string(nil), values...)
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			if s := stringValue(value); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		if s := stringValue(value); s != "" {
			return []string{s}
		}
		return nil
	}
}

func nodeProp(props components.Props, names ...string) gosx.Node {
	value := propValue(props, names...)
	switch node := value.(type) {
	case gosx.Node:
		return node
	case *gosx.Node:
		if node == nil {
			return gosx.Node{}
		}
		return *node
	case string:
		if node == "" {
			return gosx.Node{}
		}
		return gosx.Text(node)
	default:
		return gosx.Node{}
	}
}

func nodeRowsProp(props components.Props, names ...string) [][]gosx.Node {
	value := propValue(props, names...)
	switch rows := value.(type) {
	case [][]gosx.Node:
		out := make([][]gosx.Node, 0, len(rows))
		for _, row := range rows {
			out = append(out, append([]gosx.Node(nil), row...))
		}
		return out
	case [][]string:
		out := make([][]gosx.Node, 0, len(rows))
		for _, row := range rows {
			out = append(out, textRow(row))
		}
		return out
	case []any:
		out := make([][]gosx.Node, 0, len(rows))
		for _, row := range rows {
			switch row := row.(type) {
			case []gosx.Node:
				out = append(out, append([]gosx.Node(nil), row...))
			case []string:
				out = append(out, textRow(row))
			case []any:
				nodes := make([]gosx.Node, 0, len(row))
				for _, cell := range row {
					switch cell := cell.(type) {
					case gosx.Node:
						nodes = append(nodes, cell)
					case string:
						nodes = append(nodes, gosx.Text(cell))
					}
				}
				out = append(out, nodes)
			}
		}
		return out
	default:
		return nil
	}
}

func textRow(values []string) []gosx.Node {
	out := make([]gosx.Node, 0, len(values))
	for _, value := range values {
		out = append(out, gosx.Text(value))
	}
	return out
}

func intProp(props components.Props, names ...string) int {
	value := propValue(props, names...)
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	default:
		return 0
	}
}

func propValue(props components.Props, names ...string) any {
	for _, name := range names {
		if value, ok := props[name]; ok {
			return value
		}
	}
	return nil
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func sortedDefinitionNames(registry *components.Registry) []string {
	defs := registry.List()
	names := make([]string, 0, len(defs))
	for _, def := range defs {
		names = append(names, def.Name)
	}
	sort.Strings(names)
	return names
}

func controlBaseProps(props components.Props) BaseProps {
	base := baseProps(props)
	base.ID = ""
	return base
}
