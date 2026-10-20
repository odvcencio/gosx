package desktop

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	maxUpdateManifestBytes = 1 << 20
	maxUpdateSignatureSize = 4096
	updateCheckInterval    = 24 * time.Hour
)

var signedUpdateStateMu sync.Mutex

// SignedUpdateManifest is the direct-download manifest written by
// `gosx desktop package`. Its detached Ed25519 signature covers the exact
// bytes served as latest.json.
type SignedUpdateManifest struct {
	App          string            `json:"app"`
	Channel      string            `json:"channel"`
	Version      string            `json:"version"`
	Released     string            `json:"released"`
	Notes        string            `json:"notes"`
	DownloadPage string            `json:"download_page"`
	Artifacts    map[string]string `json:"artifacts"`
}

// SignedUpdateCheckOptions configures one explicit, read-only update check.
// PublicKey must be compiled into the application. StateFile belongs in the
// app's player-data directory and persists the 24-hour attempt limit.
// StartupComplete and Online are required gates: this API never starts a
// check during startup or while the caller reports the app offline.
type SignedUpdateCheckOptions struct {
	ManifestURL     string
	App             string
	Channel         string
	CurrentVersion  string
	PublicKey       ed25519.PublicKey
	StateFile       string
	Enabled         bool
	StartupComplete bool
	Online          bool
	// AllowLoopbackHTTPForTests permits an http://127.0.0.1 manifest URL for
	// local tests. Keep this false in shipped applications.
	AllowLoopbackHTTPForTests bool
	HTTPClient                *http.Client
}

// SignedUpdateStatus describes whether the explicit update check ran and its
// result. A CheckFailed status is returned with the error when a check attempt
// cannot be completed; the attempt still counts toward the 24-hour limit.
type SignedUpdateStatus string

const (
	SignedUpdateSkippedDisabled SignedUpdateStatus = "skipped-disabled"
	SignedUpdateSkippedStartup  SignedUpdateStatus = "skipped-startup"
	SignedUpdateSkippedOffline  SignedUpdateStatus = "skipped-offline"
	SignedUpdateSkippedDaily    SignedUpdateStatus = "skipped-daily-limit"
	SignedUpdateCheckFailed     SignedUpdateStatus = "check-failed"
	SignedUpdateCurrent         SignedUpdateStatus = "up-to-date"
	SignedUpdateAvailable       SignedUpdateStatus = "available"
)

// SignedUpdateResult contains notification data only. GoSX never downloads or
// installs an update through this API; the player follows DownloadPage and
// reinstalls the publisher's Setup executable.
type SignedUpdateResult struct {
	Status       SignedUpdateStatus
	CheckedAt    time.Time
	NextCheckAt  time.Time
	Version      string
	Released     string
	Notes        string
	DownloadPage string
}

type signedUpdateState struct {
	LastAttempt time.Time `json:"last_attempt"`
}

// CheckSignedUpdate verifies a publisher-hosted latest.json and its detached
// latest.json.sig, then reports whether its version is newer. It performs no
// network request unless checks are enabled, startup is complete, the caller
// reports online, and the persistent daily limit has elapsed.
func CheckSignedUpdate(ctx context.Context, options SignedUpdateCheckOptions) (SignedUpdateResult, error) {
	return checkSignedUpdate(ctx, options)
}

// CheckSignedUpdate checks the signed direct-download feed for this App. The
// caller supplies the feed URL, compiled public key, persistent state file,
// current preference, and current connectivity/startup state.
func (a *App) CheckSignedUpdate(ctx context.Context, options SignedUpdateCheckOptions) (SignedUpdateResult, error) {
	if a == nil {
		return SignedUpdateResult{}, fmt.Errorf("%w: nil app", ErrInvalidOptions)
	}
	options.App = a.options.AppID
	options.CurrentVersion = a.options.Version
	return CheckSignedUpdate(ctx, options)
}

func checkSignedUpdate(ctx context.Context, options SignedUpdateCheckOptions) (SignedUpdateResult, error) {
	if !options.Enabled {
		return SignedUpdateResult{Status: SignedUpdateSkippedDisabled}, nil
	}
	if !options.StartupComplete {
		return SignedUpdateResult{Status: SignedUpdateSkippedStartup}, nil
	}
	if !options.Online {
		return SignedUpdateResult{Status: SignedUpdateSkippedOffline}, nil
	}
	if ctx == nil {
		return SignedUpdateResult{}, fmt.Errorf("%w: update check context is nil", ErrInvalidOptions)
	}
	if err := validateSignedUpdateOptions(options); err != nil {
		return SignedUpdateResult{}, err
	}

	signedUpdateStateMu.Lock()
	defer signedUpdateStateMu.Unlock()

	now := time.Now().UTC()
	state, err := readSignedUpdateState(options.StateFile)
	if err != nil {
		return SignedUpdateResult{}, err
	}
	if !state.LastAttempt.IsZero() && now.Sub(state.LastAttempt) < updateCheckInterval {
		return SignedUpdateResult{
			Status:      SignedUpdateSkippedDaily,
			CheckedAt:   state.LastAttempt,
			NextCheckAt: state.LastAttempt.Add(updateCheckInterval),
		}, nil
	}
	state.LastAttempt = now
	if err := writeSignedUpdateState(options.StateFile, state); err != nil {
		return SignedUpdateResult{}, err
	}
	result := SignedUpdateResult{
		Status:      SignedUpdateCheckFailed,
		CheckedAt:   now,
		NextCheckAt: now.Add(updateCheckInterval),
	}
	manifest, err := fetchSignedUpdateManifest(ctx, options)
	if err != nil {
		return result, err
	}
	cmp, err := compareSignedUpdateVersion(manifest.Version, options.CurrentVersion)
	if err != nil {
		return result, fmt.Errorf("invalid update version: %w", err)
	}
	result.Version = manifest.Version
	result.Released = manifest.Released
	result.Notes = manifest.Notes
	result.DownloadPage = manifest.DownloadPage
	if cmp > 0 {
		result.Status = SignedUpdateAvailable
	} else {
		result.Status = SignedUpdateCurrent
	}
	return result, nil
}

func validateSignedUpdateOptions(options SignedUpdateCheckOptions) error {
	for field, value := range map[string]string{
		"manifest URL":    options.ManifestURL,
		"app ID":          options.App,
		"channel":         options.Channel,
		"current version": options.CurrentVersion,
		"state file":      options.StateFile,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: update check %s is empty", ErrInvalidOptions, field)
		}
	}
	if len(options.PublicKey) != ed25519.PublicKeySize {
		return fmt.Errorf("%w: update public key must be an Ed25519 public key", ErrInvalidOptions)
	}
	if _, err := parseSignedUpdateVersion(options.CurrentVersion); err != nil {
		return fmt.Errorf("%w: current version: %v", ErrInvalidOptions, err)
	}
	if _, err := validateUpdateURL(options.ManifestURL, options.AllowLoopbackHTTPForTests); err != nil {
		return fmt.Errorf("%w: manifest URL: %v", ErrInvalidOptions, err)
	}
	return nil
}

func readSignedUpdateState(path string) (signedUpdateState, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return signedUpdateState{}, nil
	}
	if err != nil {
		return signedUpdateState{}, fmt.Errorf("read update-check state: %w", err)
	}
	var state signedUpdateState
	if err := json.Unmarshal(data, &state); err != nil || state.LastAttempt.IsZero() {
		if err == nil {
			err = errors.New("last_attempt is empty")
		}
		return signedUpdateState{}, fmt.Errorf("decode update-check state: %w", err)
	}
	return state, nil
}

func writeSignedUpdateState(path string, state signedUpdateState) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return fmt.Errorf("create update-check state directory: %w", err)
	}
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode update-check state: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".update-check-*.tmp")
	if err != nil {
		return fmt.Errorf("create update-check state: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set update-check state permissions: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write update-check state: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync update-check state: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close update-check state: %w", err)
	}
	if err := os.Rename(temporaryName, path); err != nil {
		return fmt.Errorf("save update-check state: %w", err)
	}
	return nil
}

func fetchSignedUpdateManifest(ctx context.Context, options SignedUpdateCheckOptions) (SignedUpdateManifest, error) {
	client := makeSignedUpdateHTTPClient(options.HTTPClient, options.AllowLoopbackHTTPForTests)
	manifestURL := strings.TrimSpace(options.ManifestURL)
	manifestRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return SignedUpdateManifest{}, fmt.Errorf("create update manifest request: %w", err)
	}
	manifestResponse, err := client.Do(manifestRequest)
	if err != nil {
		return SignedUpdateManifest{}, fmt.Errorf("fetch update manifest: %w", err)
	}
	manifestBytes, err := readLimitedUpdateBody(manifestResponse, maxUpdateManifestBytes, "manifest")
	if err != nil {
		return SignedUpdateManifest{}, err
	}
	signatureURL, err := signedUpdateSignatureURL(manifestURL)
	if err != nil {
		return SignedUpdateManifest{}, err
	}
	signatureRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, signatureURL, nil)
	if err != nil {
		return SignedUpdateManifest{}, fmt.Errorf("create update signature request: %w", err)
	}
	signatureResponse, err := client.Do(signatureRequest)
	if err != nil {
		return SignedUpdateManifest{}, fmt.Errorf("fetch update signature: %w", err)
	}
	signatureBytes, err := readLimitedUpdateBody(signatureResponse, maxUpdateSignatureSize, "signature")
	if err != nil {
		return SignedUpdateManifest{}, err
	}
	signature, err := decodeUpdateSignature(signatureBytes)
	if err != nil {
		return SignedUpdateManifest{}, err
	}
	if !ed25519.Verify(options.PublicKey, manifestBytes, signature) {
		return SignedUpdateManifest{}, errors.New("update manifest signature is invalid")
	}
	var manifest SignedUpdateManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return SignedUpdateManifest{}, fmt.Errorf("decode signed update manifest: %w", err)
	}
	if err := validateSignedUpdateManifest(manifest, options); err != nil {
		return SignedUpdateManifest{}, err
	}
	return manifest, nil
}

func makeSignedUpdateHTTPClient(base *http.Client, allowLoopbackHTTP bool) *http.Client {
	client := &http.Client{Timeout: 15 * time.Second}
	if base != nil {
		*client = *base
		if client.Timeout <= 0 {
			client.Timeout = 15 * time.Second
		}
	}
	previousRedirect := client.CheckRedirect
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if _, err := validateUpdateURL(request.URL.String(), allowLoopbackHTTP); err != nil {
			return fmt.Errorf("reject update redirect: %w", err)
		}
		if len(via) >= 5 {
			return errors.New("too many update redirects")
		}
		if previousRedirect != nil {
			if err := previousRedirect(request, via); err != nil {
				return err
			}
			if _, err := validateUpdateURL(request.URL.String(), allowLoopbackHTTP); err != nil {
				return fmt.Errorf("reject update redirect: %w", err)
			}
		}
		return nil
	}
	return client
}

func readLimitedUpdateBody(response *http.Response, limit int64, label string) ([]byte, error) {
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("update %s returned HTTP %d", label, response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read update %s: %w", label, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("update %s exceeds %d bytes", label, limit)
	}
	return data, nil
}

func decodeUpdateSignature(data []byte) ([]byte, error) {
	if len(data) == ed25519.SignatureSize {
		return append([]byte(nil), data...), nil
	}
	trimmed := []byte(strings.TrimSpace(string(data)))
	signature, err := base64.StdEncoding.DecodeString(string(trimmed))
	if err != nil {
		signature, err = base64.RawStdEncoding.DecodeString(string(trimmed))
	}
	if err != nil || len(signature) != ed25519.SignatureSize {
		return nil, errors.New("update signature must be a 64-byte Ed25519 signature or base64 encoding")
	}
	return signature, nil
}

func signedUpdateSignatureURL(manifestURL string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(manifestURL))
	if err != nil {
		return "", fmt.Errorf("parse update manifest URL: %w", err)
	}
	u.Path += ".sig"
	return u.String(), nil
}

func validateSignedUpdateManifest(manifest SignedUpdateManifest, options SignedUpdateCheckOptions) error {
	if manifest.App != options.App {
		return fmt.Errorf("update manifest app %q does not match %q", manifest.App, options.App)
	}
	if manifest.Channel != options.Channel {
		return fmt.Errorf("update manifest channel %q does not match %q", manifest.Channel, options.Channel)
	}
	if _, err := parseSignedUpdateVersion(manifest.Version); err != nil {
		return fmt.Errorf("invalid update manifest version: %w", err)
	}
	if _, err := time.Parse("2006-01-02", manifest.Released); err != nil {
		return fmt.Errorf("update manifest released date must use YYYY-MM-DD: %w", err)
	}
	if strings.TrimSpace(manifest.Notes) == "" {
		return errors.New("update manifest notes are empty")
	}
	if _, err := validateUpdateURL(manifest.DownloadPage, false); err != nil {
		return fmt.Errorf("update manifest download page must use HTTPS: %w", err)
	}
	if len(manifest.Artifacts) == 0 {
		return errors.New("update manifest has no artifacts")
	}
	for name, digest := range manifest.Artifacts {
		if strings.TrimSpace(name) == "" {
			return errors.New("update manifest has an unnamed artifact")
		}
		decoded, err := hex.DecodeString(digest)
		if err != nil || len(decoded) != sha256.Size {
			return fmt.Errorf("update manifest artifact %q must have a SHA-256 digest", name)
		}
	}
	return nil
}

func validateUpdateURL(raw string, allowLoopbackHTTP bool) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil {
		return nil, fmt.Errorf("invalid URL %q", raw)
	}
	if u.User != nil || u.Hostname() == "" || u.Fragment != "" {
		return nil, fmt.Errorf("URL must have a host and no credentials or fragment")
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return u, nil
	case "http":
		if allowLoopbackHTTP && u.Hostname() == "127.0.0.1" {
			return u, nil
		}
		return nil, errors.New("HTTP is allowed only for 127.0.0.1 tests")
	default:
		return nil, errors.New("URL must use HTTPS")
	}
}

func compareSignedUpdateVersion(a, b string) (int, error) {
	aa, err := parseSignedUpdateVersion(a)
	if err != nil {
		return 0, err
	}
	bb, err := parseSignedUpdateVersion(b)
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

func parseSignedUpdateVersion(value string) ([4]uint64, error) {
	var version [4]uint64
	value = strings.TrimSpace(strings.TrimPrefix(value, "v"))
	parts := strings.Split(value, ".")
	if len(parts) < 2 || len(parts) > 4 {
		return version, fmt.Errorf("version %q must have two to four numeric components", value)
	}
	for i, part := range parts {
		if part == "" {
			return version, fmt.Errorf("version %q has an empty component", value)
		}
		n, err := strconv.ParseUint(part, 10, 32)
		if err != nil {
			return version, fmt.Errorf("version %q has a non-numeric component %q", value, part)
		}
		version[i] = n
	}
	return version, nil
}
