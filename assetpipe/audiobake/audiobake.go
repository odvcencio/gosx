// Package audiobake is the offline half of a game's sound: it encodes WAV
// renders as Ogg Opus with ffmpeg, skips work whose source has not changed
// (hash stamps next to the output), and copies the encoded files into a
// bundle. A baker command in the game calls it; nothing here runs in the
// browser.
package audiobake

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Opus says how EncodeOpus encodes.
type Opus struct {
	// Bitrate is ffmpeg's -b:a, such as "64k". Default "64k".
	Bitrate string
	// Filter is ffmpeg's -af audio filter graph, such as "aresample=48000";
	// empty for none.
	Filter string
	// Application is libopus's -application: "audio" (default), "voip" or
	// "lowdelay".
	Application string
	// FFmpeg is the ffmpeg executable. Default "ffmpeg" on PATH.
	FFmpeg string
}

// EncodeOpus encodes the WAV file src as variable-bitrate Ogg Opus at dst,
// replacing dst, and returns dst's size.
func EncodeOpus(src, dst string, o Opus) (int64, error) {
	if o.Bitrate == "" {
		o.Bitrate = "64k"
	}
	if o.Application == "" {
		o.Application = "audio"
	}
	if o.FFmpeg == "" {
		o.FFmpeg = "ffmpeg"
	}
	args := []string{"-y", "-loglevel", "error", "-i", src}
	if o.Filter != "" {
		args = append(args, "-af", o.Filter)
	}
	args = append(args, "-c:a", "libopus", "-b:a", o.Bitrate, "-vbr", "on", "-application", o.Application, dst)
	c := exec.Command(o.FFmpeg, args...)
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return 0, fmt.Errorf("audiobake: encode %s: %w", src, err)
	}
	info, err := os.Stat(dst)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// Sum is the SHA-256 of the parts, in order, as lowercase hex: the hash a
// stamp records.
func Sum(parts ...[]byte) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write(p)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Fresh reports whether the stamp file holds sum and every output exists: the
// bake that made the outputs used this same source, so it can be skipped.
func Fresh(stamp, sum string, outputs ...string) bool {
	got, err := os.ReadFile(stamp)
	if err != nil || strings.TrimSpace(string(got)) != sum {
		return false
	}
	for _, out := range outputs {
		if _, err := os.Stat(out); err != nil {
			return false
		}
	}
	return true
}

// WriteStamp records sum in the stamp file, followed by a newline.
func WriteStamp(stamp, sum string) error { return os.WriteFile(stamp, []byte(sum+"\n"), 0o644) }

// CopyTree copies every file under src whose name ends in ext into dst,
// keeping the folders between. keep, when not nil, chooses by the path
// relative to src (with forward slashes). A missing src copies nothing.
func CopyTree(src, dst, ext string, keep func(rel string) bool) error {
	if _, err := os.Stat(src); err != nil {
		return nil
	}
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ext) {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if keep != nil && !keep(filepath.ToSlash(rel)) {
			return nil
		}
		out := filepath.Join(dst, rel)
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		return copyFile(p, out)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
