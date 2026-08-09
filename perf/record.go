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
	"time"

	"github.com/chromedp/cdproto/page"
)

// Recorder captures browser screencast frames.
type Recorder struct {
	mu           sync.Mutex
	frames       []recordedFrame
	done         chan struct{}
	cancelListen context.CancelFunc
	ackWG        sync.WaitGroup
	stopOnce     sync.Once
	stopped      chan struct{}
}

type recordedFrame struct {
	data      []byte // JPEG bytes
	timestamp float64
	// seed marks the frame that captureSeedFrame took, not one the
	// screencast delivered. Stop counts the two kinds apart so a caller can
	// tell "the page did not change" from "the screencast never ran".
	seed bool
}

// StartRecording begins capturing screencast frames via CDP.
//
// Chrome delivers a Page.screencastFrame event only when the compositor
// commits a NEW frame. A page that draws once and then holds still delivers
// nothing at all - not even an initial image. So a plain
// Page.startScreencast over a static page captured zero frames, and Stop
// returned "no frames captured" for a page that had rendered correctly.
// TestRecordGIF caught this the first time it ever compiled.
//
// StartRecording therefore takes one screenshot itself, so every recording of
// a live page holds at least one image. The screencast then adds a frame for
// each later change.
func StartRecording(d *Driver) (*Recorder, error) {
	rec := &Recorder{
		done:    make(chan struct{}),
		stopped: make(chan struct{}),
	}

	rec.cancelListen = d.ListenTarget(func(ev any) {
		switch e := ev.(type) {
		case *page.EventScreencastFrame:
			select {
			case <-rec.stopped:
				return
			default:
			}
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

			// Ack the frame in a goroutine to avoid blocking the event loop.
			rec.ackWG.Add(1)
			go func() {
				defer rec.ackWG.Done()
				select {
				case <-rec.stopped:
					return
				default:
				}
				_ = d.RunFunc(func(ctx context.Context) error {
					return page.ScreencastFrameAck(e.SessionID).Do(ctx)
				})
			}()
		}
	})

	// Start the screencast (JPEG, quality 80, reasonable size).
	err := d.RunFunc(func(ctx context.Context) error {
		return page.StartScreencast().
			WithFormat(page.ScreencastFormatJpeg).
			WithQuality(80).
			WithMaxWidth(1280).
			WithMaxHeight(720).
			Do(ctx)
	})
	if err != nil {
		rec.cancel()
		return nil, fmt.Errorf("start screencast: %w", err)
	}

	// Seed one frame from the current paint. A failure here is not fatal: the
	// screencast may still deliver frames, and Stop reports the empty case.
	if seed, err := captureSeedFrame(d); err == nil {
		rec.mu.Lock()
		rec.frames = append(rec.frames, seed)
		rec.mu.Unlock()
	}

	return rec, nil
}

// captureSeedFrame grabs the page as it stands now, in the same JPEG encoding
// the screencast uses, so writeGIF decodes both kinds through one path.
func captureSeedFrame(d *Driver) (recordedFrame, error) {
	var data []byte
	err := d.RunFunc(func(ctx context.Context) error {
		var err error
		data, err = page.CaptureScreenshot().
			WithFormat(page.CaptureScreenshotFormatJpeg).
			WithQuality(80).
			Do(ctx)
		return err
	})
	if err != nil {
		return recordedFrame{}, err
	}
	if len(data) == 0 {
		return recordedFrame{}, fmt.Errorf("capture screenshot returned no bytes")
	}
	return recordedFrame{
		data:      data,
		timestamp: float64(time.Now().UnixMilli()),
		seed:      true,
	}, nil
}

// ScreencastFrames reports how many frames the screencast itself delivered,
// excluding the seed frame StartRecording captured. A test uses it to prove the
// event path works, because the seed alone would otherwise hide a screencast
// that never started.
func (r *Recorder) ScreencastFrames() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, f := range r.frames {
		if !f.seed {
			n++
		}
	}
	return n
}

// Stop stops recording and saves frames to the given path.
// Supports .gif output via pure Go (always available).
// Supports .mp4/.webm if ffmpeg is on PATH.
func (r *Recorder) Stop(d *Driver, path string) error {
	r.stop(d)
	r.ackWG.Wait()

	if path == "" {
		return nil
	}

	r.mu.Lock()
	frames := make([]recordedFrame, len(r.frames))
	copy(frames, r.frames)
	r.mu.Unlock()

	return r.writeFrames(frames, path)
}

func (r *Recorder) stop(d *Driver) {
	if r == nil {
		return
	}
	r.stopOnce.Do(func() {
		close(r.stopped)
		r.cancel()
		if d != nil {
			_ = d.RunFunc(func(ctx context.Context) error {
				return page.StopScreencast().Do(ctx)
			})
		}
	})
}

func (r *Recorder) cancel() {
	if r != nil && r.cancelListen != nil {
		r.cancelListen()
	}
}

func (r *Recorder) writeFrames(frames []recordedFrame, path string) error {
	// Stop the screencast.
	if len(frames) == 0 {
		// StartRecording seeds a frame, so an empty set means the screenshot
		// AND the screencast both produced nothing. Name both, because the
		// usual cause is a closed page rather than a still one.
		return fmt.Errorf("no frames captured: the seed screenshot and the screencast both " +
			"returned nothing, so the page was probably gone before Stop ran")
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
