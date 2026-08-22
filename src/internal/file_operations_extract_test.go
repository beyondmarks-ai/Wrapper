package internal

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/beyondmarks-ai/Wrapper/src/internal/ui/processbar"
)

func writeTestZip(t *testing.T, path, name, content string) {
	t.Helper()
	file, err := os.Create(path)
	require.NoError(t, err)
	writer := zip.NewWriter(file)
	entry, err := writer.Create(name)
	require.NoError(t, err)
	_, err = entry.Write([]byte(content))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	require.NoError(t, file.Close())
}

func TestSecureArchiveExtraction(t *testing.T) {
	processes := processbar.New()
	processes.ListenForChannelUpdates()
	t.Cleanup(processes.SendStopListeningMsgBlocking)

	root := t.TempDir()
	archive := filepath.Join(root, "safe.zip")
	writeTestZip(t, archive, "folder/data.txt", "verified")
	destination := filepath.Join(root, "output")
	require.NoError(t, extractCompressFile(archive, destination, &processes))
	data, err := os.ReadFile(filepath.Join(destination, "folder", "data.txt"))
	require.NoError(t, err)
	require.Equal(t, "verified", string(data))
}

func TestArchiveExtractionRejectsTraversal(t *testing.T) {
	processes := processbar.New()
	processes.ListenForChannelUpdates()
	t.Cleanup(processes.SendStopListeningMsgBlocking)

	root := t.TempDir()
	archive := filepath.Join(root, "traversal.zip")
	writeTestZip(t, archive, "../escaped.txt", "blocked")
	destination := filepath.Join(root, "output")
	err := extractCompressFile(archive, destination, &processes)
	require.Error(t, err)
	_, statErr := os.Stat(filepath.Join(root, "escaped.txt"))
	require.ErrorIs(t, statErr, os.ErrNotExist)
}
