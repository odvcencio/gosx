package textlayout

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var cssFontSizePattern = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)px`)

// ApproximateMeasurer provides a fast server-side width estimator for text
// layout planning before the browser refines the result with real font metrics.
type ApproximateMeasurer struct {
	DefaultFontSize float64
}

// MeasureBatch estimates widths from CSS font size and grapheme class.
func (m ApproximateMeasurer) MeasureBatch(font string, texts []string) ([]float64, error) {
	size := approximateFontSize(font, m.DefaultFontSize)
	widths := make([]float64, len(texts))
	for i, text := range texts {
		widths[i] = approximateTextWidth(text, size, font)
	}
	return widths, nil
}

func approximateFontSize(font string, fallback float64) float64 {
	if fallback <= 0 {
		fallback = 16
	}
	match := cssFontSizePattern.FindStringSubmatch(strings.TrimSpace(font))
	if len(match) != 2 {
		return fallback
	}
	value := parseApproximateFloat(match[1], fallback)
	if value <= 0 {
		return fallback
	}
	return value
}

func approximateTextWidth(text string, fontSize float64, font string) float64 {
	if text == "" {
		return 0
	}
	if text == " " {
		return fontSize * 0.32
	}
	if strings.Contains(strings.ToLower(font), "monospace") {
		return float64(utf8.RuneCountInString(text)) * fontSize * 0.62
	}

	width := 0.0
	for _, r := range text {
		width += approximateRuneWidth(r, fontSize)
	}
	return width
}

func approximateRuneWidth(r rune, fontSize float64) float64 {
	switch {
	case r == '\n' || r == '\r':
		return 0
	case r == '\t':
		return fontSize * 1.28
	case unicode.IsSpace(r):
		return fontSize * 0.32
	case isApproximateWideRune(r):
		return fontSize
	case unicode.IsDigit(r):
		return fontSize * 0.56
	case unicode.IsUpper(r):
		switch r {
		case 'M', 'W':
			return fontSize * 0.86
		case 'I':
			return fontSize * 0.4
		default:
			return fontSize * 0.68
		}
	case unicode.IsLower(r):
		switch r {
		case 'm', 'w':
			return fontSize * 0.8
		case 'i', 'l', 'j':
			return fontSize * 0.34
		default:
			return fontSize * 0.56
		}
	case unicode.IsPunct(r) || unicode.IsSymbol(r):
		switch r {
		case '.', ',', ':', ';', '\'', '"', '`':
			return fontSize * 0.24
		case '-', '_', '(', ')', '[', ']', '{', '}', '/', '\\', '|':
			return fontSize * 0.36
		default:
			return fontSize * 0.46
		}
	default:
		return fontSize * 0.6
	}
}

func isApproximateWideRune(r rune) bool {
	switch {
	case r >= 0x1100 && r <= 0x115F:
		return true
	case r >= 0x2329 && r <= 0x232A:
		return true
	case r >= 0x2E80 && r <= 0xA4CF:
		return true
	case r >= 0xAC00 && r <= 0xD7A3:
		return true
	case r >= 0xF900 && r <= 0xFAFF:
		return true
	case r >= 0xFE10 && r <= 0xFE19:
		return true
	case r >= 0xFE30 && r <= 0xFE6F:
		return true
	case r >= 0xFF00 && r <= 0xFF60:
		return true
	case r >= 0xFFE0 && r <= 0xFFE6:
		return true
	case r >= 0x1F300 && r <= 0x1FAFF:
		return true
	default:
		return false
	}
}

func parseApproximateFloat(raw string, fallback float64) float64 {
	if raw == "" {
		return fallback
	}
	value := 0.0
	scale := 1.0
	decimal := false
	digits := 0
	for _, r := range raw {
		switch {
		case r >= '0' && r <= '9':
			digit := float64(r - '0')
			if decimal {
				scale *= 0.1
				value += digit * scale
			} else {
				value = value*10 + digit
			}
			digits++
		case r == '.' && !decimal:
			decimal = true
		default:
			return fallback
		}
	}
	if digits == 0 {
		return fallback
	}
	return value
}
