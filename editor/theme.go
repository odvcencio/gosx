package editor

import "strings"

// ThemeDefinition describes a built-in or consumer-provided theme.
type ThemeDefinition struct {
	Name        Theme
	Label       string
	RootClass   string
	ColorScheme string
}

var builtinThemes = map[Theme]ThemeDefinition{
	ThemeDark: {
		Name:        ThemeDark,
		Label:       "Dark",
		RootClass:   "gosx-editor--theme-dark",
		ColorScheme: "dark",
	},
	ThemeLight: {
		Name:        ThemeLight,
		Label:       "Light",
		RootClass:   "gosx-editor--theme-light",
		ColorScheme: "light",
	},
}

// ResolveTheme returns metadata for the requested theme.
func ResolveTheme(theme Theme) ThemeDefinition {
	if def, ok := builtinThemes[theme]; ok {
		return def
	}

	name := strings.TrimSpace(string(theme))
	if name == "" {
		return builtinThemes[ThemeDark]
	}

	return ThemeDefinition{
		Name:        theme,
		Label:       humanizeLabel(name),
		RootClass:   "gosx-editor--theme-" + classSuffix(name),
		ColorScheme: "auto",
	}
}

// BuiltinThemes returns the editor themes shipped with gosx.
func BuiltinThemes() []ThemeDefinition {
	return []ThemeDefinition{
		builtinThemes[ThemeDark],
		builtinThemes[ThemeLight],
	}
}

func humanizeLabel(value string) string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == '-' || r == '_' || r == ' '
	})
	if len(parts) == 0 {
		return "Editor"
	}

	for i, part := range parts {
		if part == "" {
			continue
		}
		lower := strings.ToLower(part)
		parts[i] = strings.ToUpper(lower[:1]) + lower[1:]
	}

	return strings.Join(parts, " ")
}

func classSuffix(value string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			lastDash = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_' || r == ' ':
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}

	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "custom"
	}
	return out
}
