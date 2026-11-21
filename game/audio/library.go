package audio

import (
	"sync"

	"m31labs.dev/gosx/game"
)

// Library loads clips into a Player in the background, each once. A game asks
// for a clip when it first needs it (Load); the first request fetches and
// decodes it on a goroutine, requests that arrive meanwhile wait for the same
// result, and later requests are answered at once. A clip that failed to load
// is remembered, so it is not fetched again and the game can play something
// else in its place.
//
// Reset starts over with a new Player: results of loads still in flight are
// dropped, and their waiting callbacks never run.
type Library struct {
	mu      sync.Mutex
	player  *Player
	epoch   int
	loaded  map[game.AssetID]bool
	failed  map[game.AssetID]bool
	waiting map[game.AssetID][]func(bool)
}

// NewLibrary loads clips into player, decoding them with the player's host.
func NewLibrary(player *Player) *Library {
	l := &Library{}
	l.Reset(player)
	return l
}

// Reset forgets every clip and load and loads into player from now on.
func (l *Library) Reset(player *Player) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.player = player
	l.epoch++
	l.loaded = map[game.AssetID]bool{}
	l.failed = map[game.AssetID]bool{}
	l.waiting = map[game.AssetID][]func(bool){}
}

// Loaded reports whether clip id is registered with the player.
func (l *Library) Loaded(id game.AssetID) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.loaded[id]
}

// Failed reports whether loading clip id failed.
func (l *Library) Failed(id game.AssetID) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.failed[id]
}

// Load makes clip ready to play and then calls done(true), or done(false)
// when it cannot be loaded. fetch returns the clip's encoded bytes; it runs on
// a goroutine, once per clip. done may be nil, to load ahead of need. A clip
// already loaded or already failed answers at once, on the caller's goroutine.
func (l *Library) Load(clip game.AudioClip, fetch func() ([]byte, error), done func(ok bool)) {
	l.mu.Lock()
	switch {
	case l.loaded[clip.ID]:
		l.mu.Unlock()
		if done != nil {
			done(true)
		}
		return
	case l.failed[clip.ID]:
		l.mu.Unlock()
		if done != nil {
			done(false)
		}
		return
	}
	wait, started := l.waiting[clip.ID]
	if done != nil {
		wait = append(wait, done)
	}
	l.waiting[clip.ID] = wait
	epoch, player := l.epoch, l.player
	l.mu.Unlock()
	if started {
		return
	}
	go func() {
		data, err := fetch()
		var buffer Buffer
		if err == nil {
			buffer, err = player.host.DecodeAudioData(data)
		}
		l.mu.Lock()
		if l.epoch != epoch {
			l.mu.Unlock()
			return
		}
		callbacks := l.waiting[clip.ID]
		delete(l.waiting, clip.ID)
		ok := err == nil
		if ok {
			player.RegisterBuffer(clip, buffer)
			l.loaded[clip.ID] = true
		} else {
			l.failed[clip.ID] = true
		}
		l.mu.Unlock()
		for _, cb := range callbacks {
			cb(ok)
		}
	}()
}

// FetchURL fetches a clip from uri with HTTPLoader, for Library.Load.
func FetchURL(uri string) func() ([]byte, error) {
	return func() ([]byte, error) { return HTTPLoader{}.Load(uri) }
}

// FetchBytes returns a fetch that makes the clip's bytes with make, for a clip
// built in Go (a synthesized take, say).
func FetchBytes(make func() []byte) func() ([]byte, error) {
	return func() ([]byte, error) { return make(), nil }
}
