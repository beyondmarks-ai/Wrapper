//go:build !windows

package cloudauth

import "fmt"

func openBrowser(string) error {
	return fmt.Errorf("browser-based Wrapper Cloud login is currently supported only on Windows")
}
