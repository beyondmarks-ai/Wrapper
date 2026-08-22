//go:build !windows

package internal

import "os"

func renamePath(oldPath, newPath string) error {
	return os.Rename(oldPath, newPath)
}
