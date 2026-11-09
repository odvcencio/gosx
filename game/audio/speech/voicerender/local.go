package voicerender

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"m31labs.dev/gosx/game/audio/speech"
	"m31labs.dev/gosx/game/audio/speech/loudness"
)

// Local speaks with a text-to-speech program installed on the machine: macOS
// `say`, or `espeak-ng` / `espeak` elsewhere. It is for developing the
// pipeline without an API key; the voices sound synthetic and are not meant
// to ship.
type Local struct {
	// Program overrides the search ("say", "espeak-ng", "espeak").
	Program string
	// LookPath is exec.LookPath unless a test replaces it.
	LookPath func(string) (string, error)
	// TempDir is where the scratch WAV goes. Default os.TempDir().
	TempDir string
}

// Name implements Provider.
func (l *Local) Name() string { return "local" }

func (l *Local) program() (string, error) {
	look := l.LookPath
	if look == nil {
		look = exec.LookPath
	}
	if l.Program != "" {
		return look(l.Program)
	}
	for _, p := range []string{"say", "espeak-ng", "espeak"} {
		if path, err := look(p); err == nil {
			return path, nil
		}
	}
	return "", errors.New("voicerender: no local speech program found (looked for say, espeak-ng, espeak)")
}

// Synthesize implements Provider. VoiceID names a `say` voice or an espeak
// voice; empty uses the program's default.
func (l *Local) Synthesize(ctx context.Context, v speech.Voice, text string) (loudness.Audio, error) {
	prog, err := l.program()
	if err != nil {
		return loudness.Audio{}, err
	}
	dir, err := os.MkdirTemp(l.TempDir, "gosx-voice-*")
	if err != nil {
		return loudness.Audio{}, err
	}
	defer os.RemoveAll(dir)
	out := filepath.Join(dir, "out.wav")
	var args []string
	switch filepath.Base(prog) {
	case "say":
		args = []string{"-o", out, "--file-format=WAVE", "--data-format=LEI16@22050"}
		if v.VoiceID != "" {
			args = append(args, "-v", v.VoiceID)
		}
	default: // espeak, espeak-ng
		args = []string{"-w", out}
		if v.VoiceID != "" {
			args = append(args, "-v", v.VoiceID)
		}
	}
	// "--" keeps text that begins with "-" from being read as a flag.
	args = append(args, "--", text)
	if b, err := exec.CommandContext(ctx, prog, args...).CombinedOutput(); err != nil {
		return loudness.Audio{}, fmt.Errorf("voicerender: %s failed: %w: %.200s", filepath.Base(prog), err, b)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		return loudness.Audio{}, err
	}
	return loudness.ParseWAV(data)
}
