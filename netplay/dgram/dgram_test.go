package dgram

import (
	"runtime"
	"sync"
	"testing"
)

func TestQueueFIFOAndCopy(t *testing.T) {
	q := NewQueue(4)
	src := []byte{1, 2, 3}
	if !q.Push(src) {
		t.Fatal("push failed")
	}
	src[0] = 9 // Push must have copied
	q.Push([]byte{4})
	buf := make([]byte, 16)
	n, ok := q.Pop(buf)
	if !ok || n != 3 || buf[0] != 1 {
		t.Fatalf("got %d %v %v", n, ok, buf[:n])
	}
	n, ok = q.Pop(buf)
	if !ok || n != 1 || buf[0] != 4 {
		t.Fatalf("got %d %v", n, ok)
	}
	if _, ok = q.Pop(buf); ok {
		t.Fatal("empty queue returned a datagram")
	}
}

func TestQueueDropsWhenFullAndOversize(t *testing.T) {
	q := NewQueue(2)
	q.Push([]byte{1})
	q.Push([]byte{2})
	if q.Push([]byte{3}) {
		t.Fatal("full queue accepted a datagram")
	}
	if q.Push(make([]byte, MaxSize+1)) {
		t.Fatal("oversize datagram accepted")
	}
	if q.Dropped() != 2 {
		t.Fatalf("dropped %d, want 2", q.Dropped())
	}
	buf := make([]byte, 4)
	q.Pop(buf)
	if !q.Push([]byte{5}) {
		t.Fatal("push after pop failed")
	}
}

func TestQueueTruncatesToBuffer(t *testing.T) {
	q := NewQueue(2)
	q.Push([]byte{1, 2, 3, 4})
	n, ok := q.Pop(make([]byte, 2))
	if !ok || n != 2 {
		t.Fatalf("got %d %v", n, ok)
	}
}

func TestQueueConcurrent(t *testing.T) {
	q := NewQueue(64)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				q.Push([]byte{byte(j)})
				if j%100 == 0 {
					runtime.Gosched()
				}
			}
		}()
	}
	done := make(chan struct{})
	go func() {
		buf := make([]byte, 8)
		for {
			select {
			case <-done:
				return
			default:
				q.Pop(buf)
				runtime.Gosched() // js/wasm has one thread and no preemption
			}
		}
	}()
	wg.Wait()
	close(done)
}

func TestPopAllocatesNothing(t *testing.T) {
	q := NewQueue(4)
	buf := make([]byte, 8)
	p := []byte{1, 2, 3}
	if a := testing.AllocsPerRun(100, func() { q.Push(p); q.Pop(buf) }); a != 0 {
		t.Fatalf("%v allocs per push/pop", a)
	}
}
