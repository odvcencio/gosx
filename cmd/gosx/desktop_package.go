package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/x509"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"m31labs.dev/gosx/cmd/gosx/installerhost"
)

const webView2BootstrapperURL = "https://go.microsoft.com/fwlink/p/?LinkId=2124703"

//go:embed installerhost/*.go
var installerHostSource embed.FS

type desktopPackageFlags struct {
	input           string
	config          string
	output          string
	signCommand     string
	manifestSignCmd string
	manifestKeyFile string
}

func cmdDesktopPackage() {
	if len(os.Args) > 3 && isHelpArg(os.Args[3]) {
		desktopPackageUsage(os.Stdout)
		return
	}
	var options desktopPackageFlags
	fs := flag.NewFlagSet("desktop package", flag.ExitOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&options.input, "input", "", "staged application folder")
	fs.StringVar(&options.config, "config", "", "desktop package JSON config")
	fs.StringVar(&options.output, "output", "dist/desktop", "output directory")
	fs.StringVar(&options.signCommand, "sign-cmd", "", "shell command template; supports {file}, {input}, and {output}")
	fs.StringVar(&options.manifestSignCmd, "manifest-sign-cmd", "", "shell command template that writes a detached signature to {output}")
	fs.StringVar(&options.manifestKeyFile, "manifest-key", "", "Ed25519 private key file for signing latest.json")
	if err := fs.Parse(os.Args[3:]); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 0 || strings.TrimSpace(options.input) == "" || strings.TrimSpace(options.config) == "" {
		desktopPackageUsage(os.Stderr)
		os.Exit(2)
	}
	if err := packageDesktopRelease(options); err != nil {
		fmt.Fprintf(os.Stderr, "gosx desktop package: %v\n", err)
		os.Exit(1)
	}
}

func desktopPackageUsage(w io.Writer) {
	fmt.Fprintf(w, `gosx desktop package - Build direct-download Windows artifacts

Usage:
  gosx desktop package --input <staged-app> --config <config.json> [flags]

Flags:
  --input dir              Staged app folder containing the host executable
  --config file            Package config JSON
  --output dir             Output directory (default dist/desktop)
  --sign-cmd template      Sign each staged PE and then Setup.exe
  --manifest-key file      Ed25519 private key file for latest.json
  --manifest-sign-cmd cmd  Sign latest.json to {output}; conflicts with --manifest-key

The config names the app, host executable, icon, player data directory, update
public key, release notes and download page. If webview2_bootstrapper is omitted,
the Microsoft Evergreen bootstrapper is downloaded while packaging.

`)
}

func packageDesktopRelease(options desktopPackageFlags) error {
	if options.manifestKeyFile != "" && options.manifestSignCmd != "" {
		return fmt.Errorf("choose either --manifest-key or --manifest-sign-cmd")
	}
	configPath, err := filepath.Abs(options.config)
	if err != nil {
		return err
	}
	input, err := filepath.Abs(options.input)
	if err != nil {
		return err
	}
	info, err := os.Stat(input)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("input must be an existing staged app directory: %s", input)
	}
	var config installerhost.PackageConfig
	configFile, err := os.Open(configPath)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(io.LimitReader(configFile, 1<<20))
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&config)
	closeErr := configFile.Close()
	if err != nil {
		return fmt.Errorf("decode config: %w", err)
	}
	if closeErr != nil {
		return closeErr
	}
	if err := installerhost.ValidatePackageConfig(config); err != nil {
		return err
	}
	if strings.TrimSpace(config.DataDir) == "" {
		return fmt.Errorf("config data_dir is required so uninstall can ask whether to remove player data")
	}
	if strings.TrimSpace(config.DownloadPage) == "" {
		return fmt.Errorf("config download_page is required")
	}
	if !strings.HasPrefix(strings.ToLower(config.DownloadPage), "https://") {
		return fmt.Errorf("config download_page must use HTTPS")
	}
	if config.Channel == "" {
		config.Channel = "stable"
	}
	if config.Released == "" {
		config.Released = time.Now().UTC().Format("2006-01-02")
	} else if _, err := time.Parse("2006-01-02", config.Released); err != nil {
		return fmt.Errorf("config released must use YYYY-MM-DD: %w", err)
	}
	if strings.TrimSpace(config.Notes) == "" {
		return fmt.Errorf("config notes is required")
	}
	if config.Channel != "stable" && config.Channel != "beta" && config.Channel != "preview" {
		return fmt.Errorf("config channel must be stable, beta, or preview")
	}
	bootstrapperPath, bootstrapperSource, err := resolveWebView2Bootstrapper(config.WebView2Bootstrapper)
	if err != nil {
		return err
	}
	defer func() {
		if bootstrapperSource == "downloaded" {
			_ = os.Remove(bootstrapperPath)
		}
	}()
	bootstrapperSHA, err := installerhost.HashFile(bootstrapperPath)
	if err != nil {
		return err
	}
	if config.WebView2SHA256 != "" && !strings.EqualFold(config.WebView2SHA256, bootstrapperSHA) {
		return fmt.Errorf("WebView2 bootstrapper SHA-256 mismatch: config %s, actual %s", config.WebView2SHA256, bootstrapperSHA)
	}
	config.WebView2SHA256 = bootstrapperSHA

	workDir, err := os.MkdirTemp("", "gosx-desktop-package-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workDir)
	stage, err := copyStage(input, filepath.Join(workDir, "stage"))
	if err != nil {
		return err
	}
	installerSigned := "unsigned"
	if strings.TrimSpace(options.signCommand) != "" {
		if err := signStagePEFiles(stage, options.signCommand, workDir); err != nil {
			return err
		}
		installerSigned = "signed"
	}

	portablePath := filepath.Join(workDir, "portable.zip")
	portableFile, err := os.Create(portablePath)
	if err != nil {
		return err
	}
	appHashes, zipErr := installerhost.WritePortableZip(stage, portableFile)
	portableCloseErr := portableFile.Close()
	if zipErr != nil {
		return zipErr
	}
	if portableCloseErr != nil {
		return portableCloseErr
	}
	stubPath := filepath.Join(workDir, "setup-stub.exe")
	if err := buildWindowsInstallerHost(stubPath); err != nil {
		return err
	}
	uninstallerPath := filepath.Join(workDir, "uninstaller.exe")
	if err := buildWindowsUninstaller(uninstallerPath, config.AppID); err != nil {
		return fmt.Errorf("build standalone uninstaller: %w", err)
	}
	if strings.TrimSpace(options.signCommand) != "" {
		if err := runSignTemplate(options.signCommand, uninstallerPath, uninstallerPath, filepath.Join(workDir, "uninstaller-signed.exe")); err != nil {
			return fmt.Errorf("sign uninstaller: %w", err)
		}
	}
	payloadPath := filepath.Join(workDir, "payload.zip")
	payloadFile, err := os.Create(payloadPath)
	if err != nil {
		return err
	}
	payloadErr := installerhost.WriteSetupPayload(stage, bootstrapperPath, uninstallerPath, config, payloadFile)
	payloadCloseErr := payloadFile.Close()
	if payloadErr != nil {
		return payloadErr
	}
	if payloadCloseErr != nil {
		return payloadCloseErr
	}
	setupName := safeArtifactName(config.Name) + "-Setup-" + config.Version + ".exe"
	portableName := safeArtifactName(config.Name) + "-" + config.Version + "-portable.zip"
	setupPath := filepath.Join(workDir, setupName)
	if err := appendInstallerPayload(stubPath, setupPath, payloadPath); err != nil {
		return err
	}
	if strings.TrimSpace(options.signCommand) != "" {
		if err := runSignTemplate(options.signCommand, setupPath, setupPath, filepath.Join(workDir, "setup-signed.exe")); err != nil {
			return fmt.Errorf("sign Setup.exe: %w", err)
		}
	}
	portableHash, err := installerhost.HashFile(portablePath)
	if err != nil {
		return err
	}
	setupHash, err := installerhost.HashFile(setupPath)
	if err != nil {
		return err
	}
	manifest := installerhost.UpdateManifest{
		App: config.AppID, Channel: config.Channel, Version: config.Version,
		Released: config.Released, Notes: config.Notes, DownloadPage: config.DownloadPage,
		Artifacts: map[string]string{setupName: setupHash, portableName: portableHash},
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	manifestBytes = append(manifestBytes, '\n')
	manifestPath := filepath.Join(workDir, "latest.json")
	if err := os.WriteFile(manifestPath, manifestBytes, 0644); err != nil {
		return err
	}
	manifestSigned := "unsigned"
	signaturePath := filepath.Join(workDir, "latest.json.sig")
	switch {
	case options.manifestKeyFile != "":
		keyBytes, err := os.ReadFile(options.manifestKeyFile)
		if err != nil {
			return fmt.Errorf("read manifest key: %w", err)
		}
		privateKey, err := decodePrivateKey(keyBytes)
		if err != nil {
			return err
		}
		signature := ed25519.Sign(privateKey, manifestBytes)
		if err := os.WriteFile(signaturePath, []byte(base64.StdEncoding.EncodeToString(signature)+"\n"), 0644); err != nil {
			return err
		}
		manifestSigned = "ed25519-key-file"
	case options.manifestSignCmd != "":
		if err := runSignTemplate(options.manifestSignCmd, manifestPath, manifestPath, signaturePath); err != nil {
			return fmt.Errorf("sign latest.json: %w", err)
		}
		if _, err := os.Stat(signaturePath); err != nil {
			return fmt.Errorf("manifest signer did not create {output}: %w", err)
		}
		manifestSigned = "command"
	}

	metadata := installerhost.PackageMetadata{
		AppID: config.AppID, Name: config.Name, Publisher: config.Publisher,
		Version: config.Version, WebView2SHA256: bootstrapperSHA,
		InstallerSigning: installerSigned, ManifestSigning: manifestSigned,
		BootstrapperSource: bootstrapperSource, GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}
	metadataBytes, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	metadataBytes = append(metadataBytes, '\n')
	metadataPath := filepath.Join(workDir, "package-metadata.json")
	if err := os.WriteFile(metadataPath, metadataBytes, 0644); err != nil {
		return err
	}

	output, err := filepath.Abs(options.output)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(output, 0755); err != nil {
		return err
	}
	portableOut := filepath.Join(output, portableName)
	if err := packageDesktopCopyFile(portablePath, portableOut); err != nil {
		return err
	}
	for _, item := range []struct{ src, name string }{
		{setupPath, setupName}, {manifestPath, "latest.json"}, {metadataPath, "package-metadata.json"},
	} {
		if err := packageDesktopCopyFile(item.src, filepath.Join(output, item.name)); err != nil {
			return err
		}
	}
	if manifestSigned != "unsigned" {
		if err := packageDesktopCopyFile(signaturePath, filepath.Join(output, "latest.json.sig")); err != nil {
			return err
		}
	}
	var sums strings.Builder
	artifactNames := []string{setupName, portableName, "latest.json", "package-metadata.json"}
	if manifestSigned != "unsigned" {
		artifactNames = append(artifactNames, "latest.json.sig")
	}
	sort.Strings(artifactNames)
	for _, name := range artifactNames {
		digest, err := installerhost.HashFile(filepath.Join(output, name))
		if err != nil {
			return err
		}
		fmt.Fprintf(&sums, "%s  %s\n", digest, name)
	}
	if err := os.WriteFile(filepath.Join(output, "SHA256SUMS"), []byte(sums.String()), 0644); err != nil {
		return err
	}
	_ = appHashes // The portable archive carries its own per-file SHA256SUMS.
	fmt.Printf("Packaged %s %s to %s\n", config.Name, config.Version, output)
	fmt.Printf("WebView2 bootstrapper SHA-256: %s (%s)\n", bootstrapperSHA, bootstrapperSource)
	fmt.Printf("Installer signing: %s; manifest signing: %s\n", installerSigned, manifestSigned)
	return nil
}

func copyStage(input, output string) (string, error) {
	root, err := filepath.Abs(input)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(output, 0755); err != nil {
		return "", err
	}
	paths, err := installerhost.CollectStageFiles(root)
	if err != nil {
		return "", err
	}
	for _, source := range paths {
		rel, err := filepath.Rel(root, source)
		if err != nil {
			return "", err
		}
		target := filepath.Join(output, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return "", err
		}
		input, err := os.Open(source)
		if err != nil {
			return "", err
		}
		info, err := os.Stat(source)
		if err != nil {
			_ = input.Close()
			return "", err
		}
		mode := info.Mode().Perm()
		f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err != nil {
			_ = input.Close()
			return "", err
		}
		_, copyErr := io.Copy(f, input)
		closeErr := f.Close()
		readCloseErr := input.Close()
		if copyErr != nil {
			return "", copyErr
		}
		if closeErr != nil {
			return "", closeErr
		}
		if readCloseErr != nil {
			return "", readCloseErr
		}
	}
	return output, nil
}

func packageDesktopCopyFile(input, output string) error {
	source, err := os.Open(input)
	if err != nil {
		return err
	}
	defer source.Close()
	destination, err := os.Create(output)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(destination, source)
	closeErr := destination.Close()
	return errors.Join(copyErr, closeErr)
}

func safeArtifactName(value string) string {
	var out strings.Builder
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' || r == ' ' {
			out.WriteRune(r)
		} else {
			out.WriteByte('-')
		}
	}
	name := strings.TrimSpace(out.String())
	name = strings.Trim(name, ".- ")
	if name == "" {
		return "desktop-app"
	}
	return strings.ReplaceAll(name, " ", "-")
}

func resolveWebView2Bootstrapper(configured string) (string, string, error) {
	if strings.TrimSpace(configured) != "" {
		path, err := filepath.Abs(configured)
		if err != nil {
			return "", "", err
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return "", "", fmt.Errorf("webview2_bootstrapper must be a regular file: %s", path)
		}
		return path, "config", nil
	}
	file, err := os.CreateTemp("", "MicrosoftEdgeWebView2Setup-*.exe")
	if err != nil {
		return "", "", err
	}
	_ = file.Close()
	for attempt := 1; attempt <= 5; attempt++ {
		req, err := http.NewRequest(http.MethodGet, webView2BootstrapperURL, nil)
		if err == nil {
			client := http.Client{Timeout: 3 * time.Minute}
			var response *http.Response
			response, err = client.Do(req)
			if err == nil {
				if response.StatusCode < 200 || response.StatusCode >= 300 {
					err = fmt.Errorf("Microsoft bootstrapper URL returned HTTP %d", response.StatusCode)
				} else {
					out, createErr := os.Create(file.Name())
					if createErr != nil {
						err = createErr
					} else {
						_, copyErr := io.Copy(out, io.LimitReader(response.Body, 32<<20))
						closeErr := out.Close()
						err = errors.Join(copyErr, closeErr)
					}
				}
				_ = response.Body.Close()
				if err == nil {
					return file.Name(), "downloaded", nil
				}
			}
		}
		if !retryableNetworkError(err) || attempt == 5 {
			_ = os.Remove(file.Name())
			return "", "", fmt.Errorf("download WebView2 Evergreen bootstrapper from Microsoft: %w", err)
		}
		fmt.Fprintf(os.Stderr, "WebView2 bootstrapper download attempt %d/5 failed; retrying in 60 seconds: %v\n", attempt, err)
		time.Sleep(60 * time.Second)
	}
	_ = os.Remove(file.Name())
	return "", "", errors.New("download WebView2 bootstrapper failed")
}

func retryableNetworkError(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "eai_again") || strings.Contains(message, "temporary failure in name resolution") || strings.Contains(message, "i/o timeout") || strings.Contains(message, "timed out")
}

func buildWindowsInstallerHost(output string) error {
	return buildWindowsInstallerBinary(output, "package main\n\nimport \"gosx-installer-host/installerhost\"\n\nfunc main() {\n\tinstallerhost.Run()\n}\n")
}

func buildWindowsUninstaller(output, appID string) error {
	mainSource := "package main\n\nimport \"gosx-installer-host/installerhost\"\n\nfunc main() {\n\tinstallerhost.RunForAppID(" + strconv.Quote(appID) + ")\n}\n"
	return buildWindowsInstallerBinary(output, mainSource)
}

func buildWindowsInstallerBinary(output, mainSource string) error {
	workDir, err := os.MkdirTemp("", "gosx-windows-installer-build-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workDir)
	packageDir := filepath.Join(workDir, "installerhost")
	if err := os.MkdirAll(packageDir, 0755); err != nil {
		return err
	}
	entries, err := installerHostSource.ReadDir("installerhost")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := installerHostSource.ReadFile(filepath.ToSlash(filepath.Join("installerhost", name)))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(packageDir, name), data, 0644); err != nil {
			return err
		}
	}
	goMod := "module gosx-installer-host\n\ngo 1.26\n"
	if err := os.WriteFile(filepath.Join(workDir, "go.mod"), []byte(goMod), 0644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(workDir, "main.go"), []byte(mainSource), 0644); err != nil {
		return err
	}
	command := exec.Command("nice", "-n", "10", "go", "build", "-trimpath", "-ldflags=-H=windowsgui", "-o", output, ".")
	command.Dir = workDir
	command.Env = setCommandEnvironment(os.Environ(), map[string]string{
		"GOOS": "windows", "GOARCH": "amd64", "CGO_ENABLED": "0", "GOWORK": "off",
	})
	result, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("cross-build Windows installer host (CGO_ENABLED=0): %w\n%s", err, strings.TrimSpace(string(result)))
	}
	return nil
}

func setCommandEnvironment(environment []string, values map[string]string) []string {
	filtered := make([]string, 0, len(environment)+len(values))
	for _, entry := range environment {
		key, _, ok := strings.Cut(entry, "=")
		if _, replace := values[key]; ok && replace {
			continue
		}
		filtered = append(filtered, entry)
	}
	for key, value := range values {
		filtered = append(filtered, key+"="+value)
	}
	return filtered
}

func appendInstallerPayload(stub, output, payloadPath string) error {
	const magic = "GOSXSET1"
	const trailer = "GOSXEND1"
	input, err := os.Open(stub)
	if err != nil {
		return err
	}
	defer input.Close()
	payload, err := os.Open(payloadPath)
	if err != nil {
		return err
	}
	defer payload.Close()
	payloadInfo, err := payload.Stat()
	if err != nil {
		return err
	}
	file, err := os.Create(output)
	if err != nil {
		return err
	}
	_, err = io.Copy(file, input)
	if err == nil {
		_, err = file.Write([]byte(magic))
	}
	if err == nil {
		err = writeUint64(file, uint64(payloadInfo.Size()))
	}
	if err == nil {
		_, err = io.Copy(file, payload)
	}
	if err == nil {
		_, err = file.Write([]byte(trailer))
	}
	if err == nil {
		err = writeUint64(file, uint64(payloadInfo.Size()))
	}
	closeErr := file.Close()
	if err != nil {
		_ = os.Remove(output)
		return err
	}
	return closeErr
}

func writeUint64(w io.Writer, value uint64) error {
	var data [8]byte
	for i := range data {
		data[i] = byte(value >> (8 * i))
	}
	_, err := w.Write(data[:])
	return err
}

func signStagePEFiles(stage, template, workDir string) error {
	var paths []string
	err := filepath.WalkDir(stage, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		var magic [2]byte
		_, readErr := io.ReadFull(file, magic[:])
		_ = file.Close()
		if readErr == nil && magic == [2]byte{'M', 'Z'} {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(paths)
	for _, path := range paths {
		output := filepath.Join(workDir, "signed-"+filepath.Base(path))
		if err := runSignTemplate(template, path, path, output); err != nil {
			return fmt.Errorf("sign %s: %w", path, err)
		}
	}
	return nil
}

func runSignTemplate(template, input, fileValue, output string) error {
	quotedInput := desktopSignShellQuote(input)
	quotedOutput := desktopSignShellQuote(output)
	quotedFile := desktopSignShellQuote(fileValue)
	commandText := strings.ReplaceAll(template, "{input}", quotedInput)
	commandText = strings.ReplaceAll(commandText, "{file}", quotedFile)
	commandText = strings.ReplaceAll(commandText, "{output}", quotedOutput)
	if runtime.GOOS == "windows" {
		command := exec.Command("cmd.exe", "/d", "/s", "/c", commandText)
		result, err := command.CombinedOutput()
		if err != nil {
			return fmt.Errorf("run signing command: %w\n%s", err, strings.TrimSpace(string(result)))
		}
	} else {
		command := exec.Command("sh", "-c", commandText)
		result, err := command.CombinedOutput()
		if err != nil {
			return fmt.Errorf("run signing command: %w\n%s", err, strings.TrimSpace(string(result)))
		}
	}
	if strings.Contains(template, "{output}") {
		if _, err := os.Stat(output); err != nil {
			return fmt.Errorf("signing command did not create output: %w", err)
		}
		return os.Rename(output, input)
	}
	return nil
}

func desktopSignShellQuote(value string) string {
	if runtime.GOOS == "windows" {
		return strconv.Quote(value)
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func decodePrivateKey(data []byte) (ed25519.PrivateKey, error) {
	if block, _ := pem.Decode(data); block != nil {
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err == nil {
			privateKey, ok := key.(ed25519.PrivateKey)
			if ok {
				return privateKey, nil
			}
		}
		return nil, fmt.Errorf("manifest PEM key is not an Ed25519 PKCS#8 private key")
	}
	trimmed := bytes.TrimSpace(data)
	if decoded, err := base64.StdEncoding.DecodeString(string(trimmed)); err == nil {
		data = decoded
	} else if decoded, err := hex.DecodeString(string(trimmed)); err == nil {
		data = decoded
	}
	if len(data) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("manifest private key must be raw, base64, hex, or PKCS#8 Ed25519")
	}
	return ed25519.PrivateKey(append([]byte(nil), data...)), nil
}
