//go:build windows

package config

import "os"

func mkdirSecure(path string) error {
	return os.MkdirAll(path, 0700)
}

func writeFileSecure(path string, data []byte) error {
	return os.WriteFile(path, data, 0600)
}
