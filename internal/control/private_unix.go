//go:build !windows

package control

import (
	"fmt"
	"os"
)

func secureDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("control API directory is not a directory")
	}
	return os.Chmod(path, 0o700)
}

func privateFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
}
