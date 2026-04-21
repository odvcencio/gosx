package desktop

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
)

// This file carries the native-API option structs so callers on non-
// Windows builds can still reference OpenFileOptions / SaveFileOptions
// / FileFilter when constructing API surfaces. The real dialog
// implementations live in native_windows.go; the stubs in
// app_unsupported.go return ErrUnsupported when invoked.

// FileFilter is one entry in a dialog's type filter. Patterns are
// semicolon-separated and follow the Win32 convention:
//
//	FileFilter{Name: "Images", Pattern: "*.png;*.jpg;*.jpeg"}
//
// Non-Windows backends will translate to their platform equivalent when
// those backends land — e.g. NSOpenPanel allowedContentTypes on macOS.
type FileFilter struct {
	Name    string
	Pattern string
}

// OpenFileOptions configures an Open-file dialog. Every field is optional.
// AllowMultiple enables multi-select on backends that support it.
type OpenFileOptions struct {
	Title           string
	Filters         []FileFilter
	InitialDir      string
	InitialFilename string
	DefaultExt      string
	AllowMultiple   bool
}

// SaveFileOptions configures a Save-file dialog. OverwritePrompt, when
// true, asks the user to confirm before replacing an existing file.
type SaveFileOptions struct {
	Title           string
	Filters         []FileFilter
	InitialDir      string
	InitialFilename string
	DefaultExt      string
	OverwritePrompt bool
}

// DPIAwareness selects the process DPI mode before the native window is
// created. The zero value keeps the Windows backend's default:
// per-monitor-v2 when the OS exposes it.
type DPIAwareness string

const (
	DPIAwarenessDefault      DPIAwareness = ""
	DPIAwarenessUnaware      DPIAwareness = "unaware"
	DPIAwarenessSystem       DPIAwareness = "system"
	DPIAwarenessPerMonitorV2 DPIAwareness = "per-monitor-v2"
)

// AccessibilityOptions carries the native accessibility metadata GoSX owns.
// WebView2 continues to expose page content through its own UIA provider.
type AccessibilityOptions struct {
	Enabled     bool
	Name        string
	Description string
}

// Menu is a native menu tree used by App.SetMenuBar, Window.ContextMenu, and
// TrayOptions.Menu. Leaf items may supply OnClick callbacks.
type Menu struct {
	Items []MenuItem
}

// MenuItem is one native menu entry. Separator entries ignore Label, ID, and
// Submenu. Submenu entries ignore OnClick.
type MenuItem struct {
	ID        string
	Label     string
	Disabled  bool
	Checked   bool
	Separator bool
	Submenu   *Menu
	OnClick   func()
}

// MenuPlan is the platform-neutral menu payload produced for tests and native
// backends. Command IDs are assigned to actionable leaf items.
type MenuPlan struct {
	Items         []MenuPlanItem
	NextCommandID uint16
}

// MenuPlanItem is one validated menu entry in a MenuPlan.
type MenuPlanItem struct {
	ID        string
	Label     string
	CommandID uint16
	Disabled  bool
	Checked   bool
	Separator bool
	Items     []MenuPlanItem
}

// TrayOptions configures a shell tray icon. Icon is an optional path to a
// Windows .ico; empty uses the process default icon.
type TrayOptions struct {
	Icon    string
	Tooltip string
	Menu    Menu
	OnClick func(TrayEvent)
}

// TrayEvent identifies a shell interaction with the tray icon.
type TrayEvent string

const (
	TrayEventClick   TrayEvent = "click"
	TrayEventContext TrayEvent = "context"
)

// Tray is the live shell tray registration returned by App.Tray.
type Tray struct {
	app *App
}

// Close removes the tray icon from the shell notification area.
func (t *Tray) Close() error {
	if t == nil || t.app == nil {
		return fmt.Errorf("%w: nil tray", ErrInvalidOptions)
	}
	return t.app.closeTray()
}

// TrayRegistration is the validated platform-neutral payload for a tray icon.
type TrayRegistration struct {
	AppID   string
	Icon    string
	Tooltip string
	Menu    MenuPlan
}

// Notification describes a user-visible native notification.
type Notification struct {
	Title   string
	Body    string
	Actions []NotificationAction
	Silent  bool
}

// NotificationAction is one optional button in a toast payload.
type NotificationAction struct {
	ID    string
	Label string
}

// ToastPayload is the XML payload shape consumed by the Windows toast path.
type ToastPayload struct {
	XML       string
	ActionIDs []string
}

const (
	firstNativeCommandID = uint16(1000)
	maxNativeCommandID   = uint16(0xefff)
	maxToastActions      = 5
	maxTrayTooltipRunes  = 127
)

// BuildMenuPlan validates a menu tree and assigns stable command IDs. Native
// backends use the same builder so Linux tests can lock down menu payloads.
func BuildMenuPlan(menu Menu) (MenuPlan, error) {
	return buildMenuPlan(menu, firstNativeCommandID)
}

func buildMenuPlan(menu Menu, firstCommandID uint16) (MenuPlan, error) {
	next := firstCommandID
	items, err := buildMenuPlanItems(menu.Items, &next, 0)
	if err != nil {
		return MenuPlan{}, err
	}
	return MenuPlan{Items: items, NextCommandID: next}, nil
}

func buildMenuPlanItems(items []MenuItem, next *uint16, depth int) ([]MenuPlanItem, error) {
	if depth > 8 {
		return nil, fmt.Errorf("%w: menu nesting is too deep", ErrInvalidOptions)
	}
	out := make([]MenuPlanItem, 0, len(items))
	for i, item := range items {
		if strings.ContainsRune(item.ID, '\x00') {
			return nil, fmt.Errorf("%w: menu item %d id contains NUL", ErrInvalidOptions, i)
		}
		if strings.ContainsRune(item.Label, '\x00') {
			return nil, fmt.Errorf("%w: menu item %d label contains NUL", ErrInvalidOptions, i)
		}
		planItem := MenuPlanItem{
			ID:        strings.TrimSpace(item.ID),
			Label:     strings.TrimSpace(item.Label),
			Disabled:  item.Disabled,
			Checked:   item.Checked,
			Separator: item.Separator,
		}
		switch {
		case item.Separator:
			out = append(out, planItem)
			continue
		case planItem.Label == "":
			return nil, fmt.Errorf("%w: menu item %d label is empty", ErrInvalidOptions, i)
		case item.Submenu != nil:
			if len(item.Submenu.Items) == 0 {
				return nil, fmt.Errorf("%w: submenu %q is empty", ErrInvalidOptions, planItem.Label)
			}
			children, err := buildMenuPlanItems(item.Submenu.Items, next, depth+1)
			if err != nil {
				return nil, err
			}
			planItem.Items = children
		default:
			if *next == 0 || *next > maxNativeCommandID {
				return nil, fmt.Errorf("%w: menu command id space exhausted", ErrInvalidOptions)
			}
			planItem.CommandID = *next
			*next = *next + 1
		}
		out = append(out, planItem)
	}
	return out, nil
}

// BuildTrayRegistration validates the shell tray registration payload.
func BuildTrayRegistration(appID string, options TrayOptions) (TrayRegistration, error) {
	appID = strings.TrimSpace(appID)
	if err := validateAppID(appID); err != nil {
		return TrayRegistration{}, err
	}
	icon := strings.TrimSpace(options.Icon)
	tooltip := strings.TrimSpace(options.Tooltip)
	for name, value := range map[string]string{"icon": icon, "tooltip": tooltip} {
		if strings.ContainsRune(value, '\x00') {
			return TrayRegistration{}, fmt.Errorf("%w: tray %s contains NUL", ErrInvalidOptions, name)
		}
	}
	if len([]rune(tooltip)) > maxTrayTooltipRunes {
		return TrayRegistration{}, fmt.Errorf("%w: tray tooltip exceeds %d characters",
			ErrInvalidOptions, maxTrayTooltipRunes)
	}
	menu, err := BuildMenuPlan(options.Menu)
	if err != nil {
		return TrayRegistration{}, err
	}
	return TrayRegistration{AppID: appID, Icon: icon, Tooltip: tooltip, Menu: menu}, nil
}

// BuildToastPayload validates a notification and returns a Windows
// ToastGeneric XML payload.
func BuildToastPayload(notification Notification) (ToastPayload, error) {
	title := strings.TrimSpace(notification.Title)
	body := strings.TrimSpace(notification.Body)
	if title == "" {
		return ToastPayload{}, fmt.Errorf("%w: notification title is empty", ErrInvalidOptions)
	}
	for name, value := range map[string]string{"title": title, "body": body} {
		if strings.ContainsRune(value, '\x00') {
			return ToastPayload{}, fmt.Errorf("%w: notification %s contains NUL",
				ErrInvalidOptions, name)
		}
	}
	if len(notification.Actions) > maxToastActions {
		return ToastPayload{}, fmt.Errorf("%w: notification has more than %d actions",
			ErrInvalidOptions, maxToastActions)
	}

	var b strings.Builder
	b.WriteString(`<toast launch="gosx-notification">`)
	b.WriteString(`<visual><binding template="ToastGeneric"><text>`)
	b.WriteString(escapeXML(title))
	b.WriteString(`</text>`)
	if body != "" {
		b.WriteString(`<text>`)
		b.WriteString(escapeXML(body))
		b.WriteString(`</text>`)
	}
	b.WriteString(`</binding></visual>`)

	actionIDs := make([]string, 0, len(notification.Actions))
	if len(notification.Actions) > 0 {
		b.WriteString(`<actions>`)
		for i, action := range notification.Actions {
			id := strings.TrimSpace(action.ID)
			label := strings.TrimSpace(action.Label)
			if id == "" || label == "" {
				return ToastPayload{}, fmt.Errorf("%w: notification action %d is incomplete",
					ErrInvalidOptions, i)
			}
			if strings.ContainsRune(id, '\x00') || strings.ContainsRune(label, '\x00') {
				return ToastPayload{}, fmt.Errorf("%w: notification action %d contains NUL",
					ErrInvalidOptions, i)
			}
			actionIDs = append(actionIDs, id)
			b.WriteString(`<action activationType="foreground" content="`)
			b.WriteString(escapeXML(label))
			b.WriteString(`" arguments="`)
			b.WriteString(escapeXML(id))
			b.WriteString(`"/>`)
		}
		b.WriteString(`</actions>`)
	}
	if notification.Silent {
		b.WriteString(`<audio silent="true"/>`)
	}
	b.WriteString(`</toast>`)
	return ToastPayload{XML: b.String(), ActionIDs: actionIDs}, nil
}

func escapeXML(value string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(value))
	return b.String()
}

func normalizeDPIAwareness(value DPIAwareness) (DPIAwareness, error) {
	switch value {
	case DPIAwarenessDefault:
		return DPIAwarenessPerMonitorV2, nil
	case DPIAwarenessUnaware, DPIAwarenessSystem, DPIAwarenessPerMonitorV2:
		return value, nil
	default:
		return "", fmt.Errorf("%w: unsupported DPI awareness %q", ErrInvalidOptions, value)
	}
}

func dispatchFileDrop(handler func([]string), paths []string) {
	if handler == nil {
		return
	}
	handler(append([]string(nil), paths...))
}
