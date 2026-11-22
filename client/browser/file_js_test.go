//go:build js && wasm

package browser

import (
	"context"
	"errors"
	"syscall/js"
	"testing"
	"time"

	"m31labs.dev/gosx/client/jsutil"
	"m31labs.dev/gosx/internal/jsreentry"
)

// fileFixture replaces document and URL with native stand-ins: every element
// is an EventTarget that counts its clicks, and URL records the object URLs
// made and freed. Nothing in it is a Go function.
func fileFixture(t *testing.T) js.Value {
	t.Helper()
	fixture := js.Global().Get("Function").New(`
		const made = [];
		const urls = { created: [], revoked: [] };
		const document = {
			createElement(tag) {
				const element = new EventTarget();
				element.tag = tag;
				element.clicks = 0;
				element.click = function () { this.clicks++; };
				made.push(element);
				return element;
			},
		};
		const URL = {
			createObjectURL(blob) { urls.created.push(blob); return "blob:fixture/" + urls.created.length; },
			revokeObjectURL(href) { urls.revoked.push(href); },
		};
		const file = (bytes, name, type) => {
			const blob = new Blob([new Uint8Array(bytes)], { type });
			const read = blob.arrayBuffer.bind(blob);
			return Object.assign(blob, { name, reads: 0, arrayBuffer() { this.reads++; return read(); } });
		};
		return { document, URL, made, urls, file };
	`).Invoke()
	for _, name := range []string{"document", "URL"} {
		original := js.Global().Get(name)
		js.Global().Set(name, fixture.Get(name))
		t.Cleanup(func() { js.Global().Set(name, original) })
	}
	return fixture
}

func blobBytes(t *testing.T, blob js.Value) []byte {
	t.Helper()
	buffer, err := jsutil.AwaitPromise(blob.Call("arrayBuffer"))
	if err != nil {
		t.Fatal(err)
	}
	array := js.Global().Get("Uint8Array").New(buffer)
	out := make([]byte, array.Length())
	js.CopyBytesToGo(out, array)
	return out
}

func TestSaveFileDownloadsTheBytesUnderTheName(t *testing.T) {
	fixture := fileFixture(t)
	probe := jsreentry.Install(t)
	defer func(ms float64) { saveFileRevokeMS = ms }(saveFileRevokeMS)
	saveFileRevokeMS = 1

	if err := SaveFile("play-12.json", "application/json", []byte{0, 1, 255}); err != nil {
		t.Fatal(err)
	}
	link := fixture.Get("made").Index(0)
	if link.Get("tag").String() != "a" || link.Get("download").String() != "play-12.json" || link.Get("href").String() != "blob:fixture/1" {
		t.Fatalf("the link does not download the file: tag %v, download %v, href %v", link.Get("tag"), link.Get("download"), link.Get("href"))
	}
	if link.Get("clicks").Int() != 1 {
		t.Fatalf("the link was clicked %d times, want 1", link.Get("clicks").Int())
	}
	blob := fixture.Get("urls").Get("created").Index(0)
	if blob.Get("type").String() != "application/json" {
		t.Fatalf("type %q, want application/json", blob.Get("type").String())
	}
	if got := blobBytes(t, blob); string(got) != string([]byte{0, 1, 255}) {
		t.Fatalf("bytes %v, want [0 1 255]", got)
	}
	revoked := fixture.Get("urls").Get("revoked")
	for deadline := time.Now().Add(2 * time.Second); revoked.Length() == 0 && time.Now().Before(deadline); {
		time.Sleep(2 * time.Millisecond)
	}
	if revoked.Length() != 1 || revoked.Index(0).String() != "blob:fixture/1" {
		t.Fatalf("the object URL was not freed once: %d frees", revoked.Length())
	}
	probe.AssertNone(t, "SaveFile")
}

func TestSaveFileDefaultsTheTypeAndNeedsAName(t *testing.T) {
	fixture := fileFixture(t)
	if err := SaveFile("", "text/plain", []byte("x")); err == nil {
		t.Fatal("a file with no name was saved")
	}
	if fixture.Get("made").Length() != 0 {
		t.Fatal("a refused save made a link")
	}
	if err := SaveFile("raw.bin", "", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if kind := fixture.Get("urls").Get("created").Index(0).Get("type").String(); kind != "application/octet-stream" {
		t.Fatalf("type %q, want application/octet-stream", kind)
	}
}

type opened struct {
	file File
	err  error
}

func openFile(ctx context.Context, request FileRequest) <-chan opened {
	result := make(chan opened, 1)
	OpenFileContext(ctx, request, func(file File, err error) { result <- opened{file, err} })
	return result
}

func await(t *testing.T, result <-chan opened) opened {
	t.Helper()
	select {
	case r := <-result:
		return r
	case <-time.After(2 * time.Second):
		t.Fatal("done never ran")
		return opened{}
	}
}

func dispatch(element js.Value, name string) {
	element.Call("dispatchEvent", js.Global().Get("Event").New(name))
}

func TestOpenFileReadsTheChosenFile(t *testing.T) {
	fixture := fileFixture(t)
	probe := jsreentry.Install(t)
	result := openFile(context.Background(), FileRequest{Accept: ".json"})
	input := fixture.Get("made").Index(0)
	if input.Get("tag").String() != "input" || input.Get("type").String() != "file" || input.Get("accept").String() != ".json" {
		t.Fatalf("the picker is not a file input for .json: %v %v %v", input.Get("tag"), input.Get("type"), input.Get("accept"))
	}
	if input.Get("clicks").Int() != 1 {
		t.Fatalf("the picker was opened %d times, want 1", input.Get("clicks").Int())
	}
	select {
	case r := <-result:
		t.Fatalf("done ran before the viewer chose: %+v", r)
	case <-time.After(5 * time.Millisecond):
	}
	chosen := fixture.Call("file", []any{7, 0, 200}, "play-12.json", "application/json")
	input.Set("files", []any{chosen})
	dispatch(input, "change")
	r := await(t, result)
	if r.err != nil || r.file.Name != "play-12.json" || r.file.Type != "application/json" || string(r.file.Data) != string([]byte{7, 0, 200}) {
		t.Fatalf("got %+v, %v", r.file, r.err)
	}
	probe.AssertNone(t, "OpenFile")
}

func TestOpenFileReportsNoFileWhenThePickerCloses(t *testing.T) {
	for _, c := range []struct {
		name, event string
		files       bool
	}{{"cancel", "cancel", false}, {"change with no file", "change", true}} {
		t.Run(c.name, func(t *testing.T) {
			fixture := fileFixture(t)
			result := openFile(context.Background(), FileRequest{})
			input := fixture.Get("made").Index(0)
			if c.files {
				input.Set("files", []any{})
			}
			dispatch(input, c.event)
			if r := await(t, result); !errors.Is(r.err, ErrNoFile) {
				t.Fatalf("got %v, want ErrNoFile", r.err)
			}
		})
	}
}

func TestOpenFileRefusesAFileOverTheLimitUnread(t *testing.T) {
	fixture := fileFixture(t)
	result := openFile(context.Background(), FileRequest{MaxBytes: 2})
	input := fixture.Get("made").Index(0)
	chosen := fixture.Call("file", []any{1, 2, 3}, "big.json", "")
	input.Set("files", []any{chosen})
	dispatch(input, "change")
	if r := await(t, result); !errors.Is(r.err, ErrFileTooLarge) {
		t.Fatalf("got %v, want ErrFileTooLarge", r.err)
	}
	if chosen.Get("reads").Int() != 0 {
		t.Fatal("a file over the limit was read")
	}
}

func TestOpenFileStopsWithItsContext(t *testing.T) {
	fixture := fileFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	result := openFile(ctx, FileRequest{})
	cancel()
	if r := await(t, result); !errors.Is(r.err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", r.err)
	}
	done := make(chan error, 1)
	OpenFileContext(ctx, FileRequest{}, func(_ File, err error) { done <- err })
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v, want context.Canceled", err)
		}
	default:
		t.Fatal("an ended context did not report at once")
	}
	if fixture.Get("made").Length() != 1 {
		t.Fatal("an ended context opened a picker")
	}
}
