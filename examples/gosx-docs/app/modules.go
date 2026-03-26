package docs

import (
	"time"

	"github.com/odvcencio/gosx/auth"
	"github.com/odvcencio/gosx/route"
	"github.com/odvcencio/gosx/server"
)

var docsAuth *auth.Manager

func BindAuth(manager *auth.Manager) {
	docsAuth = manager
}

func AuthManager() *auth.Manager {
	return docsAuth
}

func RegisterDocsPage(title, description string, opts route.FileModuleOptions) {
	metadata := opts.Metadata
	opts.Metadata = func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
		meta := server.Metadata{
			Title:       title + " | GoSX Docs",
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
	route.MustRegisterFileModuleCaller(1, opts)
}

func RegisterStaticDocsPage(title, description string, opts route.FileModuleOptions) {
	metadata := opts.Metadata
	opts.Metadata = func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
		ctx.Cache(server.CachePolicy{
			Public:               true,
			MaxAge:               45 * time.Second,
			StaleWhileRevalidate: 5 * time.Minute,
		})
		ctx.CacheTag("docs-pages")
		ctx.CacheKey(page.Source)
		if metadata == nil {
			return server.Metadata{}, nil
		}
		return metadata(ctx, page, data)
	}
	metaMetadata := opts.Metadata
	opts.Metadata = func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
		meta := server.Metadata{
			Title:       title + " | GoSX Docs",
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
	route.MustRegisterFileModuleCaller(1, opts)
}

func mergeDocsMetadata(base, extra server.Metadata) server.Metadata {
	if extra.Title != "" {
		base.Title = extra.Title
	}
	if extra.Description != "" {
		base.Description = extra.Description
	}
	if extra.Canonical != "" {
		base.Canonical = extra.Canonical
	}
	if len(extra.Meta) > 0 {
		base.Meta = append(base.Meta, extra.Meta...)
	}
	if len(extra.Links) > 0 {
		base.Links = append(base.Links, extra.Links...)
	}
	return base
}
