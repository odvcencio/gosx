package playground

import (
	"testing"

	"m31labs.dev/gosx/route"
)

func TestRegisterManagedActionsPropagatesCompilerInitializationFailure(t *testing.T) {
	old := playgroundCompiler
	playgroundCompiler = nil
	t.Cleanup(func() { playgroundCompiler = old })

	if err := RegisterManagedActions(route.NewRouter()); err == nil {
		t.Fatal("expected compiler initialization error")
	}
}
