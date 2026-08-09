//go:build browser

package perf

import (
	"bytes"
	"fmt"
	"image/gif"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// staticRecordPage draws once and then holds still. Chrome commits no further
// compositor frame for it, so the screencast delivers nothing.
const staticRecordPage = `<html><body style="background:blue">` +
	`<h1 style="color:white">Recording Test</h1></body></html>`

// animatedRecordPage repaints ten times a second, so the compositor keeps
// committing and the screencast keeps delivering.
const animatedRecordPage = `<html><body style="background:blue">
<h1 id="h" style="color:white">Recording Test</h1>
<script>
let i = 0;
setInterval(function () {
  i += 1;
  document.body.style.background = (i % 2) ? "red" : "blue";
  document.getElementById("h").textContent = "frame " + i;
}, 100);
</script>
</body></html>`

func serveRecordPage(t *testing.T, html string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, html)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// startRecordingOn launches a browser, loads html and starts recording.
func startRecordingOn(t *testing.T, html string) (*Driver, *Recorder) {
	t.Helper()
	d := requireDriver(t, 20*time.Second)

	srv := serveRecordPage(t, html)
	if err := d.Navigate(srv.URL); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if err := d.WaitReady(); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}

	rec, err := StartRecording(d)
	if err != nil {
		t.Fatalf("StartRecording: %v", err)
	}
	return d, rec
}

// assertDecodableGIF proves the file on disk is a GIF a decoder accepts, not
// merely a non-empty file. The earlier version checked only the byte count, so a
// truncated or mis-encoded image would have passed.
func assertDecodableGIF(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if len(data) == 0 {
		t.Fatalf("%s is empty", path)
	}
	decoded, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode %s as GIF: %v", path, err)
	}
	if len(decoded.Image) == 0 {
		t.Fatalf("%s decoded to zero images", path)
	}
	return len(decoded.Image)
}

// TestRecordGIFCapturesAStillPage pins the guarantee a still page must keep: a
// recording of a page that renders and then holds still produces an image, not
// an error.
//
// Chrome sends Page.screencastFrame only when the compositor commits a change.
// A still page commits once, before startScreencast, and then nothing. So the
// screencast alone captured zero frames and Stop failed with "no frames
// captured" for a page that had rendered correctly. StartRecording now takes
// one screenshot to seed the recording, and this test holds that in place.
func TestRecordGIFCapturesAStillPage(t *testing.T) {
	d, rec := startRecordingOn(t, staticRecordPage)

	// Long enough that a working screencast would have delivered many frames
	// if the page were changing. This page is not, so the seed frame is the
	// whole recording, and that is the point.
	time.Sleep(2 * time.Second)

	gifPath := filepath.Join(t.TempDir(), "still.gif")
	if err := rec.Stop(d, gifPath); err != nil {
		t.Fatalf("Stop on a still page must still write a recording, got: %v", err)
	}
	images := assertDecodableGIF(t, gifPath)
	t.Logf("still page recorded %d GIF image(s); screencast delivered %d frame(s)",
		images, rec.ScreencastFrames())
}

// TestRecordGIFCapturesScreencastFrames proves the screencast event path works,
// which the test above cannot.
//
// The seed frame makes a still-page recording succeed. That is correct
// behaviour, and it is also a hiding place: a recorder whose ListenTarget
// handler never fires would still write a one-frame GIF and pass the test
// above. So this test drives a page that repaints and demands frames the
// SCREENCAST delivered, counted apart from the seed.
func TestRecordGIFCapturesScreencastFrames(t *testing.T) {
	d, rec := startRecordingOn(t, animatedRecordPage)

	// The page repaints every 100ms, so two seconds leaves a wide margin over
	// the two frames the assertion demands.
	time.Sleep(2 * time.Second)

	delivered := rec.ScreencastFrames()
	gifPath := filepath.Join(t.TempDir(), "animated.gif")
	if err := rec.Stop(d, gifPath); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	images := assertDecodableGIF(t, gifPath)
	t.Logf("animated page: screencast delivered %d frame(s), GIF holds %d image(s)", delivered, images)

	if delivered < 2 {
		t.Fatalf("the screencast delivered %d frames over 2s on a page repainting every 100ms; "+
			"the Page.screencastFrame listener or the frame acknowledgement is broken", delivered)
	}
	if images < 2 {
		t.Fatalf("the GIF holds %d images but the screencast delivered %d frames; "+
			"writeGIF drops frames", images, delivered)
	}
}

func TestRecordStopIsIdempotentAndCleansListener(t *testing.T) {
	d, rec := startRecordingOn(t, animatedRecordPage)

	time.Sleep(300 * time.Millisecond)
	gifPath := filepath.Join(t.TempDir(), "first.gif")
	if err := rec.Stop(d, gifPath); err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	framesAfterStop := rec.ScreencastFrames()
	if err := rec.Stop(d, filepath.Join(t.TempDir(), "second.gif")); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	if got := rec.ScreencastFrames(); got != framesAfterStop {
		t.Fatalf("frames changed after Stop: got %d, want %d", got, framesAfterStop)
	}
}

func TestRecordRepeatedStartStopCleanup(t *testing.T) {
	d := requireDriver(t, 20*time.Second)
	srv := serveRecordPage(t, animatedRecordPage)
	if err := d.Navigate(srv.URL); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if err := d.WaitReady(); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}

	for i := 0; i < 3; i++ {
		rec, err := StartRecording(d)
		if err != nil {
			t.Fatalf("StartRecording %d: %v", i, err)
		}
		time.Sleep(200 * time.Millisecond)
		if err := rec.Stop(d, filepath.Join(t.TempDir(), fmt.Sprintf("record-%d.gif", i))); err != nil {
			t.Fatalf("Stop %d: %v", i, err)
		}
		frames := rec.ScreencastFrames()
		time.Sleep(150 * time.Millisecond)
		if got := rec.ScreencastFrames(); got != frames {
			t.Fatalf("recorder %d still collected frames after Stop: got %d, want %d", i, got, frames)
		}
	}
}
