package installerhost

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	payloadConfigName   = "__gosx__/config.json"
	payloadManifestName = "__gosx__/payload-manifest.json"
	payloadBootstrapper = "__gosx__/MicrosoftEdgeWebView2Setup.exe"
	payloadUninstaller  = "__gosx__/uninstaller.exe"
	setupPrefix         = "GOSXSET1"
	setupTrailer        = "GOSXEND1"
	maxPayloadBytes     = 64 << 30
)

// PackageConfig describes a staged desktop app and its installer metadata.
type PackageConfig struct {
	AppID                string        `json:"app_id"`
	Name                 string        `json:"name"`
	Publisher            string        `json:"publisher"`
	Version              string        `json:"version"`
	Icon                 string        `json:"icon"`
	HostExe              string        `json:"host_exe"`
	WebView2Bootstrapper string        `json:"webview2_bootstrapper,omitempty"`
	WebView2SHA256       string        `json:"webview2_sha256,omitempty"`
	UpdatePublicKey      string        `json:"update_public_key"`
	DataDir              string        `json:"data_dir,omitempty"`
	Channel              string        `json:"channel,omitempty"`
	Released             string        `json:"released,omitempty"`
	Notes                string        `json:"notes,omitempty"`
	DownloadPage         string        `json:"download_page,omitempty"`
	DesktopShortcut      bool          `json:"desktop_shortcut,omitempty"`
	Signing              SigningConfig `json:"signing,omitzero"`
}

// SigningConfig contains only non-secret packaging settings. Credentials are
// supplied by the signing environment, never by the package config.
type SigningConfig struct {
	Endpoint     string `json:"endpoint,omitempty"`
	Account      string `json:"account,omitempty"`
	Profile      string `json:"profile,omitempty"`
	Tool         string `json:"tool,omitempty"`
	DlibPath     string `json:"dlib_path,omitempty"`
	SigntoolPath string `json:"signtool_path,omitempty"`
	JsignPath    string `json:"jsign_path,omitempty"`
}

// PayloadManifest binds every application file, the config and WebView2
// bootstrapper to a SHA-256 digest. The manifest itself is inside the signed
// Setup PE overlay.
type PayloadManifest struct {
	Files map[string]string `json:"files"`
}

// UpdateManifest is the direct-download update document emitted by the
// desktop packager. Signatures are detached and cover the exact JSON bytes.
type UpdateManifest struct {
	App          string            `json:"app"`
	Channel      string            `json:"channel"`
	Version      string            `json:"version"`
	Released     string            `json:"released"`
	Notes        string            `json:"notes"`
	DownloadPage string            `json:"download_page"`
	Artifacts    map[string]string `json:"artifacts"`
}

type PackageMetadata struct {
	AppID                 string `json:"app_id"`
	Name                  string `json:"name"`
	Publisher             string `json:"publisher"`
	Version               string `json:"version"`
	WebView2SHA256        string `json:"webview2_sha256"`
	InstallerSigning      string `json:"installer_signing"`
	ManifestSigning       string `json:"manifest_signing"`
	BootstrapperSource    string `json:"bootstrapper_source"`
	GeneratedAt           string `json:"generated_at"`
	SigningProvider       string `json:"signing_provider,omitempty"`
	VerifiedSignerSubject string `json:"verified_signer_subject,omitempty"`
}

type installationRecord struct {
	Config        PackageConfig `json:"config"`
	InstallRoot   string        `json:"install_root"`
	StartMenu     string        `json:"start_menu"`
	UninstallKey  string        `json:"uninstall_key"`
	DesktopLink   string        `json:"desktop_link,omitempty"`
	DataDirectory string        `json:"data_directory"`
}

func (config PackageConfig) IconPath(root string) string {
	if config.Icon == "" {
		return filepath.Join(root, config.HostExe)
	}
	return filepath.Join(root, filepath.FromSlash(config.Icon))
}

func ValidatePackageConfig(config PackageConfig) error {
	for field, value := range map[string]string{
		"app_id": config.AppID, "name": config.Name, "publisher": config.Publisher,
		"version": config.Version, "host_exe": config.HostExe, "update_public_key": config.UpdatePublicKey,
		"data_dir": config.DataDir,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("config %s is required", field)
		}
		if strings.ContainsRune(value, '\x00') {
			return fmt.Errorf("config %s contains NUL", field)
		}
	}
	if _, err := CompareInstallerVersions(config.Version, config.Version); err != nil {
		return fmt.Errorf("config version: %w", err)
	}
	if strings.ContainsAny(config.Name, `/\\:`) || config.Name == "." || config.Name == ".." || strings.TrimSpace(config.Name) != config.Name {
		return fmt.Errorf("config name must be a plain Windows folder name")
	}
	if strings.HasSuffix(config.Name, ".") || strings.HasSuffix(config.Name, " ") {
		return fmt.Errorf("config name cannot end with a dot or space")
	}
	if strings.ContainsAny(config.AppID, `/\\:`) {
		return fmt.Errorf("config app_id cannot contain path separators or colons")
	}
	if !isWindowsAbsoluteOrEnvironmentPath(config.DataDir) {
		return fmt.Errorf("config data_dir must be an absolute Windows path or start with a %%ENV_VAR%% path")
	}
	if filepath.Base(config.HostExe) != config.HostExe || strings.ContainsAny(config.HostExe, `/\\:`) {
		return fmt.Errorf("config host_exe must be a file name")
	}
	if config.Icon != "" {
		if _, err := safeArchivePath(config.Icon); err != nil {
			return fmt.Errorf("config icon: %w", err)
		}
	}
	publicKey, err := decodePublicKey(config.UpdatePublicKey)
	if err != nil || len(publicKey) != 32 {
		return fmt.Errorf("config update_public_key must be a base64 or hex Ed25519 public key")
	}
	return nil
}

// inspectInstallRootForUpgrade permits a missing or empty destination and
// returns the record for an existing install owned by appID. A non-empty
// directory is never treated as an install unless its record matches both the
// app ID and the directory being upgraded.
func inspectInstallRootForUpgrade(root, appID string) (installationRecord, error) {
	info, err := os.Lstat(root)
	if os.IsNotExist(err) {
		return installationRecord{}, nil
	}
	if err != nil {
		return installationRecord{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return installationRecord{}, fmt.Errorf("install folder %q is a symbolic link", root)
	}
	if !info.IsDir() {
		return installationRecord{}, fmt.Errorf("install folder %q exists and is not a directory", root)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return installationRecord{}, fmt.Errorf("inspect install folder %q: %w", root, err)
	}
	if len(entries) == 0 {
		return installationRecord{}, nil
	}
	record, err := readMatchingInstallRecord(root, appID)
	if err != nil {
		return installationRecord{}, fmt.Errorf("install folder %q is not a valid GoSX install for app %q: %w", root, appID, err)
	}
	return record, nil
}

func readMatchingInstallRecord(root, appID string) (installationRecord, error) {
	if strings.TrimSpace(appID) == "" {
		return installationRecord{}, fmt.Errorf("expected app ID is empty")
	}
	data, err := os.ReadFile(filepath.Join(root, "install.json"))
	if err != nil {
		return installationRecord{}, fmt.Errorf("read install.json: %w", err)
	}
	var record installationRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return installationRecord{}, fmt.Errorf("decode install.json: %w", err)
	}
	if record.Config.AppID != appID {
		return installationRecord{}, fmt.Errorf("install.json app ID %q does not match %q", record.Config.AppID, appID)
	}
	if !sameInstallPath(record.InstallRoot, root) {
		return installationRecord{}, fmt.Errorf("install.json root %q does not match this folder", record.InstallRoot)
	}
	return record, nil
}

func readRemovableInstallRecord(root, appID string, protectedRoots []string) (installationRecord, error) {
	info, err := os.Lstat(root)
	if err != nil {
		return installationRecord{}, fmt.Errorf("inspect install folder %q: %w", root, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return installationRecord{}, fmt.Errorf("refuse to remove install folder %q: it is not a regular directory", root)
	}
	if isFilesystemRoot(root) {
		return installationRecord{}, fmt.Errorf("refuse to remove install folder %q: it is a drive root", root)
	}
	for _, protectedRoot := range protectedRoots {
		if protectedRoot != "" && sameInstallPath(root, protectedRoot) {
			return installationRecord{}, fmt.Errorf("refuse to remove install folder %q: it is a protected system folder", root)
		}
	}
	record, err := readMatchingInstallRecord(root, appID)
	if err != nil {
		return installationRecord{}, fmt.Errorf("refuse to remove install folder %q: %w", root, err)
	}
	return record, nil
}

func sameInstallPath(a, b string) bool {
	cleanA, windowsA, errA := canonicalInstallPath(a)
	cleanB, windowsB, errB := canonicalInstallPath(b)
	if errA != nil || errB != nil || windowsA != windowsB {
		return false
	}
	if windowsA {
		return strings.EqualFold(cleanA, cleanB)
	}
	return cleanA == cleanB
}

func isFilesystemRoot(value string) bool {
	clean, windowsPath, err := canonicalInstallPath(value)
	if err != nil {
		return false
	}
	if windowsPath {
		if len(clean) == 3 && clean[1] == ':' && clean[2] == '/' {
			return true
		}
		if strings.HasPrefix(clean, "//") {
			parts := strings.Split(strings.Trim(clean, "/"), "/")
			return len(parts) == 2
		}
		return false
	}
	return clean == string(filepath.Separator)
}

func canonicalInstallPath(value string) (string, bool, error) {
	if strings.TrimSpace(value) == "" {
		return "", false, fmt.Errorf("path is empty")
	}
	slash := strings.ReplaceAll(value, `\`, "/")
	if len(slash) >= 3 && isASCIILetter(slash[0]) && slash[1] == ':' && slash[2] == '/' {
		drive := strings.ToUpper(slash[:1]) + ":"
		return drive + path.Clean("/"+strings.TrimLeft(slash[2:], "/")), true, nil
	}
	if strings.HasPrefix(slash, "//") {
		clean := path.Clean("/" + strings.TrimLeft(slash, "/"))
		return "//" + strings.TrimPrefix(clean, "/"), true, nil
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", false, err
	}
	if runtime.GOOS == "windows" {
		return strings.ReplaceAll(filepath.Clean(absolute), `\`, "/"), true, nil
	}
	return filepath.Clean(absolute), false, nil
}

func isASCIILetter(value byte) bool {
	return value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z'
}

func CompareInstallerVersions(a, b string) (int, error) {
	parse := func(value string) ([]uint64, error) {
		value = strings.TrimSpace(strings.TrimPrefix(value, "v"))
		parts := strings.Split(value, ".")
		if len(parts) < 2 || len(parts) > 4 {
			return nil, fmt.Errorf("version %q must have two to four numeric components", value)
		}
		out := make([]uint64, 4)
		for i, part := range parts {
			if part == "" {
				return nil, fmt.Errorf("version %q has an empty component", value)
			}
			n, err := strconv.ParseUint(part, 10, 32)
			if err != nil {
				return nil, fmt.Errorf("version %q has a non-numeric component %q", value, part)
			}
			out[i] = n
		}
		return out, nil
	}
	aa, err := parse(a)
	if err != nil {
		return 0, err
	}
	bb, err := parse(b)
	if err != nil {
		return 0, err
	}
	for i := range aa {
		if aa[i] < bb[i] {
			return -1, nil
		}
		if aa[i] > bb[i] {
			return 1, nil
		}
	}
	return 0, nil
}

func safeArchivePath(value string) (string, error) {
	value = strings.ReplaceAll(value, `\`, "/")
	if value == "" || strings.HasPrefix(value, "/") || strings.Contains(value, ":") {
		return "", fmt.Errorf("invalid archive path %q", value)
	}
	clean := path.Clean(value)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("archive path escapes its root: %q", value)
	}
	return clean, nil
}

func decodePublicKey(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if decoded, err := base64.StdEncoding.DecodeString(value); err == nil {
		return decoded, nil
	}
	return hex.DecodeString(value)
}

func isWindowsAbsoluteOrEnvironmentPath(value string) bool {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, `\\`) {
		return true
	}
	if len(value) >= 3 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) && value[1] == ':' && (value[2] == '\\' || value[2] == '/') {
		return true
	}
	if strings.HasPrefix(value, "%") {
		end := strings.Index(value[1:], "%")
		if end <= 0 {
			return false
		}
		end++
		return len(value) > end+1 && (value[end+1] == '\\' || value[end+1] == '/')
	}
	return false
}

// FindSetupOverlay locates the appended zip payload in a Setup PE. The footer
// scan tolerates an Authenticode certificate appended after the payload.
func FindSetupOverlay(file io.ReaderAt, fileSize int64) (int64, uint64, error) {
	const blockSize = 64 * 1024
	if fileSize < 32 {
		return 0, 0, fmt.Errorf("setup file is too small")
	}
	for end := fileSize; end > 0; {
		start := end - blockSize
		if start < 0 {
			start = 0
		}
		readStart := start
		if readStart > 0 {
			readStart -= 24
		}
		buffer := make([]byte, end-readStart)
		if _, err := file.ReadAt(buffer, readStart); err != nil && !errors.Is(err, io.EOF) {
			return 0, 0, err
		}
		for search := len(buffer); search > 0; {
			rel := bytes.LastIndex(buffer[:search], []byte(setupTrailer))
			if rel < 0 {
				break
			}
			trailerOffset := readStart + int64(rel)
			if trailerOffset+16 <= fileSize {
				lengthBytes := make([]byte, 8)
				if _, err := file.ReadAt(lengthBytes, trailerOffset+8); err == nil {
					length := decodeUint64(lengthBytes)
					prefixOffset := trailerOffset - int64(length) - 16
					if prefixOffset >= 0 && length <= maxPayloadBytes {
						prefix := make([]byte, 16)
						if _, err := file.ReadAt(prefix, prefixOffset); err == nil && string(prefix[:8]) == setupPrefix && decodeUint64(prefix[8:]) == length {
							return prefixOffset + 16, length, nil
						}
					}
				}
			}
			search = rel
		}
		if start == 0 {
			break
		}
		end = start + int64(len(setupTrailer)) - 1
	}
	return 0, 0, fmt.Errorf("setup payload footer was not found")
}

func decodeUint64(data []byte) uint64 {
	var value uint64
	for i := 0; i < 8; i++ {
		value |= uint64(data[i]) << (8 * i)
	}
	return value
}

func HashBytes(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func writeZipFile(zw *zip.Writer, name string, data []byte, mode fs.FileMode) error {
	header := &zip.FileHeader{Name: name, Method: zipMethod(name)}
	header.SetModTime(time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC))
	header.SetMode(mode)
	w, err := zw.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

func zipMethod(name string) uint16 {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".exe", ".dll", ".sys", ".png", ".jpg", ".jpeg", ".webp", ".ogg", ".mp3", ".mp4", ".zip", ".gz", ".br", ".pak", ".bundle", ".assetbundle":
		return zip.Store
	default:
		return zip.Deflate
	}
}

func writeZipSource(zw *zip.Writer, name, source string, mode fs.FileMode) (string, error) {
	header := &zip.FileHeader{Name: name, Method: zipMethod(name)}
	header.SetModTime(time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC))
	header.SetMode(mode)
	dst, err := zw.CreateHeader(header)
	if err != nil {
		return "", err
	}
	file, err := os.Open(source)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(dst, hash), file)
	closeErr := file.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func CollectStageFiles(root string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("staged app contains a symlink: %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("staged app contains a non-regular file: %s", path)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		archiveName, err := safeArchivePath(rel)
		if err != nil {
			return err
		}
		lowerName := strings.ToLower(archiveName)
		if lowerName == "__gosx__" || strings.HasPrefix(lowerName, "__gosx__/") || strings.EqualFold(archiveName, "SHA256SUMS") {
			return fmt.Errorf("staged app uses reserved installer path: %s", archiveName)
		}
		if strings.EqualFold(filepath.ToSlash(rel), "uninstall.exe") {
			return fmt.Errorf("staged app uses reserved installer path: uninstall.exe")
		}
		paths = append(paths, path)
		return nil
	})
	sort.Strings(paths)
	return paths, err
}

func MakePortableZip(stage string) ([]byte, map[string]string, error) {
	var out bytes.Buffer
	files, err := WritePortableZip(stage, &out)
	if err != nil {
		return nil, nil, err
	}
	return out.Bytes(), files, nil
}

// WritePortableZip streams a portable package to dst while hashing each app
// file. Large staged games do not have to fit in memory.
func WritePortableZip(stage string, dst io.Writer) (map[string]string, error) {
	paths, err := CollectStageFiles(stage)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, errors.New("staged app folder is empty")
	}
	zw := zip.NewWriter(dst)
	files := make(map[string]string, len(paths))
	for _, path := range paths {
		rel, _ := filepath.Rel(stage, path)
		name, _ := safeArchivePath(rel)
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		digest, err := writeZipSource(zw, name, path, info.Mode())
		if err != nil {
			return nil, err
		}
		files[name] = digest
	}
	var sums strings.Builder
	for _, name := range sortedKeys(files) {
		fmt.Fprintf(&sums, "%s  %s\n", files[name], name)
	}
	if err := writeZipFile(zw, "SHA256SUMS", []byte(sums.String()), 0644); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return files, nil
}

func MakeSetupPayload(stage, bootstrapper, uninstaller string, config PackageConfig) ([]byte, error) {
	var out bytes.Buffer
	if err := WriteSetupPayload(stage, bootstrapper, uninstaller, config, &out); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// WriteSetupPayload creates the signed installer overlay with streaming file
// copies and appends its SHA-256 manifest last.
// WriteSetupPayload adds a small standalone uninstaller PE to
// the verified payload. Setup extracts it as uninstall.exe instead of copying
// the full installer, which may also contain a multi-gigabyte game payload.
func WriteSetupPayload(stage, bootstrapper, uninstaller string, config PackageConfig, dst io.Writer) error {
	paths, err := CollectStageFiles(stage)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return errors.New("staged app folder is empty")
	}
	if _, err := os.Stat(filepath.Join(stage, config.HostExe)); err != nil {
		return fmt.Errorf("host executable %q is missing: %w", config.HostExe, err)
	}
	if config.Icon != "" {
		if _, err := os.Stat(filepath.Join(stage, filepath.FromSlash(config.Icon))); err != nil {
			return fmt.Errorf("icon %q is missing: %w", config.Icon, err)
		}
	}
	if uninstaller != "" {
		f, err := os.Open(uninstaller)
		if err != nil {
			return fmt.Errorf("open uninstaller executable: %w", err)
		}
		var magic [2]byte
		_, readErr := io.ReadFull(f, magic[:])
		closeErr := f.Close()
		if readErr != nil {
			return fmt.Errorf("read uninstaller executable: %w", readErr)
		}
		if closeErr != nil {
			return closeErr
		}
		if magic != [2]byte{'M', 'Z'} {
			return fmt.Errorf("uninstaller executable does not have a Windows PE signature")
		}
	}
	bootstrapperHash, err := HashFile(bootstrapper)
	if err != nil {
		return fmt.Errorf("hash WebView2 bootstrapper: %w", err)
	}
	if config.WebView2SHA256 != "" && !strings.EqualFold(config.WebView2SHA256, bootstrapperHash) {
		return fmt.Errorf("WebView2 bootstrapper SHA-256 does not match config: expected %s, got %s", config.WebView2SHA256, bootstrapperHash)
	}
	config.WebView2Bootstrapper = "MicrosoftEdgeWebView2Setup.exe"
	config.WebView2SHA256 = bootstrapperHash
	configBytes, err := json.Marshal(config)
	if err != nil {
		return err
	}
	manifest := PayloadManifest{Files: make(map[string]string, len(paths)+2)}
	zw := zip.NewWriter(dst)
	for _, path := range paths {
		rel, _ := filepath.Rel(stage, path)
		name, _ := safeArchivePath(rel)
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		digest, err := writeZipSource(zw, name, path, info.Mode())
		if err != nil {
			return err
		}
		manifest.Files[name] = digest
	}
	if err := writeZipFile(zw, payloadConfigName, configBytes, 0600); err != nil {
		return err
	}
	manifest.Files[payloadConfigName] = HashBytes(configBytes)
	digest, err := writeZipSource(zw, payloadBootstrapper, bootstrapper, 0600)
	if err != nil {
		return err
	}
	manifest.Files[payloadBootstrapper] = digest
	if uninstaller != "" {
		digest, err := writeZipSource(zw, payloadUninstaller, uninstaller, 0700)
		if err != nil {
			return fmt.Errorf("add uninstaller executable: %w", err)
		}
		manifest.Files[payloadUninstaller] = digest
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	if err := writeZipFile(zw, payloadManifestName, manifestBytes, 0600); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return nil
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
