package server

import (
	"context"
	"net/http"
	"strconv"
	"strings"
)

const (
	localeContextKey         = contextKey("gosx.locale")
	localePrefixedContextKey = contextKey("gosx.locale_prefixed")
)

// I18nConfig configures locale-aware routing.
type I18nConfig struct {
	Locales       []string
	DefaultLocale string
	PrefixDefault bool
	CookieName    string
	HeaderName    string
}

type normalizedI18nConfig struct {
	locales       []string
	localeSet     map[string]struct{}
	defaultLocale string
	prefixDefault bool
	cookieName    string
	headerName    string
}

// UseI18n installs locale-aware routing middleware on the app.
func (a *App) UseI18n(config I18nConfig) {
	a.Use(I18nMiddleware(config))
}

// I18nMiddleware detects locale path prefixes and makes the selected locale
// available through RequestLocale. Prefixed locale paths are stripped before the
// next handler runs, so /fr/about can match a route registered as /about.
func I18nMiddleware(config I18nConfig) Middleware {
	cfg := normalizeI18nConfig(config)
	return func(next http.Handler) http.Handler {
		if next == nil {
			next = http.NotFoundHandler()
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			locale, routedPath, prefixed := resolveRequestLocale(r, cfg)
			w.Header().Set("Content-Language", locale)
			appendVary(w.Header(), "Accept-Language")

			ctx := context.WithValue(r.Context(), localeContextKey, locale)
			ctx = context.WithValue(ctx, localePrefixedContextKey, prefixed)
			routed := r.WithContext(ctx)
			if prefixed && r.URL != nil {
				u := *r.URL
				u.Path = routedPath
				u.RawPath = ""
				routed.URL = &u
			}
			next.ServeHTTP(w, routed)
		})
	}
}

// RequestLocale returns the locale selected for the current request.
func RequestLocale(r *http.Request) string {
	if r == nil {
		return ""
	}
	if locale, ok := r.Context().Value(localeContextKey).(string); ok {
		return locale
	}
	return ""
}

// RequestLocalePrefixed reports whether the request path contained a locale
// prefix that was stripped before routing.
func RequestLocalePrefixed(r *http.Request) bool {
	if r == nil {
		return false
	}
	prefixed, _ := r.Context().Value(localePrefixedContextKey).(bool)
	return prefixed
}

// LocalePath returns a path for locale-aware links.
func LocalePath(locale, target string, config I18nConfig) string {
	cfg := normalizeI18nConfig(config)
	locale = normalizeLocale(locale)
	if _, ok := cfg.localeSet[locale]; !ok {
		locale = cfg.defaultLocale
	}
	target = "/" + strings.TrimLeft(strings.TrimSpace(target), "/")
	if target == "/" {
		target = ""
	}
	if locale == cfg.defaultLocale && !cfg.prefixDefault {
		if target == "" {
			return "/"
		}
		return target
	}
	return "/" + locale + target
}

func resolveRequestLocale(r *http.Request, cfg normalizedI18nConfig) (string, string, bool) {
	requestPath := "/"
	if r != nil && r.URL != nil && strings.TrimSpace(r.URL.Path) != "" {
		requestPath = r.URL.Path
	}
	if locale, rest, ok := splitLocalePrefix(requestPath, cfg.localeSet); ok {
		return locale, rest, true
	}
	if locale := localeFromConfiguredHeader(r, cfg); locale != "" {
		return locale, requestPath, false
	}
	if locale := localeFromCookie(r, cfg); locale != "" {
		return locale, requestPath, false
	}
	if locale := localeFromAcceptLanguage(r, cfg); locale != "" {
		return locale, requestPath, false
	}
	return cfg.defaultLocale, requestPath, false
}

func splitLocalePrefix(requestPath string, locales map[string]struct{}) (string, string, bool) {
	if !strings.HasPrefix(requestPath, "/") {
		requestPath = "/" + requestPath
	}
	trimmed := strings.TrimPrefix(requestPath, "/")
	if trimmed == "" {
		return "", requestPath, false
	}
	segment, rest, _ := strings.Cut(trimmed, "/")
	locale := normalizeLocale(segment)
	if _, ok := locales[locale]; !ok {
		return "", requestPath, false
	}
	if rest == "" {
		return locale, "/", true
	}
	return locale, "/" + rest, true
}

func localeFromConfiguredHeader(r *http.Request, cfg normalizedI18nConfig) string {
	if r == nil || cfg.headerName == "" {
		return ""
	}
	return firstSupportedLocale(r.Header.Get(cfg.headerName), cfg.localeSet)
}

func localeFromCookie(r *http.Request, cfg normalizedI18nConfig) string {
	if r == nil || cfg.cookieName == "" {
		return ""
	}
	cookie, err := r.Cookie(cfg.cookieName)
	if err != nil {
		return ""
	}
	return firstSupportedLocale(cookie.Value, cfg.localeSet)
}

func localeFromAcceptLanguage(r *http.Request, cfg normalizedI18nConfig) string {
	if r == nil {
		return ""
	}
	for _, part := range strings.Split(r.Header.Get("Accept-Language"), ",") {
		value := strings.TrimSpace(part)
		if value == "" {
			continue
		}
		tag, params, _ := strings.Cut(value, ";")
		if acceptLanguageQZero(params) {
			continue
		}
		if locale := firstSupportedLocale(tag, cfg.localeSet); locale != "" {
			return locale
		}
	}
	return ""
}

func acceptLanguageQZero(params string) bool {
	for _, param := range strings.Split(params, ";") {
		key, value, ok := strings.Cut(strings.TrimSpace(param), "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(key), "q") {
			continue
		}
		quality, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		return err == nil && quality <= 0
	}
	return false
}

func firstSupportedLocale(value string, locales map[string]struct{}) string {
	locale := normalizeLocale(value)
	if locale == "" {
		return ""
	}
	if _, ok := locales[locale]; ok {
		return locale
	}
	if base, _, ok := strings.Cut(locale, "-"); ok {
		if _, supported := locales[base]; supported {
			return base
		}
	}
	return ""
}

func normalizeI18nConfig(config I18nConfig) normalizedI18nConfig {
	locales := make([]string, 0, len(config.Locales))
	seen := map[string]struct{}{}
	for _, locale := range config.Locales {
		locale = normalizeLocale(locale)
		if locale == "" {
			continue
		}
		if _, ok := seen[locale]; ok {
			continue
		}
		seen[locale] = struct{}{}
		locales = append(locales, locale)
	}
	defaultLocale := normalizeLocale(config.DefaultLocale)
	if defaultLocale == "" {
		if len(locales) == 0 {
			defaultLocale = "en"
		} else {
			defaultLocale = locales[0]
		}
	}
	if _, ok := seen[defaultLocale]; !ok {
		seen[defaultLocale] = struct{}{}
		locales = append([]string{defaultLocale}, locales...)
	}
	return normalizedI18nConfig{
		locales:       locales,
		localeSet:     seen,
		defaultLocale: defaultLocale,
		prefixDefault: config.PrefixDefault,
		cookieName:    strings.TrimSpace(config.CookieName),
		headerName:    strings.TrimSpace(config.HeaderName),
	}
}

func normalizeLocale(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "_", "-"))
	if value == "" {
		return ""
	}
	return strings.ToLower(value)
}

func appendVary(header http.Header, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	current := header.Values("Vary")
	for _, existing := range current {
		for _, part := range strings.Split(existing, ",") {
			if strings.EqualFold(strings.TrimSpace(part), value) {
				return
			}
		}
	}
	header.Add("Vary", value)
}
