package island

import (
	"strings"
	"testing"

	"m31labs.dev/gosx/hydrate"
)

func TestHubDocumentSelectsHashedOptionalFeatureWithoutWASM(t *testing.T) {
	r := NewRenderer("notes")
	if _, err := r.BindHubDocument("notes", "/notes/ws", hydrate.HubDocumentConfig{RootID: "notes"}); err != nil {
		t.Fatal(err)
	}
	s := r.Summary()
	if !s.Bootstrap || !s.Manifest || r.clientRuntimePlan().SharedRuntime {
		t.Fatalf("document should use only the browser host: %+v", s)
	}
	if path := r.FeaturePaths()["hub-document"]; !strings.Contains(path, "bootstrap-feature-hub-document") {
		t.Fatalf("feature path = %q", path)
	}
	if m := r.clientManifest(); len(m.Hubs) != 1 || m.Hubs[0].Document == nil || m.Runtime.Path != "" {
		t.Fatalf("manifest: %+v", m)
	}
}
