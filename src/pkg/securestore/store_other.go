//go:build !windows

package securestore

import "fmt"

func protect([]byte) ([]byte, error) {
	return nil, fmt.Errorf("secure cloud credentials are currently supported only on Windows")
}

func unprotect([]byte) ([]byte, error) {
	return nil, fmt.Errorf("secure cloud credentials are currently supported only on Windows")
}
