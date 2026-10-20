package desktop

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

const updatePromptDownloadPage = "https://example.invalid/download"

func TestOfferSignedUpdateYesOpensAllowlistedDownloadPage(t *testing.T) {
	server, options := updatePromptFixture(t, "2.0.0", updatePromptDownloadPage, "Release notes")
	defer server.Close()

	app := &App{options: Options{AppID: "wb.test", Version: "1.0.0"}}
	var messageOptions MessageOptions
	var promptedApp *App
	var openedURL string
	setUpdatePromptHooks(t, func(gotApp *App, gotOptions MessageOptions) (MessageResult, error) {
		promptedApp, messageOptions = gotApp, gotOptions
		return MessageResultYes, nil
	}, func(_ *App, url string) error {
		openedURL = url
		return nil
	})

	options.Check.ManifestURL = server.URL + "/latest.json"
	options.AllowedDownloadPages = []string{updatePromptDownloadPage}
	result, err := app.OfferSignedUpdate(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if result.Check.Status != SignedUpdateAvailable || !result.Prompted || !result.Accepted || !result.Opened {
		t.Fatalf("unexpected prompt result: %+v", result)
	}
	if promptedApp != app || messageOptions.Title != "Test App update available" || messageOptions.Text != "Test App 2.0.0 is available. Open the download page?\n\nRelease notes" || messageOptions.Kind != MessageInfo || messageOptions.Buttons != MessageYesNo {
		t.Fatalf("unexpected message app/options: app=%p options=%+v", promptedApp, messageOptions)
	}
	if openedURL != updatePromptDownloadPage {
		t.Fatalf("opened URL = %q, want exact allowlisted URL %q", openedURL, updatePromptDownloadPage)
	}
}

func TestOfferSignedUpdateNoDoesNotOpenPage(t *testing.T) {
	server, options := updatePromptFixture(t, "2.0.0", updatePromptDownloadPage, "Fixture notes")
	defer server.Close()
	options.Check.ManifestURL = server.URL + "/latest.json"
	options.AllowedDownloadPages = []string{updatePromptDownloadPage}
	app := &App{options: Options{AppID: "wb.test", Version: "1.0.0"}}
	promptCount, openCount := 0, 0
	setUpdatePromptHooks(t, func(_ *App, _ MessageOptions) (MessageResult, error) {
		promptCount++
		return MessageResultNo, nil
	}, func(_ *App, _ string) error {
		openCount++
		return nil
	})

	result, err := app.OfferSignedUpdate(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Prompted || result.Accepted || result.Opened || promptCount != 1 || openCount != 0 {
		t.Fatalf("unexpected decline result: %+v prompts=%d opens=%d", result, promptCount, openCount)
	}
}

func TestOfferSignedUpdateCurrentDoesNotPrompt(t *testing.T) {
	server, options := updatePromptFixture(t, "1.0.0", updatePromptDownloadPage, "Fixture notes")
	defer server.Close()
	options.Check.ManifestURL = server.URL + "/latest.json"
	options.AllowedDownloadPages = []string{updatePromptDownloadPage}
	app := &App{options: Options{AppID: "wb.test", Version: "1.0.0"}}
	promptCount := 0
	setUpdatePromptHooks(t, func(_ *App, _ MessageOptions) (MessageResult, error) {
		promptCount++
		return MessageResultYes, nil
	}, func(_ *App, _ string) error { return nil })

	result, err := app.OfferSignedUpdate(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if result.Check.Status != SignedUpdateCurrent || result.Prompted || result.Accepted || result.Opened || promptCount != 0 {
		t.Fatalf("unexpected current-version result: %+v prompts=%d", result, promptCount)
	}
}

func TestOfferSignedUpdateRejectsUnallowlistedDownloadPage(t *testing.T) {
	server, options := updatePromptFixture(t, "2.0.0", updatePromptDownloadPage, "Fixture notes")
	defer server.Close()
	options.Check.ManifestURL = server.URL + "/latest.json"
	options.AllowedDownloadPages = []string{"https://example.invalid/other"}
	app := &App{options: Options{AppID: "wb.test", Version: "1.0.0"}}
	promptCount, openCount := 0, 0
	setUpdatePromptHooks(t, func(_ *App, _ MessageOptions) (MessageResult, error) {
		promptCount++
		return MessageResultYes, nil
	}, func(_ *App, _ string) error {
		openCount++
		return nil
	})

	result, err := app.OfferSignedUpdate(context.Background(), options)
	if err == nil || result.Check.Status != SignedUpdateAvailable || result.Prompted || result.Opened || promptCount != 0 || openCount != 0 {
		t.Fatalf("unallowlisted page result=%+v error=%v prompts=%d opens=%d", result, err, promptCount, openCount)
	}
}

func TestOfferSignedUpdateRejectsInvalidOptions(t *testing.T) {
	cases := []struct {
		name string
		edit func(*UpdatePromptOptions)
	}{
		{"empty name", func(options *UpdatePromptOptions) { options.AppName = " " }},
		{"empty allowlist", func(options *UpdatePromptOptions) { options.AllowedDownloadPages = nil }},
		{"HTTP page", func(options *UpdatePromptOptions) {
			options.AllowedDownloadPages = []string{"http://127.0.0.1/download"}
		}},
		{"NUL in name", func(options *UpdatePromptOptions) { options.AppName = "bad\x00name" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			options := UpdatePromptOptions{AppName: "Test App", AllowedDownloadPages: []string{updatePromptDownloadPage}}
			tc.edit(&options)
			_, err := (&App{}).OfferSignedUpdate(context.Background(), options)
			if err == nil || !strings.Contains(err.Error(), ErrInvalidOptions.Error()) {
				t.Fatalf("invalid options error = %v, want ErrInvalidOptions", err)
			}
		})
	}
}

func TestOfferSignedUpdateTruncatesNotesTo400Characters(t *testing.T) {
	notes := strings.Repeat("n", 401)
	server, options := updatePromptFixture(t, "2.0.0", updatePromptDownloadPage, notes)
	defer server.Close()
	options.Check.ManifestURL = server.URL + "/latest.json"
	options.AllowedDownloadPages = []string{updatePromptDownloadPage}
	app := &App{options: Options{AppID: "wb.test", Version: "1.0.0"}}
	var got MessageOptions
	setUpdatePromptHooks(t, func(_ *App, message MessageOptions) (MessageResult, error) {
		got = message
		return MessageResultNo, nil
	}, func(_ *App, _ string) error { return nil })

	if _, err := app.OfferSignedUpdate(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	want := "Test App 2.0.0 is available. Open the download page?\n\n" + strings.Repeat("n", 400)
	if got.Text != want {
		t.Fatalf("message text length/content mismatch: got %d chars, want %d", len([]rune(got.Text)), len([]rune(want)))
	}
}

func updatePromptFixture(t *testing.T, version, downloadPage, notes string) (*httptest.Server, UpdatePromptOptions) {
	t.Helper()
	privateKey, publicKey, _, _ := signedUpdateFixture(t, version, "wb.test", "stable")
	manifestBytes, err := json.Marshal(SignedUpdateManifest{
		App: "wb.test", Channel: "stable", Version: version, Released: "2026-09-27",
		Notes: notes, DownloadPage: downloadPage,
		Artifacts: map[string]string{"WELDBREAKERS-Setup.exe": strings.Repeat("a", 64)},
	})
	if err != nil {
		t.Fatal(err)
	}
	signature := ed25519.Sign(privateKey, manifestBytes)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.Path, ".sig") {
			_, _ = fmt.Fprintln(w, base64.StdEncoding.EncodeToString(signature))
			return
		}
		_, _ = w.Write(manifestBytes)
	}))
	check := SignedUpdateCheckOptions{
		ManifestURL: server.URL + "/latest.json", App: "wb.test", Channel: "stable", CurrentVersion: "1.0.0",
		PublicKey: publicKey, StateFile: filepath.Join(t.TempDir(), "update-state.json"),
		Enabled: true, StartupComplete: true, Online: true, AllowLoopbackHTTPForTests: true,
	}
	return server, UpdatePromptOptions{Check: check, AppName: "Test App", AllowedDownloadPages: []string{downloadPage}}
}

func setUpdatePromptHooks(t *testing.T, show func(*App, MessageOptions) (MessageResult, error), open func(*App, string) error) {
	t.Helper()
	oldShow, oldOpen := showUpdateMessage, openUpdateURL
	showUpdateMessage, openUpdateURL = show, open
	t.Cleanup(func() {
		showUpdateMessage, openUpdateURL = oldShow, oldOpen
	})
}
