//go:build windows

package remote

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSafeArchiveTargetRejectsWindowsSpecialNames(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{
		"file.txt:secret",
		"CON",
		"con.txt",
		"folder/NUL.log",
		"trailing.",
		"trailing ",
	} {
		_, err := safeArchiveTarget(root, name)
		require.ErrorIs(t, err, ErrUnsafePath, name)
	}
}
