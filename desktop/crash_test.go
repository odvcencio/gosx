package desktop

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCaptureCrashWritesDumpAndStack(t *testing.T) {
	dir := t.TempDir()
	report, err := CaptureCrash("deliberate panic", CrashReporterOptions{DumpDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{report.DumpPath, report.StackPath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected crash artifact %s: %v", path, err)
		}
	}
	stack := readDesktopFile(t, report.StackPath)
	if !strings.Contains(stack, "deliberate panic") {
		t.Fatalf("expected reason in stack report, got %q", stack)
	}
}

func TestUploadCrashReportPostsMultipartPayload(t *testing.T) {
	dir := t.TempDir()
	dump := filepath.Join(dir, "crash.dmp")
	stack := filepath.Join(dir, "crash.stack.txt")
	if err := os.WriteFile(dump, []byte("dump"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stack, []byte("stack"), 0644); err != nil {
		t.Fatal(err)
	}
	seenDump := false
	seenStack := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		seenDump = len(r.MultipartForm.File["dump"]) == 1
		seenStack = len(r.MultipartForm.File["stack"]) == 1
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	if err := UploadCrashReport(server.URL, dump, stack); err != nil {
		t.Fatal(err)
	}
	if !seenDump || !seenStack {
		t.Fatalf("expected dump and stack uploads, dump=%v stack=%v", seenDump, seenStack)
	}
}

func TestRunWithCrashReporterConvertsPanicToError(t *testing.T) {
	dir := t.TempDir()
	var captured CrashReport
	err := runWithCrashReporter(CrashReporterOptions{
		Enabled: true,
		DumpDir: dir,
		OnCrash: func(report CrashReport) {
			captured = report
		},
	}, func() error {
		panic("boom")
	})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected panic error, got %v", err)
	}
	if captured.DumpPath == "" {
		t.Fatalf("expected OnCrash report, got %#v", captured)
	}
}

func readDesktopFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
