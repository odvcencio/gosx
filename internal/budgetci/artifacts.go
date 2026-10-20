package budgetci

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"

	"m31labs.dev/gosx/internal/regularfile"
	"m31labs.dev/gosx/perf/budget"
)

// PublishArtifacts validates both representations before creating output.
// The complete directory appears atomically and contains no native diagnostics.
func PublishArtifacts(validator *budget.PublicValidator, report *budget.Report, output string) (resultErr error) {
	if validator == nil || report == nil || output == "" || filepath.Clean(output) == "." || filepath.Clean(output) == string(filepath.Separator) {
		return failure("invalid-input", "/artifacts")
	}
	var jsonData, markdown bytes.Buffer
	if err := validator.WriteJSON(&jsonData, *report); err != nil {
		return err
	}
	if err := validator.WriteMarkdown(&markdown, *report); err != nil {
		return err
	}
	if jsonData.Len() > nativeLimit || markdown.Len() > nativeLimit {
		return failure("invalid-input", "/artifacts/size")
	}
	if _, err := os.Lstat(output); !os.IsNotExist(err) {
		return failure("environment", "/artifacts/output")
	}
	parent := filepath.Dir(output)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return failure("environment", "/artifacts/output")
	}
	stage, err := os.MkdirTemp(parent, ".budget-public-")
	if err != nil {
		return failure("environment", "/artifacts/output")
	}
	defer func() {
		if os.RemoveAll(stage) != nil {
			resultErr = failure("cleanup", "/artifacts/stage")
		}
	}()
	for _, file := range []struct {
		name string
		data []byte
	}{{"report.json", jsonData.Bytes()}, {"report.md", markdown.Bytes()}} {
		if os.WriteFile(filepath.Join(stage, file.name), file.data, 0600) != nil {
			return failure("environment", "/artifacts/write")
		}
	}
	if err := ValidateArtifacts(validator, stage); err != nil {
		return err
	}
	if err := os.Rename(stage, output); err != nil {
		return failure("environment", "/artifacts/publish")
	}
	return nil
}

// ValidateArtifacts admits exactly a report and its matching Markdown. Unknown
// files, links, oversized bodies and inconsistent representations are errors.
func ValidateArtifacts(validator *budget.PublicValidator, directory string) error {
	if validator == nil || directory == "" {
		return failure("invalid-input", "/artifacts")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return failure("environment", "/artifacts")
	}
	defer root.Close()
	dir, err := root.Open(".")
	if err != nil {
		return failure("environment", "/artifacts")
	}
	entries, readErr := dir.ReadDir(3)
	closeErr := dir.Close()
	if readErr != nil && readErr != io.EOF || closeErr != nil || len(entries) != 2 {
		return failure("invalid-input", "/artifacts/files")
	}
	bodies := map[string][]byte{}
	for _, entry := range entries {
		name := entry.Name()
		if name != "report.json" && name != "report.md" || entry.Type()&os.ModeSymlink != 0 {
			return failure("invalid-input", "/artifacts/files")
		}
		f, err := regularfile.Open(root, name)
		if err != nil {
			if errors.Is(err, regularfile.ErrUnsafe) {
				return failure("invalid-input", "/artifacts/files")
			}
			return failure("environment", "/artifacts/files")
		}
		info, statErr := f.Stat()
		if statErr != nil || !info.Mode().IsRegular() || info.Size() > nativeLimit {
			f.Close()
			return failure("invalid-input", "/artifacts/files")
		}
		data, readErr := io.ReadAll(io.LimitReader(f, nativeLimit+1))
		closeErr := f.Close()
		if readErr != nil || closeErr != nil || len(data) > nativeLimit {
			return failure("invalid-input", "/artifacts/files")
		}
		format := "json"
		if name == "report.md" {
			format = "markdown"
		}
		if err := validator.Validate(bytes.NewReader(data), format); err != nil {
			return err
		}
		bodies[name] = data
	}
	decoded, err := budget.DecodeRecord(bytes.NewReader(bodies["report.json"]))
	report, ok := decoded.(*budget.Report)
	if err != nil || !ok {
		return failure("invalid-input", "/artifacts/report")
	}
	var expected bytes.Buffer
	if err := validator.WriteMarkdown(&expected, *report); err != nil {
		return err
	}
	if !bytes.Equal(expected.Bytes(), bodies["report.md"]) {
		return failure("wrong-fixture", "/artifacts/markdown")
	}
	return nil
}
