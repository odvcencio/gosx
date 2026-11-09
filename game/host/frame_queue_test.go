package host

import "testing"

func TestFrameQueueRunsRequestsInOrderAndSkipsCancelled(t *testing.T) {
	var q frameQueue
	var ran []int
	a := q.add(func(float64) { ran = append(ran, 1) })
	b := q.add(func(float64) { ran = append(ran, 2) })
	c := q.add(func(float64) { ran = append(ran, 3) })
	if a == 0 || a == b || b == c {
		t.Fatalf("handles %d %d %d must be distinct and non-zero", a, b, c)
	}
	q.cancel(b)
	q.begin()
	if !q.empty() {
		t.Fatal("begin must move every waiting request into the batch")
	}
	cb, _ := q.next()
	cb(0)
	q.cancel(c) // cancelled while the batch fires
	late := q.add(func(float64) { ran = append(ran, 4) })
	if _, ok := q.next(); ok {
		t.Fatal("a cancelled request in the firing batch ran")
	}
	if len(ran) != 1 || ran[0] != 1 || q.empty() {
		t.Fatalf("ran %v; a request made during a batch must wait", ran)
	}
	q.begin()
	cb, _ = q.next()
	cb(0)
	if len(ran) != 2 || ran[1] != 4 || late == c {
		t.Fatalf("ran %v", ran)
	}
}

func TestFrameQueueSteadyStateAllocatesNothing(t *testing.T) {
	var q frameQueue
	fn := func(float64) {}
	for i := 0; i < 4; i++ {
		q.add(fn)
		q.begin()
		q.next()
	}
	allocs := testing.AllocsPerRun(1000, func() {
		h := q.add(fn)
		q.add(fn)
		q.cancel(h)
		q.begin()
		for cb, ok := q.next(); ok; cb, ok = q.next() {
			cb(0)
		}
	})
	if allocs != 0 {
		t.Fatalf("frame queue allocated %v times per frame", allocs)
	}
}
