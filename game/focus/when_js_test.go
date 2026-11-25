//go:build js && wasm

package focus

import (
	"syscall/js"
	"testing"

	"m31labs.dev/gosx/client/browser"
)

// fakeDOM installs a small document with the parts When and Keep read: a tree
// of elements with attributes, disabled, offsetWidth and an inert flag that
// refuses the focus, the active element, and querySelector(All), closest and
// contains for the selectors these tests use.
const fakeDOM = `(() => {
  const doc = {};
  const one = /^([a-z]+|\*)?(?:\.([\w-]+))?(?:\[([\w-]+)(?:="((?:[^"\\]|\\.)*)")?\])?(:not\(:disabled\)|:disabled)?$/i;
  const matches = (e, sel) => sel.split(',').some(s => {
    const m = one.exec(s.trim());
    if (!m) throw new Error('the fake document cannot match ' + s);
    if (m[1] && m[1] !== '*' && e.tagName !== m[1].toUpperCase()) return false;
    if (m[2] && !String(e.attrs.class || '').split(/\s+/).includes(m[2])) return false;
    if (m[3] && m[4] === undefined && !e.hasAttribute(m[3])) return false;
    if (m[3] && m[4] !== undefined && e.getAttribute(m[3]) !== m[4].replace(/\\(.)/g, '$1')) return false;
    if (m[5] === ':not(:disabled)' && e.disabled) return false;
    if (m[5] === ':disabled' && !e.disabled) return false;
    return true;
  });
  const below = root => { const out = []; const walk = e => { for (const c of e.children) { out.push(c); walk(c); } }; walk(root); return out; };
  const mk = (tag, attrs, width) => {
    const el = {tagName: tag.toUpperCase(), attrs: Object.assign({}, attrs), disabled: false, offsetWidth: width, drawnWidth: width, inert: false, children: [], parent: null, isConnected: true, scrollTop: 0, scrollHeight: 0, clientHeight: 0};
    el.getAttribute = n => (n in el.attrs ? String(el.attrs[n]) : null);
    el.hasAttribute = n => n in el.attrs;
    el.focus = () => { if (!el.inert && !el.disabled && el.isConnected) doc.activeElement = el; };
    el.contains = o => { for (let e = o; e; e = e.parent) if (e === el) return true; return false; };
    el.querySelectorAll = sel => below(el).filter(e => matches(e, sel));
    el.querySelector = sel => el.querySelectorAll(sel)[0] || null;
    el.closest = sel => { for (let e = el; e; e = e.parent) if (matches(e, sel)) return e; return null; };
    Object.defineProperty(el, 'parentElement', {get: () => el.parent});
    el.events = [];
    el.dispatchEvent = ev => { el.events.push(ev.type); return true; };
    // A click on a summary toggles its details, as a browser does: what the
    // details holds is drawn while it is open and not drawn while it is shut.
    el.click = () => {
      el.events.push('click');
      const d = el.parent;
      if (el.tagName !== 'SUMMARY' || !d) return;
      if (d.hasAttribute('open')) delete d.attrs.open; else d.attrs.open = '';
      for (const c of d.children) if (c !== el) for (const e of [c, ...below(c)]) e.offsetWidth = d.hasAttribute('open') ? e.drawnWidth : 0;
    };
    el.add = (...cs) => { for (const c of cs) { c.parent = el; el.children.push(c); } return el; };
    // clear takes every child away, as writing new markup does: a focus inside
    // falls to the body.
    el.clear = () => {
      for (const c of el.children) {
        if (c.contains(doc.activeElement)) doc.activeElement = doc.body;
        for (const e of [c, ...below(c)]) e.isConnected = false;
        c.parent = null;
      }
      el.children = [];
    };
    return el;
  };
  doc.body = mk('body', {}, 800);
  doc.documentElement = doc.body;
  doc.activeElement = doc.body;
  doc.querySelectorAll = sel => doc.body.querySelectorAll(sel);
  doc.querySelector = sel => doc.body.querySelector(sel);
  doc.getElementById = id => doc.body.querySelector('[id="' + id + '"]');
  doc.mk = mk;
  globalThis.getComputedStyle = e => ({overflowY: e.attrs.overflow || 'visible'});
  return doc;
})()`

type dom struct {
	t   *testing.T
	doc js.Value
}

func newDOM(t *testing.T) dom {
	t.Helper()
	global := js.Global()
	previous := global.Get("document")
	doc := global.Call("eval", fakeDOM)
	global.Set("document", doc)
	t.Cleanup(func() {
		if waiting != nil {
			waiting.Cancel()
		}
		global.Delete("getComputedStyle")
		if previous.IsUndefined() {
			global.Delete("document")
		} else {
			global.Set("document", previous)
		}
	})
	return dom{t, doc}
}

// el makes an element of tag with attrs, drawn width pixels wide (0 is not
// drawn), and puts it in parent (the body when parent is undefined).
func (d dom) el(parent js.Value, tag string, attrs map[string]any, width int) js.Value {
	e := d.doc.Call("mk", tag, attrs, width)
	if parent.IsUndefined() {
		parent = d.doc.Get("body")
	}
	parent.Call("add", e)
	return e
}

func (d dom) active() js.Value { return d.doc.Get("activeElement") }

func (d dom) focused(want js.Value, what string) {
	d.t.Helper()
	if !d.active().Equal(want) {
		d.t.Fatalf("%s: the focus is on %v %v", what, d.active().Get("tagName"), d.active().Get("attrs").Call("toString"))
	}
}

var none = js.Undefined()

func TestWhenFocusesTheControlOnceItIsDrawnAndEnabled(t *testing.T) {
	d := newDOM(t)
	doc := browser.CurrentDocument()
	p := When(doc, `[id="featured"]`, 10)
	if !Step() {
		t.Fatal("a request for a control not in the page stopped waiting")
	}
	// The island draws the row disabled, then hidden, then shown and enabled.
	row := d.el(none, "button", map[string]any{"id": "featured"}, 0)
	row.Set("disabled", true)
	if !Step() || !d.active().Equal(d.doc.Get("body")) {
		t.Fatal("a disabled control took the focus")
	}
	row.Set("disabled", false)
	if !Step() || !d.active().Equal(d.doc.Get("body")) {
		t.Fatal("a control that is not drawn took the focus")
	}
	row.Set("offsetWidth", 120)
	if Step() {
		t.Fatal("the request still waits after its control took the focus")
	}
	d.focused(row, "once drawn")
	if p.Waiting() {
		t.Fatal("a done request waits")
	}
}

func TestWhenWaitsOutAControlThatRefusesTheFocus(t *testing.T) {
	d := newDOM(t)
	final := d.el(none, "button", map[string]any{"id": "final-again"}, 90)
	final.Set("inert", true) // under an open modal dialog
	p := When(browser.CurrentDocument(), `[id="final-again"]`, 3)
	if !Step() || !p.Waiting() {
		t.Fatal("the request gave up while the control refused the focus")
	}
	final.Set("inert", false)
	Step()
	d.focused(final, "after the dialog closed")
}

func TestWhenTakesTheFirstReadyMatchAndANewerRequestWins(t *testing.T) {
	d := newDOM(t)
	d.el(none, "button", map[string]any{"class": "primary"}, 0) // not drawn
	off := d.el(none, "button", map[string]any{"class": "primary"}, 40)
	off.Set("disabled", true)
	shown := d.el(none, "button", map[string]any{"class": "primary"}, 40)
	d.el(none, "button", map[string]any{"id": "film-play"}, 40)
	old := When(browser.CurrentDocument(), `[id="film-play"]`, 5)
	// The selector does not leave out the disabled one: When does.
	When(browser.CurrentDocument(), `button.primary`, 5)
	Step()
	d.focused(shown, "the newer request")
	if old.Waiting() {
		t.Fatal("the replaced request still waits")
	}
	Step()
	d.focused(shown, "a replaced request moved the focus")
}

func TestKeepPutsTheFocusBackOnTheSameControl(t *testing.T) {
	d := newDOM(t)
	bar := d.el(none, "div", map[string]any{"id": "playbar"}, 600)
	snap := d.el(bar, "button", map[string]any{"data-control": "snap"}, 80)
	snap.Call("focus")
	area := browser.CurrentDocument().ByID("playbar")
	var again js.Value
	had := Keep(area, func() {
		bar.Call("clear")
		d.el(bar, "button", map[string]any{"data-control": "audible", "class": "primary"}, 80)
		again = d.el(bar, "button", map[string]any{"data-control": "snap"}, 80)
	})
	if !had {
		t.Fatal("Keep reports the focus was not in the panel")
	}
	d.focused(again, "after the rewrite")
}

func TestKeepFallsBackToTheMainButton(t *testing.T) {
	d := newDOM(t)
	bar := d.el(none, "div", map[string]any{"id": "playbar"}, 600)
	call := d.el(bar, "button", map[string]any{"data-call": "7"}, 80)
	call.Call("focus")
	area := browser.CurrentDocument().ByID("playbar")
	var main js.Value
	Keep(area, func() {
		bar.Call("clear")
		d.el(bar, "button", map[string]any{"data-control": "back"}, 80)
		main = d.el(bar, "button", map[string]any{"data-control": "snap", "class": "primary"}, 80)
	})
	d.focused(main, "the call card went")

	// The same control, now disabled, gives way to the main button too.
	main.Call("focus")
	var off js.Value
	Keep(area, func() {
		bar.Call("clear")
		off = d.el(bar, "button", map[string]any{"data-control": "snap", "class": "primary"}, 80)
		off.Set("disabled", true)
		main = d.el(bar, "button", map[string]any{"data-control": "hurry"}, 80)
	})
	d.focused(main, "the control is disabled and there is no main button")
}

func TestKeepLeavesAFocusOutsideAlone(t *testing.T) {
	d := newDOM(t)
	d.el(none, "div", map[string]any{"id": "playbar"}, 600)
	outside := d.el(none, "button", map[string]any{"id": "pause"}, 40)
	outside.Call("focus")
	ran := false
	if Keep(browser.CurrentDocument().ByID("playbar"), func() { ran = true }) || !ran {
		t.Fatalf("Keep with the focus outside: reported inside or did not write (ran %v)", ran)
	}
	d.focused(outside, "a focus outside the panel")
}
