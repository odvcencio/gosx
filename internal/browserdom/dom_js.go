//go:build js && wasm

package browserdom

import "syscall/js"

// Element is an opaque DOM node. Its zero value represents a missing element.
// Keep the handle inline: reading an event target or query result allocates no wrapper.
type Element struct{ value js.Value }
type Document struct{ value js.Value }
type WindowTarget struct{ value js.Value }
type MediaQuery struct{ value js.Value }
type Elements struct{ value js.Value }
type Style struct{ value js.Value }
type Classes struct{ value js.Value }
type Dataset struct{ value js.Value }

// Event reads native fields lazily; it performs no JSON or snapshot allocation.
type Event struct{ value js.Value }

// Listener owns one callback and removes it before releasing its Go function.
type Listener struct {
	target   js.Value
	event    string
	fn       js.Func
	capture  bool
	disposed bool
}

// FromJS is internal framework interop, unavailable to importing applications.
func FromJS(v js.Value) Element {
	if !v.Truthy() {
		return Element{}
	}
	return Element{value: v}
}
func CurrentDocument() Document          { return Document{js.Global().Get("document")} }
func Window() WindowTarget               { return WindowTarget{js.Global()} }
func MatchMedia(query string) MediaQuery { return MediaQuery{js.Global().Call("matchMedia", query)} }
func (e Element) Valid() bool            { return e.value.Truthy() }
func (e Element) Equal(other Element) bool {
	if !e.Valid() || !other.Valid() {
		return e.Valid() == other.Valid()
	}
	return e.value.Equal(other.value)
}
func (e Element) Query(selector string) Element {
	if !e.Valid() {
		return Element{}
	}
	return FromJS(e.value.Call("querySelector", selector))
}
func (e Element) QueryAll(selector string) Elements {
	if !e.Valid() {
		return Elements{}
	}
	return Elements{e.value.Call("querySelectorAll", selector)}
}
func (e Element) Closest(selector string) Element {
	if !e.Valid() || e.value.Get("closest").Type() != js.TypeFunction {
		return Element{}
	}
	return FromJS(e.value.Call("closest", selector))
}
func (e Element) Attr(name string) string {
	if !e.Valid() {
		return ""
	}
	v := e.value.Call("getAttribute", name)
	if v.Type() != js.TypeString {
		return ""
	}
	return v.String()
}
func (e Element) SetAttr(name, value string) {
	if e.Valid() {
		e.value.Call("setAttribute", name, value)
	}
}
func (e Element) HasAttr(name string) bool {
	return e.Valid() && e.value.Call("hasAttribute", name).Bool()
}
func (e Element) RemoveAttr(name string) {
	if e.Valid() {
		e.value.Call("removeAttribute", name)
	}
}
func (e Element) Classes() Classes {
	if !e.Valid() {
		return Classes{}
	}
	return Classes{e.value.Get("classList")}
}
func (e Element) Style() Style {
	if !e.Valid() {
		return Style{}
	}
	return Style{e.value.Get("style")}
}
func (e Element) Dataset() Dataset {
	if !e.Valid() {
		return Dataset{}
	}
	return Dataset{e.value.Get("dataset")}
}
func (e Element) Parent() Element {
	if !e.Valid() {
		return Element{}
	}
	return FromJS(e.value.Get("parentElement"))
}
func (e Element) Append(children ...Element) {
	if !e.Valid() {
		return
	}
	for _, child := range children {
		if child.Valid() {
			e.value.Call("appendChild", child.value)
		}
	}
}
func (e Element) InsertBefore(child, before Element) {
	if e.Valid() && child.Valid() {
		v := js.Null()
		if before.Valid() {
			v = before.value
		}
		e.value.Call("insertBefore", child.value, v)
	}
}
func (e Element) ReplaceMarkup(markup Markup) {
	if e.Valid() {
		e.value.Set("innerHTML", string(markup))
	}
}
func (e Element) AppendMarkup(markup Markup) {
	if e.Valid() {
		e.value.Call("insertAdjacentHTML", "beforeend", string(markup))
	}
}
func (e Element) Rect() Rect {
	if !e.Valid() {
		return Rect{}
	}
	v := e.value.Call("getBoundingClientRect")
	left, top, width, height := v.Get("left").Float(), v.Get("top").Float(), v.Get("width").Float(), v.Get("height").Float()
	return Rect{Left: left, Top: top, Right: left + width, Bottom: top + height, Width: width, Height: height}
}
func (e Element) ClientRectCount() int {
	if !e.Valid() {
		return 0
	}
	return e.value.Call("getClientRects").Length()
}
func (e Element) HasPointerCapture(id int) bool {
	return e.Valid() && e.value.Call("hasPointerCapture", id).Bool()
}
func (e Element) SetPointerCapture(id int) {
	if e.Valid() {
		e.value.Call("setPointerCapture", id)
	}
}
func (e Element) ReleasePointerCapture(id int) {
	if e.Valid() {
		e.value.Call("releasePointerCapture", id)
	}
}
func (e Element) On(name string, fn func(Event), options ...ListenerOptions) *Listener {
	if !e.Valid() {
		return &Listener{disposed: true}
	}
	return listen(e.value, name, fn, options)
}
func (d Document) On(name string, fn func(Event), options ...ListenerOptions) *Listener {
	return listen(d.value, name, fn, options)
}
func (w WindowTarget) On(name string, fn func(Event), options ...ListenerOptions) *Listener {
	return listen(w.value, name, fn, options)
}
func (m MediaQuery) On(name string, fn func(Event), options ...ListenerOptions) *Listener {
	return listen(m.value, name, fn, options)
}
func (m MediaQuery) Matches() bool { return m.value.Truthy() && m.value.Get("matches").Bool() }
func listen(target js.Value, name string, fn func(Event), options []ListenerOptions) *Listener {
	l := &Listener{target: target, event: name}
	if !target.Truthy() || fn == nil {
		l.disposed = true
		return l
	}
	o := ListenerOptions{}
	if len(options) > 0 {
		o = options[0]
	}
	l.capture = o.Capture
	l.fn = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if !l.disposed && len(args) > 0 {
			fn(Event{args[0]})
			if o.Once {
				l.Dispose()
			}
		}
		return nil
	})
	if o.Passive || o.Once {
		target.Call("addEventListener", name, l.fn, map[string]any{"capture": o.Capture, "passive": o.Passive, "once": o.Once})
	} else {
		target.Call("addEventListener", name, l.fn, o.Capture)
	}
	return l
}
func (l *Listener) Dispose() {
	if l == nil || l.disposed {
		return
	}
	l.disposed = true
	l.target.Call("removeEventListener", l.event, l.fn, l.capture)
	l.fn.Release()
}
func (e Elements) Len() int {
	if !e.value.Truthy() {
		return 0
	}
	return e.value.Length()
}
func (e Elements) At(index int) Element {
	if index < 0 || index >= e.Len() {
		return Element{}
	}
	return FromJS(e.value.Index(index))
}
func (d Document) ByID(id string) Element { return FromJS(d.value.Call("getElementById", id)) }
func (d Document) Query(selector string) Element {
	return FromJS(d.value.Call("querySelector", selector))
}
func (d Document) QueryAll(selector string) Elements {
	return Elements{d.value.Call("querySelectorAll", selector)}
}
func (d Document) Create(tag string) Element { return FromJS(d.value.Call("createElement", tag)) }
func (d Document) HasFocus() bool            { return d.value.Call("hasFocus").Bool() }
func (d Document) SetTitle(title string)     { d.value.Set("title", title) }
func (d Document) ReadyState() string        { return d.value.Get("readyState").String() }
func (d Document) Hidden() bool              { return d.value.Get("hidden").Bool() }
func (s Style) Set(name, value string) {
	if !s.value.Truthy() {
		return
	}
	if len(name) > 1 && name[:2] == "--" {
		s.value.Call("setProperty", name, value)
	} else {
		s.value.Set(name, value)
	}
}
func (s Style) Get(name string) string {
	if !s.value.Truthy() {
		return ""
	}
	if len(name) > 1 && name[:2] == "--" {
		return s.value.Call("getPropertyValue", name).String()
	}
	return s.value.Get(name).String()
}
func (s Style) Remove(name string) {
	if s.value.Truthy() {
		s.value.Call("removeProperty", name)
	}
}
func (c Classes) Add(names ...string) {
	if c.value.Truthy() {
		for _, name := range names {
			c.value.Call("add", name)
		}
	}
}
func (c Classes) Remove(names ...string) {
	if c.value.Truthy() {
		for _, name := range names {
			c.value.Call("remove", name)
		}
	}
}
func (c Classes) Toggle(name string, force ...bool) bool {
	if !c.value.Truthy() {
		return false
	}
	if len(force) > 0 {
		return c.value.Call("toggle", name, force[0]).Bool()
	}
	return c.value.Call("toggle", name).Bool()
}
func (c Classes) Contains(name string) bool {
	return c.value.Truthy() && c.value.Call("contains", name).Bool()
}
func (d Dataset) Set(name, value string) {
	if d.value.Truthy() {
		d.value.Set(name, value)
	}
}
func (d Dataset) Get(name string) string {
	if !d.value.Truthy() {
		return ""
	}
	v := d.value.Get(name)
	if v.Type() != js.TypeString {
		return ""
	}
	return v.String()
}
func (e Event) Target() Element           { return FromJS(e.value.Get("target")) }
func (e Event) CurrentTarget() Element    { return FromJS(e.value.Get("currentTarget")) }
func (e Event) RelatedTarget() Element    { return FromJS(e.value.Get("relatedTarget")) }
func (e Event) PreventDefault()           { e.value.Call("preventDefault") }
func (e Event) StopPropagation()          { e.value.Call("stopPropagation") }
func (e Event) StopImmediatePropagation() { e.value.Call("stopImmediatePropagation") }

// DetailRevision is the revision carried by framework command-applied events.
func (e Event) DetailRevision() uint64 {
	v := e.value.Get("detail")
	if !v.Truthy() {
		return 0
	}
	r := v.Get("revision")
	if r.Type() != js.TypeNumber {
		return 0
	}
	return uint64(r.Float())
}
func (e Element) ID() string {
	if !e.Valid() {
		return ""
	}
	return e.value.Get("id").String()
}
func (e Element) TagName() string {
	if !e.Valid() {
		return ""
	}
	return e.value.Get("tagName").String()
}
func (e Element) Text() string {
	if !e.Valid() {
		return ""
	}
	return e.value.Get("textContent").String()
}
func (e Element) Value() string {
	if !e.Valid() {
		return ""
	}
	return e.value.Get("value").String()
}
func (e Element) ClassName() string {
	if !e.Valid() {
		return ""
	}
	return e.value.Get("className").String()
}
func (e Element) SetText(value string) {
	if e.Valid() {
		e.value.Set("textContent", value)
	}
}
func (e Element) SetValue(value string) {
	if e.Valid() {
		e.value.Set("value", value)
	}
}
func (e Element) SetClassName(value string) {
	if e.Valid() {
		e.value.Set("className", value)
	}
}
func (e Element) Hidden() bool { return e.Valid() && e.value.Get("hidden").Truthy() }
func (e Element) SetHidden(value bool) {
	if e.Valid() {
		e.value.Set("hidden", value)
	}
}
func (e Element) Checked() bool { return e.Valid() && e.value.Get("checked").Truthy() }
func (e Element) SetChecked(value bool) {
	if e.Valid() {
		e.value.Set("checked", value)
	}
}
func (e Element) Disabled() bool { return e.Valid() && e.value.Get("disabled").Truthy() }
func (e Element) SetDisabled(value bool) {
	if e.Valid() {
		e.value.Set("disabled", value)
	}
}
func (e Element) Connected() bool { return e.Valid() && e.value.Get("isConnected").Truthy() }
func (e Element) Open() bool      { return e.Valid() && e.value.Get("open").Truthy() }
func (e Element) SetOpen(value bool) {
	if e.Valid() {
		e.value.Set("open", value)
	}
}
func (e Element) Focus() {
	if e.Valid() {
		e.value.Call("focus")
	}
}
func (e Element) Click() {
	if e.Valid() {
		e.value.Call("click")
	}
}
func (e Element) Remove() {
	if e.Valid() {
		e.value.Call("remove")
	}
}
func (e Element) Blur() {
	if e.Valid() {
		e.value.Call("blur")
	}
}
func (d Document) Body() Element          { return FromJS(d.value.Get("body")) }
func (d Document) Head() Element          { return FromJS(d.value.Get("head")) }
func (d Document) Root() Element          { return FromJS(d.value.Get("documentElement")) }
func (d Document) ActiveElement() Element { return FromJS(d.value.Get("activeElement")) }
func (e Event) Key() string {
	v := e.value.Get("key")
	if v.Type() != js.TypeString {
		return ""
	}
	return v.String()
}
func (e Event) Code() string {
	v := e.value.Get("code")
	if v.Type() != js.TypeString {
		return ""
	}
	return v.String()
}
func (e Event) Type() string {
	v := e.value.Get("type")
	if v.Type() != js.TypeString {
		return ""
	}
	return v.String()
}
func (e Event) PointerType() string {
	v := e.value.Get("pointerType")
	if v.Type() != js.TypeString {
		return ""
	}
	return v.String()
}
func (e Event) Repeat() bool   { return e.value.Get("repeat").Truthy() }
func (e Event) ShiftKey() bool { return e.value.Get("shiftKey").Truthy() }
func (e Event) CtrlKey() bool  { return e.value.Get("ctrlKey").Truthy() }
func (e Event) AltKey() bool   { return e.value.Get("altKey").Truthy() }
func (e Event) MetaKey() bool  { return e.value.Get("metaKey").Truthy() }
func (e Event) Matches() bool  { return e.value.Get("matches").Truthy() }
func (e Event) ClientX() float64 {
	v := e.value.Get("clientX")
	if v.Type() != js.TypeNumber {
		return 0
	}
	return v.Float()
}
func (e Event) ClientY() float64 {
	v := e.value.Get("clientY")
	if v.Type() != js.TypeNumber {
		return 0
	}
	return v.Float()
}
func (e Event) PageX() float64 {
	v := e.value.Get("pageX")
	if v.Type() != js.TypeNumber {
		return 0
	}
	return v.Float()
}
func (e Event) PageY() float64 {
	v := e.value.Get("pageY")
	if v.Type() != js.TypeNumber {
		return 0
	}
	return v.Float()
}
func (e Event) Pressure() float64 {
	v := e.value.Get("pressure")
	if v.Type() != js.TypeNumber {
		return 0
	}
	return v.Float()
}
func (e Event) TimeStamp() float64 {
	v := e.value.Get("timeStamp")
	if v.Type() != js.TypeNumber {
		return 0
	}
	return v.Float()
}
func (e Event) DeltaX() float64 {
	v := e.value.Get("deltaX")
	if v.Type() != js.TypeNumber {
		return 0
	}
	return v.Float()
}
func (e Event) DeltaY() float64 {
	v := e.value.Get("deltaY")
	if v.Type() != js.TypeNumber {
		return 0
	}
	return v.Float()
}
func (e Event) PointerID() int {
	v := e.value.Get("pointerId")
	if v.Type() != js.TypeNumber {
		return 0
	}
	return v.Int()
}
func (e Event) Button() int {
	v := e.value.Get("button")
	if v.Type() != js.TypeNumber {
		return 0
	}
	return v.Int()
}
func (e Event) Buttons() int {
	v := e.value.Get("buttons")
	if v.Type() != js.TypeNumber {
		return 0
	}
	return v.Int()
}

// Value is internal framework interop; applications cannot import this package.
func Value(e Element) js.Value {
	if !e.Valid() {
		return js.Null()
	}
	return e.value
}
func FromEventJS(value js.Value) Event { return Event{value} }

// Listen is internal framework interop for platform-owned event targets.
func Listen(target js.Value, event string, fn func(Event), opts ...ListenerOptions) *Listener {
	return listen(target, event, fn, opts)
}
func (d Document) CreateText(text string) Element {
	return FromJS(d.value.Call("createTextNode", text))
}
func (e Element) CheckValidity() bool  { return e.Valid() && e.value.Call("checkValidity").Bool() }
func (e Element) ReportValidity() bool { return e.Valid() && e.value.Call("reportValidity").Bool() }
func (e Element) ChildElementCount() int {
	if !e.Valid() {
		return 0
	}
	return e.value.Get("childElementCount").Int()
}
func (e Event) Trusted() bool { return e.value.Get("isTrusted").Truthy() }

// DataString returns only string-valued message data, ignoring other payloads.
func (e Event) DataString() string {
	v := e.value.Get("data")
	if v.Type() != js.TypeString {
		return ""
	}
	return v.String()
}

// OffsetWidth measures layout; reading it can intentionally commit pending style changes.
func (e Element) OffsetWidth() int {
	if !e.Valid() {
		return 0
	}
	return e.value.Get("offsetWidth").Int()
}
func (e Element) Contains(other Element) bool {
	return e.Valid() && other.Valid() && e.value.Call("contains", other.value).Bool()
}
func (e Element) SetSelectionRange(start, end int) {
	if e.Valid() {
		e.value.Call("setSelectionRange", start, end)
	}
}
func (e Element) ScrollNearest() {
	if e.Valid() {
		e.value.Call("scrollIntoView", map[string]any{"block": "nearest"})
	}
}
func (e Element) OptionCount() int {
	if !e.Valid() {
		return 0
	}
	return e.value.Get("options").Length()
}
func (e Element) SelectedIndex() int {
	if !e.Valid() {
		return -1
	}
	return e.value.Get("selectedIndex").Int()
}
func (e Element) SetSelectedIndex(index int) {
	if e.Valid() {
		e.value.Set("selectedIndex", index)
	}
}
func (e Element) StepUp(steps int) {
	if e.Valid() {
		e.value.Call("stepUp", steps)
	}
}
func (e Element) Dispatch(name string, bubbles bool) {
	if e.Valid() {
		event := js.Global().Get("Event").New(name, map[string]any{"bubbles": bubbles})
		e.value.Call("dispatchEvent", event)
	}
}
