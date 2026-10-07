package server

import (
	"log"

	"m31labs.dev/gosx/internal/observationcatalog"
	"m31labs.dev/gosx/internal/telemetryerr"
)

// ObservationPattern is one declared kind and registered path, with sorted
// methods. Patterns retain their owner's relative scope; method prefixes are
// represented in Methods. "*" means any method; GET also admits HEAD.
type ObservationPattern = observationcatalog.Pattern

// ObservationCatalogObserver receives a private catalog copy once, during the
// first Build and before serving. Implementations must copy retained slices.
type ObservationCatalogObserver interface {
	ObserveCatalog([]ObservationPattern)
}

// ObservationCatalogProvider exposes registered patterns without matching or
// executing requests. Zero limit defaults to 512 registered routes. Derived
// page-error rows add no route slots; output contains at most twice the limit.
// Truncation of routes or method sets reports overflow.
type ObservationCatalogProvider interface {
	ObservationPatterns(limit int) ([]ObservationPattern, bool)
}

// HeadConfigurable lets a mounted document owner receive App head decorators.
// It must preserve existing page head and navigation contributions and invoke
// each decorator once against the existing PageState before document rendering.
type HeadConfigurable interface {
	SetHeadDecorators([]HeadDecorator)
}

// UseObservationCatalogObserver attaches an observer before the first Build.
func (a *App) UseObservationCatalogObserver(o ObservationCatalogObserver) error {
	if a == nil || o == nil {
		return &telemetryerr.ConfigError{Field: "catalog_observer", Code: "required"}
	}
	a.shutdown.mu.Lock()
	defer a.shutdown.mu.Unlock()
	if !a.ConfigurationOpen() {
		return telemetryerr.ErrAfterBuild
	}
	a.catalogObservers = append(a.catalogObservers, o)
	return nil
}

// ObservationPatterns returns a private, sorted selection from registered App
// and mounted provider patterns. Configure routes before Build. Identical
// owner-relative rows from different mounts intentionally merge their methods.
// Each row retains at most 64 methods; overflow also reports method truncation.
func (a *App) ObservationPatterns(limit int) ([]ObservationPattern, bool) {
	c := observationcatalog.New(limit)
	if a == nil {
		return c.Result()
	}
	for _, route := range a.pageRoutes {
		c.Register("page", route.pattern)
	}
	for _, route := range a.apiRoutes {
		c.Register("api", route.pattern)
	}
	for _, route := range a.redirects {
		c.Register("redirect", route.pattern)
	}
	for _, route := range a.rewrites {
		c.Register("rewrite", route.pattern)
	}
	for _, route := range a.mounts {
		c.Register("mount", route.pattern)
		if provider, ok := route.handler.(ObservationCatalogProvider); ok {
			rows, overflow := provider.ObservationPatterns(limit)
			if overflow {
				c.Overflow()
			}
			for _, row := range rows {
				c.Add(row)
			}
		}
	}
	return c.Result()
}

func (a *App) notifyObservationCatalog() {
	a.catalogOnce.Do(func() {
		if len(a.catalogObservers) == 0 {
			return
		}
		rows, _ := a.ObservationPatterns(0)
		for _, observer := range a.catalogObservers {
			func() {
				defer func() {
					if recover() != nil {
						log.Print("[gosx] observation catalog callback panic")
					}
				}()
				observer.ObserveCatalog(observationcatalog.Clone(rows))
			}()
		}
	})
}
