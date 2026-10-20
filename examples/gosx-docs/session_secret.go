package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"strings"
)

func sessionSecret() (string, error) {
	secret := strings.TrimSpace(os.Getenv("SESSION_SECRET"))
	switch secret {
	case "", "change-me-in-production", "gosx-app-session-secret", "gosx-docs-session-secret":
		mode := strings.TrimSpace(os.Getenv("GOSX_ENV"))
		dev := strings.EqualFold(mode, "development") || (mode == "" && os.Getenv("GOSX_DEV") == "1")
		if !dev {
			return "", fmt.Errorf("set SESSION_SECRET to a random secret of at least 16 bytes; missing or placeholder secrets are only allowed in development")
		}
		var random [32]byte
		if _, err := rand.Read(random[:]); err != nil {
			return "", fmt.Errorf("generate development session secret: %w", err)
		}
		log.Print("Using a random per-process development session secret; sessions reset on restart. Set SESSION_SECRET for persistent sessions.")
		return base64.RawURLEncoding.EncodeToString(random[:]), nil
	default:
		if len(secret) < 16 {
			return "", fmt.Errorf("SESSION_SECRET must be at least 16 bytes")
		}
		return secret, nil
	}
}
