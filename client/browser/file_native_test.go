//go:build !(js && wasm)

package browser

import (
	"context"
	"errors"
	"testing"
)

func TestNativeFilesAreUnavailable(t *testing.T) {
	if err := SaveFile("play.json", "application/json", []byte("{}")); err == nil {
		t.Fatal("a native build saved a file")
	}
	calls := 0
	OpenFile(FileRequest{}, func(file File, err error) {
		calls++
		if err == nil || file.Data != nil {
			t.Errorf("a native build opened a file: %+v %v", file, err)
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	OpenFileContext(ctx, FileRequest{}, func(_ File, err error) {
		calls++
		if !errors.Is(err, context.Canceled) {
			t.Errorf("an ended context: %v", err)
		}
	})
	OpenFile(FileRequest{}, nil)
	if calls != 2 {
		t.Fatalf("done ran %d times, want 2", calls)
	}
}

func TestFileRequestLimit(t *testing.T) {
	if got := (FileRequest{}).maxBytes(); got != DefaultFileMaxBytes {
		t.Fatalf("default limit %d, want %d", got, DefaultFileMaxBytes)
	}
	if got := (FileRequest{MaxBytes: 10}).maxBytes(); got != 10 {
		t.Fatalf("limit %d, want 10", got)
	}
}
