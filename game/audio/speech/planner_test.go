package speech

import (
	"strings"
	"testing"
)

func TestCompileInternsFragments(t *testing.T) {
	c, lib := compileRacing(t)
	have := map[string]string{}
	for _, f := range c.Fragments() {
		have[f.Key] = f.Text
	}
	for key, text := range map[string]string{
		"pool/driver/reyes":         "Reyes",
		"pool/driver/okafor":        "okafor",
		"lit/takes the lead":        "takes the lead",
		"lit/lap":                   "lap",
		"lit/of":                    "of",
		"num/seven":                 "seven",
		"num/forty":                 "forty",
		"pool/corner/hairpin":       "hairpin",
		"lit/is closing in":         "is closing in",
		"lit/oh no!":                "oh no!",
		"lit/is in the wall at the": "is in the wall at the",
	} {
		if have[key] != text {
			t.Errorf("fragment %q = %q, want %q", key, have[key], text)
		}
	}
	if _, ok := have["num/hundred"]; ok {
		t.Error("hundred is interned although number_max is 99")
	}
	if len(lib.Missing()) != lib.Len() {
		t.Errorf("a fresh library should miss every key: missing %d of %d", len(lib.Missing()), lib.Len())
	}
}

func TestCompileErrors(t *testing.T) {
	cases := map[string]string{
		"unknown pool": `{"phrases":[{"id":"a","event":"e","variants":[{"say":"{x:nope}"}]}]}`,
		"unclosed":     `{"phrases":[{"id":"a","event":"e","variants":[{"say":"hi {x"}]}]}`,
		"dup id":       `{"phrases":[{"id":"a","event":"e","variants":[{"say":"hi"}]},{"id":"a","event":"e","variants":[{"say":"yo"}]}]}`,
		"no variants":  `{"phrases":[{"id":"a","event":"e","variants":[]}]}`,
		"bad op":       `{"phrases":[{"id":"a","event":"e","when":[{"field":"f","op":"~","value":1}],"variants":[{"say":"hi"}]}]}`,
		"string gt":    `{"phrases":[{"id":"a","event":"e","when":[{"field":"f","op":">","value":"x"}],"variants":[{"say":"hi"}]}]}`,
		"empty say":    `{"phrases":[{"id":"a","event":"e","variants":[{"say":"  "}]}]}`,
		"number range": `{"number_max":5000,"phrases":[{"id":"a","event":"e","variants":[{"say":"hi"}]}]}`,
		"pause range":  `{"phrases":[{"id":"a","event":"e","variants":[{"say":"hi {~99999}"}]}]}`,
		"neg weight":   `{"phrases":[{"id":"a","event":"e","weight":-1,"variants":[{"say":"hi"}]}]}`,
	}
	for name, src := range cases {
		g, err := ParseGrammar([]byte(src))
		if err == nil {
			_, err = g.Compile(NewLibrary(48000))
		}
		if err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if _, err := ParseGrammar([]byte(`{"phrases":[],"bogus":1}`)); err == nil {
		t.Error("unknown grammar field accepted")
	}
}

func TestNumberBinding(t *testing.T) {
	g, _ := ParseGrammar([]byte(`{"number_max":999,"phrases":[{"id":"n","event":"n","variants":[{"say":"{n:number}"}]}]}`))
	lib := NewLibrary(48000)
	c, err := g.Compile(lib)
	if err != nil {
		t.Fatal(err)
	}
	p := NewPlanner(c, PlannerConfig{Seed: 1})
	clock := 0.0
	want := map[int]string{
		0: "zero", 7: "seven", 12: "twelve", 19: "nineteen", 20: "twenty", 47: "forty seven",
		100: "one hundred", 101: "one hundred one", 315: "three hundred fifteen", 999: "nine hundred ninety nine",
	}
	for n, words := range want {
		var ev Event
		ev.Reset("n")
		ev.Num("n", float64(n))
		clock += 100
		d := p.Submit(&ev, clock, Playing{})
		if d.Action != Play {
			t.Fatalf("%d: action %v reason %v", n, d.Action, d.Reason)
		}
		got := strings.ReplaceAll(strings.ReplaceAll(say(lib, &d.Utt), "num/", ""), "|", " ")
		if got != words {
			t.Errorf("%d spoken as %q, want %q", n, got, words)
		}
	}
	for _, bad := range []float64{-1, 1000, 2.5} {
		var ev Event
		ev.Reset("n")
		ev.Num("n", bad)
		if d := p.Submit(&ev, 1e6, Playing{}); d.Action != Drop || d.Reason != ReasonUnbound {
			t.Errorf("%v: got %v/%v, want Drop/Unbound", bad, d.Action, d.Reason)
		}
	}
}

func TestPoolBindingByNameAndNumber(t *testing.T) {
	c, lib := compileRacing(t)
	p := NewPlanner(c, PlannerConfig{Seed: 3})
	var ev Event
	ev.Reset("crash")
	ev.Str("driver", "vance").Str("corner", "hairpin")
	d := p.Submit(&ev, 0, Playing{})
	if d.Action != Play {
		t.Fatalf("action %v %v", d.Action, d.Reason)
	}
	s := say(lib, &d.Utt)
	if !strings.Contains(s, "pool/driver/vance") || !strings.Contains(s, "pool/corner/hairpin") {
		t.Errorf("utterance %q lacks the bound names", s)
	}

	// An unknown name leaves every crash variant unbound.
	ev.Reset("crash")
	ev.Str("driver", "nobody").Str("corner", "hairpin")
	if d := p.Submit(&ev, 100, Playing{}); d.Action != Drop || d.Reason != ReasonUnbound {
		t.Errorf("unknown name: %v/%v, want Drop/Unbound", d.Action, d.Reason)
	}

	// A driver with no name is called by number when a variant allows it.
	ev.Reset("lead_change")
	ev.Num("num", 47)
	got := map[string]bool{}
	for i := 0; i < 20; i++ {
		d := p.Submit(&ev, 1000+float64(i)*10, Playing{})
		if d.Action != Play {
			t.Fatalf("lead change by number: %v %v", d.Action, d.Reason)
		}
		got[say(lib, &d.Utt)] = true
	}
	if len(got) != 1 || !got["num/forty|num/seven|lit/takes the lead"] {
		t.Errorf("by-number lines = %v", got)
	}
}

func TestConditionsPickPhrase(t *testing.T) {
	c, lib := compileRacing(t)
	p := NewPlanner(c, PlannerConfig{Seed: 1})
	var ev Event
	ev.Reset("gap")
	ev.Str("driver", "okafor").Num("gap", 0.4)
	d := p.Submit(&ev, 0, Playing{})
	if s := say(lib, &d.Utt); !strings.HasSuffix(s, "lit/is closing in") {
		t.Errorf("close gap said %q", s)
	}
	ev.Reset("gap")
	ev.Str("driver", "okafor").Num("gap", 3)
	d = p.Submit(&ev, 100, Playing{})
	if s := say(lib, &d.Utt); !strings.HasSuffix(s, "lit/is pulling away") {
		t.Errorf("far gap said %q", s)
	}
	// Context supplies fields the event lacks.
	ev.Reset("gap")
	ev.Str("driver", "okafor")
	p.Context().Num("gap", 0.2)
	d = p.Submit(&ev, 200, Playing{})
	if s := say(lib, &d.Utt); !strings.HasSuffix(s, "lit/is closing in") {
		t.Errorf("context gap said %q", s)
	}
	// The event field shadows the context.
	ev.Num("gap", 9)
	d = p.Submit(&ev, 300, Playing{})
	if s := say(lib, &d.Utt); !strings.HasSuffix(s, "lit/is pulling away") {
		t.Errorf("shadowed gap said %q", s)
	}
}

func TestVariantCondition(t *testing.T) {
	c, lib := compileRacing(t)
	p := NewPlanner(c, PlannerConfig{Seed: 5})
	seenPause := false
	for i := 0; i < 40; i++ {
		var ev Event
		ev.Reset("lap")
		ev.Num("lap", 3).Num("total", 20)
		d := p.Submit(&ev, float64(i)*100, Playing{})
		if strings.Contains(say(lib, &d.Utt), "that is") {
			t.Fatal("lap < 10 chose the lap >= 10 variant")
		}
	}
	for i := 0; i < 40; i++ {
		var ev Event
		ev.Reset("lap")
		ev.Num("lap", 12).Num("total", 20)
		d := p.Submit(&ev, 10000+float64(i)*100, Playing{})
		if d.Utt.Frags[0].GapMs == 200 {
			seenPause = true
		}
	}
	if !seenPause {
		t.Error("the {~200} pause never reached the utterance")
	}
}

func TestCooldowns(t *testing.T) {
	c, _ := compileRacing(t)
	p := NewPlanner(c, PlannerConfig{Seed: 2})
	lap := func(now float64) Decision {
		var ev Event
		ev.Reset("lap")
		ev.Num("lap", 2).Num("total", 9)
		return p.Submit(&ev, now, Playing{})
	}
	if d := lap(0); d.Action != Play {
		t.Fatalf("first lap call: %v", d.Action)
	}
	if d := lap(7.9); d.Action != Drop || d.Reason != ReasonCooldown {
		t.Errorf("phrase cooldown: %v/%v", d.Action, d.Reason)
	}
	if d := lap(8.0); d.Action != Play {
		t.Errorf("after phrase cooldown: %v/%v", d.Action, d.Reason)
	}

	// Category cooldown: gap.close shares "chatter" (4 s) with lap.call.
	var ev Event
	ev.Reset("gap")
	ev.Str("driver", "vance").Num("gap", 1)
	if d := p.Submit(&ev, 10, Playing{}); d.Action != Drop || d.Reason != ReasonCooldown {
		t.Errorf("category cooldown: %v/%v", d.Action, d.Reason)
	}
	if d := p.Submit(&ev, 12.1, Playing{}); d.Action != Play {
		t.Errorf("after category cooldown: %v/%v", d.Action, d.Reason)
	}
}

func TestRecentHistoryAvoidsRepeats(t *testing.T) {
	c, lib := compileRacing(t)
	p := NewPlanner(c, PlannerConfig{Seed: 11, History: 1})
	prev := ""
	for i := 0; i < 60; i++ {
		var ev Event
		ev.Reset("lead_change")
		ev.Str("driver", "okafor")
		d := p.Submit(&ev, float64(i)*10, Playing{})
		if d.Action != Play {
			t.Fatalf("%d: %v %v", i, d.Action, d.Reason)
		}
		s := say(lib, &d.Utt)
		if s == prev {
			t.Fatalf("line %d repeated %q with a one-line history window", i, s)
		}
		prev = s
	}
}

func TestHistoryIgnoredWhenOnlyChoice(t *testing.T) {
	c, _ := compileRacing(t)
	p := NewPlanner(c, PlannerConfig{Seed: 1, History: 4})
	for i := 0; i < 5; i++ {
		var ev Event
		ev.Reset("gap")
		ev.Str("driver", "vance").Num("gap", 1)
		if d := p.Submit(&ev, float64(i)*100, Playing{}); d.Action != Play {
			t.Fatalf("%d: sole candidate was refused: %v/%v", i, d.Action, d.Reason)
		}
	}
}

func TestDeterministicFromSeed(t *testing.T) {
	run := func(seed uint64) string {
		c, lib := compileRacing(t)
		p := NewPlanner(c, PlannerConfig{Seed: seed})
		var out []string
		for i := 0; i < 30; i++ {
			var ev Event
			ev.Reset("lead_change")
			ev.Str("driver", []string{"okafor", "vance", "reyes", "lindqvist"}[i%4])
			d := p.Submit(&ev, float64(i)*10, Playing{})
			out = append(out, say(lib, &d.Utt))
		}
		return strings.Join(out, "\n")
	}
	if run(7) != run(7) {
		t.Error("same seed gave different speech")
	}
	if run(7) == run(8) {
		t.Error("different seeds gave identical speech")
	}
}

func TestInterruptRules(t *testing.T) {
	c, _ := compileRacing(t)
	p := NewPlanner(c, PlannerConfig{Seed: 1})
	crash := func(now float64, pl Playing) Decision {
		var ev Event
		ev.Reset("crash")
		ev.Str("driver", "vance").Str("corner", "one")
		return p.Submit(&ev, now, pl)
	}
	lead := func(now float64, pl Playing) Decision {
		var ev Event
		ev.Reset("lead_change")
		ev.Str("driver", "vance")
		return p.Submit(&ev, now, pl)
	}
	// A crash (9) cuts an interruptible lap call (1).
	if d := crash(0, Playing{Active: true, Priority: 1, Interruptible: true}); d.Action != Interrupt {
		t.Errorf("crash over chatter: %v/%v", d.Action, d.Reason)
	}
	// It cannot cut an uninterruptible line.
	if d := crash(100, Playing{Active: true, Priority: 1, Interruptible: false}); d.Action != Drop || d.Reason != ReasonBusy {
		t.Errorf("crash over uninterruptible: %v/%v", d.Action, d.Reason)
	}
	// A lead change (5) cannot cut an uninterruptible crash call (9) and has max_wait, so it queues.
	if d := lead(200, Playing{Active: true, Priority: 9, Interruptible: false}); d.Action != Queued {
		t.Errorf("lead behind crash: %v/%v", d.Action, d.Reason)
	}
	if p.Pending() != 1 {
		t.Errorf("pending = %d, want 1", p.Pending())
	}
	// A second lead change lands in the cooldown (3 s) and drops.
	if d := lead(201, Playing{Active: true, Priority: 9, Interruptible: false}); d.Action != Drop || d.Reason != ReasonCooldown {
		t.Errorf("repeat lead: %v/%v", d.Action, d.Reason)
	}
	// The voice is still busy: nothing comes out of the queue.
	if d := p.Poll(202, Playing{Active: true, Priority: 9}); d.Action != Drop {
		t.Errorf("poll while busy: %v", d.Action)
	}
	// When the voice frees up, the line plays.
	if d := p.Poll(202.5, Playing{}); d.Action != Play {
		t.Errorf("poll when idle: %v", d.Action)
	}
	if p.Pending() != 0 {
		t.Errorf("pending after poll = %d", p.Pending())
	}
	// A queued line that waits too long expires.
	lead(300, Playing{Active: true, Priority: 9})
	if d := p.Poll(304, Playing{}); d.Action != Drop {
		t.Errorf("expired line played: %v", d.Action)
	}
}

func TestEventPriorityOverride(t *testing.T) {
	c, _ := compileRacing(t)
	p := NewPlanner(c, PlannerConfig{Seed: 1})
	var ev Event
	ev.Reset("lap")
	ev.Num("lap", 2).Num("total", 9)
	ev.Priority = 20
	d := p.Submit(&ev, 0, Playing{Active: true, Priority: 9, Interruptible: true})
	if d.Action != Interrupt || d.Utt.Priority != 20 {
		t.Errorf("override: %v priority %d", d.Action, d.Utt.Priority)
	}
}

func TestQueueReplacesLowest(t *testing.T) {
	g, _ := ParseGrammar([]byte(`{"phrases":[
	  {"id":"lo","event":"lo","priority":1,"max_wait":9,"variants":[{"say":"low"}]},
	  {"id":"hi","event":"hi","priority":4,"max_wait":9,"variants":[{"say":"high"}]}]}`))
	c, err := g.Compile(NewLibrary(48000))
	if err != nil {
		t.Fatal(err)
	}
	p := NewPlanner(c, PlannerConfig{QueueSize: 1})
	busy := Playing{Active: true, Priority: 50}
	if d := p.Submit(&Event{Kind: "lo"}, 0, busy); d.Action != Queued {
		t.Fatalf("lo: %v", d.Action)
	}
	if d := p.Submit(&Event{Kind: "lo"}, 1, busy); d.Action != Drop || d.Reason != ReasonQueueFull {
		t.Errorf("second lo: %v/%v", d.Action, d.Reason)
	}
	if d := p.Submit(&Event{Kind: "hi"}, 2, busy); d.Action != Queued {
		t.Errorf("hi should replace lo: %v", d.Action)
	}
	if d := p.Poll(3, Playing{}); d.Action != Play || d.Utt.Priority != 4 {
		t.Errorf("poll: %v priority %d", d.Action, d.Utt.Priority)
	}
}

func TestSubmitDoesNotAllocate(t *testing.T) {
	c, _ := compileRacing(t)
	p := NewPlanner(c, PlannerConfig{Seed: 4})
	names := [...]string{"okafor", "vance", "reyes", "lindqvist"}
	i := 0
	allocs := testing.AllocsPerRun(500, func() {
		var ev Event
		ev.Reset("lead_change")
		ev.Str("driver", names[i%4])
		i++
		p.Submit(&ev, float64(i)*10, Playing{})
		ev.Reset("lap")
		ev.Num("lap", float64(i%90)).Num("total", 99)
		p.Submit(&ev, float64(i)*10, Playing{Active: true, Priority: 2})
		p.Poll(float64(i)*10, Playing{})
	})
	if allocs != 0 {
		t.Errorf("Submit allocates %v times per run", allocs)
	}
}
