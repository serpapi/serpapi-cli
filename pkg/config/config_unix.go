//go:build !windows

package config

import (
	"fmt"
	"os"
)

func mkdirSecure(path string) error {
	return os.MkdirAll(path, 0700)
}

func writeFileSecure(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	n, err := f.Write(data)
	if err != nil {
		f.Close()
		return err
	}
	if n != len(data) {
		f.Close()
		return fmt.Errorf("incomplete write: wrote %d of %d bytes", n, len(data))
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
