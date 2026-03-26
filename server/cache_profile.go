package server

import "time"

// CacheProfile applies opinionated page/data cache semantics on top of the
// lower-level CachePolicy primitives.
type CacheProfile struct {
	Policy       CachePolicy
	Tags         []string
	Keys         []string
	ETag         string
	LastModified time.Time
}

type CacheProfileTarget interface {
	Cache(CachePolicy)
	CacheTag(...string)
	CacheKey(...string)
	SetETag(string)
	SetLastModified(time.Time)
}

// DynamicPage disables storage for fully dynamic responses.
func DynamicPage() CacheProfile {
	return CacheProfile{
		Policy: NoStoreCache(),
	}
}

// StaticPage marks a response as immutable and publicly cacheable.
func StaticPage(tags ...string) CacheProfile {
	return CacheProfile{
		Policy: CachePolicy{
			Public:    true,
			MaxAge:    365 * 24 * time.Hour,
			Immutable: true,
		},
		Tags: append([]string(nil), tags...),
	}
}

// RevalidatePage marks a page as publicly cacheable with optional stale-while-
// revalidate behavior.
func RevalidatePage(maxAge, staleWhileRevalidate time.Duration, tags ...string) CacheProfile {
	return CacheProfile{
		Policy: CachePolicy{
			Public:               true,
			MaxAge:               maxAge,
			StaleWhileRevalidate: staleWhileRevalidate,
		},
		Tags: append([]string(nil), tags...),
	}
}

// PublicData marks a shared data response as publicly cacheable.
func PublicData(maxAge time.Duration, tags ...string) CacheProfile {
	return CacheProfile{
		Policy: PublicCache(maxAge),
		Tags:   append([]string(nil), tags...),
	}
}

// PrivateData marks a user-scoped data response as privately cacheable.
func PrivateData(maxAge time.Duration, tags ...string) CacheProfile {
	return CacheProfile{
		Policy: PrivateCache(maxAge),
		Tags:   append([]string(nil), tags...),
	}
}

// ApplyCacheProfile applies a cache profile to any compatible request context.
func ApplyCacheProfile(target CacheProfileTarget, profile CacheProfile) {
	if target == nil {
		return
	}
	target.Cache(profile.Policy)
	if len(profile.Tags) > 0 {
		target.CacheTag(profile.Tags...)
	}
	if len(profile.Keys) > 0 {
		target.CacheKey(profile.Keys...)
	}
	if profile.ETag != "" {
		target.SetETag(profile.ETag)
	}
	if !profile.LastModified.IsZero() {
		target.SetLastModified(profile.LastModified)
	}
}
