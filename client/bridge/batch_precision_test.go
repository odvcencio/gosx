package bridge

import (
	"fmt"
	"testing"

	runtimewasm "m31labs.dev/gosx/client/runtime/wasm"
	"m31labs.dev/gosx/client/vm"
)

func TestSharedSignalBatchIntegerPrecision(t *testing.T) {
	for _, changedOnly := range []bool{false, true} {
		for _, number := range []string{
			"0", "1", "-1", "9007199254740991", "9007199254740992",
			"9007199254740993", "9007199254740994", "-9007199254740993",
			"-9007199254740994", "-9007199254740995", "9223372036854775807",
			"9223372036854775806", "-9223372036854775808", "-9223372036854775807",
		} {
			for _, raw := range []string{number, fmt.Sprintf(`{"id":%s,"items":[%s,{"version":%s}],"weight":1.25}`, number, number, number)} {
				t.Run(fmt.Sprintf("changed=%v/%s", changedOnly, raw), func(t *testing.T) {
					b := New()
					var events []string
					b.SetSharedSignalCallback(func(name, value string) {
						if name != "$value" {
							t.Fatalf("unexpected signal %s", name)
						}
						events = append(events, value)
					})
					payload := `{"$value":` + raw + `}`
					for range 2 {
						if err := b.setSharedSignalBatchJSON(payload, changedOnly); err != nil {
							t.Fatal(err)
						}
					}
					got, err := b.GetSharedSignalJSON("$value")
					if err != nil || got != raw {
						t.Fatalf("stored JSON = %s (%v), want %s", got, err, raw)
					}
					wantEvents := 2
					if changedOnly {
						wantEvents = 1
					}
					if len(events) != wantEvents {
						t.Fatalf("notifications = %d, want %d", len(events), wantEvents)
					}
					for _, event := range events {
						if event != raw {
							t.Fatalf("notification = %s, want %s", event, raw)
						}
					}
					// Both bridge entry points must produce the same typed values.
					single := New()
					if err := single.SetSharedSignalJSON("$value", raw); err != nil {
						t.Fatal(err)
					}
					left, _ := single.store.Get("$value")
					right, _ := b.store.Get("$value")
					if !vm.EqualJSON(left, right) {
						t.Fatal("single and batch setters disagree on value types")
					}
				})
			}
		}
	}
}

func TestSharedSignalBatchDistinctIntegersReachIslandMailbox(t *testing.T) {
	for _, changedOnly := range []bool{false, true} {
		t.Run(fmt.Sprint(changedOnly), func(t *testing.T) {
			b := New()
			prog := sharedDispatchProgram("ExactSharedID", "noop", nil, nil)
			if err := b.HydrateIsland("reader", prog.Name, `{}`, encodeDispatchProgram(t, prog), "json"); err != nil {
				t.Fatal(err)
			}
			var texts []string
			b.SetPatchMailboxCallback(func(_ string, mailbox []byte) {
				_, id, patches, err := runtimewasm.DecodePatchMailbox(mailbox)
				if err != nil || id != "reader" || len(patches) != 1 || patches[0].Kind != vm.PatchSetText {
					t.Fatalf("unexpected mailbox: id=%q patches=%+v error=%v", id, patches, err)
				}
				texts = append(texts, patches[0].Text)
			})
			for _, number := range []string{
				"9007199254740992", "9007199254740993", "9007199254740994", "9007199254740993",
				"9223372036854775807", "9223372036854775806", "-9223372036854775808", "-9223372036854775807",
			} {
				before := len(texts)
				payload := `{"$shared.value":` + number + `}`
				if err := b.setSharedSignalBatchJSON(payload, changedOnly); err != nil {
					t.Fatal(err)
				}
				if len(texts) != before+1 || texts[before] != number {
					t.Fatalf("island text updates = %q, want next %s", texts, number)
				}
				// Repeating a snapshot must neither suppress the next distinct ID
				// nor create a redundant DOM patch for an unchanged display.
				if err := b.setSharedSignalBatchJSON(payload, changedOnly); err != nil {
					t.Fatal(err)
				}
				if len(texts) != before+1 {
					t.Fatal("duplicate ID produced a DOM patch")
				}
			}
		})
	}
}

func TestSharedSignalBatchDecoderCompatibility(t *testing.T) {
	for _, changedOnly := range []bool{false, true} {
		b := New()
		for _, raw := range []string{"", `{}`, `null`, " \n null \t "} {
			if err := b.setSharedSignalBatchJSON(raw, changedOnly); err != nil {
				t.Fatalf("empty batch %q: %v", raw, err)
			}
		}
		if err := b.SetSharedSignalJSON("$value", `17`); err != nil {
			t.Fatal(err)
		}
		for _, raw := range []string{
			`[]`, `[1]`, `1`, `true`, `"text"`, `{} {}`, " \t ",
			`{"$value":99,"":1}`, `{"$value":99,"$nested":{"bad":1e400}}`,
			`{"$value":99,"$nested":[{"bad":-1e400}]}`,
		} {
			if err := b.setSharedSignalBatchJSON(raw, changedOnly); err == nil {
				t.Fatalf("invalid batch %q accepted", raw)
			}
			if got, err := b.GetSharedSignalJSON("$value"); err != nil || got != "17" {
				t.Fatalf("invalid batch changed live state: %s (%v)", got, err)
			}
		}
		// JSON fractions and numbers outside int64 retain the existing VM
		// conversion behavior; only in-range integer tokens require exactness.
		for _, raw := range []string{`1.25`, `1e20`, `18446744073709551616`, `1e3`, `null`, `true`, `"9007199254740993"`} {
			want, err := vm.ValueFromJSON(raw)
			if err != nil {
				t.Fatal(err)
			}
			if err := b.setSharedSignalBatchJSON(`{"$value":`+raw+`}`, changedOnly); err != nil {
				t.Fatal(err)
			}
			got, _ := b.store.Get("$value")
			if !vm.EqualJSON(got, want) {
				t.Fatalf("conversion of %s changed", raw)
			}
		}
	}
}
