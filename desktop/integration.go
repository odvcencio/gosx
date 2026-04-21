package desktop

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	// MaxInstanceMessageBytes caps the WM_COPYDATA/single-instance payload.
	MaxInstanceMessageBytes = 64 * 1024

	registryStringValue RegistryValueKind = "string"
)

// InstanceMessage is the payload forwarded from a second process to the
// already-running desktop instance.
type InstanceMessage struct {
	AppID      string   `json:"app_id,omitempty"`
	Args       []string `json:"args,omitempty"`
	WorkingDir string   `json:"working_dir,omitempty"`
}

// BuildInstanceMessage encodes a second-launch payload. Args are the command
// line arguments excluding argv[0].
func BuildInstanceMessage(appID string, args []string, workingDir string) ([]byte, error) {
	appID = strings.TrimSpace(appID)
	if appID != "" {
		if err := validateAppID(appID); err != nil {
			return nil, err
		}
	}
	if strings.ContainsRune(workingDir, '\x00') {
		return nil, fmt.Errorf("%w: working directory contains NUL", ErrInvalidOptions)
	}
	copiedArgs := append([]string(nil), args...)
	for i, arg := range copiedArgs {
		if strings.ContainsRune(arg, '\x00') {
			return nil, fmt.Errorf("%w: arg %d contains NUL", ErrInvalidOptions, i)
		}
	}

	payload, err := json.Marshal(InstanceMessage{
		AppID:      appID,
		Args:       copiedArgs,
		WorkingDir: workingDir,
	})
	if err != nil {
		return nil, fmt.Errorf("encode instance message: %w", err)
	}
	if len(payload) > MaxInstanceMessageBytes {
		return nil, fmt.Errorf("%w: instance message exceeds %d bytes",
			ErrInvalidOptions, MaxInstanceMessageBytes)
	}
	return payload, nil
}

// ParseInstanceMessage decodes a second-launch payload.
func ParseInstanceMessage(payload []byte) (InstanceMessage, error) {
	if len(payload) == 0 {
		return InstanceMessage{}, fmt.Errorf("%w: empty instance message", ErrInvalidOptions)
	}
	if len(payload) > MaxInstanceMessageBytes {
		return InstanceMessage{}, fmt.Errorf("%w: instance message exceeds %d bytes",
			ErrInvalidOptions, MaxInstanceMessageBytes)
	}
	var msg InstanceMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		return InstanceMessage{}, fmt.Errorf("%w: decode instance message: %v",
			ErrInvalidOptions, err)
	}
	if msg.AppID != "" {
		if err := validateAppID(msg.AppID); err != nil {
			return InstanceMessage{}, err
		}
	}
	if strings.ContainsRune(msg.WorkingDir, '\x00') {
		return InstanceMessage{}, fmt.Errorf("%w: working directory contains NUL",
			ErrInvalidOptions)
	}
	for i, arg := range msg.Args {
		if strings.ContainsRune(arg, '\x00') {
			return InstanceMessage{}, fmt.Errorf("%w: arg %d contains NUL",
				ErrInvalidOptions, i)
		}
	}
	if msg.Args == nil {
		msg.Args = []string{}
	}
	return msg, nil
}

// RegistryValueKind describes a value emitted by the pure registry builders.
// Windows currently writes all values as REG_SZ.
type RegistryValueKind string

// RegistryValue is one value under an HKCU\Software\Classes registry key.
// Name is empty for the default value.
type RegistryValue struct {
	Key   string
	Name  string
	Value string
	Kind  RegistryValueKind
}

// RegistryPlan is a platform-neutral description of per-user Windows shell
// integration keys. The Windows backend applies it under HKCU.
type RegistryPlan struct {
	Values []RegistryValue
}

// ProtocolRegistration configures a custom URI scheme registration.
type ProtocolRegistration struct {
	Scheme     string
	AppID      string
	AppName    string
	Executable string
	Icon       string
	Arguments  []string
}

// FileAssociationRegistration configures a file extension association.
type FileAssociationRegistration struct {
	Extension   string
	AppID       string
	AppName     string
	ProgID      string
	Description string
	Executable  string
	Icon        string
	Arguments   []string
}

// BuildProtocolRegistration returns the per-user HKCU\Software\Classes values
// needed for a URL protocol.
func BuildProtocolRegistration(reg ProtocolRegistration) (RegistryPlan, error) {
	scheme, err := normalizeScheme(reg.Scheme)
	if err != nil {
		return RegistryPlan{}, err
	}
	appID := strings.TrimSpace(reg.AppID)
	if err := validateAppID(appID); err != nil {
		return RegistryPlan{}, err
	}
	appName := strings.TrimSpace(reg.AppName)
	if appName == "" {
		appName = appID
	}
	exe, err := normalizeCommandPath(reg.Executable, "executable")
	if err != nil {
		return RegistryPlan{}, err
	}
	icon := strings.TrimSpace(reg.Icon)
	if icon == "" {
		icon = exe
	} else if _, err := normalizeCommandPath(icon, "icon"); err != nil {
		return RegistryPlan{}, err
	}
	command, err := buildShellOpenCommand(exe, reg.Arguments)
	if err != nil {
		return RegistryPlan{}, err
	}

	key := `Software\Classes\` + scheme
	return RegistryPlan{Values: []RegistryValue{
		{Key: key, Value: "URL:" + appName + " Protocol", Kind: registryStringValue},
		{Key: key, Name: "URL Protocol", Value: "", Kind: registryStringValue},
		{Key: key + `\DefaultIcon`, Value: icon, Kind: registryStringValue},
		{Key: key + `\shell\open\command`, Value: command, Kind: registryStringValue},
	}}, nil
}

// BuildFileAssociation returns the per-user HKCU\Software\Classes values
// needed for a file extension and ProgID open command.
func BuildFileAssociation(reg FileAssociationRegistration) (RegistryPlan, error) {
	ext, err := normalizeExtension(reg.Extension)
	if err != nil {
		return RegistryPlan{}, err
	}
	appID := strings.TrimSpace(reg.AppID)
	if err := validateAppID(appID); err != nil {
		return RegistryPlan{}, err
	}
	progID := strings.TrimSpace(reg.ProgID)
	if progID == "" {
		progID = appID + "." + strings.TrimPrefix(ext, ".")
	}
	if err := validateProgID(progID); err != nil {
		return RegistryPlan{}, err
	}
	description := strings.TrimSpace(reg.Description)
	if description == "" {
		appName := strings.TrimSpace(reg.AppName)
		if appName == "" {
			appName = appID
		}
		description = appName + " " + strings.ToUpper(strings.TrimPrefix(ext, ".")) + " File"
	}
	exe, err := normalizeCommandPath(reg.Executable, "executable")
	if err != nil {
		return RegistryPlan{}, err
	}
	icon := strings.TrimSpace(reg.Icon)
	if icon == "" {
		icon = exe
	} else if _, err := normalizeCommandPath(icon, "icon"); err != nil {
		return RegistryPlan{}, err
	}
	command, err := buildShellOpenCommand(exe, reg.Arguments)
	if err != nil {
		return RegistryPlan{}, err
	}

	extKey := `Software\Classes\` + ext
	progKey := `Software\Classes\` + progID
	return RegistryPlan{Values: []RegistryValue{
		{Key: extKey, Value: progID, Kind: registryStringValue},
		{Key: extKey + `\OpenWithProgids`, Name: progID, Value: "", Kind: registryStringValue},
		{Key: progKey, Value: description, Kind: registryStringValue},
		{Key: progKey + `\DefaultIcon`, Value: icon, Kind: registryStringValue},
		{Key: progKey + `\shell\open\command`, Value: command, Kind: registryStringValue},
	}}, nil
}

func defaultAppID(title string) string {
	slug := slugIdentifier(title)
	if slug == "" {
		return "gosx.app"
	}
	return "gosx." + slug
}

func slugIdentifier(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastSep := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastSep = false
		case r == '-' || r == '_' || r == '.' || r == ' ':
			if b.Len() > 0 && !lastSep {
				b.WriteByte('.')
				lastSep = true
			}
		}
	}
	return strings.Trim(b.String(), ".")
}

func normalizeScheme(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "", fmt.Errorf("%w: protocol scheme is empty", ErrInvalidOptions)
	}
	if !isASCIILetter(value[0]) {
		return "", fmt.Errorf("%w: protocol scheme must start with a letter", ErrInvalidOptions)
	}
	for i := 1; i < len(value); i++ {
		c := value[i]
		if !(isASCIILetter(c) || isASCIIDigit(c) || c == '+' || c == '-' || c == '.') {
			return "", fmt.Errorf("%w: protocol scheme contains invalid character %q",
				ErrInvalidOptions, c)
		}
	}
	return value, nil
}

func normalizeExtension(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "", fmt.Errorf("%w: file extension is empty", ErrInvalidOptions)
	}
	if !strings.HasPrefix(value, ".") {
		value = "." + value
	}
	if value == "." {
		return "", fmt.Errorf("%w: file extension is empty", ErrInvalidOptions)
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c == '\x00' || c == '\\' || c == '/' || c == ':' || c == '"' || c <= ' ' {
			return "", fmt.Errorf("%w: file extension contains invalid character %q",
				ErrInvalidOptions, c)
		}
	}
	return value, nil
}

func validateAppID(value string) error {
	if value == "" {
		return fmt.Errorf("%w: app id is empty", ErrInvalidOptions)
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if !(isASCIILetter(c) || isASCIIDigit(c) || c == '.' || c == '-' || c == '_') {
			return fmt.Errorf("%w: app id contains invalid character %q",
				ErrInvalidOptions, c)
		}
	}
	return nil
}

func validateProgID(value string) error {
	if value == "" {
		return fmt.Errorf("%w: prog id is empty", ErrInvalidOptions)
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if !(isASCIILetter(c) || isASCIIDigit(c) || c == '.' || c == '-' || c == '_') {
			return fmt.Errorf("%w: prog id contains invalid character %q",
				ErrInvalidOptions, c)
		}
	}
	return nil
}

func normalizeCommandPath(value, label string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%w: %s is empty", ErrInvalidOptions, label)
	}
	if strings.ContainsAny(value, "\x00\"") {
		return "", fmt.Errorf("%w: %s contains invalid character", ErrInvalidOptions, label)
	}
	return value, nil
}

func buildShellOpenCommand(executable string, args []string) (string, error) {
	var b strings.Builder
	b.WriteString(quoteWindowsCommandArg(executable))
	for i, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			continue
		}
		if strings.ContainsAny(arg, "\x00\"") {
			return "", fmt.Errorf("%w: command argument %d contains invalid character",
				ErrInvalidOptions, i)
		}
		b.WriteByte(' ')
		b.WriteString(quoteWindowsCommandArg(arg))
	}
	b.WriteString(` "%1"`)
	return b.String(), nil
}

func quoteWindowsCommandArg(value string) string {
	return `"` + value + `"`
}

func isASCIILetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isASCIIDigit(c byte) bool {
	return c >= '0' && c <= '9'
}
