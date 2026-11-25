//go:build js && wasm

package kitui

import (
	"syscall/js"
	"testing"

	"m31labs.dev/gosx/client/browser"
	"m31labs.dev/gosx/game/actions"
	"m31labs.dev/gosx/markup"
)

// fakeDialog installs a document with a dialog, its body and its footer.
// Elements have attributes, children, hidden, open and focus, querySelector
// for a class or the first enabled control, closest and contains, and
// listeners the test fires. A write replaces a part's children with the
// controls the test names (a class per control, "primary" for the main one).
const fakeDialog = `(() => {
  const doc = {activeElement: null};
  const mk = (tag, attrs) => {
    const el = {tagName: tag.toUpperCase(), attrs: Object.assign({}, attrs), children: [], parent: null, hidden: false, open: false, on: {}, id: (attrs && attrs.id) || ''};
    el.getAttribute = n => (n in el.attrs ? String(el.attrs[n]) : null);
    el.hasAttribute = n => n in el.attrs;
    el.focus = () => { doc.activeElement = el; };
    el.contains = o => { for (let e = o; e; e = e.parent) if (e === el) return true; return false; };
    const all = () => { const out = []; const walk = e => { for (const c of e.children) { out.push(c); walk(c); } }; walk(el); return out; };
    el.querySelector = sel => all().find(e => sel.startsWith('.') ? String(e.attrs.class || '').split(' ').includes(sel.slice(1).split(':')[0]) :
      sel.startsWith('[') ? (sel.slice(1, -1) in e.attrs) : ['BUTTON', 'SELECT', 'INPUT'].includes(e.tagName)) || null;
    el.closest = sel => { for (let e = el; e; e = e.parent) if (e.hasAttribute(sel.slice(1, -1))) return e; return null; };
    el.addEventListener = (name, fn) => { el.on[name] = fn; };
    el.showModal = () => { if (el.open) throw new Error('already open'); el.open = true; };
    el.close = () => { el.open = false; };
    el.fill = (...classes) => { el.children = classes.map(c => { const b = mk('button', {class: c, 'data-do': c}); b.parent = el; return b; }); };
    return el;
  };
  const dialog = mk('dialog', {id: 'dialog'});
  const body = mk('div', {id: 'content'}), foot = mk('div', {id: 'foot'});
  for (const p of [body, foot]) { p.parent = dialog; dialog.children.push(p); }
  doc.getElementById = id => ({dialog, content: body, foot})[id] || null;
  doc.querySelector = sel => (sel === 'dialog[open]' && dialog.open ? dialog : null);
  return doc;
})()`

func installFakeDialog(t *testing.T) js.Value {
	t.Helper()
	previous := js.Global().Get("document")
	doc := js.Global().Call("eval", fakeDialog)
	js.Global().Set("document", doc)
	t.Cleanup(func() { js.Global().Set("document", previous) })
	return doc
}

// fill writes into the fake: the markup's text names the controls.
func fill(part browser.Element, n markup.Node) {
	controls := []any{}
	if text := string(markup.RenderHTML(n)); text != "" {
		for _, c := range splitWords(text) {
			controls = append(controls, c)
		}
	}
	js.Global().Get("document").Call("getElementById", part.ID()).Call("fill", controls...)
}

func splitWords(s string) []string {
	var out []string
	word := ""
	for _, r := range s + " " {
		if r == ' ' {
			if word != "" {
				out = append(out, word)
			}
			word = ""
			continue
		}
		word += string(r)
	}
	return out
}

func TestModalShowsFocusesItsMainControlAndCloses(t *testing.T) {
	doc := installFakeDialog(t)
	m := FindModal(browser.CurrentDocument(), DialogProps{ID: "dialog", BodyID: "content", FootID: "foot"})
	m.Write = fill
	m.Foot.SetHidden(false)
	m.Show(markup.Text("subtle primary"))
	active := doc.Get("activeElement")
	if !m.Open() || !AnyOpen(browser.CurrentDocument()) {
		t.Fatal("Show must open the dialog")
	}
	if active.IsNull() || active.Call("getAttribute", "class").String() != "primary" {
		t.Fatal("Show must focus the body's main control")
	}
	if !doc.Call("getElementById", "foot").Get("hidden").Bool() || doc.Call("getElementById", "foot").Get("children").Length() != 0 {
		t.Fatal("Show must empty and hide the footer")
	}
	// A dialog already open is written again without opening twice.
	m.Show(markup.Text("only"))
	m.Footer(markup.Text("primary"))
	foot := doc.Call("getElementById", "foot")
	if foot.Get("hidden").Bool() || !foot.Call("contains", doc.Get("activeElement")).Bool() {
		t.Fatal("Footer must show the footer and focus its main control")
	}
	m.Close()
	if m.Open() || AnyOpen(browser.CurrentDocument()) {
		t.Fatal("Close must close the dialog")
	}
}

func TestModalRunsActionsAndSwallowsTheEscapeThatClosedIt(t *testing.T) {
	doc := installFakeDialog(t)
	m := FindModal(browser.CurrentDocument(), DialogProps{ID: "dialog", BodyID: "content"})
	m.Write = fill
	m.Show(markup.Text("guide primary"))
	var ran []string
	m.OnAction("data-do", func(name string, _ browser.Element) { ran = append(ran, name) })
	dialog := doc.Call("getElementById", "dialog")
	button := doc.Call("getElementById", "content").Get("children").Index(0)
	dialog.Get("on").Call("click", map[string]any{"target": button})
	dialog.Get("on").Call("click", map[string]any{"target": dialog})
	if len(ran) != 1 || ran[0] != "guide" {
		t.Fatalf("actions run: %v, want [guide]", ran)
	}

	keys := actions.NewMap()
	keys.Define(actions.Context{Name: "play", Actions: []actions.ActionDef{{Name: "pause", Bindings: []actions.Binding{{Source: actions.Key("Escape")}}}}})
	keys.Push("play")
	keys.Update(0)
	keys.Feed(actions.Event{Device: actions.DeviceKeyboard, Source: actions.Key("Escape"), Value: 1})
	m.SwallowEscape(keys)
	dialog.Get("on").Call("cancel", map[string]any{"target": dialog})
	keys.Update(0)
	if keys.Pressed("pause") {
		t.Fatal("the Escape that closed the dialog must not also reach the game")
	}
}
