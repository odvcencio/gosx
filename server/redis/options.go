package redis

import (
	"crypto/sha1"
	"encoding/hex"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultPrefix  = "gosx"
	defaultLockTTL = 2 * time.Minute
)

// Options configures Redis-backed GoSX server adapters.
type Options struct {
	// Prefix namespaces all keys written by the adapter. Set this explicitly
	// per environment or deployment when multiple apps share a Redis cluster.
	Prefix string

	// LockTTL controls how long ISR refresh leases remain valid before another
	// instance may retry the same refresh.
	LockTTL time.Duration

	// ArtifactTTL optionally expires stored ISR artifacts.
	ArtifactTTL time.Duration

	// StateTTL optionally expires stored ISR freshness state.
	StateTTL time.Duration
}

func (o Options) normalized() Options {
	if strings.TrimSpace(o.Prefix) == "" {
		o.Prefix = defaultPrefix
	} else {
		o.Prefix = strings.Trim(strings.TrimSpace(o.Prefix), ":")
	}
	if o.LockTTL <= 0 {
		o.LockTTL = defaultLockTTL
	}
	if o.ArtifactTTL < 0 {
		o.ArtifactTTL = 0
	}
	if o.StateTTL < 0 {
		o.StateTTL = 0
	}
	return o
}

func (o Options) key(parts ...string) string {
	normalized := o.normalized()
	return normalized.Prefix + ":" + strings.Join(parts, ":")
}

func (o Options) revalidationSeqKey() string {
	return o.key("revalidation", "seq")
}

func (o Options) revalidationPathKey(target string) string {
	return o.key("revalidation", "path", cleanCachePath(target))
}

func (o Options) revalidationTagKey(tag string) string {
	return o.key("revalidation", "tag", strings.TrimSpace(tag))
}

func (o Options) isrArtifactBodyKey(staticDir, pagePath, file string) string {
	return o.key("isr", "artifact", "body", digestKey(filepath.Clean(strings.TrimSpace(staticDir)), cleanCachePath(pagePath), cleanArtifactFile(file)))
}

func (o Options) isrArtifactMetaKey(staticDir, pagePath, file string) string {
	return o.key("isr", "artifact", "meta", digestKey(filepath.Clean(strings.TrimSpace(staticDir)), cleanCachePath(pagePath), cleanArtifactFile(file)))
}

func (o Options) isrStateKey(bundleRoot, pagePath string) string {
	return o.key("isr", "state", digestKey(filepath.Clean(strings.TrimSpace(bundleRoot)), cleanCachePath(pagePath)))
}

func (o Options) isrLockKey(bundleRoot, pagePath string) string {
	return o.key("isr", "lock", digestKey(filepath.Clean(strings.TrimSpace(bundleRoot)), cleanCachePath(pagePath)))
}

func digestKey(parts ...string) string {
	sum := sha1.Sum([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

func cleanCachePath(target string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return "/"
	}
	cleaned := path.Clean(target)
	if !strings.HasPrefix(cleaned, "/") {
		cleaned = "/" + cleaned
	}
	return cleaned
}

func cleanArtifactFile(file string) string {
	file = strings.TrimSpace(file)
	if file == "" {
		return ""
	}
	cleaned := path.Clean(strings.ReplaceAll(file, "\\", "/"))
	return strings.TrimPrefix(cleaned, "/")
}

func pathCandidates(requestPath string) []string {
	requestPath = cleanCachePath(requestPath)
	if requestPath == "/" {
		return []string{"/"}
	}
	trimmed := strings.TrimPrefix(requestPath, "/")
	segments := strings.Split(trimmed, "/")
	candidates := make([]string, 0, len(segments)+1)
	candidates = append(candidates, "/")
	current := ""
	for _, segment := range segments {
		if segment == "" {
			continue
		}
		current += "/" + segment
		candidates = append(candidates, current)
	}
	return candidates
}
