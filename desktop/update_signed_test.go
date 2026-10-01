package desktop

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

type updateRoundTripFunc func(*http.Request) (*http.Response, error)

func (f updateRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestCheckSignedUpdateAvailableAndDailyLimit(t *testing.T) {
	_, publicKey, manifestBytes, signature := signedUpdateFixture(t, "2.0.0", "wb.test", "stable")
	var requests atomic.Int32
	options := signedUpdateOptions(t, publicKey, func(request *http.Request) (*http.Response, error) {
		requests.Add(1)
		if strings.HasSuffix(request.URL.Path, ".sig") {
			return updateHTTPResponse(request, http.StatusOK, base64.StdEncoding.EncodeToString(signature)+"\n"), nil
		}
		return updateHTTPResponse(request, http.StatusOK, string(manifestBytes)), nil
	})

	result, err := CheckSignedUpdate(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != SignedUpdateAvailable || result.Version != "2.0.0" || result.DownloadPage != "https://example.invalid/download" {
		t.Fatalf("unexpected update result: %+v", result)
	}
	if requests.Load() != 2 {
		t.Fatalf("request count = %d, want manifest and signature", requests.Load())
	}
	if result.NextCheckAt.Sub(result.CheckedAt) != updateCheckInterval {
		t.Fatalf("daily window = %s, want %s", result.NextCheckAt.Sub(result.CheckedAt), updateCheckInterval)
	}

	result, err = CheckSignedUpdate(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != SignedUpdateSkippedDaily {
		t.Fatalf("second check status = %q, want daily skip", result.Status)
	}
	if requests.Load() != 2 {
		t.Fatalf("daily-limited check made another request: %d", requests.Load())
	}
}

func TestCheckSignedUpdateSameVersionAndManifestMismatch(t *testing.T) {
	_, publicKey, manifestBytes, signature := signedUpdateFixture(t, "1.0.0", "wb.test", "stable")
	options := signedUpdateOptions(t, publicKey, signedUpdateTransport(manifestBytes, signature))
	result, err := CheckSignedUpdate(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != SignedUpdateCurrent {
		t.Fatalf("same-version status = %q, want up-to-date", result.Status)
	}

	_, publicKey, manifestBytes, signature = signedUpdateFixture(t, "2.0.0", "other.app", "stable")
	options = signedUpdateOptions(t, publicKey, signedUpdateTransport(manifestBytes, signature))
	if _, err := CheckSignedUpdate(context.Background(), options); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("app mismatch error = %v", err)
	}
}

func TestAppCheckSignedUpdateUsesAppIDAndVersion(t *testing.T) {
	_, publicKey, manifestBytes, signature := signedUpdateFixture(t, "2.0.0", "wb.test", "stable")
	options := signedUpdateOptions(t, publicKey, signedUpdateTransport(manifestBytes, signature))
	options.App = "wrong.app"
	options.CurrentVersion = "9.0.0"
	app := &App{options: Options{AppID: "wb.test", Version: "1.0.0"}}

	result, err := app.CheckSignedUpdate(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != SignedUpdateAvailable || result.Version != "2.0.0" {
		t.Fatalf("app update result = %+v; wanted newer version", result)
	}
}

func TestCheckSignedUpdateRejectsBadSignatureAndConsumesAttempt(t *testing.T) {
	_, publicKey, manifestBytes, signature := signedUpdateFixture(t, "2.0.0", "wb.test", "stable")
	badSignature := append([]byte(nil), signature...)
	badSignature[0] ^= 0xff
	var requests atomic.Int32
	options := signedUpdateOptions(t, publicKey, func(request *http.Request) (*http.Response, error) {
		requests.Add(1)
		if strings.HasSuffix(request.URL.Path, ".sig") {
			return updateHTTPResponse(request, http.StatusOK, base64.StdEncoding.EncodeToString(badSignature)), nil
		}
		return updateHTTPResponse(request, http.StatusOK, string(manifestBytes)), nil
	})
	result, err := CheckSignedUpdate(context.Background(), options)
	if err == nil || !strings.Contains(err.Error(), "signature is invalid") {
		t.Fatalf("bad signature error = %v", err)
	}
	if result.Status != SignedUpdateCheckFailed {
		t.Fatalf("bad signature status = %q, want check-failed", result.Status)
	}
	result, err = CheckSignedUpdate(context.Background(), options)
	if err != nil || result.Status != SignedUpdateSkippedDaily {
		t.Fatalf("retry after failed attempt = %+v, %v; want daily skip", result, err)
	}
	if requests.Load() != 2 {
		t.Fatalf("failed check retry made network requests: %d", requests.Load())
	}
}

func TestCheckSignedUpdateSkipsWithoutNetwork(t *testing.T) {
	_, publicKey, _, _ := signedUpdateFixture(t, "2.0.0", "wb.test", "stable")
	var requests atomic.Int32
	base := signedUpdateOptions(t, publicKey, func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		return nil, fmt.Errorf("unexpected request")
	})
	cases := []struct {
		name   string
		change func(*SignedUpdateCheckOptions)
		status SignedUpdateStatus
	}{
		{name: "disabled", change: func(options *SignedUpdateCheckOptions) { options.Enabled = false }, status: SignedUpdateSkippedDisabled},
		{name: "startup", change: func(options *SignedUpdateCheckOptions) { options.StartupComplete = false }, status: SignedUpdateSkippedStartup},
		{name: "offline", change: func(options *SignedUpdateCheckOptions) { options.Online = false }, status: SignedUpdateSkippedOffline},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			options := base
			options.StateFile = filepath.Join(t.TempDir(), "state.json")
			test.change(&options)
			result, err := CheckSignedUpdate(context.Background(), options)
			if err != nil || result.Status != test.status {
				t.Fatalf("result = %+v, %v; want status %q", result, err, test.status)
			}
			if _, err := os.Stat(options.StateFile); !os.IsNotExist(err) {
				t.Fatalf("skip wrote state file: stat error = %v", err)
			}
		})
	}
	if requests.Load() != 0 {
		t.Fatalf("skip cases made %d network requests", requests.Load())
	}
}

func TestCheckSignedUpdateHTTPSAndLoopbackTestPolicy(t *testing.T) {
	_, publicKey, manifestBytes, signature := signedUpdateFixture(t, "2.0.0", "wb.test", "stable")
	options := signedUpdateOptions(t, publicKey, signedUpdateTransport(manifestBytes, signature))
	options.ManifestURL = "http://127.0.0.1:8210/latest.json"
	if _, err := CheckSignedUpdate(context.Background(), options); err == nil || !strings.Contains(err.Error(), "HTTP is allowed only") {
		t.Fatalf("unapproved loopback HTTP error = %v", err)
	}
	options.AllowLoopbackHTTPForTests = true
	if _, err := CheckSignedUpdate(context.Background(), options); err != nil {
		t.Fatalf("approved test loopback HTTP failed: %v", err)
	}

	options = signedUpdateOptions(t, publicKey, signedUpdateTransport(manifestBytes, signature))
	for _, raw := range []string{"http://localhost:8210/latest.json", "http://127.0.0.2:8210/latest.json", "file:///tmp/latest.json"} {
		if _, err := validateUpdateURL(raw, true); err == nil {
			t.Errorf("accepted disallowed URL %q", raw)
		}
	}
	if _, err := validateUpdateURL("https://updates.example/latest.json", false); err != nil {
		t.Fatalf("rejected HTTPS URL: %v", err)
	}
}

func TestCheckSignedUpdateRejectsUnsafeRedirect(t *testing.T) {
	_, publicKey, _, _ := signedUpdateFixture(t, "2.0.0", "wb.test", "stable")
	options := signedUpdateOptions(t, publicKey, func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusFound,
			Header:     http.Header{"Location": []string{"http://updates.example/steal"}},
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    request,
		}, nil
	})
	options.ManifestURL = "https://updates.example/latest.json"
	if _, err := CheckSignedUpdate(context.Background(), options); err == nil || !strings.Contains(err.Error(), "reject update redirect") {
		t.Fatalf("unsafe redirect error = %v", err)
	}
}

func TestCheckSignedUpdateRequiresValidPersistentState(t *testing.T) {
	_, publicKey, manifestBytes, signature := signedUpdateFixture(t, "2.0.0", "wb.test", "stable")
	options := signedUpdateOptions(t, publicKey, signedUpdateTransport(manifestBytes, signature))
	if err := os.MkdirAll(filepath.Dir(options.StateFile), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(options.StateFile, []byte("not-json"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := CheckSignedUpdate(context.Background(), options); err == nil || !strings.Contains(err.Error(), "decode update-check state") {
		t.Fatalf("corrupt state error = %v", err)
	}
}

func signedUpdateFixture(t *testing.T, version, app, channel string) (ed25519.PrivateKey, ed25519.PublicKey, []byte, []byte) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	manifest := SignedUpdateManifest{
		App: app, Channel: channel, Version: version, Released: "2026-09-27",
		Notes: "Fixture update notes.", DownloadPage: "https://example.invalid/download",
		Artifacts: map[string]string{"WELDBREAKERS-Setup.exe": strings.Repeat("a", 64)},
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return privateKey, publicKey, manifestBytes, ed25519.Sign(privateKey, manifestBytes)
}

func signedUpdateOptions(t *testing.T, publicKey ed25519.PublicKey, transport updateRoundTripFunc) SignedUpdateCheckOptions {
	t.Helper()
	return SignedUpdateCheckOptions{
		ManifestURL: "https://updates.example/latest.json", App: "wb.test", Channel: "stable",
		CurrentVersion: "1.0.0", PublicKey: publicKey,
		StateFile: filepath.Join(t.TempDir(), "player-data", "update-check.json"),
		Enabled:   true, StartupComplete: true, Online: true,
		HTTPClient: &http.Client{Transport: transport},
	}
}

func signedUpdateTransport(manifest, signature []byte) updateRoundTripFunc {
	return func(request *http.Request) (*http.Response, error) {
		if strings.HasSuffix(request.URL.Path, ".sig") {
			return updateHTTPResponse(request, http.StatusOK, base64.StdEncoding.EncodeToString(signature)+"\n"), nil
		}
		return updateHTTPResponse(request, http.StatusOK, string(manifest)), nil
	}
}

func updateHTTPResponse(request *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}
}

func TestSignedUpdateManifestSignatureCoversExactBytes(t *testing.T) {
	_, publicKey, manifest, signature := signedUpdateFixture(t, "2.0.0", "wb.test", "stable")
	mutated := append([]byte(nil), manifest...)
	mutated = append(mutated, ' ')
	if ed25519.Verify(publicKey, mutated, signature) {
		t.Fatal("signature verified after manifest bytes changed")
	}
	if len(signature) != ed25519.SignatureSize {
		t.Fatalf("signature size = %d", len(signature))
	}
}
