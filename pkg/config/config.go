package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	clierrors "github.com/serpapi/serpapi-cli/pkg/errors"
)

type configFile struct {
	APIKey string `toml:"api_key"`
}

// Dir returns the directory where the CLI stores its config.
func Dir() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "serpapi")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".config", "serpapi")
	}
	return filepath.Join(home, ".config", "serpapi")
}

// Path returns the full path to the config file.
func Path() string {
	return filepath.Join(Dir(), "config.toml")
}

// ErrNoAPIKey is returned by ResolveAPIKey when no source has a key.
var ErrNoAPIKey = &clierrors.UsageError{
	Message: "No API key found. Pass --api-key, set SERPAPI_KEY, or run 'serpapi login'. Get a key at https://serpapi.com/manage-api-key",
}

// LoadAPIKey returns the API key saved in the config file, or "" when there
// is none. A file that can't be read or parsed is an error naming the file
// and line; it never quotes the file, which holds the key.
func LoadAPIKey() (string, error) {
	data, err := os.ReadFile(Path())
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", &clierrors.UsageError{Message: "Cannot read the config file: " + err.Error() +
			"; fix its permissions, or pass --api-key or SERPAPI_KEY"}
	}
	var cfg configFile
	if err := toml.Unmarshal(data, &cfg); err != nil {
		where := ""
		var parseErr toml.ParseError
		if errors.As(err, &parseErr) && parseErr.Position.Line > 0 {
			where = fmt.Sprintf(" (line %d)", parseErr.Position.Line)
		}
		return "", &clierrors.UsageError{Message: "The config file " + Path() + " is not valid TOML" + where +
			"; run 'serpapi login' to rewrite it, or pass --api-key or SERPAPI_KEY"}
	}
	return strings.TrimSpace(cfg.APIKey), nil
}

// SaveConfig writes the API key to the config file atomically.
func SaveConfig(apiKey string) error {
	dir := Dir()
	if err := mkdirSecure(dir); err != nil {
		return &clierrors.UsageError{Message: "Failed to create config dir: " + err.Error()}
	}

	cfg := configFile{APIKey: apiKey}
	content, err := tomlMarshal(cfg)
	if err != nil {
		return &clierrors.UsageError{Message: err.Error()}
	}

	finalPath := Path()
	tmpPath := finalPath + ".tmp"

	if err := writeFileSecure(tmpPath, content); err != nil {
		return &clierrors.UsageError{Message: err.Error()}
	}

	// os.Rename replaces an existing file on Windows too (MoveFileEx with
	// MOVEFILE_REPLACE_EXISTING), so the old key stays until the new one lands.
	if err := os.Rename(tmpPath, finalPath); err != nil {
		os.Remove(tmpPath)
		return &clierrors.UsageError{Message: "Failed to save config: " + err.Error()}
	}

	return nil
}

func tomlMarshal(cfg configFile) ([]byte, error) {
	buf := new(bytes.Buffer)
	enc := toml.NewEncoder(buf)
	if err := enc.Encode(cfg); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ResolveAPIKey returns the given key (from a flag or environment variable)
// or, when it is empty, the key saved in the config file. It returns
// ErrNoAPIKey when there is none, and the config file's error when the
// saved key is needed but the file is broken.
func ResolveAPIKey(fromFlag string) (string, error) {
	if fromFlag != "" {
		return fromFlag, nil
	}
	key, err := LoadAPIKey()
	if err != nil {
		return "", err
	}
	if key == "" {
		return "", ErrNoAPIKey
	}
	return key, nil
}
