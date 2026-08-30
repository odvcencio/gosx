package scene3drenderersource

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type architectureContract struct {
	SourceSets map[string]struct {
		Sources []string `json:"sources"`
	} `json:"sourceSets"`
	ExtraProtectedSources []string `json:"extraProtectedSources"`
}

const labelPrefix = "scene3d-renderer-source-set:"
const sourcePrefix = "scene3d-protected-source:"

func BackendLabel(backend string) string { return labelPrefix + backend }

func ProtectedSourceLabel(source string) string {
	return sourcePrefix + strings.TrimSuffix(source, ".ts")
}

func ReadBackend(t testing.TB, backend string) string {
	t.Helper()
	root := repoRoot(t)
	contract := loadContract(t, root)
	entry, ok := contract.SourceSets[backend]
	if !ok || len(entry.Sources) == 0 {
		t.Fatalf("Scene3D renderer source set %q is not registered", backend)
	}
	parts := make([]string, 0, len(entry.Sources))
	for _, source := range entry.Sources {
		parts = append(parts, readContractSource(t, root, source))
	}
	return strings.Join(parts, "\n")
}

func ReadSource(t testing.TB, label string) string {
	t.Helper()
	if strings.HasPrefix(label, labelPrefix) {
		return ReadBackend(t, strings.TrimPrefix(label, labelPrefix))
	}
	source := strings.TrimPrefix(label, sourcePrefix)
	if !strings.HasSuffix(source, ".ts") {
		source += ".ts"
	}
	root := repoRoot(t)
	contract := loadContract(t, root)
	for _, protected := range contract.ExtraProtectedSources {
		if protected == source {
			return readContractSource(t, root, source)
		}
	}
	t.Fatalf("Scene3D protected source %q is not registered", source)
	return ""
}

func repoRoot(t testing.TB) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func loadContract(t testing.TB, root string) architectureContract {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "client", "js", "testdata", "scene3d-renderer-architecture.json"))
	if err != nil {
		t.Fatalf("read Scene3D renderer architecture contract: %v", err)
	}
	var contract architectureContract
	if err := json.Unmarshal(raw, &contract); err != nil {
		t.Fatalf("parse Scene3D renderer architecture contract: %v", err)
	}
	return contract
}

func readContractSource(t testing.TB, root, source string) string {
	t.Helper()
	clean := filepath.Clean(filepath.FromSlash(source))
	if filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)+".."+string(filepath.Separator)) {
		t.Fatalf("Scene3D renderer source %q escapes the contract root", source)
	}
	path := filepath.Join(root, "client", "js", clean)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read Scene3D renderer source %s: %v", source, err)
	}
	if len(data) == 0 {
		t.Fatalf("Scene3D renderer source %s is empty", source)
	}
	return string(data)
}

func Describe(label string) string {
	switch {
	case strings.HasPrefix(label, labelPrefix):
		return fmt.Sprintf("Scene3D %s renderer source set", strings.TrimPrefix(label, labelPrefix))
	case strings.HasPrefix(label, sourcePrefix):
		return fmt.Sprintf("Scene3D protected source %s", strings.TrimPrefix(label, sourcePrefix))
	default:
		return label
	}
}
