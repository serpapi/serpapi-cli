package config

import (
	"bytes"
	"os"
	"path/filepath"

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

// LoadAPIKey reads the API key from the config file, if present.
func LoadAPIKey() (string, bool) {
	data, err := os.ReadFile(Path())
	if err != nil {
		return "", false
	}
	var cfg configFile
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return "", false
	}
	if cfg.APIKey == "" {
		return "", false
	}
	return cfg.APIKey, true
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

	// os.Remove before os.Rename is needed for Windows, which cannot
	// atomically rename over an existing file. On Unix, os.Rename is atomic.
	if err := os.Remove(finalPath); err != nil && !os.IsNotExist(err) {
		os.Remove(tmpPath)
		return &clierrors.UsageError{Message: "Failed to save config: " + err.Error()}
	}
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

// ResolveAPIKey resolves the API key from the given flag value (which may
// already incorporate the env var), falling back to the config file.
func ResolveAPIKey(fromFlag string) (string, error) {
	if fromFlag != "" {
		return fromFlag, nil
	}
	if key, ok := LoadAPIKey(); ok {
		return key, nil
	}
	return "", &clierrors.UsageError{
		Message: "No API key found. Run 'serpapi login' or set SERPAPI_KEY.",
	}
}
