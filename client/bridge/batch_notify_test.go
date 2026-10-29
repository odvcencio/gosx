package bridge

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"m31labs.dev/gosx/client/vm"
)

// recordNotifications returns the per-signal notifications of a bridge, in
// order, as name=text.
func recordNotifications(b *Bridge) *[]string {
	var sent []string
	b.SetSharedSignalCallback(func(name, valueJSON string) { sent = append(sent, name+"="+valueJSON) })
	return &sent
}

func notificationsByName(sent []string) map[string]string {
	got := map[string]string{}
	for _, entry := range sent {
		name, text, _ := strings.Cut(entry, "=")
		got[name] = text
	}
	return got
}

func TestBatchNotificationsAreTheRuntimesOwnEncoding(t *testing.T) {
	for _, changedOnly := range []bool{false, true} {
		t.Run(fmt.Sprint("changedOnly=", changedOnly), func(t *testing.T) {
			b := New()
			sent := recordNotifications(b)
			// The sender's spacing, key order and number forms are not Go's, and what the page
			// is told is what the runtime stored, written as it always has been.
			batch := `{ "$hud" : { "tick" : 4 , "cash": {"v":1.50} } , "$bar":["b", "a"],"$n":  7.0, "$s":"a\u003cb<&>\u2028" }`
			if err := b.setSharedSignalBatchJSON(batch, changedOnly); err != nil {
				t.Fatal(err)
			}
			got := notificationsByName(*sent)
			want := map[string]string{
				"$hud": `{"cash":{"v":1.5},"tick":4}`,
				"$bar": `["b","a"]`,
				"$n":   `7`,
				"$s":   `"a\u003cb\u003c\u0026\u003e\u2028"`,
			}
			if len(got) != len(want) || len(*sent) != len(want) {
				t.Fatalf("notifications = %v", *sent)
			}
			for name, text := range want {
				if got[name] != text {
					t.Errorf("%s: told %q, want %q", name, got[name], text)
				}
				// And that is what json.Marshal says about the stored value.
				value, _ := b.GetStore().Get(name)
				if marshaled, err := json.Marshal(value.ToAny()); err != nil || string(marshaled) != got[name] {
					t.Errorf("%s: told %q, json.Marshal says %q (%v)", name, got[name], marshaled, err)
				}
			}
			if told, err := b.GetSharedSignalJSON("$hud"); err != nil || told != want["$hud"] {
				t.Errorf("GetSharedSignalJSON = %q (%v)", told, err)
			}
		})
	}
}

func TestBatchNotificationEncodesTheStoredValueWhenItChanged(t *testing.T) {
	b := New()
	sent := recordNotifications(b)
	// An observer that edits a value after the batch stored it, and another that
	// replaces it, both before the notification: the text must follow the store.
	observe := b.store.onChange
	b.store.SetObserver(func(name string, value vm.Value) {
		switch name {
		case "$edited":
			value.Map()["x"] = vm.IntVal(5)
		case "$replaced":
			value = vm.ObjectVal(map[string]vm.Value{"x": vm.IntVal(2)})
		}
		observe(name, value)
	})
	if err := b.SetSharedSignalBatchJSON(`{"$edited":{"x":1},"$replaced":{"x":1},"$plain":{"x":1}}`); err != nil {
		t.Fatal(err)
	}
	got := notificationsByName(*sent)
	for name, want := range map[string]string{"$edited": `{"x":5}`, "$replaced": `{"x":2}`, "$plain": `{"x":1}`} {
		if got[name] != want {
			t.Errorf("%s: told %q, want %q", name, got[name], want)
		}
	}
}

func TestBatchRepeatedAndAliasedNamesNotifyTheStoredValue(t *testing.T) {
	b := New()
	sent := recordNotifications(b)
	if err := b.SetSharedSignalBatchJSON(`{"$a":1, "$a":2}`); err != nil {
		t.Fatal(err)
	}
	if len(*sent) != 1 || (*sent)[0] != "$a=2" {
		t.Fatalf("repeated name: %v", *sent)
	}
	*sent = nil
	// $scene.event.* is a read-only alias of $surface.event.*. The store keeps
	// the canonical signal, and the sender's spelling is told as well.
	if err := b.SetSharedSignalBatchJSON(`{"$scene.event.targetID" : "a" }`); err != nil {
		t.Fatal(err)
	}
	if len(*sent) != 2 || (*sent)[0] != `$surface.event.targetID="a"` || (*sent)[1] != `$scene.event.targetID="a"` {
		t.Fatalf("one aliased name: %v", *sent)
	}
	*sent = nil
	// Both spellings name one signal; the store keeps one of the two values.
	if err := b.SetSharedSignalBatchJSON(`{"$scene.event.type" : "down", "$surface.event.type" : "up"}`); err != nil {
		t.Fatal(err)
	}
	stored, _ := b.GetStore().Get("$surface.event.type")
	want, _ := json.Marshal(stored.ToAny())
	if len(*sent) == 0 {
		t.Fatal("no notification")
	}
	for _, entry := range *sent {
		if entry != "$surface.event.type="+string(want) && entry != "$scene.event.type="+string(want) {
			t.Errorf("notification %q does not carry the stored %s", entry, want)
		}
	}
}

func TestBatchTellsTheBrowserTheLegacySpellingTheSenderUsed(t *testing.T) {
	// The page keeps one copy of a signal for each name it has read. A page that
	// read $scene.event.selectedID holds a copy under that spelling, and the
	// runtime announces changes under $surface.event.selectedID only. A sender that
	// spelled the legacy name is therefore told the value under that spelling too,
	// as the page's own priming of the sender's names did before batches went to the runtime.
	for _, changedOnly := range []bool{false, true} {
		t.Run(fmt.Sprint("changedOnly=", changedOnly), func(t *testing.T) {
			b := New()
			var handoffs [][]SignalUpdate
			b.SetSharedSignalBatchCallback(func(updates []SignalUpdate) {
				handoffs = append(handoffs, append([]SignalUpdate(nil), updates...))
			})
			text := func(i int) string {
				var out []string
				for _, update := range handoffs[i] {
					out = append(out, update.Name+"="+update.JSON)
				}
				return strings.Join(out, " ")
			}

			if err := b.setSharedSignalBatchJSON(`{"$scene.event.selectedID":"n1","$a":1}`, changedOnly); err != nil {
				t.Fatal(err)
			}
			if got := text(0); got != `$surface.event.selectedID="n1" $a=1 $scene.event.selectedID="n1"` &&
				got != `$a=1 $surface.event.selectedID="n1" $scene.event.selectedID="n1"` {
				t.Fatalf("first batch: %s", got)
			}

			// The value did not change. A changed-only snapshot has nothing to say about
			// it under either spelling, because the page holds it under the legacy
			// spelling already. An event batch means its write and says it again.
			if err := b.setSharedSignalBatchJSON(`{"$scene.event.selectedID":"n1"}`, changedOnly); err != nil {
				t.Fatal(err)
			}
			if changedOnly {
				if len(handoffs) != 1 {
					t.Fatalf("an unchanged legacy name in a changed-only batch: %d handoffs, last %q", len(handoffs), text(len(handoffs)-1))
				}
			} else if len(handoffs) != 2 || !strings.HasSuffix(text(1), `$scene.event.selectedID="n1"`) {
				t.Fatalf("a repeated legacy name in an event batch: %d handoffs, last %q", len(handoffs), text(len(handoffs)-1))
			}

			// The canonical spelling gets no extra announcement.
			n := len(handoffs)
			if err := b.setSharedSignalBatchJSON(`{"$surface.event.selectedID":"n2"}`, changedOnly); err != nil {
				t.Fatal(err)
			}
			if len(handoffs) != n+1 || text(n) != `$surface.event.selectedID="n2"` {
				t.Fatalf("canonical spelling: %q", text(len(handoffs)-1))
			}
		})
	}
}

func TestBatchLegacySpellingWithoutABatchCallbackGoesOutOneByOne(t *testing.T) {
	b := New()
	sent := recordNotifications(b)
	if err := b.SetChangedSharedSignalBatchJSON(`{"$scene.event.selectedID":"n1"}`); err != nil {
		t.Fatal(err)
	}
	if strings.Join(*sent, " ") != `$surface.event.selectedID="n1" $scene.event.selectedID="n1"` {
		t.Fatalf("per-signal = %v", *sent)
	}
}

// recordHandoffs returns the batch handoffs of a bridge, in order, and a function
// that writes the changes of handoff i as name=text.
func recordHandoffs(b *Bridge) (*[][]SignalUpdate, func(i int) string) {
	var handoffs [][]SignalUpdate
	b.SetSharedSignalBatchCallback(func(updates []SignalUpdate) {
		handoffs = append(handoffs, append([]SignalUpdate(nil), updates...))
	})
	return &handoffs, func(i int) string {
		var out []string
		for _, update := range handoffs[i] {
			out = append(out, update.Name+"="+update.JSON)
		}
		return strings.Join(out, " ")
	}
}

func TestChangedOnlyBatchTellsALegacySpellingOnceWhileItsValueStays(t *testing.T) {
	// Every page subscriber of a name wakes when the runtime announces the name, with
	// the value it already holds or not. A changed-only snapshot that repeats a value
	// must not wake the subscribers of the legacy spelling it was sent under.
	b := New()
	handoffs, text := recordHandoffs(b)

	const snapshot = `{"$scene.event.pointerX":5,"$other":1}`
	for i := 0; i < 3; i++ {
		if err := b.SetChangedSharedSignalBatchJSON(snapshot); err != nil {
			t.Fatal(err)
		}
	}
	if len(*handoffs) != 1 {
		t.Fatalf("three identical changed-only batches made %d handoffs; the last was %q", len(*handoffs), text(len(*handoffs)-1))
	}
	if got := text(0); got != `$surface.event.pointerX=5 $other=1 $scene.event.pointerX=5` &&
		got != `$other=1 $surface.event.pointerX=5 $scene.event.pointerX=5` {
		t.Fatalf("first batch: %s", got)
	}

	// A different value is a change under both spellings.
	if err := b.SetChangedSharedSignalBatchJSON(`{"$scene.event.pointerX":6}`); err != nil {
		t.Fatal(err)
	}
	if len(*handoffs) != 2 || text(1) != `$surface.event.pointerX=6 $scene.event.pointerX=6` {
		t.Fatalf("a changed value: %d handoffs, the last %q", len(*handoffs), text(len(*handoffs)-1))
	}
	// And the new value is then held back like the old one.
	if err := b.SetChangedSharedSignalBatchJSON(`{"$scene.event.pointerX":6}`); err != nil {
		t.Fatal(err)
	}
	if len(*handoffs) != 2 {
		t.Fatalf("the repeated new value made a handoff: %q", text(len(*handoffs)-1))
	}
}

func TestChangedOnlyBatchRepairsALegacyCopyThatAWriteLeftStale(t *testing.T) {
	// A write under the canonical name tells the page the new value under that name
	// only, so its copy under the legacy spelling is out of date. The next changed-only
	// snapshot that names the legacy spelling repairs the copy, once.
	b := New()
	handoffs, text := recordHandoffs(b)

	if err := b.SetChangedSharedSignalBatchJSON(`{"$scene.event.selectedID":"n1"}`); err != nil {
		t.Fatal(err)
	}
	if len(*handoffs) != 1 {
		t.Fatalf("first batch: %d handoffs", len(*handoffs))
	}
	if err := b.SetSharedSignalJSON("$surface.event.selectedID", `"n2"`); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 3; i++ {
		if err := b.SetChangedSharedSignalBatchJSON(`{"$scene.event.selectedID":"n2"}`); err != nil {
			t.Fatal(err)
		}
	}
	if len(*handoffs) != 2 || text(1) != `$scene.event.selectedID="n2"` {
		t.Fatalf("after a canonical write: %d handoffs, the last %q", len(*handoffs), text(len(*handoffs)-1))
	}
}

func TestEventBatchTellsALegacySpellingEveryTime(t *testing.T) {
	// An event batch is not a snapshot: its sender means every write, and subscribers
	// hear the repeat, as they do under the canonical name.
	b := New()
	handoffs, text := recordHandoffs(b)

	for i := 0; i < 3; i++ {
		if err := b.SetSharedSignalBatchJSON(`{"$scene.event.clickCount":2}`); err != nil {
			t.Fatal(err)
		}
	}
	if len(*handoffs) != 3 {
		t.Fatalf("three identical event batches made %d handoffs", len(*handoffs))
	}
	for i := range *handoffs {
		if !strings.HasSuffix(text(i), `$scene.event.clickCount=2`) {
			t.Fatalf("event batch %d: %q", i+1, text(i))
		}
	}
}

func TestBatchChangesLeaveTogetherThroughTheBatchCallback(t *testing.T) {
	b := New()
	var handoffs [][]SignalUpdate
	b.SetSharedSignalBatchCallback(func(updates []SignalUpdate) {
		handoffs = append(handoffs, append([]SignalUpdate(nil), updates...))
	})
	sent := recordNotifications(b)

	if err := b.SetSharedSignalBatchJSON(` {"$a":1,"$b":{"x": [1, 2]},"$c":"s"} `); err != nil {
		t.Fatal(err)
	}
	if len(handoffs) != 1 || len(*sent) != 0 {
		t.Fatalf("handoffs=%d per-signal=%v", len(handoffs), *sent)
	}
	got := map[string]string{}
	for _, update := range handoffs[0] {
		got[update.Name] = update.JSON
	}
	if len(handoffs[0]) != 3 || got["$a"] != `1` || got["$b"] != `{"x":[1,2]}` || got["$c"] != `"s"` {
		t.Fatalf("handoff = %+v", handoffs[0])
	}

	// A snapshot that changes nothing hands over nothing.
	if err := b.SetChangedSharedSignalBatchJSON(`{"$a":1,"$c":"s"}`); err != nil {
		t.Fatal(err)
	}
	if len(handoffs) != 1 {
		t.Fatalf("an unchanged snapshot made a handoff: %+v", handoffs[1:])
	}

	// A write outside a batch still goes out on its own.
	if err := b.SetSharedSignalJSON("$d", "5"); err != nil {
		t.Fatal(err)
	}
	if len(handoffs) != 1 || len(*sent) != 1 || (*sent)[0] != "$d=5" {
		t.Fatalf("single write: handoffs=%d per-signal=%v", len(handoffs), *sent)
	}
}

func TestBatchCallbackRunsAfterTheStoreHasEveryValueAndBeforeIslandsRender(t *testing.T) {
	b := New()
	recorders := map[string]*vm.HostRecorder{}
	b.RegisterIslandHostFactory("probe", func(id string) vm.HostReceiver {
		r := vm.NewHostRecorder()
		recorders[id] = r
		return r
	})
	hydrateBatchReader(t, b, "reader", 4, true)
	recorders["reader"].Reset()
	renders := -1
	calls := 0
	b.SetSharedSignalBatchCallback(func(updates []SignalUpdate) {
		calls++
		renders = len(recorders["reader"].Calls)
		if len(updates) != 4 {
			t.Errorf("updates = %+v", updates)
		}
		for i := range 4 {
			value, ok := b.GetStore().Get(fmt.Sprintf("$batch.%d", i))
			if !ok || value.Int64() != 3 {
				t.Errorf("$batch.%d not stored when the batch left: %v %v", i, value.String(), ok)
			}
		}
		if !b.GetStore().updating {
			t.Error("the update had settled before the batch left")
		}
	})
	if err := b.SetSharedSignalBatchJSON(batchJSON(4, 3)); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || renders != 0 {
		t.Fatalf("batch calls = %d, island renders when it left = %d (want 1 and 0)", calls, renders)
	}
	if len(recorders["reader"].Calls) != 1 {
		t.Fatalf("island renders after the batch = %d, want 1", len(recorders["reader"].Calls))
	}
}

func TestBatchCallbackWritesLeaveOneByOne(t *testing.T) {
	b := New()
	var handoffs int
	sent := recordNotifications(b)
	b.SetSharedSignalBatchCallback(func(updates []SignalUpdate) {
		handoffs++
		// What a browser subscriber does: write another signal at once.
		if err := b.SetSharedSignalJSON("$reply", `"seen"`); err != nil {
			t.Error(err)
		}
	})
	if err := b.SetSharedSignalBatchJSON(`{"$a":1}`); err != nil {
		t.Fatal(err)
	}
	if handoffs != 1 || len(*sent) != 1 || (*sent)[0] != `$reply="seen"` {
		t.Fatalf("handoffs=%d per-signal=%v", handoffs, *sent)
	}
	// The next batch still travels as a batch.
	if err := b.SetSharedSignalBatchJSON(`{"$a":2}`); err != nil {
		t.Fatal(err)
	}
	if handoffs != 2 {
		t.Fatalf("handoffs=%d after the second batch", handoffs)
	}
}

func TestBatchCallbackWriteToABatchSignalReachesTheBrowserBeforeTheBatchDoes(t *testing.T) {
	// A subscriber the batch wakes may write a signal that is in the same batch.
	// That write leaves at once, so it reaches the browser while the batch is
	// still being delivered, and the batch's own text for that signal is then out
	// of date. The runtime does not rewrite the batch; this pins the order so the
	// page can tell which copy is newer (see gosxNotifySharedSignals).
	b := New()
	var order []string
	b.SetSharedSignalCallback(func(name, valueJSON string) { order = append(order, "single "+name+"="+valueJSON) })
	var handed []SignalUpdate
	b.SetSharedSignalBatchCallback(func(updates []SignalUpdate) {
		order = append(order, "batch begins")
		handed = append([]SignalUpdate(nil), updates...)
		if err := b.SetSharedSignalJSON("$b", `"fromA"`); err != nil {
			t.Error(err)
		}
		order = append(order, "batch ends")
	})
	if err := b.SetSharedSignalBatchJSON(`{"$a":1,"$b":"batch"}`); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(order, "; "); got != `batch begins; single $b="fromA"; batch ends` {
		t.Fatalf("order = %s", got)
	}
	texts := map[string]string{}
	for _, update := range handed {
		texts[update.Name] = update.JSON
	}
	if texts["$b"] != `"batch"` || len(handed) != 2 {
		t.Fatalf("the batch handed over %+v, want its own text for $b", handed)
	}
	if stored, _ := b.GetStore().Get("$b"); stored.String() != "fromA" {
		t.Fatalf("the store holds %q, want the later write", stored.String())
	}
}

func TestBatchNestedInsideTheBatchCallbackHasItsOwnHandoff(t *testing.T) {
	b := New()
	var order []string
	depth := 0
	b.SetSharedSignalBatchCallback(func(updates []SignalUpdate) {
		var names []string
		for _, update := range updates {
			names = append(names, update.Name)
		}
		order = append(order, strings.Join(names, ","))
		if depth == 0 {
			depth++
			if err := b.SetSharedSignalBatchJSON(`{"$inner":1}`); err != nil {
				t.Error(err)
			}
		}
	})
	if err := b.SetSharedSignalBatchJSON(`{"$outer":1}`); err != nil {
		t.Fatal(err)
	}
	if strings.Join(order, "|") != "$outer|$inner" {
		t.Fatalf("handoffs = %v", order)
	}
}

func TestBatchCallbackStillSendsWhatWasNotifiedBeforeAnObserverPanics(t *testing.T) {
	b := New()
	var handoffs [][]SignalUpdate
	b.SetSharedSignalBatchCallback(func(updates []SignalUpdate) {
		handoffs = append(handoffs, append([]SignalUpdate(nil), updates...))
	})
	b.SetSharedSignalJSON("$a", "0")
	b.GetStore().Signal("$a", vm.IntVal(0)).Subscribe(func() { panic("observer") })
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		_ = b.SetSharedSignalBatchJSON(`{"$a":1}`)
	}()
	if recovered != "observer" {
		t.Fatalf("recovered = %v", recovered)
	}
	if len(handoffs) != 1 || len(handoffs[0]) != 1 || handoffs[0][0].Name != "$a" || handoffs[0][0].JSON != "1" {
		t.Fatalf("the browser was left behind the store: %+v", handoffs)
	}
	// The bridge is still usable and still batches.
	b.GetStore().Signal("$a", vm.IntVal(0))
	if err := b.SetSharedSignalBatchJSON(`{"$b":2}`); err != nil {
		t.Fatal(err)
	}
	if len(handoffs) != 2 || handoffs[1][0].Name != "$b" {
		t.Fatalf("handoffs after recovery: %+v", handoffs)
	}
}

func TestBatchWithoutCallbacksStillStoresValues(t *testing.T) {
	b := New()
	if err := b.SetSharedSignalBatchJSON(`{"$a":{"x":[1,2]}}`); err != nil {
		t.Fatal(err)
	}
	value, ok := b.GetStore().Get("$a")
	if text, err := json.Marshal(value.ToAny()); !ok || err != nil || string(text) != `{"x":[1,2]}` {
		t.Fatalf("stored %v %v", value.String(), ok)
	}
}

func TestBatchRefusesBlankNamesWithoutApplyingAnything(t *testing.T) {
	// The page used to refuse a blank name before it sent a batch here, and a
	// single write refuses one. The batch must too, and apply none of its members.
	for _, batch := range []string{`{"":1,"$a":2}`, `{"  ":1,"$a":2}`, `{"\t":1,"$a":2}`, `{"$a":2,"\u00a0":1}`} {
		for _, changedOnly := range []bool{false, true} {
			b := New()
			sent := recordNotifications(b)
			err := b.setSharedSignalBatchJSON(batch, changedOnly)
			if err == nil || !strings.Contains(err.Error(), "name required") {
				t.Errorf("%q changedOnly=%v: err = %v", batch, changedOnly, err)
			}
			if _, ok := b.GetStore().Get("$a"); ok || len(*sent) != 0 {
				t.Errorf("%q changedOnly=%v: the batch was partly applied: %v", batch, changedOnly, *sent)
			}
		}
	}
}

// jsTrimmed lists every code point that JavaScript's String.prototype.trim
// removes (Node 26: for each code point c, String.fromCodePoint(c).trim() === "").
var jsTrimmed = []rune{
	0x0009, 0x000A, 0x000B, 0x000C, 0x000D, 0x0020, 0x00A0, 0x1680,
	0x2000, 0x2001, 0x2002, 0x2003, 0x2004, 0x2005, 0x2006, 0x2007, 0x2008, 0x2009, 0x200A,
	0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF,
}

func TestBlankSignalNameUsesTheWhiteSpaceJavaScriptTrims(t *testing.T) {
	trimmed := map[rune]bool{}
	for _, r := range jsTrimmed {
		trimmed[r] = true
	}
	for r := rune(0); r <= 0x10FFFF; r++ {
		if r >= 0xD800 && r <= 0xDFFF {
			continue
		}
		if got := isJSSpace(r); got != trimmed[r] {
			t.Fatalf("isJSSpace(U+%04X) = %v, JavaScript trims it: %v", r, got, trimmed[r])
		}
	}
	// U+0085 is white space to Go and not to JavaScript, and U+FEFF is the reverse.
	for name, blank := range map[string]bool{
		"": true, " ": true, "\t\n": true, "\u00a0\u3000": true, "\ufeff": true, "\u2028x": false,
		"\u0085": false, "\u200b": false, "\u180e": false, "$a": false, " $a ": false,
	} {
		if got := blankSignalName(name); got != blank {
			t.Errorf("blankSignalName(%q) = %v, want %v", name, got, blank)
		}
	}
}

func TestBatchRefusesTheNamesThePageRefuses(t *testing.T) {
	for _, batch := range []string{`{"\ufeff":1,"$a":2}`, `{"\u2003":1,"$a":2}`} {
		err := New().setSharedSignalBatchJSON(batch, false)
		if err == nil || !strings.Contains(err.Error(), "name required") {
			t.Errorf("%q: err = %v", batch, err)
		}
	}
	// A name the page accepts is stored.
	b := New()
	if err := b.SetSharedSignalBatchJSON(`{"\u0085":1}`); err != nil {
		t.Fatalf("U+0085 is not blank in JavaScript: %v", err)
	}
	if _, ok := b.GetStore().Get("\u0085"); !ok {
		t.Error("a name of U+0085 was not stored")
	}
}

func TestMarshalValueReusesItsBufferWithoutLettingValuesShareIt(t *testing.T) {
	b := New()
	first, err := b.marshalValue(vm.ObjectVal(map[string]vm.Value{"a": vm.IntVal(1)}))
	if err != nil {
		t.Fatal(err)
	}
	second, err := b.marshalValue(vm.StringVal("a long second value that overwrites the buffer"))
	if err != nil {
		t.Fatal(err)
	}
	if first != `{"a":1}` || second != `"a long second value that overwrites the buffer"` {
		t.Fatalf("first = %q, second = %q", first, second)
	}
	if _, err := b.marshalValue(vm.FloatVal(posInf())); err == nil {
		t.Fatal("a float that is not finite was encoded")
	}
	if again, err := b.marshalValue(vm.IntVal(3)); err != nil || again != "3" {
		t.Fatalf("after a refusal: %q, %v", again, err)
	}
}

func posInf() float64 {
	var zero float64
	return 1 / zero
}
