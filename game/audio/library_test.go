package audio

import (
	"errors"
	"sync"
	"testing"
	"time"

	"m31labs.dev/gosx/game"
)

func TestLibraryLoadsEachClipOnceAndAnswersEveryWaiter(t *testing.T) {
	host := newFakeHost()
	mixer := NewMixer(host)
	player := NewPlayer(host, mixer.Graph(), nil)
	lib := NewLibrary(player)
	release := make(chan struct{})
	fetches := 0
	fetch := func() ([]byte, error) { fetches++; <-release; return []byte("snap"), nil }
	var wg sync.WaitGroup
	wg.Add(2)
	var results []bool
	var mu sync.Mutex
	done := func(ok bool) { mu.Lock(); results = append(results, ok); mu.Unlock(); wg.Done() }
	clip := game.AudioClip{ID: "snap", Bus: SFXBus}
	lib.Load(clip, fetch, done)
	lib.Load(clip, fetch, done)
	close(release)
	wg.Wait()
	if fetches != 1 || len(results) != 2 || !results[0] || !results[1] || !lib.Loaded("snap") {
		t.Fatalf("fetches %d, results %v, loaded %v: two requests share one load", fetches, results, lib.Loaded("snap"))
	}
	// Loaded: answered at once, on the caller's goroutine.
	now := false
	lib.Load(clip, fetch, func(ok bool) { now = ok })
	if !now || fetches != 1 {
		t.Fatal("a loaded clip was not answered at once")
	}
	if _, err := player.Play(game.AudioPlayback{Clip: "snap"}); err != nil {
		t.Fatalf("the loaded clip does not play: %v", err)
	}
}

func TestLibraryRemembersAFailureAndResetDropsLoadsInFlight(t *testing.T) {
	host := newFakeHost()
	player := NewPlayer(host, NewMixer(host).Graph(), nil)
	lib := NewLibrary(player)
	failed := make(chan bool, 1)
	lib.Load(game.AudioClip{ID: "riff"}, func() ([]byte, error) { return nil, errors.New("404") }, func(ok bool) { failed <- ok })
	if <-failed || !lib.Failed("riff") {
		t.Fatal("a fetch error must report false and be remembered")
	}
	calls := 0
	lib.Load(game.AudioClip{ID: "riff"}, func() ([]byte, error) { calls++; return nil, nil }, func(ok bool) {
		if ok {
			t.Error("a failed clip loaded on a second request")
		}
	})
	if calls != 0 {
		t.Fatal("a failed clip was fetched again")
	}
	release := make(chan struct{})
	ran := make(chan struct{}, 1)
	lib.Load(game.AudioClip{ID: "crowd"}, func() ([]byte, error) { <-release; return []byte("x"), nil }, func(bool) { ran <- struct{}{} })
	lib.Reset(NewPlayer(host, NewMixer(host).Graph(), nil))
	close(release)
	select {
	case <-ran:
		t.Fatal("a load from before Reset called its waiter")
	case <-time.After(50 * time.Millisecond):
	}
	if lib.Loaded("crowd") || lib.Failed("riff") {
		t.Fatal("Reset must forget every clip")
	}
}

func TestSFXDropsALateCue(t *testing.T) {
	sfx, _, _ := newSFX(t, SFXConfig{}, "cheer")
	_, err := sfx.Play(SFXRequest{Playback: game.AudioPlayback{Clip: "cheer"}, Asked: time.Now().Add(-time.Second), MaxLate: 550 * time.Millisecond})
	if !errors.Is(err, ErrVoiceLate) || sfx.Stats().Dropped != 1 {
		t.Fatalf("a cue a second late played (%v)", err)
	}
	if _, err := sfx.Play(SFXRequest{Playback: game.AudioPlayback{Clip: "cheer"}, Asked: time.Now(), MaxLate: 550 * time.Millisecond}); err != nil {
		t.Fatalf("a cue on time was refused: %v", err)
	}
}
