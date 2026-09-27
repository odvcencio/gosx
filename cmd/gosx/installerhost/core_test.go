package installerhost

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompareInstallerVersions(t *testing.T) {
	for _, test := range []struct {
		a, b string
		want int
	}{
		{"1.2.0", "1.1.99", 1},
		{"1.2.0.0", "1.2", 0},
		{"0.9.8", "1.0.0", -1},
	} {
		got, err := CompareInstallerVersions(test.a, test.b)
		if err != nil || got != test.want {
			t.Fatalf("compareInstallerVersions(%q, %q) = %d, %v; want %d", test.a, test.b, got, err, test.want)
		}
	}
	if _, err := CompareInstallerVersions("1.beta", "1.0"); err == nil {
		t.Fatal("expected non-numeric version to fail")
	}
}

func TestSafeArchivePath(t *testing.T) {
	for _, value := range []string{"../outside", `..\\outside`, `/absolute`, `C:\\outside`, "a/../../outside"} {
		if _, err := safeArchivePath(value); err == nil {
			t.Errorf("safeArchivePath(%q) accepted a traversal path", value)
		}
	}
	if got, err := safeArchivePath(`assets\\ui.png`); err != nil || got != "assets/ui.png" {
		t.Fatalf("safeArchivePath normalized to %q, %v", got, err)
	}
}

func TestFindSetupOverlayAllowsSignatureTrailer(t *testing.T) {
	payload := []byte("PK-installer-payload")
	data := append([]byte("MZ-test-stub"), []byte(setupPrefix)...)
	data = appendLittleEndianUint64(data, uint64(len(payload)))
	payloadStart := int64(len(data))
	data = append(data, payload...)
	data = append(data, []byte(setupTrailer)...)
	footerLengthOffset := len(data)
	data = appendLittleEndianUint64(data, uint64(len(payload)))
	data = append(data, []byte("AUTHENTICODE-CERTIFICATE")...)
	offset, length, err := FindSetupOverlay(bytesReaderAt(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if offset != payloadStart || length != uint64(len(payload)) {
		t.Fatalf("overlay = (%d, %d); want (%d, %d)", offset, length, payloadStart, len(payload))
	}
	data[footerLengthOffset] ^= 1
	if _, _, err := FindSetupOverlay(bytesReaderAt(data), int64(len(data))); err == nil {
		t.Fatal("expected corrupt overlay footer to fail")
	}
}

func TestWriteSetupPayloadIncludesHashedStandaloneUninstaller(t *testing.T) {
	stage := t.TempDir()
	host := []byte("MZgame")
	bootstrapper := []byte("MZwebview2")
	uninstaller := []byte("MZsmall-uninstaller")
	if err := os.WriteFile(filepath.Join(stage, "game.exe"), host, 0755); err != nil {
		t.Fatal(err)
	}
	bootstrapperPath := filepath.Join(t.TempDir(), "WebView2Setup.exe")
	if err := os.WriteFile(bootstrapperPath, bootstrapper, 0755); err != nil {
		t.Fatal(err)
	}
	uninstallerPath := filepath.Join(t.TempDir(), "uninstaller.exe")
	if err := os.WriteFile(uninstallerPath, uninstaller, 0755); err != nil {
		t.Fatal(err)
	}
	config := PackageConfig{
		AppID: "com.example.game", Name: "Example Game", Publisher: "Example Studio",
		Version: "1.0.0", HostExe: "game.exe", DataDir: `%LOCALAPPDATA%\\Example Game`,
		UpdatePublicKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
	}
	var payload bytes.Buffer
	if err := WriteSetupPayload(stage, bootstrapperPath, uninstallerPath, config, &payload); err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytesReaderAt(payload.Bytes()), int64(payload.Len()))
	if err != nil {
		t.Fatal(err)
	}
	var manifest PayloadManifest
	var foundUninstaller bool
	for _, entry := range archive.File {
		switch entry.Name {
		case payloadManifestName:
			reader, err := entry.Open()
			if err != nil {
				t.Fatal(err)
			}
			err = json.NewDecoder(reader).Decode(&manifest)
			_ = reader.Close()
			if err != nil {
				t.Fatal(err)
			}
		case payloadUninstaller:
			foundUninstaller = true
		}
	}
	if !foundUninstaller {
		t.Fatal("setup payload is missing the standalone uninstaller")
	}
	if got := manifest.Files[payloadUninstaller]; got != HashBytes(uninstaller) {
		t.Fatalf("uninstaller hash = %q, want %q", got, HashBytes(uninstaller))
	}
}

func appendLittleEndianUint64(data []byte, value uint64) []byte {
	for i := 0; i < 8; i++ {
		data = append(data, byte(value>>(8*i)))
	}
	return data
}

func TestValidatePackageConfigRequiresEd25519KeyAndInstallMetadata(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	config := PackageConfig{
		AppID: "com.example.game", Name: "Example Game", Publisher: "Example Studio",
		Version: "1.0.0", HostExe: "game.exe", Icon: "game.ico",
		DataDir:         `%LOCALAPPDATA%\\Example Game\\Saves`,
		UpdatePublicKey: base64.StdEncoding.EncodeToString(publicKey),
	}
	if err := ValidatePackageConfig(config); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	config.UpdatePublicKey = "short"
	if err := ValidatePackageConfig(config); err == nil {
		t.Fatal("expected malformed update public key to fail")
	}
}

func TestInspectInstallRootForUpgradeRefusesUnownedNonEmptyFolder(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Example Game")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(root, "user-file.txt")
	if err := os.WriteFile(sentinel, []byte("keep me"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := inspectInstallRootForUpgrade(root, "com.example.game")
	if err == nil || !strings.Contains(err.Error(), root) || !strings.Contains(err.Error(), "not a valid GoSX install") {
		t.Fatalf("inspectInstallRootForUpgrade error = %v; want a clear error naming %q", err, root)
	}
	if data, readErr := os.ReadFile(sentinel); readErr != nil || string(data) != "keep me" {
		t.Fatalf("unowned file changed after rejected upgrade: data=%q err=%v", data, readErr)
	}
}

func TestInspectInstallRootForUpgradeRequiresMatchingAppIDAndRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Example Game")
	writeTestInstallRecord(t, root, "com.other.game")
	if _, err := inspectInstallRootForUpgrade(root, "com.example.game"); err == nil || !strings.Contains(err.Error(), "app ID") {
		t.Fatalf("mismatched app ID error = %v; want a clear rejection", err)
	}
	writeTestInstallRecord(t, root, "com.example.game")
	record, err := inspectInstallRootForUpgrade(root, "com.example.game")
	if err != nil {
		t.Fatalf("matching install record rejected: %v", err)
	}
	if record.Config.AppID != "com.example.game" {
		t.Fatalf("record app ID = %q; want com.example.game", record.Config.AppID)
	}
}

func TestReadRemovableInstallRecordRefusesMissingOrMismatchedRecord(t *testing.T) {
	for _, test := range []struct {
		name        string
		recordApp   string
		writeRecord bool
	}{
		{name: "missing"},
		{name: "different app", recordApp: "com.other.game", writeRecord: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "Example Game")
			if err := os.MkdirAll(root, 0755); err != nil {
				t.Fatal(err)
			}
			sentinel := filepath.Join(root, "keep.txt")
			if err := os.WriteFile(sentinel, []byte("keep me"), 0644); err != nil {
				t.Fatal(err)
			}
			if test.writeRecord {
				writeTestInstallRecord(t, root, test.recordApp)
			}
			if _, err := readRemovableInstallRecord(root, "com.example.game", nil); err == nil {
				t.Fatal("readRemovableInstallRecord accepted an unowned directory")
			}
			if data, err := os.ReadFile(sentinel); err != nil || string(data) != "keep me" {
				t.Fatalf("unowned directory changed after rejected uninstall: data=%q err=%v", data, err)
			}
		})
	}
}

func TestReadRemovableInstallRecordProtectsCriticalDirectories(t *testing.T) {
	root := filepath.Join(t.TempDir(), "profile")
	writeTestInstallRecord(t, root, "com.example.game")
	if _, err := readRemovableInstallRecord(root, "com.example.game", []string{root}); err == nil || !strings.Contains(err.Error(), "protected") {
		t.Fatalf("protected directory error = %v; want refusal", err)
	}
	if !isFilesystemRoot("/") || !isFilesystemRoot(`C:\`) || !isFilesystemRoot(`\\server\share`) {
		t.Fatal("filesystem roots must be protected")
	}
	if isFilesystemRoot(`C:\Games`) || isFilesystemRoot(`\\server\share\Game`) {
		t.Fatal("non-root install directory was classified as a filesystem root")
	}
}

func writeTestInstallRecord(t *testing.T, root, appID string) {
	t.Helper()
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	record := installationRecord{
		Config:      PackageConfig{AppID: appID, Version: "1.0.0"},
		InstallRoot: root,
	}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "install.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestMakePortableZipContainsHashesAndRejectsSymlinks(t *testing.T) {
	stage := t.TempDir()
	if err := os.Mkdir(filepath.Join(stage, "assets"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "game.exe"), []byte("MZtest"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "assets", "page.html"), []byte("<main>game</main>"), 0644); err != nil {
		t.Fatal(err)
	}
	archiveBytes, files, err := MakePortableZip(stage)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files["game.exe"] == "" || files["assets/page.html"] == "" {
		t.Fatalf("unexpected hashes: %#v", files)
	}
	reader, err := zip.NewReader(bytesReaderAt(archiveBytes), int64(len(archiveBytes)))
	if err != nil {
		t.Fatal(err)
	}
	var sums string
	for _, file := range reader.File {
		if file.Name == "SHA256SUMS" {
			r, err := file.Open()
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(r)
			_ = r.Close()
			if err != nil {
				t.Fatal(err)
			}
			sums = string(data)
		}
	}
	if !strings.Contains(sums, files["game.exe"]+"  game.exe") {
		t.Fatalf("portable SHA256SUMS omits host executable: %q", sums)
	}
	if err := os.Symlink(filepath.Join(stage, "game.exe"), filepath.Join(stage, "alias.exe")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, _, err := MakePortableZip(stage); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected staged symlink to fail, got %v", err)
	}
}

type memoryReaderAt []byte

func bytesReaderAt(data []byte) memoryReaderAt { return memoryReaderAt(data) }

func (data memoryReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(data)) {
		return 0, io.EOF
	}
	n := copy(p, data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}
