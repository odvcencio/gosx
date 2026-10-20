package telemetryerr

import (
	"errors"
	"testing"
)

func TestConfigErrorClassification(t *testing.T) {
	err := &ConfigError{Field: "app", Code: "required"}
	if !errors.Is(err, ErrInvalidOptions) || errors.Is(err, ErrAfterBuild) {
		t.Fatal("configuration error identity")
	}
	var config *ConfigError
	if !errors.As(err, &config) || config.Field != "app" || config.Code != "required" {
		t.Fatal("configuration error fields")
	}
}
