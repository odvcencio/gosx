//go:build !(js && wasm)

package browser

import "context"

// SaveFile needs a browser; elsewhere it saves nothing and returns an error.
func SaveFile(string, string, []byte) error { return errPlatformUnsupported }

// OpenFile needs a browser; elsewhere done runs at once with an error.
func OpenFile(request FileRequest, done func(File, error)) {
	OpenFileContext(context.Background(), request, done)
}

// OpenFileContext needs a browser; elsewhere done runs at once with ctx's
// error or an unsupported-platform error.
func OpenFileContext(ctx context.Context, _ FileRequest, done func(File, error)) {
	if done == nil {
		return
	}
	if ctx != nil && ctx.Err() != nil {
		done(File{}, ctx.Err())
		return
	}
	done(File{}, errPlatformUnsupported)
}
