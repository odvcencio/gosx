package docs

import (
	"github.com/odvcencio/gosx/auth"
	"github.com/odvcencio/gosx/route"
	"github.com/odvcencio/gosx/server"
)

var docsAuth *auth.Manager
var docsMagicLinks *auth.MagicLinks
var docsWebAuthn *auth.WebAuthn
var docsOAuth *auth.OAuth
var docsOAuthProviders []map[string]string
var docsPublicAssetURL func(string) string

func BindAuth(manager *auth.Manager) {
	docsAuth = manager
}

func AuthManager() *auth.Manager {
	return docsAuth
}

func BindMagicLinks(manager *auth.MagicLinks) {
	docsMagicLinks = manager
}

func MagicLinks() *auth.MagicLinks {
	return docsMagicLinks
}

func BindWebAuthn(manager *auth.WebAuthn) {
	docsWebAuthn = manager
}

func WebAuthnManager() *auth.WebAuthn {
	return docsWebAuthn
}

func BindOAuth(manager *auth.OAuth, providers []map[string]string) {
	docsOAuth = manager
	docsOAuthProviders = append([]map[string]string(nil), providers...)
}

func OAuthManager() *auth.OAuth {
	return docsOAuth
}

func OAuthProviders() []map[string]string {
	return append([]map[string]string(nil), docsOAuthProviders...)
}

func BindPublicAssetURL(fn func(string) string) {
	docsPublicAssetURL = fn
}

func PublicAssetURL(path string) string {
	if docsPublicAssetURL != nil {
		return docsPublicAssetURL(path)
	}
	return server.AssetURL(path)
}

func RegisterDocsPage(title, description string, opts route.FileModuleOptions) {
	metadata := opts.Metadata
	bindings := opts.Bindings
	opts.Metadata = func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
		meta := server.Metadata{
			Title:       server.Title{Absolute: title + " | GoSX"},
			Description: description,
		}
		if metadata == nil {
			return meta, nil
		}
		extra, err := metadata(ctx, page, data)
		if err != nil {
			return server.Metadata{}, err
		}
		return mergeDocsMetadata(meta, extra), nil
	}
	opts.Bindings = func(ctx *route.RouteContext, page route.FilePage, data any) route.FileTemplateBindings {
		bound := defaultDocsBindings()
		if bindings == nil {
			return bound
		}
		return mergeDocsBindings(bound, bindings(ctx, page, data))
	}
	route.MustRegisterFileModuleCaller(1, opts)
}

func RegisterStaticDocsPage(title, description string, opts route.FileModuleOptions) {
	metaMetadata := opts.Metadata
	bindings := opts.Bindings
	opts.Metadata = func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
		meta := server.Metadata{
			Title:       server.Title{Absolute: title + " | GoSX"},
			Description: description,
		}
		if metaMetadata == nil {
			return meta, nil
		}
		extra, err := metaMetadata(ctx, page, data)
		if err != nil {
			return server.Metadata{}, err
		}
		return mergeDocsMetadata(meta, extra), nil
	}
	opts.Bindings = func(ctx *route.RouteContext, page route.FilePage, data any) route.FileTemplateBindings {
		bound := defaultDocsBindings()
		if bindings == nil {
			return bound
		}
		return mergeDocsBindings(bound, bindings(ctx, page, data))
	}
	route.MustRegisterFileModuleCaller(1, opts)
}

func mergeDocsMetadata(base, extra server.Metadata) server.Metadata {
	if !isZeroDocsTitle(extra.Title) {
		base.Title = extra.Title
	}
	if extra.Description != "" {
		base.Description = extra.Description
	}
	if extra.MetadataBase != "" {
		base.MetadataBase = extra.MetadataBase
	}
	if extra.Alternates != nil {
		base.Alternates = extra.Alternates
	}
	if extra.Robots != nil {
		base.Robots = extra.Robots
	}
	if extra.Icons != nil {
		base.Icons = extra.Icons
	}
	if extra.Manifest != "" {
		base.Manifest = extra.Manifest
	}
	if extra.Verification != nil {
		base.Verification = extra.Verification
	}
	if len(extra.ThemeColor) > 0 {
		base.ThemeColor = append([]server.ThemeColor(nil), extra.ThemeColor...)
	}
	if extra.OpenGraph != nil {
		base.OpenGraph = extra.OpenGraph
	}
	if extra.Twitter != nil {
		base.Twitter = extra.Twitter
	}
	if len(extra.JSONLD) > 0 {
		base.JSONLD = append([]any(nil), extra.JSONLD...)
	}
	if len(extra.Other) > 0 {
		base.Other = append(base.Other, extra.Other...)
	}
	if len(extra.Links) > 0 {
		base.Links = append(base.Links, extra.Links...)
	}
	return base
}

func isZeroDocsTitle(title server.Title) bool {
	return title.Absolute == "" && title.Default == "" && title.Template == ""
}

func defaultDocsBindings() route.FileTemplateBindings {
	return route.FileTemplateBindings{
		Funcs: map[string]any{
			"CodeBlock":     CodeBlock,
			"StatCard":      StatCard,
			"CapabilityTag": CapabilityTag,
			"Tooltip":       Tooltip,
		},
		Components: map[string]any{
			"CodeBlock":     CodeBlock,
			"StatCard":      StatCard,
			"CapabilityTag": CapabilityTag,
			"Tooltip":       Tooltip,
		},
	}
}

func mergeDocsBindings(base, extra route.FileTemplateBindings) route.FileTemplateBindings {
	out := route.FileTemplateBindings{
		Values:     make(map[string]any, len(base.Values)+len(extra.Values)),
		Funcs:      make(map[string]any, len(base.Funcs)+len(extra.Funcs)),
		Components: make(map[string]any, len(base.Components)+len(extra.Components)),
	}
	for key, value := range base.Values {
		out.Values[key] = value
	}
	for key, value := range base.Funcs {
		out.Funcs[key] = value
	}
	for key, value := range base.Components {
		out.Components[key] = value
	}
	for key, value := range extra.Values {
		out.Values[key] = value
	}
	for key, value := range extra.Funcs {
		out.Funcs[key] = value
	}
	for key, value := range extra.Components {
		out.Components[key] = value
	}
	return out
}
