package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
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
	wantNoTempFiles(t)
}

func wantNoTempFiles(t *testing.T) {
	t.Helper()
	left, err := filepath.Glob(filepath.Join(Dir(), "config.toml*tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(left) > 0 {
		t.Errorf("temporary files left behind: %v", left)
	}
}

// A file someone else left or planted at the old fixed temporary name must
// not decide the saved key's permissions or where the key is written.
func TestSaveConfigIgnoresFilesAtTheOldTempName(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes and symlinks differ on Windows")
	}
	isolate(t)
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		t.Fatal(err)
	}
	stale := Path() + ".tmp"
	if err := os.WriteFile(stale, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(stale, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := SaveConfig("stale-key"); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(Path()); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("config = %v, %v; want mode 0600 despite a 0644 %s", info, err, filepath.Base(stale))
	}

	victim := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(victim, []byte("untouched\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(stale); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, stale); err != nil {
		t.Fatal(err)
	}

	if err := SaveConfig("symlink-key"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(victim); string(b) != "untouched\n" {
		t.Errorf("symlink target = %q, want it untouched", b)
	}
	if info, err := os.Lstat(Path()); err != nil || !info.Mode().IsRegular() {
		t.Errorf("config = %v, %v; want a regular file", info, err)
	}
	if got, err := ResolveAPIKey(""); err != nil || got != "symlink-key" {
		t.Errorf("ResolveAPIKey = %q, %v; want symlink-key", got, err)
	}
}

func TestSaveConfigConcurrently(t *testing.T) {
	isolate(t)
	keys := []string{"key-0", "key-1", "key-2", "key-3", "key-4", "key-5", "key-6", "key-7"}
	errs := make(chan error, len(keys))
	var wg sync.WaitGroup
	for _, key := range keys {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- SaveConfig(key)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("SaveConfig: %v", err)
		}
	}
	got, err := ResolveAPIKey("")
	if err != nil || !slices.Contains(keys, got) {
		t.Errorf("ResolveAPIKey = %q, %v; want one of the saved keys", got, err)
	}
	wantNoTempFiles(t)
}

// Without HOME there is no config directory, and a relative one would put the
// key in, or take it from, whatever directory the command runs in.
func TestNoConfigDirWithoutHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows finds the config directory through other variables")
	}
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	cwd := t.TempDir()
	t.Chdir(cwd)
	planted := filepath.Join(cwd, ".config", "serpapi", "config.toml")
	if err := os.MkdirAll(filepath.Dir(planted), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planted, []byte(`api_key = "planted-key"`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if key, err := ResolveAPIKey(""); err != ErrNoAPIKey {
		t.Errorf("ResolveAPIKey = %q, %v; want ErrNoAPIKey, not the key in the current directory", key, err)
	}
	err := SaveConfig("new-key")
	var usageErr *clierrors.UsageError
	if !errors.As(err, &usageErr) || !strings.Contains(usageErr.Message, "HOME") {
		t.Errorf("SaveConfig = %v; want a UsageError naming HOME", err)
	}
	if b, _ := os.ReadFile(planted); !strings.Contains(string(b), "planted-key") {
		t.Errorf("the planted file was overwritten: %q", b)
	}
}
