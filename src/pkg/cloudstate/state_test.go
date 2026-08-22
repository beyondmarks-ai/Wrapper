package cloudstate

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/beyondmarks-ai/Wrapper/src/pkg/remote"
)

func TestStateRoundTripAndSharedRoots(t *testing.T) {
	root := t.TempDir()
	state := State{APIURL: "https://api.example.com", Device: remote.Device{ID: "device-1"}}
	require.NoError(t, AddSharedRoot(&state, root))
	require.NoError(t, AddSharedRoot(&state, root))
	require.Len(t, state.SharedRoots, 1)
	path := filepath.Join(t.TempDir(), "cloud.json")
	require.NoError(t, Save(path, state))
	state.DownloadDir = "D:\\Downloads\\Wrapper"
	// Saving again exercises atomic replacement of an existing file on Windows.
	if filepath.Separator != "\\"[0] {
		state.DownloadDir = "/tmp/wrapper"
	}
	require.NoError(t, Save(path, state))
	loaded, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, state.Device.ID, loaded.Device.ID)
	require.Len(t, loaded.SharedRoots, 1)
	require.NoError(t, RemoveSharedRoot(&loaded, root))
	require.Empty(t, loaded.SharedRoots)
}

func TestSharedRootsAreBounded(t *testing.T) {
	state := State{SharedRoots: make([]string, MaxSharedRoots)}
	for index := range state.SharedRoots {
		state.SharedRoots[index] = filepath.Join("Z:\\missing", fmt.Sprintf("%d", index))
	}
	err := AddSharedRoot(&state, t.TempDir())
	require.ErrorContains(t, err, "at most")
	require.Len(t, state.SharedRoots, MaxSharedRoots)
}
