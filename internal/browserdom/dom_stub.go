//go:build !js || !wasm

package browserdom

type Element struct{}
type Document struct{}
type WindowTarget struct{}
type MediaQuery struct{}
type Elements struct{}
type Style struct{}
type Classes struct{}
type Dataset struct{}
type Event struct{}
type Listener struct{}

func CurrentDocument() Document                      { return Document{} }
func Window() WindowTarget                           { return WindowTarget{} }
func MatchMedia(query string) MediaQuery             { return MediaQuery{} }
func (e Element) Valid() bool                        { return false }
func (e Element) Equal(other Element) bool           { return true }
func (e Element) Query(selector string) Element      { return Element{} }
func (e Element) QueryAll(selector string) Elements  { return Elements{} }
func (e Element) Closest(selector string) Element    { return Element{} }
func (e Element) Attr(name string) string            { return "" }
func (e Element) SetAttr(name, value string)         {}
func (e Element) HasAttr(name string) bool           { return false }
func (e Element) RemoveAttr(name string)             {}
func (e Element) Classes() Classes                   { return Classes{} }
func (e Element) Style() Style                       { return Style{} }
func (e Element) Dataset() Dataset                   { return Dataset{} }
func (e Element) Parent() Element                    { return Element{} }
func (e Element) Append(children ...Element)         {}
func (e Element) InsertBefore(child, before Element) {}
func (e Element) ReplaceMarkup(markup Markup)        {}
func (e Element) AppendMarkup(markup Markup)         {}
func (e Element) Rect() Rect                         { return Rect{} }
func (e Element) ClientRectCount() int               { return 0 }
func (e Element) HasPointerCapture(id int) bool      { return false }
func (e Element) SetPointerCapture(id int)           {}
func (e Element) ReleasePointerCapture(id int)       {}
func (e Element) On(name string, fn func(Event), options ...ListenerOptions) *Listener {
	return &Listener{}
}
func (d Document) On(name string, fn func(Event), options ...ListenerOptions) *Listener {
	return &Listener{}
}
func (w WindowTarget) On(name string, fn func(Event), options ...ListenerOptions) *Listener {
	return &Listener{}
}
func (m MediaQuery) On(name string, fn func(Event), options ...ListenerOptions) *Listener {
	return &Listener{}
}
func (m MediaQuery) Matches() bool                       { return false }
func (l *Listener) Dispose()                             {}
func (e Elements) Len() int                              { return 0 }
func (e Elements) At(index int) Element                  { return Element{} }
func (d Document) ByID(id string) Element                { return Element{} }
func (d Document) Query(selector string) Element         { return Element{} }
func (d Document) QueryAll(selector string) Elements     { return Elements{} }
func (d Document) Create(tag string) Element             { return Element{} }
func (d Document) HasFocus() bool                        { return false }
func (d Document) SetTitle(title string)                 {}
func (d Document) ReadyState() string                    { return "" }
func (d Document) Hidden() bool                          { return false }
func (s Style) Set(name, value string)                   {}
func (s Style) Get(name string) string                   { return "" }
func (s Style) Remove(name string)                       {}
func (c Classes) Add(names ...string)                    {}
func (c Classes) Remove(names ...string)                 {}
func (c Classes) Toggle(name string, force ...bool) bool { return false }
func (c Classes) Contains(name string) bool              { return false }
func (d Dataset) Set(name, value string)                 {}
func (d Dataset) Get(name string) string                 { return "" }
func (e Event) Target() Element                          { return Element{} }
func (e Event) CurrentTarget() Element                   { return Element{} }
func (e Event) RelatedTarget() Element                   { return Element{} }
func (e Event) PreventDefault()                          {}
func (e Event) StopPropagation()                         {}
func (e Event) StopImmediatePropagation()                {}
func (e Event) DetailRevision() uint64                   { return 0 }
func (e Element) ID() string                             { return "" }
func (e Element) TagName() string                        { return "" }
func (e Element) Text() string                           { return "" }
func (e Element) Value() string                          { return "" }
func (e Element) ClassName() string                      { return "" }
func (e Element) SetText(value string)                   {}
func (e Element) SetValue(value string)                  {}
func (e Element) SetClassName(value string)              {}
func (e Element) Hidden() bool                           { return false }
func (e Element) SetHidden(value bool)                   {}
func (e Element) Checked() bool                          { return false }
func (e Element) SetChecked(value bool)                  {}
func (e Element) Disabled() bool                         { return false }
func (e Element) SetDisabled(value bool)                 {}
func (e Element) Connected() bool                        { return false }
func (e Element) Open() bool                             { return false }
func (e Element) SetOpen(value bool)                     {}
func (e Element) Focus()                                 {}
func (e Element) Click()                                 {}
func (e Element) Remove()                                {}
func (e Element) Blur()                                  {}
func (d Document) Body() Element                         { return Element{} }
func (d Document) Head() Element                         { return Element{} }
func (d Document) Root() Element                         { return Element{} }
func (d Document) ActiveElement() Element                { return Element{} }
func (e Event) Key() string                              { return "" }
func (e Event) Code() string                             { return "" }
func (e Event) Type() string                             { return "" }
func (e Event) PointerType() string                      { return "" }
func (e Event) Repeat() bool                             { return false }
func (e Event) ShiftKey() bool                           { return false }
func (e Event) CtrlKey() bool                            { return false }
func (e Event) AltKey() bool                             { return false }
func (e Event) MetaKey() bool                            { return false }
func (e Event) Matches() bool                            { return false }
func (e Event) ClientX() float64                         { return 0 }
func (e Event) ClientY() float64                         { return 0 }
func (e Event) PageX() float64                           { return 0 }
func (e Event) PageY() float64                           { return 0 }
func (e Event) Pressure() float64                        { return 0 }
func (e Event) TimeStamp() float64                       { return 0 }
func (e Event) DeltaX() float64                          { return 0 }
func (e Event) DeltaY() float64                          { return 0 }
func (e Event) PointerID() int                           { return 0 }
func (e Event) Button() int                              { return 0 }
func (e Event) Buttons() int                             { return 0 }
func (d Document) CreateText(string) Element             { return Element{} }
func (e Element) CheckValidity() bool                    { return false }
func (e Element) ReportValidity() bool                   { return false }
func (e Element) ChildElementCount() int                 { return 0 }
func (e Event) Trusted() bool                            { return false }
func (e Event) DataString() string                       { return "" }
func (e Element) OffsetWidth() int                       { return 0 }
func (e Element) Contains(Element) bool                  { return false }
func (e Element) SetSelectionRange(int, int)             {}
func (e Element) ScrollNearest()                         {}
func (e Element) OptionCount() int                       { return 0 }
func (e Element) SelectedIndex() int                     { return -1 }
func (e Element) SetSelectedIndex(int)                   {}
func (e Element) StepUp(int)                             {}
func (e Element) Dispatch(string, bool)                  {}
