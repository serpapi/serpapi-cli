package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	clierrors "github.com/serpapi/serpapi-cli/pkg/errors"
)

// isolate points the user config directory at a temporary directory so tests
// never read or overwrite the real config.
func isolate(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	t.Setenv("AppData", dir)
}

func TestConfigPathContainsSerpapi(t *testing.T) {
	isolate(t)
	path := Path()
	if !strings.Contains(path, "serpapi") {
		t.Errorf("config path %q does not contain 'serpapi'", path)
	}
}

func TestResolveAPIKeyFromFlag(t *testing.T) {
	isolate(t)
	key, err := ResolveAPIKey("test-key-123")
	if err != nil {
		t.Fatal(err)
	}
	if key != "test-key-123" {
		t.Errorf("expected test-key-123, got %s", key)
	}
}

func TestResolveAPIKeyMissingReturnsError(t *testing.T) {
	isolate(t)
	_, err := ResolveAPIKey("")
	var usageErr *clierrors.UsageError
	if !errors.As(err, &usageErr) || !strings.Contains(usageErr.Message, "No API key") {
		t.Errorf("expected UsageError about the missing key, got %v", err)
	}
}

func TestSaveConfigRoundTripAndOverwrite(t *testing.T) {
	isolate(t)
	for _, key := range []string{"first-key", "second-key"} {
		if err := SaveConfig(key); err != nil {
			t.Fatal(err)
		}
		got, err := ResolveAPIKey("")
		if err != nil || got != key {
			t.Fatalf("ResolveAPIKey = %q, %v; want %q", got, err, key)
		}
	}

	info, err := os.Stat(Path())
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("config mode = %v, want 0600", info.Mode().Perm())
	}
	if _, err := os.Stat(Path() + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temporary file left behind: %v", err)
	}
}
