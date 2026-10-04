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
// and supplies a separate loopback listen address for GoSX servers. PORT stays
// numeric for ordinary Go listeners. Other configuration, including PUBLIC_URL
// and its absence (application defaults), is preserved.
func Environment(base []string, port string) ([]string, error) {
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return nil, fmt.Errorf("generate local app session secret: %w", err)
	}
	env := make([]string, 0, len(base)+3)
	for _, entry := range base {
		key, _, _ := strings.Cut(entry, "=")
		if key != "SESSION_SECRET" && key != "PORT" && key != "GOSX_LISTEN_ADDR" {
			env = append(env, entry)
		}
	}
	return append(env, "SESSION_SECRET="+base64.RawURLEncoding.EncodeToString(secret[:]),
		"PORT="+port, "GOSX_LISTEN_ADDR=127.0.0.1:"+port), nil
}
