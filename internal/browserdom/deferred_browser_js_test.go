//go:build js && wasm

package browserdom

import (
	"strings"
	"syscall/js"
	"testing"

	"m31labs.dev/gosx/internal/jsreentry"
)

// The tests in this file run against a real DOM. The normal Node runner has none,
// so they skip there. Run them in a browser wasm host, as for
// TestInstantiateTemplateInBrowser. They check, in Chrome for Testing 156, what
// the documentation of Listener and of package client/browser says about the
// calls that make the browser run listeners before they return. Their native
// counterparts in deferred_js_test.go run everywhere.

// browserPage returns the page's document, or skips the test when the host has
// no DOM.
func browserPage(t *testing.T) js.Value {
	t.Helper()
	doc := js.Global().Get("document")
	if !doc.Truthy() || doc.Get("createElement").Type() != js.TypeFunction || !doc.Get("body").Truthy() {
		t.Skip("requires a browser DOM")
	}
	return doc
}

// pageLogger returns a JavaScript array and a function that adds a native
// listener to a target. The listener appends label:event:state to the array, and
// state says whether the event target is connected at that moment.
func pageLogger() (log, listen js.Value) {
	fixture := nativeValue(`(function () {
		const log = [];
		return {
			log,
			listen(target, name, label) {
				target.addEventListener(name, function () {
					log.push(label + ":" + name + ":" + (target.isConnected ? "connected" : "removed"));
				});
			},
		};
	})()`)
	return fixture.Get("log"), fixture.Get("listen")
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func joinLog(log js.Value) string {
	words := make([]string, log.Length())
	for i := range words {
		words[i] = log.Index(i).String()
	}
	return strings.Join(words, " ")
}

// Taking the focused text field out of the document fires blur and focusout
// before the call returns: Element.Remove on the field, and SetText and
// ReplaceMarkup on its parent. The Go listeners run when the call returns, after
// the page's listeners and after the removal, and still read the field's value.
func TestRemovingTheFocusedFieldInABrowserNeverRunsGoListenersInsideTheCall(t *testing.T) {
	doc := browserPage(t)
	probe := jsreentry.Install(t)
	calls := []struct {
		name string
		call func(parent, field Element)
	}{
		{"Remove", func(_, field Element) { field.Remove() }},
		{"SetText", func(parent, _ Element) { parent.SetText("replacement") }},
		{"ReplaceMarkup", func(parent, _ Element) { parent.ReplaceMarkup(Markup("<b>replacement</b>")) }},
	}
	for _, c := range calls {
		t.Run(c.name, func(t *testing.T) {
			log, listen := pageLogger()
			parent, field := doc.Call("createElement", "div"), doc.Call("createElement", "input")
			field.Set("value", "draft")
			parent.Call("appendChild", field)
			doc.Get("body").Call("appendChild", parent)
			defer parent.Call("remove")
			field.Call("focus")
			if !doc.Get("activeElement").Equal(field) {
				t.Fatal("the browser did not focus the field, so nothing here would fire blur")
			}
			var listeners []*Listener
			for _, name := range []string{"blur", "focusout"} {
				listen.Invoke(field, name, "js-before")
				listeners = append(listeners, Listen(field, name, func(e Event) {
					current := e.CurrentTarget()
					log.Call("push", "go:"+e.Type()+":"+current.Value()+":connected="+yesNo(current.Connected())+":parent="+yesNo(current.Parent().Valid()))
				}))
				listen.Invoke(field, name, "js-after")
			}
			defer func() {
				for _, l := range listeners {
					l.Dispose()
				}
			}()
			probe.Reset()
			c.call(FromJS(parent), FromJS(field))
			probe.AssertNone(t, "%s", c.name)
			want := "js-before:blur:connected js-after:blur:connected js-before:focusout:connected js-after:focusout:connected" +
				" go:blur:draft:connected=no:parent=no go:focusout:draft:connected=no:parent=no"
			if got := joinLog(log); got != want {
				t.Fatalf("%s:\n got %q\nwant %q", c.name, got, want)
			}
		})
	}
}

// CheckValidity and ReportValidity of a real required field. ReportValidity also
// focuses the field, which fires focus and focusin before it returns.
func TestValidityChecksInABrowserNeverRunGoListenersInsideTheCall(t *testing.T) {
	doc := browserPage(t)
	probe := jsreentry.Install(t)
	calls := []struct {
		name   string
		events []string
		call   func(Element) bool
	}{
		{"CheckValidity", []string{"invalid"}, Element.CheckValidity},
		{"ReportValidity", []string{"invalid", "focus", "focusin"}, Element.ReportValidity},
	}
	for _, c := range calls {
		t.Run(c.name, func(t *testing.T) {
			log, listen := pageLogger()
			field := doc.Call("createElement", "input")
			field.Set("required", true)
			doc.Get("body").Call("appendChild", field)
			defer field.Call("remove")
			var listeners []*Listener
			for _, name := range c.events {
				listen.Invoke(field, name, "js-before")
				listeners = append(listeners, Listen(field, name, func(e Event) {
					e.PreventDefault()
					log.Call("push", "go:"+e.Type())
				}))
				listen.Invoke(field, name, "js-after")
			}
			defer func() {
				for _, l := range listeners {
					l.Dispose()
				}
			}()
			probe.Reset()
			if c.call(FromJS(field)) {
				t.Fatalf("%s returned true for an empty required field", c.name)
			}
			probe.AssertNone(t, "%s", c.name)
			var want []string
			for _, name := range c.events {
				want = append(want, "js-before:"+name+":connected", "js-after:"+name+":connected")
			}
			for _, name := range c.events {
				want = append(want, "go:"+name)
			}
			if got := joinLog(log); got != strings.Join(want, " ") {
				t.Fatalf("%s:\n got %q\nwant %q", c.name, got, strings.Join(want, " "))
			}
		})
	}
}

// What a Go listener reads from Event.Target and Event.RelatedTarget when the
// event goes through a shadow tree. The listener runs after the dispatch, so it
// reads what the browser leaves: both cleared when the event never left the
// shadow tree, and otherwise as seen from the document.
func TestDeferredListenersReadShadowTreeTargetsAsTheBrowserLeavesThem(t *testing.T) {
	browserPage(t)
	probe := jsreentry.Install(t)
	scene := nativeValue(`(function () {
		const outer = document.createElement("button");
		const host = document.createElement("div");
		const root = host.attachShadow({ mode: "open" });
		const a = document.createElement("button");
		const b = document.createElement("button");
		const nestedHost = document.createElement("div");
		root.append(a, b, nestedHost);
		const c = document.createElement("button");
		nestedHost.attachShadow({ mode: "open" }).append(c);
		document.body.append(outer, host);
		return { outer, host, a, b, c };
	})()`)
	defer scene.Get("outer").Call("remove")
	defer scene.Get("host").Call("remove")
	names := map[string]Element{}
	for _, name := range []string{"outer", "host", "a", "b", "c"} {
		names[name] = FromJS(scene.Get(name))
	}
	nameOf := func(e Element) string {
		if !e.Valid() {
			return "none"
		}
		for name, known := range names {
			if known.Equal(e) {
				return name
			}
		}
		return "unknown"
	}
	blurEverything := func() {
		for _, name := range []string{"outer", "a", "b", "c"} {
			scene.Get(name).Call("blur")
		}
	}
	// hasFocus reports whether the element is the focused element of its own tree.
	hasFocus := func(name string) bool {
		active := scene.Get(name).Call("getRootNode").Get("activeElement")
		return active.Truthy() && active.Equal(scene.Get(name))
	}
	rows := []struct {
		name   string
		start  string // the element that has focus before the call, if any
		events map[string][]string
		call   func()
		want   string
	}{
		{"click inside a shadow tree is composed", "", map[string][]string{"a": {"click"}}, func() { names["a"].Click() },
			"click@a target=host related=none current=a"},
		{"click in a nested shadow tree names the outermost host", "", map[string][]string{"c": {"click"}}, func() { names["c"].Click() },
			"click@c target=host related=none current=c"},
		{"an event that is not composed is cleared", "", map[string][]string{"b": {"ping"}}, func() { names["b"].Dispatch("ping", true) },
			"ping@b target=none related=none current=b"},
		{"an event outside any shadow tree is not cleared", "", map[string][]string{"outer": {"ping"}}, func() { names["outer"].Dispatch("ping", true) },
			"ping@outer target=outer related=none current=outer"},
		{"focus moves between two elements of one shadow tree", "a", map[string][]string{"a": {"focusout"}, "b": {"focusin"}}, func() { names["b"].Focus() },
			"focusout@a target=none related=none current=a | focusin@b target=none related=none current=b"},
		{"focus moves into a nested shadow tree", "b", map[string][]string{"c": {"focusin"}}, func() { names["c"].Focus() },
			"focusin@c target=none related=none current=c"},
		{"focus moves from the document into a shadow tree", "outer", map[string][]string{"a": {"focusin"}}, func() { names["a"].Focus() },
			"focusin@a target=host related=outer current=a"},
		{"focus moves from a shadow tree into the document", "a", map[string][]string{"outer": {"focusin"}}, func() { names["outer"].Focus() },
			"focusin@outer target=outer related=host current=outer"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			blurEverything()
			if row.start != "" {
				scene.Get(row.start).Call("focus")
				if !hasFocus(row.start) {
					t.Fatalf("the browser did not focus %s", row.start)
				}
			}
			var seen []string
			var listeners []*Listener
			for _, on := range []string{"outer", "a", "b", "c"} {
				for _, event := range row.events[on] {
					listeners = append(listeners, names[on].On(event, func(e Event) {
						seen = append(seen, event+"@"+on+" target="+nameOf(e.Target())+" related="+nameOf(e.RelatedTarget())+" current="+nameOf(e.CurrentTarget()))
					}))
				}
			}
			probe.Reset()
			row.call()
			for _, l := range listeners {
				l.Dispose()
			}
			probe.AssertNone(t, "%s", row.name)
			if got := strings.Join(seen, " | "); got != row.want {
				t.Fatalf("%s:\n got %q\nwant %q", row.name, got, row.want)
			}
		})
	}
}
