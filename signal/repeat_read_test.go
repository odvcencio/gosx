//go:build !tinygo || !wasm

package signal

import (
	"sync"
	"testing"
)

func TestRepeatedReadStillBelongsToTheInnermostWindow(t *testing.T) {
	shared, innerOnly := New(1), New(2)
	var inner []dependency
	_, outer := trackDependencies(func() int {
		shared.Get()
		_, inner = trackDependencies(func() int {
			shared.Get()
			shared.Set(3)
			return shared.Get() + innerOnly.Get()
		})
		return shared.Get()
	})
	if len(outer) != 1 || outer[0].source != shared || outer[0].revision != 0 {
		t.Fatalf("outer window lost its first read: %+v", outer)
	}
	if len(inner) != 2 || inner[0].source != shared || inner[0].revision != 0 || inner[1].source != innerOnly {
		t.Fatalf("inner window did not collect its own reads: %+v", inner)
	}
}

func TestRepeatedReadDoesNotHideAnotherGoroutinesFirstRead(t *testing.T) {
	shared, afterClose := New(1), New(2)
	firstOpen, secondRead, firstClosed := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var first, second []dependency
	var done sync.WaitGroup
	done.Add(2)
	go func() {
		defer done.Done()
		_, first = trackDependencies(func() int {
			close(firstOpen)
			<-secondRead
			// The newer window has already read shared; this older window
			// still needs to record its own first read.
			return shared.Get()
		})
		close(firstClosed)
	}()
	go func() {
		defer done.Done()
		<-firstOpen
		_, second = trackDependencies(func() int {
			shared.Get()
			close(secondRead)
			<-firstClosed
			return shared.Get() + afterClose.Get()
		})
	}()
	done.Wait()
	for i, deps := range [][]dependency{first, second} {
		if len(deps) != i+1 || deps[0].source != shared || deps[0].revision != 0 {
			t.Fatalf("window %d lost its dependency: %+v", i, deps)
		}
	}
	if second[1].source != afterClose {
		t.Fatalf("remaining window lost its first new read: %+v", second)
	}
}
