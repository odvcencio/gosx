// Package localapp configures disposable app processes used by local build
// and test harnesses. Its secrets must never be used for a deployed server.
package localapp

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
)

// Environment replaces inherited session secrets with a fresh random secret
// and binds the app to loopback. Other configuration, including PUBLIC_URL
// for canonical build metadata, is preserved when configured.
func Environment(base []string, port string) ([]string, error) {
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return nil, fmt.Errorf("generate local app session secret: %w", err)
	}
	env := make([]string, 0, len(base)+3)
	publicURL := ""
	for _, entry := range base {
		key, value, _ := strings.Cut(entry, "=")
		if key == "PUBLIC_URL" {
			publicURL = value
			continue
		}
		if key != "SESSION_SECRET" && key != "PORT" {
			env = append(env, entry)
		}
	}
	if publicURL == "" {
		publicURL = "http://127.0.0.1:" + port
	}
	return append(env, "SESSION_SECRET="+base64.RawURLEncoding.EncodeToString(secret[:]),
		"PORT=127.0.0.1:"+port, "PUBLIC_URL="+publicURL), nil
}
