package voicerender

import (
	"os"
	"path/filepath"

	"m31labs.dev/gosx/game/audio/speech/loudness"
)

// cache stores raw provider audio under its content hash, so a rerun with
// the same text, voice settings and provider makes no call. It holds
// provider output before trim and normalization, so changing a loudness
// target never re-bills.
type cache struct{ dir string }

func (c cache) path(key string) string { return filepath.Join(c.dir, key+".wav") }

func (c cache) has(key string) bool {
	if c.dir == "" {
		return false
	}
	_, err := os.Stat(c.path(key))
	return err == nil
}

func (c cache) get(key string) (loudness.Audio, bool) {
	if c.dir == "" {
		return loudness.Audio{}, false
	}
	data, err := os.ReadFile(c.path(key))
	if err != nil {
		return loudness.Audio{}, false
	}
	a, err := loudness.ParseWAV(data)
	if err != nil {
		return loudness.Audio{}, false // a corrupt entry is a miss
	}
	return a, true
}

func (c cache) put(key string, a loudness.Audio) error {
	if c.dir == "" {
		return nil
	}
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(c.dir, "put-*")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(loudness.WAV16(a))
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		os.Remove(tmp.Name())
		if werr != nil {
			return werr
		}
		return cerr
	}
	return os.Rename(tmp.Name(), c.path(key))
}
