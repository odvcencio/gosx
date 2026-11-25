//go:build js && wasm

package browser

import (
	"context"
	"errors"
	"fmt"
	"syscall/js"

	"m31labs.dev/gosx/client/jsutil"
)

// saveFileRevokeMS is how long SaveFile keeps the file's object URL before it
// frees it, in milliseconds. A browser can still be reading the URL some time
// after the click that started the download.
var saveFileRevokeMS = 60_000.0

// SaveFile hands the viewer a file: the browser downloads data under name,
// as type mime (empty means application/octet-stream). Where the file goes,
// and whether the browser asks first, is the browser's choice; SaveFile
// returns once the download has started and cannot report where it went.
//
// SaveFile clicks a detached link, so no listener of the page runs inside
// the call. Browsers allow a download without a user gesture, but some ask
// the viewer before a second one from the same page.
func SaveFile(name, mime string, data []byte) (err error) {
	if name == "" {
		return errors.New("browser: save file: name required")
	}
	if mime == "" {
		mime = saveFileType
	}
	defer func() {
		if value := recover(); value != nil {
			err = fmt.Errorf("browser: save file: %v", value)
		}
	}()
	blob := js.Global().Get("Blob").New([]any{jsutil.NewUint8ArrayFromBytes(data)}, map[string]any{"type": mime})
	url := js.Global().Get("URL")
	href := url.Call("createObjectURL", blob)
	link := js.Global().Get("document").Call("createElement", "a")
	link.Set("href", href)
	link.Set("download", name)
	jsutil.Call(link, "click")
	// A native function frees the URL later, so no Go code waits for it.
	js.Global().Call("setTimeout", url.Get("revokeObjectURL").Call("bind", url, href), saveFileRevokeMS)
	return nil
}

// OpenFile asks the viewer to choose one file and reads it whole. It opens the
// picker synchronously, so call it while the caller's user gesture still
// holds: in reaction to a click or a key press (a browser does not count a
// controller's button as a gesture, and without a gesture no picker opens and
// done never runs). done runs on its own goroutine, with the file, with
// ErrNoFile when the viewer closes the picker without a file, or with
// ErrFileTooLarge. Hand its result to the frame loop; do not draw from it.
func OpenFile(request FileRequest, done func(File, error)) {
	OpenFileContext(context.Background(), request, done)
}

// OpenFileContext is OpenFile that gives up when ctx ends: done then runs with
// ctx's error, and a file chosen later is not read.
func OpenFileContext(ctx context.Context, request FileRequest, done func(File, error)) {
	if ctx == nil {
		ctx = context.Background()
	}
	if done == nil {
		done = func(File, error) {}
	}
	if err := ctx.Err(); err != nil {
		done(File{}, err)
		return
	}
	defer func() {
		if value := recover(); value != nil {
			done(File{}, fmt.Errorf("browser: open file: %v", value))
		}
	}()
	input := js.Global().Get("document").Call("createElement", "input")
	input.Set("type", "file")
	if request.Accept != "" {
		input.Set("accept", request.Accept)
	}
	// The listeners are the promise's native resolve function, so the browser
	// never runs Go code inside a call; a goroutine waits on the promise.
	chosen, resolve, _ := jsutil.NewPromiseWithResolvers()
	once := map[string]any{"once": true}
	input.Call("addEventListener", "change", resolve, once)
	input.Call("addEventListener", "cancel", resolve, once)
	jsutil.Call(input, "click")
	limit := request.maxBytes()
	go func() {
		file, err := readChosenFile(ctx, input, chosen, limit)
		done(file, err)
	}()
}

func readChosenFile(ctx context.Context, input, chosen js.Value, limit int) (out File, err error) {
	defer func() {
		if value := recover(); value != nil {
			out, err = File{}, fmt.Errorf("browser: open file: %v", value)
		}
	}()
	event, err := jsutil.AwaitPromiseContext(ctx, chosen)
	if err != nil {
		return File{}, err
	}
	files := input.Get("files")
	if event.Get("type").String() != "change" || !files.Truthy() || files.Length() == 0 {
		return File{}, ErrNoFile
	}
	file := files.Index(0)
	out = File{Name: file.Get("name").String()}
	if kind := file.Get("type"); kind.Type() == js.TypeString {
		out.Type = kind.String()
	}
	if size := file.Get("size"); size.Type() != js.TypeNumber || size.Float() > float64(limit) {
		return File{}, ErrFileTooLarge
	}
	buffer, err := jsutil.AwaitPromiseContext(ctx, file.Call("arrayBuffer"))
	if err != nil {
		return File{}, err
	}
	array := js.Global().Get("Uint8Array").New(buffer)
	if array.Length() > limit {
		return File{}, ErrFileTooLarge
	}
	out.Data = make([]byte, array.Length())
	js.CopyBytesToGo(out.Data, array)
	return out, nil
}
