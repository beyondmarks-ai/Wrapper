package securestore

import (
	"fmt"
	"os"
	"path/filepath"
)

func Write(path string, plain []byte) error {
	protected, err := protect(plain)
	if err != nil {
		return err
	}
	directory := filepath.Dir(path)
	if err = os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create secure storage directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".wrapper-secret-*.tmp")
	if err != nil {
		return fmt.Errorf("create protected data: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath) //nolint:errcheck
	if err = temporary.Chmod(0o600); err == nil {
		_, err = temporary.Write(protected)
	}
	if err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write protected data: %w", err)
	}
	if err = commitFile(temporaryPath, path); err != nil {
		return fmt.Errorf("commit protected data: %w", err)
	}
	return nil
}

func Read(path string) ([]byte, error) {
	protected, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read protected data: %w", err)
	}
	return unprotect(protected)
}
