package securestore

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWriteIsReplaceableAndProtected(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("DPAPI is Windows-only")
	}
	path := filepath.Join(t.TempDir(), "tokens.bin")
	require.NoError(t, Write(path, []byte("first")))
	require.NoError(t, Write(path, []byte("second")))
	plain, err := Read(path)
	require.NoError(t, err)
	require.Equal(t, []byte("second"), plain)
}
