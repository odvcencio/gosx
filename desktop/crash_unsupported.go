//go:build !windows || (windows && !amd64 && !arm64)

package desktop

import (
	"fmt"
	"os"
)

func writePlatformCrashDump(path, reason string, stack []byte) error {
	return os.WriteFile(path, crashStackReport(reason, stack), 0644)
}

func promptCrashUpload(CrashReport) bool {
	return false
}

func installSEHCrashReporter(CrashReporterOptions) error {
	return fmt.Errorf("%w: SEH crash reporter unavailable", ErrUnsupported)
}
