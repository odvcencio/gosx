package flow

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

type fakePauser struct{ calls []bool }

func (p *fakePauser) SetPaused(v bool) { p.calls = append(p.calls, v) }

type fakeInput struct{ last []string }

func (i *fakeInput) SetStack(n ...string) { i.last = append([]string(nil), n...) }

func recorder(log *[]string, name string) State {
	rec := func(what string) { *log = append(*log, name+"."+what) }
	return State{
		Name:    name,
		Enter:   func(f *Flow, from string) { rec("enter<" + from) },
		Exit:    func(f *Flow, to string) { rec("exit>" + to) },
		Suspend: func(f *Flow) { rec("suspend") },
		Resume:  func(f *Flow) { rec("resume") },
	}
}

func TestHookOrder(t *testing.T) {
	var log []string
	f := New(Config{}, recorder(&log, "title"), recorder(&log, "play"), recorder(&log, "pause"))
	_ = f.Push("title")
	_ = f.Replace("play")
	_ = f.Push("pause")
	_ = f.Pop()
	_ = f.Reset("title")
	want := []string{
		"title.enter<",
		"title.exit>play", "play.enter<title",
		"play.suspend", "pause.enter<play",
		"pause.exit>play", "play.resume",
		"play.exit>title", "title.enter<play",
	}
	if !reflect.DeepEqual(log, want) {
		t.Fatalf("got  %v\nwant %v", log, want)
	}
}

func TestPauseAndInputContexts(t *testing.T) {
	p, in := &fakePauser{}, &fakeInput{}
	var changes [][]string
	f := New(Config{Pauser: p, Input: in, OnChange: func(s []string) { changes = append(changes, s) }},
		State{Name: Play, Input: "gameplay", PauseOnBlur: true},
		State{Name: Pause, Input: "menu", Pause: true},
		State{Name: Title, Input: "menu", Pause: true},
	)
	_ = f.Push(Play)
	if f.Paused() || !reflect.DeepEqual(in.last, []string{"gameplay"}) {
		t.Fatal(f.Paused(), in.last)
	}
	_ = f.Push(Pause)
	if !f.Paused() || !reflect.DeepEqual(in.last, []string{"gameplay", "menu"}) {
		t.Fatal(f.Paused(), in.last)
	}
	if p.calls[len(p.calls)-1] != true {
		t.Fatal("runtime not paused")
	}
	_ = f.Pop()
	if f.Paused() || p.calls[len(p.calls)-1] != false {
		t.Fatal("runtime not resumed")
	}
	if len(changes) != 3 || !reflect.DeepEqual(changes[1], []string{Play, Pause}) {
		t.Fatalf("changes %v", changes)
	}
}

func TestBlurAndHidden(t *testing.T) {
	p := &fakePauser{}
	f := New(Config{Pauser: p},
		State{Name: Play, PauseOnBlur: true},
		State{Name: Pause, Pause: true},
		State{Name: Title},
	)
	_ = f.Push(Title)
	f.Blur()
	if f.Has(Pause) {
		t.Fatal("title must not pause on blur")
	}
	_ = f.Reset(Play)
	f.Blur()
	if !f.Is(Pause) || !f.Paused() {
		t.Fatal("blur should push pause")
	}
	f.Blur()
	if len(f.Stack()) != 2 {
		t.Fatal("blur twice pushed twice", f.Stack())
	}
	_ = f.Pop()
	f.Hidden(true)
	if !f.Is(Pause) || !f.Paused() {
		t.Fatal("hidden should pause")
	}
	_ = f.Pop()
	if !f.Paused() {
		t.Fatal("still hidden: runtime stays paused")
	}
	f.Hidden(false)
	if f.Paused() {
		t.Fatal("visible again, nothing on stack asks for pause")
	}
}

func TestReentrantTransitionRunsAfterHook(t *testing.T) {
	var log []string
	f := New(Config{})
	f.Register(State{Name: "a", Enter: func(f *Flow, from string) {
		log = append(log, "a.enter")
		_ = f.Replace("b") // requested from a hook
		log = append(log, "a.enter.done")
	}})
	f.Register(State{Name: "b", Enter: func(*Flow, string) { log = append(log, "b.enter") }})
	_ = f.Push("a")
	if !reflect.DeepEqual(log, []string{"a.enter", "a.enter.done", "b.enter"}) || f.Current() != "b" {
		t.Fatalf("%v %v", log, f.Stack())
	}
}

func TestErrorsAndUpdate(t *testing.T) {
	f := New(Config{})
	if err := f.Push("nope"); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatal(err)
	}
	if f.Pop() != ErrEmptyStack {
		t.Fatal("pop empty")
	}
	var total time.Duration
	f.Register(State{Name: "a", Update: func(_ *Flow, dt time.Duration) { total += dt }})
	f.Register(State{Name: "b"})
	_ = f.Push("a")
	f.Update(time.Second)
	_ = f.Push("b")
	f.Update(time.Second) // a is not on top
	if total != time.Second {
		t.Fatal(total)
	}
}
