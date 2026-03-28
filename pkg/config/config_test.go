package config

import (
	"strings"
	"testing"

	clierrors "github.com/serpapi/serpapi-cli/pkg/errors"
)

func TestConfigPathContainsSerpapi(t *testing.T) {
	path := Path()
	if !strings.Contains(path, "serpapi") {
		t.Errorf("config path %q does not contain 'serpapi'", path)
	}
}

func TestResolveAPIKeyFromFlag(t *testing.T) {
	key, err := ResolveAPIKey("test-key-123")
	if err != nil {
		t.Fatal(err)
	}
	if key != "test-key-123" {
		t.Errorf("expected test-key-123, got %s", key)
	}
}

func TestResolveAPIKeyMissingReturnsError(t *testing.T) {
	// With empty flag and no config file, should return error
	_, err := ResolveAPIKey("")
	if err == nil {
		// It might succeed if there's a real config file; skip in that case
		t.Skip("config file exists, cannot test missing key")
	}
	if _, ok := err.(*clierrors.UsageError); !ok {
		t.Errorf("expected UsageError, got %T", err)
	}
}
