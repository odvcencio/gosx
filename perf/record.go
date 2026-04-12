package perf

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/color/palette"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// Recorder captures browser screencast frames.
type Recorder struct {
	mu     sync.Mutex
	frames []recordedFrame
	done   chan struct{}
}

type recordedFrame struct {
	data      []byte  // JPEG bytes
	timestamp float64
}

// StartRecording begins capturing screencast frames via CDP.
func StartRecording(d *Driver) (*Recorder, error) {
	rec := &Recorder{
		done: make(chan struct{}),
	}

	chromedp.ListenTarget(d.ctx, func(ev any) {
		switch e := ev.(type) {
		case *page.EventScreencastFrame:
			raw, err := base64.StdEncoding.DecodeString(e.Data)
			if err != nil {
				return
			}
			var ts float64
			if e.Metadata != nil && e.Metadata.Timestamp != nil {
				ts = float64(e.Metadata.Timestamp.Time().UnixMilli())
			}
			rec.mu.Lock()
			rec.frames = append(rec.frames, recordedFrame{
				data:      raw,
				timestamp: ts,
			})
			rec.mu.Unlock()

			// Ack the frame in a goroutine to avoid deadlocking the
			// event dispatch loop (chromedp.Run blocks until done).
			go func() {
				_ = chromedp.Run(d.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
					return page.ScreencastFrameAck(e.SessionID).Do(ctx)
				}))
			}()
		}
	})

	// Start the screencast (JPEG, quality 80, reasonable size).
	err := chromedp.Run(d.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return page.StartScreencast().
			WithFormat(page.ScreencastFormatJpeg).
			WithQuality(80).
			WithMaxWidth(1280).
			WithMaxHeight(720).
			Do(ctx)
	}))
	if err != nil {
		return nil, fmt.Errorf("start screencast: %w", err)
	}

	return rec, nil
}

// Stop stops recording and saves frames to the given path.
// Supports .gif output via pure Go (always available).
// Supports .mp4/.webm if ffmpeg is on PATH.
func (r *Recorder) Stop(d *Driver, path string) error {
	// Stop the screencast.
	_ = chromedp.Run(d.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return page.StopScreencast().Do(ctx)
	}))

	r.mu.Lock()
	frames := make([]recordedFrame, len(r.frames))
	copy(frames, r.frames)
	r.mu.Unlock()

	if len(frames) == 0 {
		return fmt.Errorf("no frames captured")
	}

	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".gif":
		return writeGIF(frames, path)
	case ".mp4", ".webm":
		return writeVideo(frames, path, ext)
	default:
		return fmt.Errorf("unsupported output format %q (use .gif, .mp4, or .webm)", ext)
	}
}

func writeGIF(frames []recordedFrame, path string) error {
	g := &gif.GIF{}

	for _, f := range frames {
		img, err := jpeg.Decode(bytes.NewReader(f.data))
		if err != nil {
			continue // skip corrupt frames
		}

		bounds := img.Bounds()
		paletted := image.NewPaletted(bounds, palette.Plan9)
		draw.Draw(paletted, bounds, img, bounds.Min, draw.Src)

		g.Image = append(g.Image, paletted)
		g.Delay = append(g.Delay, 10) // 100ms = 10 centiseconds
	}

	if len(g.Image) == 0 {
		return fmt.Errorf("no decodable frames for GIF")
	}

	out, err := os.Create(path)
	if err != nil {
		return err
	}
	defer out.Close()

	return gif.EncodeAll(out, g)
}

func writeVideo(frames []recordedFrame, path, ext string) error {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		// Fallback: write as .gif with a warning.
		gifPath := strings.TrimSuffix(path, ext) + ".gif"
		fmt.Fprintf(os.Stderr, "gosx perf: ffmpeg not found, falling back to %s\n", gifPath)
		return writeGIF(frames, gifPath)
	}

	// Write raw JPEG frames to ffmpeg via stdin pipe.
	// Use concat demuxer approach: write frames to temp dir, feed list.
	dir, err := os.MkdirTemp("", "gosx-perf-record-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	// Write each frame as a numbered JPEG.
	listFile := filepath.Join(dir, "frames.txt")
	var list strings.Builder
	for i, f := range frames {
		framePath := filepath.Join(dir, fmt.Sprintf("frame_%05d.jpg", i))
		if err := os.WriteFile(framePath, f.data, 0644); err != nil {
			return err
		}
		list.WriteString(fmt.Sprintf("file '%s'\nduration 0.1\n", framePath))
	}
	if err := os.WriteFile(listFile, []byte(list.String()), 0644); err != nil {
		return err
	}

	args := []string{
		"-y",
		"-f", "concat",
		"-safe", "0",
		"-i", listFile,
		"-vsync", "vfr",
	}
	if ext == ".mp4" {
		args = append(args, "-c:v", "libx264", "-pix_fmt", "yuv420p")
	} else {
		args = append(args, "-c:v", "libvpx-vp9")
	}
	args = append(args, path)

	cmd := exec.Command(ffmpegPath, args...)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
