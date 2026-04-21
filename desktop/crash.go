package desktop

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"
)

// CrashReporterOptions configures panic/minidump capture for a desktop app.
type CrashReporterOptions struct {
	Enabled        bool
	DumpDir        string
	UploadEndpoint string
	ConsentPrompt  bool
	OnCrash        func(CrashReport)
}

// CrashReport describes a captured crash dump and optional upload attempt.
type CrashReport struct {
	DumpPath       string
	StackPath      string
	Reason         string
	UploadEndpoint string
	Uploaded       bool
	UploadError    string
}

func runWithCrashReporter(options CrashReporterOptions, run func() error) (err error) {
	_ = installSEHCrashReporter(options)
	defer func() {
		if recovered := recover(); recovered != nil {
			report, crashErr := CaptureCrash(fmt.Sprint(recovered), options)
			if options.OnCrash != nil {
				options.OnCrash(report)
			}
			if crashErr != nil {
				err = fmt.Errorf("desktop panic captured: %v: %w", recovered, crashErr)
				return
			}
			err = fmt.Errorf("desktop panic captured: %v (dump: %s)", recovered, report.DumpPath)
		}
	}()
	return run()
}

// CaptureCrash writes a platform crash dump plus a Go stack sidecar. On
// Windows the dump path is a minidump; unsupported platforms write a text dump
// so tests and server-side tooling can exercise the same workflow.
func CaptureCrash(reason string, options CrashReporterOptions) (CrashReport, error) {
	dir := strings.TrimSpace(options.DumpDir)
	if dir == "" {
		dir = defaultCrashDumpDir()
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return CrashReport{}, err
	}
	stamp := time.Now().UTC().Format("20060102-150405.000000000")
	dumpPath := filepath.Join(dir, "gosx-crash-"+stamp+".dmp")
	stackPath := filepath.Join(dir, "gosx-crash-"+stamp+".stack.txt")
	stack := debug.Stack()
	report := CrashReport{
		DumpPath:       dumpPath,
		StackPath:      stackPath,
		Reason:         reason,
		UploadEndpoint: strings.TrimSpace(options.UploadEndpoint),
	}
	if err := writePlatformCrashDump(dumpPath, reason, stack); err != nil {
		return report, err
	}
	if err := os.WriteFile(stackPath, crashStackReport(reason, stack), 0644); err != nil {
		return report, err
	}
	if report.UploadEndpoint != "" && options.ConsentPrompt && promptCrashUpload(report) {
		if err := UploadCrashReport(report.UploadEndpoint, report.DumpPath, report.StackPath); err != nil {
			report.UploadError = err.Error()
			return report, err
		}
		report.Uploaded = true
	}
	return report, nil
}

func defaultCrashDumpDir() string {
	if dir, err := os.UserCacheDir(); err == nil && strings.TrimSpace(dir) != "" {
		return filepath.Join(dir, "gosx", "crashes")
	}
	return filepath.Join(os.TempDir(), "gosx-crashes")
}

func crashStackReport(reason string, stack []byte) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "reason: %s\n", reason)
	fmt.Fprintf(&b, "time: %s\n\n", time.Now().UTC().Format(time.RFC3339Nano))
	b.Write(stack)
	return b.Bytes()
}

// UploadCrashReport posts the dump and stack sidecar to endpoint as multipart
// form data. The caller is responsible for collecting user consent first.
func UploadCrashReport(endpoint, dumpPath, stackPath string) error {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return fmt.Errorf("%w: crash upload endpoint is empty", ErrInvalidOptions)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := addCrashFilePart(writer, "dump", dumpPath); err != nil {
		return err
	}
	if stackPath != "" {
		if err := addCrashFilePart(writer, "stack", stackPath); err != nil {
			return err
		}
	}
	if err := writer.Close(); err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	client := http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%w: crash upload returned HTTP %d", ErrInvalidOptions, resp.StatusCode)
	}
	io.Copy(io.Discard, resp.Body)
	return nil
}

func addCrashFilePart(writer *multipart.Writer, field, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	part, err := writer.CreateFormFile(field, filepath.Base(path))
	if err != nil {
		return err
	}
	_, err = io.Copy(part, file)
	return err
}
