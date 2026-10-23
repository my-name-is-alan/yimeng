//go:build !windows

package vault

import (
	"fmt"
	"os"
)

func createKeyFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
}

func secureKeyFile(path string) (string, error) {
	st, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if st.Mode().Perm()&0o077 == 0 {
		return "", nil
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return "", err
	}
	return fmt.Sprintf("master key %s was accessible by other users; permissions reset to 0600", path), nil
}
