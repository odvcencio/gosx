//go:build windows && (amd64 || arm64)

package installerhost

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const (
	maxFileBytes = 16 << 30
	maxZipFiles  = 200000
)

var errInstallerCancelled = errors.New("installation cancelled by user")

type installOverrides struct {
	installRoot        string
	uninstallRoot      string
	startMenu          string
	registryKey        string
	workDir            string
	failExtract        bool
	uninstallParentPID uint32
}

type installationRecord struct {
	Config        PackageConfig `json:"config"`
	InstallRoot   string        `json:"install_root"`
	StartMenu     string        `json:"start_menu"`
	UninstallKey  string        `json:"uninstall_key"`
	DesktopLink   string        `json:"desktop_link,omitempty"`
	DataDirectory string        `json:"data_directory"`
}

type verifiedPayload struct {
	archive   *zip.Reader
	config    PackageConfig
	setupFile *os.File
}

func Run() {
	runtime.LockOSThread()
	args := os.Args[1:]
	code := runInstaller(args)
	os.Exit(code)
}

func runInstaller(args []string) int {
	silent, uninstall, worker, deleteData, err := parseInstallerArgs(args)
	if err != nil {
		installerError("Installer arguments are invalid", err, false)
		return 2
	}
	overrides, err := parseInstallOverrides(args)
	if err != nil {
		installerError("Installer test overrides are invalid", err, silent)
		return 2
	}
	if uninstall {
		if worker {
			if overrides.uninstallParentPID != 0 {
				if err := waitForUninstallParent(overrides.uninstallParentPID); err != nil {
					writeInstallerTestDiagnostic(overrides, err)
					installerError("Uninstall failed", err, silent)
					return 1
				}
			}
			err = runUninstaller(silent, deleteData, overrides)
		} else {
			err = startUninstallWorker(args, overrides)
		}
		if err != nil {
			writeInstallerTestDiagnostic(overrides, err)
			installerError("Uninstall failed", err, silent)
			return 1
		}
		return 0
	}
	payload, err := readVerifiedPayload()
	if err != nil {
		writeInstallerTestDiagnostic(overrides, err)
		installerError("The setup payload could not be verified", err, silent)
		return 1
	}
	defer payload.setupFile.Close()
	if err := installPayload(payload, overrides, silent); err != nil {
		if errors.Is(err, errInstallerCancelled) {
			return 0
		}
		writeInstallerTestDiagnostic(overrides, err)
		installerError("Installation failed", err, silent)
		return 1
	}
	return 0
}

func writeInstallerTestDiagnostic(overrides installOverrides, err error) {
	if overrides.workDir == "" || err == nil {
		return
	}
	_ = os.MkdirAll(overrides.workDir, 0755)
	_ = os.WriteFile(filepath.Join(overrides.workDir, "installer-error.log"), []byte(err.Error()+"\r\n"), 0600)
}

func parseInstallerArgs(args []string) (silent, uninstall, worker, deleteData bool, err error) {
	for _, arg := range args {
		switch strings.ToLower(strings.TrimSpace(arg)) {
		case "/s", "--silent":
			silent = true
		case "--uninstall":
			uninstall = true
		case "--uninstall-worker":
			uninstall, worker = true, true
		case "/delete-data", "--delete-data":
			deleteData = true
		}
	}
	return silent, uninstall, worker, deleteData, nil
}

func parseInstallOverrides(args []string) (installOverrides, error) {
	var out installOverrides
	for i := 0; i < len(args); i++ {
		arg := args[i]
		key, value, hasValue := strings.Cut(arg, "=")
		if !hasValue && i+1 < len(args) && strings.HasPrefix(arg, "--test-") && key != "--test-fail-after-extract" {
			value = args[i+1]
			i++
		}
		switch key {
		case "--test-install-root":
			out.installRoot = value
		case "--uninstall-root":
			out.uninstallRoot = value
		case "--uninstall-parent-pid":
			pid, err := strconv.ParseUint(value, 10, 32)
			if err != nil || pid == 0 {
				return installOverrides{}, fmt.Errorf("invalid uninstall parent process ID %q", value)
			}
			out.uninstallParentPID = uint32(pid)
		case "--test-start-menu":
			out.startMenu = value
		case "--test-uninstall-key":
			out.registryKey = value
		case "--test-work-dir":
			out.workDir = value
		case "--test-fail-after-extract":
			out.failExtract = true
		}
	}
	provided := 0
	for _, value := range []string{out.installRoot, out.startMenu, out.registryKey} {
		if value != "" {
			provided++
		}
	}
	if provided != 0 && provided != 3 {
		return installOverrides{}, fmt.Errorf("test install root, start menu and uninstall key must be supplied together")
	}
	if out.registryKey != "" && !strings.HasPrefix(out.registryKey, "GoSXTest-") {
		return installOverrides{}, fmt.Errorf("test uninstall key must start with GoSXTest-")
	}
	if strings.ContainsAny(out.registryKey, `/\\:`) {
		return installOverrides{}, fmt.Errorf("uninstall key must be a single registry key name")
	}
	for _, path := range []string{out.installRoot, out.startMenu, out.workDir} {
		if strings.ContainsRune(path, '\x00') {
			return installOverrides{}, fmt.Errorf("test path contains NUL")
		}
	}
	return out, nil
}

func readVerifiedPayload() (*verifiedPayload, error) {
	setup, err := os.Open(os.Args[0])
	if err != nil {
		return nil, err
	}
	keepOpen := false
	defer func() {
		if !keepOpen {
			_ = setup.Close()
		}
	}()
	info, err := setup.Stat()
	if err != nil {
		return nil, err
	}
	offset, length, err := FindSetupOverlay(setup, info.Size())
	if err != nil {
		return nil, err
	}
	if length > maxPayloadBytes {
		return nil, fmt.Errorf("payload exceeds the %d-byte safety limit", maxPayloadBytes)
	}
	section := io.NewSectionReader(setup, offset, int64(length))
	archive, err := zip.NewReader(section, int64(length))
	if err != nil {
		return nil, fmt.Errorf("open setup zip payload: %w", err)
	}
	if len(archive.File) > maxZipFiles {
		return nil, fmt.Errorf("payload contains too many files")
	}
	var manifestFile *zip.File
	entries := make(map[string]*zip.File, len(archive.File))
	seen := make(map[string]string, len(archive.File))
	var total uint64
	for _, entry := range archive.File {
		name, err := safeArchivePath(entry.Name)
		if err != nil || name != entry.Name {
			return nil, fmt.Errorf("invalid path in payload: %q", entry.Name)
		}
		folded := strings.ToLower(name)
		if original, ok := seen[folded]; ok {
			return nil, fmt.Errorf("duplicate Windows payload paths %q and %q", original, name)
		}
		seen[folded] = name
		if entry.Mode()&os.ModeSymlink != 0 || !entry.Mode().IsRegular() {
			return nil, fmt.Errorf("payload entry is not a regular file: %s", name)
		}
		if folded == "uninstall.exe" {
			return nil, fmt.Errorf("payload entry conflicts with the generated uninstaller")
		}
		if entry.UncompressedSize64 > maxFileBytes {
			return nil, fmt.Errorf("payload file exceeds the safety limit: %s", name)
		}
		total += entry.UncompressedSize64
		if total > maxPayloadBytes {
			return nil, fmt.Errorf("payload expands beyond the safety limit")
		}
		entries[name] = entry
		if name == payloadManifestName {
			manifestFile = entry
		}
	}
	if manifestFile == nil {
		return nil, fmt.Errorf("payload SHA-256 manifest is missing")
	}
	manifestBytes, err := readZipEntry(manifestFile, 4<<20)
	if err != nil {
		return nil, fmt.Errorf("read payload SHA-256 manifest: %w", err)
	}
	var manifest PayloadManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return nil, fmt.Errorf("decode payload SHA-256 manifest: %w", err)
	}
	if len(manifest.Files) != len(entries)-1 {
		return nil, fmt.Errorf("payload SHA-256 manifest does not list every payload file")
	}
	for name, entry := range entries {
		if name == payloadManifestName {
			continue
		}
		expected, ok := manifest.Files[name]
		if !ok {
			return nil, fmt.Errorf("payload SHA-256 manifest omits %s", name)
		}
		digest, err := hashZipEntry(entry, maxFileBytes)
		if err != nil {
			return nil, fmt.Errorf("hash payload file %s: %w", name, err)
		}
		if !strings.EqualFold(expected, digest) {
			return nil, fmt.Errorf("payload SHA-256 mismatch for %s", name)
		}
	}
	configEntry := entries[payloadConfigName]
	if configEntry == nil || entries[payloadBootstrapper] == nil || entries[payloadUninstaller] == nil {
		return nil, fmt.Errorf("payload is missing installer metadata, the WebView2 bootstrapper, or the standalone uninstaller")
	}
	uninstallerReader, err := entries[payloadUninstaller].Open()
	if err != nil {
		return nil, fmt.Errorf("open standalone uninstaller: %w", err)
	}
	var uninstallerMagic [2]byte
	_, readUninstallerErr := io.ReadFull(uninstallerReader, uninstallerMagic[:])
	closeUninstallerErr := uninstallerReader.Close()
	if readUninstallerErr != nil {
		return nil, fmt.Errorf("read standalone uninstaller: %w", readUninstallerErr)
	}
	if closeUninstallerErr != nil {
		return nil, closeUninstallerErr
	}
	if uninstallerMagic != [2]byte{'M', 'Z'} {
		return nil, fmt.Errorf("standalone uninstaller is not a Windows executable")
	}
	configBytes, err := readZipEntry(configEntry, 1<<20)
	if err != nil {
		return nil, err
	}
	var config PackageConfig
	if err := json.Unmarshal(configBytes, &config); err != nil {
		return nil, fmt.Errorf("decode setup configuration: %w", err)
	}
	if err := ValidatePackageConfig(config); err != nil {
		return nil, fmt.Errorf("setup configuration is invalid: %w", err)
	}
	keepOpen = true
	return &verifiedPayload{archive: archive, config: config, setupFile: setup}, nil
}

func readZipEntry(entry *zip.File, limit uint64) ([]byte, error) {
	if entry.UncompressedSize64 > limit {
		return nil, fmt.Errorf("payload entry exceeds read limit")
	}
	r, err := entry.Open()
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(r, int64(limit)+1))
	closeErr := r.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if uint64(len(data)) > limit {
		return nil, fmt.Errorf("payload entry exceeds read limit")
	}
	return data, nil
}

func hashZipEntry(entry *zip.File, limit uint64) (string, error) {
	if entry.UncompressedSize64 > limit {
		return "", fmt.Errorf("payload entry exceeds hash limit")
	}
	r, err := entry.Open()
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	n, readErr := io.Copy(hash, io.LimitReader(r, int64(limit)+1))
	closeErr := r.Close()
	if readErr != nil {
		return "", readErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	if uint64(n) > limit {
		return "", fmt.Errorf("payload entry exceeds hash limit")
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func installPayload(payload *verifiedPayload, overrides installOverrides, silent bool) error {
	config := payload.config
	installRoot := overrides.installRoot
	if installRoot == "" {
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData == "" {
			return fmt.Errorf("LOCALAPPDATA is not set")
		}
		installRoot = filepath.Join(localAppData, "Programs", config.Name)
	}
	installRoot, err := filepath.Abs(installRoot)
	if err != nil {
		return err
	}
	startMenu := overrides.startMenu
	if startMenu == "" {
		configDir, err := os.UserConfigDir()
		if err != nil {
			return err
		}
		startMenu = filepath.Join(configDir, "Microsoft", "Windows", "Start Menu", "Programs", config.Name)
	}
	startMenu, err = filepath.Abs(startMenu)
	if err != nil {
		return err
	}
	registryKey := overrides.registryKey
	if registryKey == "" {
		registryKey = "GoSX_" + strings.NewReplacer(".", "_", "-", "_").Replace(config.AppID)
	}
	if info, err := os.Stat(installRoot); err == nil && info.IsDir() {
		if oldConfig, loadErr := readInstalledConfig(installRoot); loadErr == nil {
			cmp, compareErr := CompareInstallerVersions(config.Version, oldConfig.Version)
			if compareErr != nil {
				return compareErr
			}
			if cmp < 0 {
				message := fmt.Sprintf("An older version (%s) is already installed. Install version %s anyway?", oldConfig.Version, config.Version)
				if silent {
					return fmt.Errorf("cannot downgrade to %s in silent mode", config.Version)
				}
				if !askYesNo("Install older version?", message, false) {
					return errInstallerCancelled
				}
			}
		}
	}
	if err := closeRunningApp(filepath.Join(installRoot, config.HostExe), silent); err != nil {
		return err
	}
	progress := newInstallProgress(config.Name, silent)
	defer progress.Close()
	if err := progress.Set("Checking WebView2 Runtime..."); err != nil {
		return err
	}
	if !webView2RuntimeInstalled() {
		if err := installWebView2Bootstrapper(payload, overrides, silent); err != nil {
			return err
		}
		if !webView2RuntimeInstalled() {
			return fmt.Errorf("WebView2 Runtime still is not available after bootstrapper completed")
		}
	}
	parent := filepath.Dir(installRoot)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return err
	}
	stageRoot := installRoot + fmt.Sprintf(".gosx-new-%d", os.Getpid())
	backupRoot := installRoot + fmt.Sprintf(".gosx-old-%d", os.Getpid())
	_ = os.RemoveAll(stageRoot)
	_ = os.RemoveAll(backupRoot)
	if err := os.Mkdir(stageRoot, 0755); err != nil {
		return err
	}
	stageReady := false
	defer func() {
		if !stageReady {
			_ = os.RemoveAll(stageRoot)
		}
	}()
	if err := progress.Set("Extracting application files..."); err != nil {
		return err
	}
	if err := extractPayload(payload, stageRoot, progress); err != nil {
		return err
	}
	if overrides.failExtract {
		return fmt.Errorf("test interruption after extraction")
	}
	dataDirectory, err := expandWindowsEnvironment(config.DataDir)
	if err != nil {
		return fmt.Errorf("resolve data_dir: %w", err)
	}
	if err := validateDataDirectory(dataDirectory, installRoot); err != nil {
		return err
	}
	record := installationRecord{
		Config: config, InstallRoot: installRoot, StartMenu: startMenu,
		UninstallKey: registryKey, DataDirectory: dataDirectory,
	}
	if config.DesktopShortcut {
		publicDesktop, err := shellFolderDesktop()
		if err != nil {
			return err
		}
		record.DesktopLink = filepath.Join(publicDesktop, config.Name+".lnk")
	}
	recordBytes, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(stageRoot, "install.json"), recordBytes, 0600); err != nil {
		return err
	}
	if err := progress.Set("Preparing shortcuts and uninstall entry..."); err != nil {
		return err
	}
	oldMoved := false
	if _, err := os.Stat(installRoot); err == nil {
		if err := os.Rename(installRoot, backupRoot); err != nil {
			return fmt.Errorf("move current install aside: %w", err)
		}
		oldMoved = true
	}
	if err := os.Rename(stageRoot, installRoot); err != nil {
		if oldMoved {
			_ = os.Rename(backupRoot, installRoot)
		}
		return fmt.Errorf("activate new install: %w", err)
	}
	stageReady = true
	shortcutPath := filepath.Join(startMenu, config.Name+".lnk")
	if err := createShellShortcut(shortcutPath, filepath.Join(installRoot, config.HostExe), installRoot, config.IconPath(installRoot), config.Name); err != nil {
		_ = os.RemoveAll(installRoot)
		if oldMoved {
			_ = os.Rename(backupRoot, installRoot)
		}
		return fmt.Errorf("create Start menu shortcut: %w", err)
	}
	if record.DesktopLink != "" {
		if err := createShellShortcut(record.DesktopLink, filepath.Join(installRoot, config.HostExe), installRoot, config.IconPath(installRoot), config.Name); err != nil {
			_ = os.Remove(shortcutPath)
			_ = os.RemoveAll(installRoot)
			if oldMoved {
				_ = os.Rename(backupRoot, installRoot)
			}
			return fmt.Errorf("create desktop shortcut: %w", err)
		}
	}
	if err := writeUninstallEntry(record); err != nil {
		_ = deleteUninstallEntry(registryKey)
		_ = os.Remove(shortcutPath)
		if record.DesktopLink != "" {
			_ = os.Remove(record.DesktopLink)
		}
		_ = os.RemoveAll(installRoot)
		if oldMoved {
			_ = os.Rename(backupRoot, installRoot)
		}
		return fmt.Errorf("write current-user uninstall entry: %w", err)
	}
	if oldMoved {
		_ = os.RemoveAll(backupRoot)
	}
	if err := progress.Set("Installation complete."); err != nil {
		return err
	}
	if !silent && askYesNo("Launch "+config.Name+"?", "Installation is complete. Start the app now?", true) {
		cmd := exec.Command(filepath.Join(installRoot, config.HostExe))
		cmd.Dir = installRoot
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("launch app: %w", err)
		}
		_ = cmd.Process.Release()
	}
	return nil
}

func extractPayload(payload *verifiedPayload, root string, progress *installProgress) error {
	for _, entry := range payload.archive.File {
		if entry.Name == payloadManifestName || entry.Name == payloadConfigName || entry.Name == payloadBootstrapper {
			continue
		}
		name := "uninstall.exe"
		if entry.Name != payloadUninstaller {
			var err error
			name, err = safeArchivePath(entry.Name)
			if err != nil {
				return err
			}
		}
		target := filepath.Join(root, filepath.FromSlash(name))
		if !pathWithin(root, target) {
			return fmt.Errorf("payload path escapes the install root: %s", name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		r, err := entry.Open()
		if err != nil {
			return err
		}
		mode := entry.Mode().Perm()
		if mode == 0 {
			mode = 0644
		}
		f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err != nil {
			_ = r.Close()
			return err
		}
		_, copyErr := io.Copy(f, io.LimitReader(r, int64(maxFileBytes)+1))
		closeFileErr := f.Close()
		closeZipErr := r.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeFileErr != nil {
			return closeFileErr
		}
		if closeZipErr != nil {
			return closeZipErr
		}
		if err := progress.Pump(); err != nil {
			return err
		}
	}
	if _, err := os.Stat(filepath.Join(root, payload.config.HostExe)); err != nil {
		return fmt.Errorf("extracted host executable is missing: %w", err)
	}
	return nil
}

func installWebView2Bootstrapper(payload *verifiedPayload, overrides installOverrides, silent bool) error {
	entry := (*zip.File)(nil)
	for _, file := range payload.archive.File {
		if file.Name == payloadBootstrapper {
			entry = file
			break
		}
	}
	if entry == nil {
		return fmt.Errorf("bundled WebView2 bootstrapper is missing")
	}
	workDir := overrides.workDir
	if workDir == "" {
		workDir = os.TempDir()
	}
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return err
	}
	bootstrapper := filepath.Join(workDir, fmt.Sprintf("gosx-webview2-%d.exe", os.Getpid()))
	data, err := readZipEntry(entry, 32<<20)
	if err != nil {
		return err
	}
	if err := os.WriteFile(bootstrapper, data, 0700); err != nil {
		return err
	}
	defer os.Remove(bootstrapper)
	command := exec.Command(bootstrapper, "/silent", "/install")
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := command.Run(); err != nil {
		message := "WebView2 Runtime could not be installed. Check your internet connection and try again. You can also install it from https://developer.microsoft.com/microsoft-edge/webview2/."
		if !silent {
			showMessage("WebView2 Runtime is required", message+"\n\nDetails: "+err.Error(), messageBoxOK|messageBoxIconError)
		}
		return fmt.Errorf("WebView2 bootstrapper failed: %w; the Evergreen bootstrapper needs an internet connection", err)
	}
	return nil
}

func closeRunningApp(hostPath string, silent bool) error {
	for {
		processes, err := runningProcessesForPath(hostPath)
		if err != nil {
			return err
		}
		if len(processes) == 0 {
			return nil
		}
		message := "Please close " + filepath.Base(hostPath) + " before installing the update, then choose Retry."
		if silent || !askRetryCancel("App is running", message) {
			return fmt.Errorf("app is still running; close it and run setup again")
		}
		time.Sleep(300 * time.Millisecond)
	}
}

func readInstalledConfig(root string) (PackageConfig, error) {
	data, err := os.ReadFile(filepath.Join(root, "install.json"))
	if err != nil {
		return PackageConfig{}, err
	}
	var record installationRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return PackageConfig{}, err
	}
	return record.Config, nil
}

func runUninstaller(silent, deleteData bool, overrides installOverrides) error {
	root := overrides.installRoot
	if root == "" {
		root = overrides.uninstallRoot
	}
	var err error
	if root == "" {
		root, err = filepath.Abs(filepath.Dir(os.Args[0]))
		if err != nil {
			return err
		}
	}
	recordPath := filepath.Join(root, "install.json")
	data, err := os.ReadFile(recordPath)
	if err != nil {
		return fmt.Errorf("read installation record: %w", err)
	}
	var record installationRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return err
	}
	if overrides.installRoot != "" {
		record.InstallRoot = overrides.installRoot
	}
	if overrides.startMenu != "" {
		record.StartMenu = overrides.startMenu
	}
	if overrides.registryKey != "" {
		record.UninstallKey = overrides.registryKey
	}
	if err := validateDataDirectory(record.DataDirectory, record.InstallRoot); err != nil {
		return err
	}
	removeData := deleteData
	if !silent {
		question := "Remove player data too?\n\n" + record.DataDirectory + "\n\nChoose No to keep it."
		removeData = askYesNo("Remove player data?", question, false)
	}
	if removeData && record.DataDirectory == "" {
		return fmt.Errorf("the app did not provide a player data directory")
	}
	if err := os.Remove(filepath.Join(record.StartMenu, record.Config.Name+".lnk")); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove Start menu shortcut: %w", err)
	}
	_ = os.Remove(record.StartMenu)
	if record.DesktopLink != "" {
		if err := os.Remove(record.DesktopLink); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove desktop shortcut: %w", err)
		}
	}
	if err := os.RemoveAll(record.InstallRoot); err != nil {
		return fmt.Errorf("remove install root: %w", err)
	}
	if removeData {
		if err := os.RemoveAll(record.DataDirectory); err != nil {
			return fmt.Errorf("remove player data directory: %w", err)
		}
	}
	if err := deleteUninstallEntry(record.UninstallKey); err != nil {
		return fmt.Errorf("remove current-user uninstall entry: %w", err)
	}
	return nil
}

func startUninstallWorker(args []string, overrides installOverrides) error {
	workDir := overrides.workDir
	if workDir == "" {
		workDir = os.TempDir()
	}
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return err
	}
	workerPath := filepath.Join(workDir, fmt.Sprintf("gosx-uninstall-%d.exe", os.Getpid()))
	if err := copySelfTo(workerPath); err != nil {
		return err
	}
	workerArgs := make([]string, 0, len(args)+1)
	workerArgs = append(workerArgs, "--uninstall-worker")
	workerArgs = append(workerArgs, fmt.Sprintf("--uninstall-parent-pid=%d", os.Getpid()))
	installRoot := overrides.installRoot
	if installRoot == "" {
		installRoot = filepath.Dir(os.Args[0])
	}
	workerArgs = append(workerArgs, "--uninstall-root="+installRoot)
	for _, arg := range args {
		if arg != "--uninstall" && arg != "--uninstall-worker" && !strings.HasPrefix(arg, "--uninstall-root=") {
			workerArgs = append(workerArgs, arg)
		}
	}
	command := exec.Command(workerPath, workerArgs...)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := command.Start(); err != nil {
		_ = os.Remove(workerPath)
		return err
	}
	return command.Process.Release()
}

func waitForUninstallParent(pid uint32) error {
	const (
		processSynchronize = 0x00100000
		waitObject0        = 0
		waitTimeout        = 0x00000102
		waitFailed         = 0xFFFFFFFF
	)
	handle, _, callErr := procOpenProcess.Call(processSynchronize, 0, uintptr(pid))
	if handle == 0 {
		if code, ok := callErr.(syscall.Errno); ok && code == 87 {
			return nil // The parent exited before the worker opened its process handle.
		}
		return fmt.Errorf("open uninstall parent process %d: %w", pid, callErr)
	}
	defer procCloseHandle.Call(handle)
	result, _, callErr := procWaitForSingleObject.Call(handle, 30_000)
	switch uint32(result) {
	case waitObject0:
		return nil
	case waitTimeout:
		return fmt.Errorf("uninstall parent process %d did not exit within 30 seconds", pid)
	case waitFailed:
		return fmt.Errorf("wait for uninstall parent process %d: %w", pid, callErr)
	default:
		return fmt.Errorf("wait for uninstall parent process %d returned unexpected status 0x%x", pid, result)
	}
}

func copySelfTo(target string) error {
	data, err := os.ReadFile(os.Args[0])
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	return os.WriteFile(target, data, 0700)
}

func expandWindowsEnvironment(value string) (string, error) {
	wide, err := syscall.UTF16PtrFromString(value)
	if err != nil {
		return "", err
	}
	needed, _, callErr := procExpandEnvironmentStringsW.Call(uintptr(unsafe.Pointer(wide)), 0, 0)
	if needed == 0 {
		return "", callErr
	}
	buffer := make([]uint16, needed)
	result, _, callErr := procExpandEnvironmentStringsW.Call(uintptr(unsafe.Pointer(wide)), uintptr(unsafe.Pointer(&buffer[0])), uintptr(needed))
	if result == 0 {
		return "", callErr
	}
	return syscall.UTF16ToString(buffer), nil
}

func pathWithin(root, target string) bool {
	rootAbs, rootErr := filepath.Abs(root)
	targetAbs, targetErr := filepath.Abs(target)
	if rootErr != nil || targetErr != nil {
		return false
	}
	rel, err := filepath.Rel(rootAbs, targetAbs)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, `..`+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func validateDataDirectory(dataDirectory, installRoot string) error {
	dataAbs, err := filepath.Abs(dataDirectory)
	if err != nil {
		return fmt.Errorf("resolve player data directory: %w", err)
	}
	rootAbs, err := filepath.Abs(installRoot)
	if err != nil {
		return fmt.Errorf("resolve install root: %w", err)
	}
	if pathWithin(dataAbs, rootAbs) || pathWithin(rootAbs, dataAbs) {
		return fmt.Errorf("player data directory must be separate from the install root")
	}
	return nil
}

func installerError(title string, err error, silent bool) {
	if !silent {
		showMessage(title, err.Error(), messageBoxOK|messageBoxIconError)
	}
}
