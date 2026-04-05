package redis

import (
	"crypto/sha1"
	"encoding/hex"
	"strings"
	"time"
)

const (
	defaultPrefix            = "gosx"
	defaultExpiredTokenGrace = 24 * time.Hour
)

// Options configures Redis-backed auth stores.
type Options struct {
	// Prefix namespaces all Redis keys written by the auth adapters.
	Prefix string

	// ExpiredTokenGrace keeps expired magic-link records around long enough for
	// Consume to distinguish "expired" from "invalid" before Redis evicts them.
	ExpiredTokenGrace time.Duration
}

func (o Options) normalized() Options {
	if strings.TrimSpace(o.Prefix) == "" {
		o.Prefix = defaultPrefix
	} else {
		o.Prefix = strings.Trim(strings.TrimSpace(o.Prefix), ":")
	}
	if o.ExpiredTokenGrace <= 0 {
		o.ExpiredTokenGrace = defaultExpiredTokenGrace
	}
	return o
}

func (o Options) key(parts ...string) string {
	normalized := o.normalized()
	return normalized.Prefix + ":" + strings.Join(parts, ":")
}

func (o Options) magicLinkTokenKey(token string) string {
	return o.key("auth", "magic_link", "token", digestKey(strings.TrimSpace(token)))
}

func (o Options) webAuthnCredentialKey(id string) string {
	return o.key("auth", "webauthn", "credential", digestKey(normalizeCredentialID(id)))
}

func (o Options) webAuthnUserKey(userID string) string {
	return o.key("auth", "webauthn", "user", digestKey(strings.TrimSpace(userID)), "credentials")
}

func digestKey(parts ...string) string {
	sum := sha1.Sum([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

func normalizeCredentialID(id string) string {
	return strings.TrimSpace(id)
}
