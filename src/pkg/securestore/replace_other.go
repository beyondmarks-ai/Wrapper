//go:build !windows

package securestore

import "os"

func commitFile(source, destination string) error { return os.Rename(source, destination) }
