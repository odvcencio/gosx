package desktop

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"
)

// UpdatePromptOptions configures a signed update check and the confirmation
// shown before opening its download page. At least one trusted download page
// must be provided, and each must be HTTPS, because signed manifests only
// carry HTTPS download pages.
type UpdatePromptOptions struct {
	Check                SignedUpdateCheckOptions
	AppName              string
	AllowedDownloadPages []string
}

// UpdatePromptResult reports the signed check and the user's response. Opened
// is true only when the accepted page was successfully handed to the platform.
type UpdatePromptResult struct {
	Check    SignedUpdateResult
	Prompted bool
	Accepted bool
	Opened   bool
}

var (
	showUpdateMessage = func(app *App, options MessageOptions) (MessageResult, error) {
		return app.ShowMessage(options)
	}
	openUpdateURL = func(app *App, page string) error {
		return app.OpenURL(page)
	}
)

// OfferSignedUpdate checks the signed update feed and, when an update is
// available and its download page is allowlisted, asks whether to open it.
// It never downloads or installs an update.
func (a *App) OfferSignedUpdate(ctx context.Context, options UpdatePromptOptions) (UpdatePromptResult, error) {
	if err := validateUpdatePromptOptions(options); err != nil {
		return UpdatePromptResult{}, err
	}

	check, err := a.CheckSignedUpdate(ctx, options.Check)
	result := UpdatePromptResult{Check: check}
	if err != nil {
		return result, err
	}
	if check.Status != SignedUpdateAvailable {
		return result, nil
	}
	if !containsUpdateDownloadPage(options.AllowedDownloadPages, check.DownloadPage) {
		return result, fmt.Errorf("signed update download page %q is not allowlisted", check.DownloadPage)
	}

	text := fmt.Sprintf("%s %s is available. Open the download page?", options.AppName, check.Version)
	if check.Notes != "" {
		notes := []rune(check.Notes)
		if len(notes) > 400 {
			notes = notes[:400]
		}
		text += "\n\n" + string(notes)
	}
	result.Prompted = true
	answer, err := showUpdateMessage(a, MessageOptions{
		Title: options.AppName + " update available", Text: text,
		Kind: MessageInfo, Buttons: MessageYesNo,
	})
	if err != nil {
		return result, err
	}
	if answer != MessageResultYes {
		return result, nil
	}
	result.Accepted = true
	if err := openUpdateURL(a, check.DownloadPage); err != nil {
		return result, err
	}
	result.Opened = true
	return result, nil
}

func validateUpdatePromptOptions(options UpdatePromptOptions) error {
	if strings.TrimSpace(options.AppName) == "" {
		return fmt.Errorf("%w: update prompt app name is empty", ErrInvalidOptions)
	}
	if strings.IndexByte(options.AppName, 0) >= 0 || !utf8.ValidString(options.AppName) {
		return fmt.Errorf("%w: update prompt app name contains invalid text", ErrInvalidOptions)
	}
	if len(options.AllowedDownloadPages) == 0 {
		return fmt.Errorf("%w: update prompt download page allowlist is empty", ErrInvalidOptions)
	}
	for _, page := range options.AllowedDownloadPages {
		if err := validateAllowedUpdateDownloadPage(page); err != nil {
			return err
		}
	}
	return nil
}

func validateAllowedUpdateDownloadPage(page string) error {
	parsed, err := url.Parse(page)
	if err != nil || parsed == nil || parsed.Opaque != "" || parsed.Hostname() == "" || parsed.User != nil || strings.TrimSpace(page) != page || strings.IndexByte(page, 0) >= 0 {
		return fmt.Errorf("%w: invalid allowed update download page %q", ErrInvalidOptions, page)
	}
	if !strings.EqualFold(parsed.Scheme, "https") {
		return fmt.Errorf("%w: allowed update download page must use HTTPS: %q", ErrInvalidOptions, page)
	}
	return nil
}

func containsUpdateDownloadPage(allowlist []string, page string) bool {
	for _, allowed := range allowlist {
		if page == allowed {
			return true
		}
	}
	return false
}
