package main

import (
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"m31labs.dev/gosx/cmd/gosx/installerhost"
)

func TestDesktopSignConfigPrecedence(t *testing.T) {
	config := installerhost.SigningConfig{Endpoint: "https://config.example.com", Account: "config-account", Profile: "config-profile", Tool: "jsign", DlibPath: "config.dll", SigntoolPath: "config-sign", JsignPath: "config-jsign"}
	env := map[string]string{"GOSX_ARTIFACT_SIGNING_ENDPOINT": "https://env.example.com", "GOSX_ARTIFACT_SIGNING_ACCOUNT": "env-account", "GOSX_ARTIFACT_SIGNING_PROFILE": "env-profile", "GOSX_ARTIFACT_SIGNING_TOOL": "signtool", "GOSX_ARTIFACT_SIGNING_DLIB": "env.dll"}
	getenv := func(key string) string { return env[key] }
	got, err := resolveArtifactSigningConfig(installerhost.SigningConfig{}, config, getenv, "windows")
	want := installerhost.SigningConfig{Endpoint: "https://env.example.com", Account: "env-account", Profile: "env-profile", Tool: "signtool", DlibPath: "env.dll", SigntoolPath: "config-sign", JsignPath: "config-jsign"}
	if err != nil || got != want {
		t.Fatalf("env overrides config: got %+v, %v", got, err)
	}
	flags := installerhost.SigningConfig{Endpoint: "https://flags.example.com", Account: "flag-account", Profile: "flag-profile", Tool: "jsign", DlibPath: "flag.dll", SigntoolPath: "flag-sign", JsignPath: "flag-jsign"}
	got, err = resolveArtifactSigningConfig(flags, config, getenv, "windows")
	if err != nil || got != flags {
		t.Fatalf("flags override env: got %+v, %v", got, err)
	}
	got, err = resolveArtifactSigningConfig(installerhost.SigningConfig{}, config, func(string) string { return "" }, "linux")
	if err != nil || got != config {
		t.Fatalf("config fallback: got %+v, %v", got, err)
	}
}

func TestDesktopSignValidation(t *testing.T) {
	getenv := func(string) string { return "" }
	_, err := resolveArtifactSigningConfig(installerhost.SigningConfig{}, installerhost.SigningConfig{}, getenv, "windows")
	for _, missing := range []string{"endpoint", "account", "profile", "dlib_path"} {
		if err == nil || !strings.Contains(err.Error(), missing) {
			t.Fatalf("missing %s not reported: %v", missing, err)
		}
	}
	valid := installerhost.SigningConfig{Endpoint: "https://signing.example.com", Account: "example-account", Profile: "example-profile", Tool: "jsign"}
	for _, endpoint := range []string{"http://signing.example.com", "https:", "https://user:password@signing.example.com", "https://signing.example.com?token=value", "https://signing.example.com#fragment"} {
		config := valid
		config.Endpoint = endpoint
		if _, err := resolveArtifactSigningConfig(config, installerhost.SigningConfig{}, getenv, "linux"); err == nil {
			t.Errorf("accepted endpoint %q", endpoint)
		}
	}
	for _, host := range []string{"linux", "windows"} {
		config := valid
		config.Tool = ""
		if host == "windows" {
			config.DlibPath = "example.dll"
		}
		got, err := resolveArtifactSigningConfig(config, installerhost.SigningConfig{}, getenv, host)
		if err != nil {
			t.Fatal(err)
		}
		want := "jsign"
		if host == "windows" {
			want = "signtool"
		}
		if got.Tool != want {
			t.Fatalf("%s default tool = %s", host, got.Tool)
		}
	}
	for _, tool := range []string{"invalid", "signtool"} {
		config := valid
		config.Tool = tool
		config.DlibPath = "example.dll"
		if _, err := resolveArtifactSigningConfig(config, installerhost.SigningConfig{}, getenv, "linux"); err == nil {
			t.Fatalf("accepted %s on Linux", tool)
		}
	}
	config := valid
	config.Profile = "bad/profile"
	if _, err := resolveArtifactSigningConfig(config, installerhost.SigningConfig{}, getenv, "linux"); err == nil {
		t.Fatal("accepted ambiguous alias")
	}
}

func TestDesktopSignCommandArguments(t *testing.T) {
	file := filepath.Join(t.TempDir(), "app '; $().exe")
	signer := artifactSigner{toolPath: "sign-tool", metadataDir: "metadata dir", token: "test-secret", config: installerhost.SigningConfig{Tool: "signtool", DlibPath: "dlib path.dll"}}
	want := []string{"sign-tool", "sign", "/v", "/fd", "SHA256", "/tr", artifactSigningTimestampURL, "/td", "SHA256", "/dlib", "dlib path.dll", "/dmdf", filepath.Join("metadata dir", "metadata.json"), file}
	if got := signer.command(file).Args; !reflect.DeepEqual(got, want) {
		t.Fatalf("signtool args: %q", got)
	}
	signer.config = installerhost.SigningConfig{Tool: "jsign", Endpoint: "https://signing.example.com", Account: "example-account", Profile: "example-profile"}
	command := signer.command(file)
	want = []string{"sign-tool", "--storetype", "TRUSTEDSIGNING", "--keystore", "https://signing.example.com", "--alias", "example-account/example-profile", "--storepass", "env:GOSX_ARTIFACT_SIGNING_TOKEN", "--alg", "SHA-256", "--tsaurl", artifactSigningTimestampURL, "--tsmode", "RFC3161", file}
	if !reflect.DeepEqual(command.Args, want) {
		t.Fatalf("jsign args: %q", command.Args)
	}
	if strings.Contains(strings.Join(command.Args, " "), signer.token) {
		t.Fatal("token in argv")
	}
	if !strings.Contains(strings.Join(command.Env, "\n"), artifactSigningTokenEnv+"="+signer.token) {
		t.Fatal("token missing from child environment")
	}
}

func TestDesktopSignFlagsAndConflicts(t *testing.T) {
	var options desktopPackageFlags
	fs := flag.NewFlagSet("package", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	desktopSigningFlags(fs, &options)
	if err := fs.Parse([]string{"--sign-provider", azureArtifactSigningProvider, "--sign-endpoint", "https://signing.example.com", "--sign-account", "example-account", "--sign-profile", "example-profile", "--sign-tool", "jsign", "--signtool-path", "signtool", "--dlib-path", "dlib.dll", "--jsign-path", "jsign", "--verify-signatures", "--expect-subject", "Example Publisher"}); err != nil {
		t.Fatal(err)
	}
	if options.signProvider != azureArtifactSigningProvider || !options.verifySignatures || options.signing.Profile != "example-profile" || options.expectSubject != "Example Publisher" {
		t.Fatalf("flags not wired: %+v", options)
	}
	options.signCommand = "custom signer"
	if err := packageDesktopRelease(options); err == nil || !strings.Contains(err.Error(), "choose either --sign-cmd or --sign-provider") {
		t.Fatalf("conflict = %v", err)
	}
	if err := packageDesktopRelease(desktopPackageFlags{signProvider: "other"}); err == nil || !strings.Contains(err.Error(), "--sign-provider") {
		t.Fatalf("unknown provider = %v", err)
	}
	if err := packageDesktopRelease(desktopPackageFlags{verifySignatures: true}); err == nil || !strings.Contains(err.Error(), "requires") {
		t.Fatalf("unsigned verification = %v", err)
	}
}

func TestDesktopSignConfigRejectsSecrets(t *testing.T) {
	for _, key := range []string{"token", "client_secret", "tenant_id"} {
		var config installerhost.PackageConfig
		decoder := json.NewDecoder(strings.NewReader(`{"signing":{"` + key + `":"test-secret"}}`))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&config); err == nil {
			t.Errorf("accepted secret config key %s", key)
		}
	}
}

func TestDesktopVerifySubjects(t *testing.T) {
	for _, test := range []struct {
		kind, output, expected, want string
		fail                         bool
	}{
		{"powershell", `{"Status":"Valid","Subject":"CN=Example Publisher"}`, "Example", "CN=Example Publisher", false},
		{"powershell", `{"Status":"NotSigned","Subject":""}`, "", "", true},
		{"powershell", `{"Status":"HashMismatch","Subject":"CN=Example Publisher"}`, "", "", true},
		{"powershell", `invalid JSON`, "", "", true},
		{"powershell", `{"Status":"Valid","Subject":"CN=Example Publisher"}`, "Other", "", true},
		{"osslsigncode", "Signature Index: 0\nSigner's certificate:\nSubject: CN=Example Publisher\nSubject: CN=Other CA\nSignature verification: ok", "Example", "CN=Example Publisher", false},
		{"osslsigncode", "Signer's certificate:\nSubject: CN=Wrong Publisher\nSubject: CN=Expected CA\nSignature verification: ok", "Expected", "", true},
		{"osslsigncode", "Signature Index: 0\nSigner's certificate:\nSubject: CN=Expected\nSignature verification: failed\nSignature Index: 1\nSigner's certificate:\nSubject: CN=Other\nSignature verification: ok", "Expected", "", true},
		{"jsign", "Signature #1 (SHA256 with RSA 4096) by Example Publisher is valid", "Example", "CN=Example Publisher", false},
		{"jsign", "Signature #1 (SHA256 with RSA 4096) by Example Publisher is invalid", "Example", "", true},
		{"jsign", "Signature #1 (SHA256 with RSA 4096) by Other is invalid\nSignature #2 (SHA256 with RSA 4096) by Example Publisher is valid", "Example", "CN=Example Publisher", false},
	} {
		got, err := verifiedAuthenticodeSubject(test.kind, test.output, test.expected)
		if (err != nil) != test.fail || got != test.want {
			t.Errorf("%s: got %q, %v; want %q, fail %v", test.kind, got, err, test.want, test.fail)
		}
	}
}

func TestDesktopVerifyHostAndArguments(t *testing.T) {
	missing := func(string) (string, error) { return "", errors.New("missing") }
	for _, host := range []string{"windows", "linux"} {
		if _, err := prepareAuthenticodeVerifier(host, "", missing); err == nil || !strings.Contains(err.Error(), "cannot verify on this host") {
			t.Fatalf("%s missing verifier: %v", host, err)
		}
	}
	for _, kind := range []string{"powershell", "osslsigncode", "jsign"} {
		v := authenticodeVerifier{kind: kind, toolPath: "verify-tool"}
		file := "file';$().exe"
		command := v.command(file)
		if kind == "powershell" {
			if strings.Contains(command.Args[len(command.Args)-1], file) || !strings.Contains(strings.Join(command.Env, "\n"), "GOSX_AUTHENTICODE_FILE="+file) {
				t.Fatal("PowerShell path interpolated into code")
			}
		} else if command.Args[len(command.Args)-1] != file {
			t.Fatalf("%s path not one argument", kind)
		}
	}
}

func buildDesktopSigningTestTool(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-sign-tool")
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	command := exec.Command("go", "build", "-o", path, "testdata/artifact_signing_tool.go")
	command.Env = setCommandEnvironment(os.Environ(), map[string]string{"GOWORK": "off", "GOOS": runtime.GOOS, "GOARCH": runtime.GOARCH})
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build fake signing tool: %v\n%s", err, output)
	}
	return path
}

func clearDesktopSigningEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range []string{"GOSX_ARTIFACT_SIGNING_ENDPOINT", "GOSX_ARTIFACT_SIGNING_ACCOUNT", "GOSX_ARTIFACT_SIGNING_PROFILE", "GOSX_ARTIFACT_SIGNING_TOOL", "GOSX_ARTIFACT_SIGNING_DLIB", artifactSigningTokenEnv, "AZURE_CLIENT_SECRET", "GOSX_TEST_TOOL_FAIL"} {
		t.Setenv(key, "")
	}
}

func TestDesktopSignTokenAcquisitionAndRedaction(t *testing.T) {
	clearDesktopSigningEnvironment(t)
	tool := buildDesktopSigningTestTool(t)
	bin := t.TempDir()
	az := filepath.Join(bin, "az")
	if runtime.GOOS == "windows" {
		az += ".exe"
	}
	if err := packageDesktopCopyFile(tool, az); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(az, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	options := desktopPackageFlags{signing: installerhost.SigningConfig{Tool: "jsign", JsignPath: tool, Endpoint: "https://signing.example.com", Account: "example-account", Profile: "example-profile"}}
	signer, err := prepareArtifactSigner(options, installerhost.SigningConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer signer.close()
	if signer.token != "test-access-token" {
		t.Fatal("az token not captured")
	}
	t.Setenv("GOSX_TEST_TOOL_FAIL", "1")
	err = signer.sign("app.exe")
	if err == nil || strings.Contains(err.Error(), signer.token) || !strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatal("signing failure did not redact token")
	}
	t.Setenv(artifactSigningTokenEnv, "env-test-token")
	signer, err = prepareArtifactSigner(options, installerhost.SigningConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if signer.token != "env-test-token" {
		t.Fatal("environment token did not take precedence")
	}
	if text := redactSigningText("env-test-token env-test-token", "env-test-token"); text != "[REDACTED] [REDACTED]" {
		t.Fatal("repeated tokens not redacted")
	}
}

func TestDesktopSignMetadataPermissionsAndCleanup(t *testing.T) {
	signer := artifactSigner{token: "test-secret", config: installerhost.SigningConfig{Endpoint: "https://signing.example.com", Account: "example-account", Profile: "example-profile", Tool: "signtool"}}
	if err := signer.writeMetadata(); err != nil {
		t.Fatal(err)
	}
	defer signer.close()
	path := filepath.Join(signer.metadataDir, "metadata.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var metadata map[string]string
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"Endpoint": signer.config.Endpoint, "CodeSigningAccountName": signer.config.Account, "CertificateProfileName": signer.config.Profile}
	if !reflect.DeepEqual(metadata, want) || strings.Contains(string(data), signer.token) {
		t.Fatal("dlib metadata contains unexpected settings")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("metadata mode = %o", info.Mode().Perm())
		}
		info, err = os.Stat(signer.metadataDir)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0700 {
			t.Fatalf("metadata directory mode = %o", info.Mode().Perm())
		}
	}
	signer.close()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("metadata not removed")
	}
}

func TestDesktopSignTemplatePreserved(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell template")
	}
	stage := t.TempDir()
	path := filepath.Join(stage, "app 'quoted'.exe")
	if err := os.WriteFile(path, []byte("MZ-test"), 0644); err != nil {
		t.Fatal(err)
	}
	asset := filepath.Join(stage, "asset.txt")
	if err := os.WriteFile(asset, []byte("asset"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := signStagePEFiles(stage, "cp {input} {output}; printf signed >> {output}", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "MZ-testsigned" {
		t.Fatalf("legacy signer output: %q, %v", data, err)
	}
	data, err = os.ReadFile(asset)
	if err != nil || string(data) != "asset" {
		t.Fatal("legacy signer changed non-PE asset")
	}
}
