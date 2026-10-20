package hydrate

import (
	"fmt"
	"strings"
	"unicode"
)

// HubDocumentConfig binds one server-rendered view to a versioned text document.
// RootID identifies a page element containing a textarea marked
// data-gosx-document-source. Optional descendants use data-gosx-document-preview,
// -status, -version, -presence, -identity, -indicator and -cursors.
//
// Updates carry {text, version, html?}; edits carry {text, version}. HTML is
// trusted server-rendered preview content, never a client-authored HTML payload.
// Cursor offsets use UTF-16 code units, matching native textarea selections.
// Applications retain authorization, document storage and rendering on the server.
type HubDocumentConfig struct {
	RootID           string            `json:"rootId"`
	Version          uint64            `json:"version"`
	DebounceMS       int               `json:"debounceMs"`
	CursorThrottleMS int               `json:"cursorThrottleMs"`
	CursorStaleMS    int               `json:"cursorStaleMs"`
	MaxTextBytes     int               `json:"maxTextBytes"`
	Events           HubDocumentEvents `json:"events"`
}

// HubDocumentEvents names the document protocol on an ordinary GoSX Hub.
// Empty names select the defaults documented on each field.
type HubDocumentEvents struct {
	Update   string `json:"update"`   // doc:update
	Edit     string `json:"edit"`     // doc:edit
	Sync     string `json:"sync"`     // doc:sync; request a current snapshot and identity
	Cursor   string `json:"cursor"`   // cursor:update
	Roster   string `json:"roster"`   // cursor:roster
	Leave    string `json:"leave"`    // cursor:leave
	Presence string `json:"presence"` // presence:count
	Self     string `json:"self"`     // presence:self
}

// AddHubDocument registers an opt-in framework document view. Invalid config
// leaves the manifest untouched. Zero timings and size select bounded defaults.
func (m *Manifest) AddHubDocument(name, path string, config HubDocumentConfig) (string, error) {
	name, path = strings.TrimSpace(name), strings.TrimSpace(path)
	config.RootID = strings.TrimSpace(config.RootID)
	if name == "" || path == "" || config.RootID == "" || len(config.RootID) > 256 || strings.IndexFunc(config.RootID, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return "", fmt.Errorf("hub document requires a hub name, path and whitespace-free root ID")
	}
	if config.Version > 1<<53-1 {
		return "", fmt.Errorf("hub document version exceeds the browser's exact integer range")
	}
	for _, entry := range m.Hubs {
		if entry.Document != nil && entry.Document.RootID == config.RootID {
			return "", fmt.Errorf("hub document root %q is already bound", config.RootID)
		}
	}
	for _, setting := range []struct {
		value              *int
		fallback, min, max int
	}{
		{&config.DebounceMS, 100, 1, 5000},
		{&config.CursorThrottleMS, 60, 16, 1000},
		{&config.CursorStaleMS, 15000, 1000, 600000},
		{&config.MaxTextBytes, 64 * 1024, 1, 1024 * 1024},
	} {
		if *setting.value == 0 {
			*setting.value = setting.fallback
		}
		if *setting.value < setting.min || *setting.value > setting.max {
			return "", fmt.Errorf("hub document timing or size is outside its supported bounds")
		}
	}
	seen := map[string]bool{}
	for _, event := range []struct {
		value    *string
		fallback string
	}{
		{&config.Events.Update, "doc:update"}, {&config.Events.Edit, "doc:edit"},
		{&config.Events.Sync, "doc:sync"}, {&config.Events.Cursor, "cursor:update"},
		{&config.Events.Roster, "cursor:roster"}, {&config.Events.Leave, "cursor:leave"},
		{&config.Events.Presence, "presence:count"}, {&config.Events.Self, "presence:self"},
	} {
		*event.value = strings.TrimSpace(*event.value)
		if *event.value == "" {
			*event.value = event.fallback
		}
		if len(*event.value) > 128 || strings.HasPrefix(*event.value, "__") || strings.IndexFunc(*event.value, unicode.IsControl) >= 0 || seen[*event.value] {
			return "", fmt.Errorf("hub document event names must be distinct and at most 128 characters")
		}
		seen[*event.value] = true
	}
	if err := m.RequireFeature("hub-document"); err != nil {
		return "", err
	}
	id := m.AddHub(name, path, nil)
	m.Hubs[len(m.Hubs)-1].Document = &config
	return id, nil
}
