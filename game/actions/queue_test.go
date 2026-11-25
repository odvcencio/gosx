package actions

import "testing"

func TestQueueTakesOneCommandPerStepInOrder(t *testing.T) {
	var q Queue[string]
	q.Push("pump-fake")
	q.Push("throwaway")
	for _, want := range []string{"pump-fake", "throwaway", ""} {
		got, ok := q.Take()
		if got != want || ok != (want != "") {
			t.Fatalf("Take = %q, %v; want %q", got, ok, want)
		}
	}
}

func TestQueueRepeatIsOnePress(t *testing.T) {
	var q Queue[string]
	q.Push("spin")
	q.Push("juke-left")
	if q.Push("spin") || q.Push("juke-left") {
		t.Fatal("a command that already waits was queued again")
	}
	if q.Len() != 2 {
		t.Fatalf("Len = %d, want 2", q.Len())
	}
	q.Take()
	// Once taken, the same command is a new press.
	if !q.Push("spin") {
		t.Fatal("a command taken by a step could not be pressed again")
	}
	if got, _ := q.Take(); got != "juke-left" {
		t.Fatalf("Take = %q, want juke-left", got)
	}
	if got, _ := q.Take(); got != "spin" {
		t.Fatalf("Take = %q, want spin", got)
	}
}

func TestQueueDropsTheZeroValueAndBeyondTheLimit(t *testing.T) {
	q := Queue[string]{Limit: 2}
	if q.Push("") {
		t.Fatal("the zero value was queued")
	}
	q.Push("a")
	q.Push("b")
	if q.Push("c") {
		t.Fatal("a push beyond Limit was queued")
	}
	var d Queue[int]
	for i := 1; i <= DefaultQueueLimit+1; i++ {
		d.Push(i)
	}
	if d.Len() != DefaultQueueLimit {
		t.Fatalf("zero Limit holds %d, want %d", d.Len(), DefaultQueueLimit)
	}
}

func TestQueueNextAndClear(t *testing.T) {
	var q Queue[string]
	if _, ok := q.Next(); ok {
		t.Fatal("Next on an empty queue")
	}
	q.Push("a")
	q.Push("b")
	if v, ok := q.Next(); v != "a" || !ok || q.Len() != 2 {
		t.Fatalf("Next = %q, %v with Len %d; want a, true, 2", v, ok, q.Len())
	}
	q.Clear()
	if _, ok := q.Take(); ok || q.Len() != 0 {
		t.Fatal("Clear left a command")
	}
	if !q.Push("a") {
		t.Fatal("a cleared command could not be pressed again")
	}
}
