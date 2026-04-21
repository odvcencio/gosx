package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteMSIXManifestIncludesFullTrustAndCapabilities(t *testing.T) {
	dir := t.TempDir()
	opts := msixPackageOptions{
		IdentityName:         "gosx.test.app",
		Publisher:            "CN=GoSX Test",
		PublisherDisplayName: "GoSX Test",
		DisplayName:          "GoSX Test App",
		Description:          "GoSX package",
		Version:              "1.2.3.4",
		Executable:           `server\app.exe`,
		Capabilities:         []string{"internetClient", "runFullTrust", "broadFileSystemAccess"},
	}
	path := filepath.Join(dir, "AppxManifest.xml")
	if err := writeMSIXManifest(path, opts); err != nil {
		t.Fatal(err)
	}
	xml := readFile(t, path)
	for _, snippet := range []string{
		`<Identity Name="gosx.test.app" Publisher="CN=GoSX Test" Version="1.2.3.4"></Identity>`,
		`Executable="server\app.exe"`,
		`EntryPoint="Windows.FullTrustApplication"`,
		`<Capability Name="internetClient"></Capability>`,
		`<rescap:Capability Name="runFullTrust"></rescap:Capability>`,
		`<rescap:Capability Name="broadFileSystemAccess"></rescap:Capability>`,
	} {
		if !strings.Contains(xml, snippet) {
			t.Fatalf("expected %q in manifest:\n%s", snippet, xml)
		}
	}
}

func TestStageMSIXPackageDirectoryWritesManifestLogosAndPayload(t *testing.T) {
	distDir := filepath.Join(t.TempDir(), "dist")
	mustWriteFile(t, filepath.Join(distDir, "server", "app.exe"), "binary")
	mustWriteFile(t, filepath.Join(distDir, "assets", "runtime", "bootstrap.abc.js"), "runtime")
	mustWriteFile(t, filepath.Join(distDir, "msix", "old", "stale.txt"), "stale")

	opts := msixPackageOptions{
		IdentityName:         "gosx.test.app",
		Publisher:            "CN=GoSX Test",
		PublisherDisplayName: "GoSX Test",
		DisplayName:          "GoSX Test App",
		Description:          "GoSX package",
		Version:              "1.2.3.4",
		Executable:           `server\app.exe`,
		PackageDir:           filepath.Join(distDir, "msix", "package"),
		Capabilities:         []string{"internetClient", "runFullTrust"},
	}
	if err := stageMSIXPackageDirectory(distDir, opts); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{
		"server/app.exe",
		"assets/runtime/bootstrap.abc.js",
		"AppxManifest.xml",
		"Assets/Square44x44Logo.png",
		"Assets/Square150x150Logo.png",
		"Assets/StoreLogo.png",
	} {
		if _, err := os.Stat(filepath.Join(opts.PackageDir, rel)); err != nil {
			t.Fatalf("expected MSIX payload %s: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(opts.PackageDir, "msix", "old", "stale.txt")); !os.IsNotExist(err) {
		t.Fatalf("did not expect nested msix staging directory, stat err=%v", err)
	}
}

func TestWriteAppInstallerManifestDerivesPackageURI(t *testing.T) {
	dir := t.TempDir()
	appInstallerURI, packageURI := resolveAppInstallerURIs("https://updates.example.test/releases")
	opts := msixPackageOptions{
		IdentityName:    "gosx.test.app",
		Publisher:       "CN=GoSX Test",
		Version:         "1.2.3.4",
		Architecture:    "x64",
		AppInstallerURI: appInstallerURI,
		MainPackageURI:  packageURI,
	}
	path := filepath.Join(dir, "app.appinstaller")
	if err := writeAppInstallerManifest(path, opts); err != nil {
		t.Fatal(err)
	}
	xml := readFile(t, path)
	for _, snippet := range []string{
		`Uri="https://updates.example.test/releases/app.appinstaller"`,
		`Uri="https://updates.example.test/releases/app.msix"`,
		`ProcessorArchitecture="x64"`,
		`<ForceUpdateFromAnyVersion>true</ForceUpdateFromAnyVersion>`,
	} {
		if !strings.Contains(xml, snippet) {
			t.Fatalf("expected %q in appinstaller:\n%s", snippet, xml)
		}
	}
}

func TestPackAndSignMSIXUseConfiguredTools(t *testing.T) {
	dir := t.TempDir()
	makeAppxLog := filepath.Join(dir, "makeappx.log")
	signLog := filepath.Join(dir, "signtool.log")
	makeAppx := fakeTool(t, filepath.Join(dir, "makeappx"), makeAppxLog)
	signtool := fakeTool(t, filepath.Join(dir, "signtool"), signLog)

	t.Setenv("GOSX_MAKEAPPX", makeAppx)
	t.Setenv("GOSX_SIGNTOOL", signtool)
	t.Setenv("GOSX_CODESIGN_CERT", filepath.Join(dir, "cert.pfx"))
	t.Setenv("GOSX_CODESIGN_KEY", "secret")
	t.Setenv("GOSX_CODESIGN_TIMESTAMP", "https://timestamp.example.test")

	if err := packMSIXPackage(filepath.Join(dir, "package"), filepath.Join(dir, "app.msix")); err != nil {
		t.Fatal(err)
	}
	if err := signMSIXPackage(filepath.Join(dir, "app.msix")); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, makeAppxLog); !strings.Contains(got, "pack /d") || !strings.Contains(got, "app.msix") {
		t.Fatalf("unexpected MakeAppx args: %q", got)
	}
	if got := readFile(t, signLog); !strings.Contains(got, "sign /fd SHA256") || !strings.Contains(got, "/p secret") {
		t.Fatalf("unexpected signtool args: %q", got)
	}
}

func TestMSIXVersionValidationAndIdentitySanitizer(t *testing.T) {
	if err := validateMSIXVersion("1.2.3.4"); err != nil {
		t.Fatal(err)
	}
	if err := validateMSIXVersion("1.2.3"); err == nil {
		t.Fatal("expected short MSIX version to fail")
	}
	if got := sanitizeMSIXIdentity(" GoSX Demo_App!! "); got != "GoSX.Demo.App" {
		t.Fatalf("unexpected sanitized identity %q", got)
	}
}

func fakeTool(t *testing.T, path string, logPath string) string {
	t.Helper()
	script := "#!/usr/bin/env sh\nprintf '%s\\n' \"$*\" >> " + shellQuote(logPath) + "\n"
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

func shellQuote(path string) string {
	return "'" + strings.ReplaceAll(path, "'", "'\\''") + "'"
}
