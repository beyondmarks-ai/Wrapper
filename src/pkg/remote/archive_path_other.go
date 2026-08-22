//go:build !windows

package remote

import "strings"

func safePlatformArchivePath(path string) bool { return !strings.Contains(path, ":") }
