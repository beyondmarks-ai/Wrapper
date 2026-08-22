package remote

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIdentityStoreRoundTripAndReplace(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("DPAPI is Windows-only")
	}
	path := filepath.Join(t.TempDir(), "identity.bin")
	first, err := NewIdentity()
	require.NoError(t, err)
	second, err := NewIdentity()
	require.NoError(t, err)
	require.NoError(t, SaveIdentity(path, first))
	require.NoError(t, SaveIdentity(path, second))
	loaded, err := LoadIdentity(path)
	require.NoError(t, err)
	require.Equal(t, second, loaded)
}
