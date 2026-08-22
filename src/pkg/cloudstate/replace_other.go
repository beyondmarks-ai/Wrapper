//go:build !windows

package cloudstate

import "os"

func commitFile(source, destination string) error {
	return os.Rename(source, destination)
}
