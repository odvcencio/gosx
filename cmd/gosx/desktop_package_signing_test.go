package main

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"m31labs.dev/gosx/cmd/gosx/installerhost"
)

const desktopTestSignedMarker = "\nGOSX-TEST-SIGNED\n"

func TestDesktopPackageCLIHelper(t *testing.T) {
	if os.Getenv("GOSX_TEST_DESKTOP_CLI") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Args = append([]string{"gosx"}, os.Args[i+1:]...)
			cmdDesktop()
			os.Exit(0)
		}
	}
	os.Exit(2)
}

func TestDesktopPackageAzureFakeTool(t *testing.T) {
	clearDesktopSigningEnvironment(t)
	tool := buildDesktopSigningTestTool(t)
	bin := t.TempDir()
	verifierName := "osslsigncode"
	if runtime.GOOS == "windows" {
		verifierName = "powershell.exe"
	}
	verifyTool := filepath.Join(bin, verifierName)
	if err := packageDesktopCopyFile(tool, verifyTool); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(verifyTool, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(artifactSigningTokenEnv, "test-access-token")
	logPath := filepath.Join(t.TempDir(), "signing.jsonl")
	t.Setenv("GOSX_TEST_SIGN_LOG", logPath)

	root := t.TempDir()
	stage := filepath.Join(root, "stage")
	if err := os.MkdirAll(filepath.Join(stage, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	originals := map[string][]byte{"app.exe": []byte("MZ-host"), "nested/helper.dll": []byte("MZ-library"), "nested/runtime.bin": []byte("MZ-runtime"), "assets.txt": []byte("not a PE")}
	for name, data := range originals {
		if err := os.WriteFile(filepath.Join(stage, filepath.FromSlash(name)), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	bootstrapper := filepath.Join(root, "bootstrapper.exe")
	if err := os.WriteFile(bootstrapper, []byte("MZ-bootstrapper"), 0644); err != nil {
		t.Fatal(err)
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyFile := filepath.Join(root, "manifest.key")
	if err := os.WriteFile(keyFile, privateKey, 0600); err != nil {
		t.Fatal(err)
	}
	config := installerhost.PackageConfig{AppID: "example-app", Name: "Example App", Publisher: "Example Publisher", Version: "1.0.0", HostExe: "app.exe", DataDir: `%LOCALAPPDATA%\Example App`, UpdatePublicKey: base64.StdEncoding.EncodeToString(publicKey), DownloadPage: "https://example.com/download", Released: "2026-01-01", Notes: "Test package", WebView2Bootstrapper: bootstrapper, Signing: installerhost.SigningConfig{Endpoint: "https://config.example.com", Account: "config-account", Profile: "config-profile", Tool: "jsign", JsignPath: tool}}
	configFile := filepath.Join(root, "package.json")
	configBytes, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configFile, configBytes, 0644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "output")

	// Missing provider values fail before bootstrapper I/O or installer builds.
	badConfig := config
	badConfig.Signing = installerhost.SigningConfig{}
	badConfig.WebView2Bootstrapper = filepath.Join(root, "missing.exe")
	badBytes, _ := json.Marshal(badConfig)
	badFile := filepath.Join(root, "invalid.json")
	if err := os.WriteFile(badFile, badBytes, 0644); err != nil {
		t.Fatal(err)
	}
	err = packageDesktopRelease(desktopPackageFlags{input: stage, config: badFile, output: output, signProvider: azureArtifactSigningProvider})
	if err == nil || !strings.Contains(err.Error(), "Artifact Signing is missing:") {
		t.Fatalf("provider preflight: %v", err)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("preflight created output")
	}

	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, "-test.run=^TestDesktopPackageCLIHelper$", "--", "desktop", "package", "--input", stage, "--config", configFile, "--output", output, "--sign-provider", azureArtifactSigningProvider, "--sign-endpoint", "https://flags.example.com", "--verify-signatures", "--expect-subject", "Example Publisher", "--manifest-key", keyFile)
	command.Env = setCommandEnvironment(os.Environ(), map[string]string{"GOSX_TEST_DESKTOP_CLI": "1", "GOWORK": "off"})
	result, err := command.CombinedOutput()
	if bytes.Contains(result, []byte("test-access-token")) {
		t.Fatal("token leaked in packaging output")
	}
	if err != nil {
		t.Fatalf("CLI package: %v\n%s", err, result)
	}

	var metadata installerhost.PackageMetadata
	readDesktopTestJSON(t, filepath.Join(output, "package-metadata.json"), &metadata)
	if metadata.SigningProvider != azureArtifactSigningProvider || metadata.VerifiedSignerSubject != "CN=Example Publisher" || metadata.InstallerSigning != "signed" || metadata.ManifestSigning != "ed25519-key-file" {
		t.Fatalf("metadata = %+v", metadata)
	}

	portableData, err := os.ReadFile(filepath.Join(output, "Example-App-1.0.0-portable.zip"))
	if err != nil {
		t.Fatal(err)
	}
	portable := desktopTestZipFiles(t, portableData)
	for name, original := range originals {
		want := original
		if bytes.HasPrefix(original, []byte("MZ")) {
			want = append(append([]byte(nil), original...), []byte(desktopTestSignedMarker)...)
		}
		if !bytes.Equal(portable[name], want) {
			t.Fatalf("portable file %s has wrong signature marker", name)
		}
		unchanged, err := os.ReadFile(filepath.Join(stage, filepath.FromSlash(name)))
		if err != nil || !bytes.Equal(unchanged, original) {
			t.Fatalf("input changed: %s", name)
		}
	}
	setupData, err := os.ReadFile(filepath.Join(output, "Example-App-Setup-1.0.0.exe"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(setupData, []byte(desktopTestSignedMarker)) {
		t.Fatal("Setup not signed last")
	}
	offset, length, err := installerhost.FindSetupOverlay(bytes.NewReader(setupData), int64(len(setupData)))
	if err != nil {
		t.Fatal(err)
	}
	payload := desktopTestZipFiles(t, setupData[offset:offset+int64(length)])
	if !bytes.HasSuffix(payload["__gosx__/uninstaller.exe"], []byte(desktopTestSignedMarker)) {
		t.Fatal("uninstaller not signed in payload")
	}
	var payloadManifest installerhost.PayloadManifest
	if err := json.Unmarshal(payload["__gosx__/payload-manifest.json"], &payloadManifest); err != nil {
		t.Fatal(err)
	}
	for name, digest := range payloadManifest.Files {
		sum := sha256.Sum256(payload[name])
		if hex.EncodeToString(sum[:]) != digest {
			t.Fatalf("payload hash %s does not cover signed bytes", name)
		}
	}
	for name := range originals {
		if !bytes.Equal(payload[name], portable[name]) {
			t.Fatalf("payload and portable differ: %s", name)
		}
	}
	var manifest installerhost.UpdateManifest
	manifestBytes, err := os.ReadFile(filepath.Join(output, "latest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	for name, digest := range manifest.Artifacts {
		got, err := installerhost.HashFile(filepath.Join(output, name))
		if err != nil || got != digest {
			t.Fatalf("latest.json hash mismatch for %s", name)
		}
	}
	signatureText, err := os.ReadFile(filepath.Join(output, "latest.json.sig"))
	if err != nil {
		t.Fatal(err)
	}
	signature, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(signatureText)))
	if err != nil || !ed25519.Verify(publicKey, manifestBytes, signature) {
		t.Fatal("Ed25519 manifest signature invalid")
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	var operations []struct{ Op, File string }
	decoder := json.NewDecoder(bytes.NewReader(logData))
	for {
		var operation struct{ Op, File string }
		if err := decoder.Decode(&operation); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		operations = append(operations, operation)
	}
	wantFiles := []string{"app.exe", "helper.dll", "runtime.bin", "uninstaller.exe", "Example-App-Setup-1.0.0.exe"}
	if len(operations) != 2*len(wantFiles) {
		t.Fatalf("sign/verify operation count = %d", len(operations))
	}
	for i, file := range wantFiles {
		if operations[2*i].Op != "sign" || operations[2*i+1].Op != "verify" || operations[2*i].File != file || operations[2*i+1].File != file {
			t.Fatalf("sign/verify ordering at %s: %+v", file, operations)
		}
	}
	for _, name := range []string{"package-metadata.json", "latest.json"} {
		data, err := os.ReadFile(filepath.Join(output, name))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte("test-access-token")) {
			t.Fatal("token in artifacts")
		}
	}
}

func readDesktopTestJSON(t *testing.T, path string, target any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatal(err)
	}
}

func desktopTestZipFiles(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	files := make(map[string][]byte)
	for _, entry := range reader.File {
		file, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(file)
		closeErr := file.Close()
		if err != nil || closeErr != nil {
			t.Fatalf("read ZIP entry %s: %v, %v", entry.Name, err, closeErr)
		}
		files[entry.Name] = data
	}
	return files
}
