package localapp

import (
	"encoding/base64"
	"reflect"
	"strings"
	"testing"
)

func TestEnvironmentIsDisposableAndLocal(t *testing.T) {
	base := []string{"SESSION_SECRET=real-deployment-secret", "PORT=8080", "SESSION_SECRET=change-me-in-production", "PUBLIC_URL=https://example.test"}
	original := append([]string(nil), base...)
	seen := map[string]bool{}
	for range 2 {
		env, err := Environment(base, "9000")
		if err != nil {
			t.Fatal(err)
		}
		counts := map[string]int{}
		values := map[string]string{}
		for _, entry := range env {
			key, value, _ := strings.Cut(entry, "=")
			counts[key]++
			values[key] = value
		}
		decoded, err := base64.RawURLEncoding.DecodeString(values["SESSION_SECRET"])
		if err != nil || len(decoded) != 32 || counts["SESSION_SECRET"] != 1 || seen[values["SESSION_SECRET"]] {
			t.Fatal("local app secret must be fresh, contain 32 random bytes, and replace all inherited values")
		}
		seen[values["SESSION_SECRET"]] = true
		if counts["PORT"] != 1 || values["PORT"] != "127.0.0.1:9000" || values["PUBLIC_URL"] != "https://example.test" {
			t.Fatal("local binding or canonical URL changed")
		}
	}
	if !reflect.DeepEqual(base, original) {
		t.Fatal("parent environment was changed")
	}
}

func TestEnvironmentSetsLocalOriginWhenUnconfigured(t *testing.T) {
	for _, base := range [][]string{nil, {"PUBLIC_URL="}} {
		env, err := Environment(base, "9000")
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, entry := range env {
			if strings.HasPrefix(entry, "PUBLIC_URL=") {
				count++
				if entry != "PUBLIC_URL=http://127.0.0.1:9000" {
					t.Fatal("invalid local origin")
				}
			}
		}
		if count != 1 {
			t.Fatal("local origin must be explicit and unique")
		}
	}
}
