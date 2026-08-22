package remote

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEncryptedArchiveRoundTrip(t *testing.T) {
	identity, err := NewIdentity()
	require.NoError(t, err)
	recipient, err := identity.Recipient()
	require.NoError(t, err)
	source := t.TempDir()
	folder := filepath.Join(source, "project")
	require.NoError(t, os.MkdirAll(filepath.Join(folder, "empty"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(folder, "hello.txt"), []byte("hello wrapper"), 0o600))

	var encrypted bytes.Buffer
	manifest, err := BuildEncryptedPayload(context.Background(), "transfer-1", []string{folder}, recipient, &encrypted)
	require.NoError(t, err)
	require.NotEmpty(t, manifest.SHA256)

	destination := t.TempDir()
	require.NoError(t, ExtractEncryptedPayload(context.Background(), identity, &encrypted, destination, manifest, ConflictKeepBoth))
	content, err := os.ReadFile(filepath.Join(destination, "project", "hello.txt"))
	require.NoError(t, err)
	require.Equal(t, "hello wrapper", string(content))
	info, err := os.Stat(filepath.Join(destination, "project", "empty"))
	require.NoError(t, err)
	require.True(t, info.IsDir())
}

func TestArchiveRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	_, err := safeArchiveTarget(root, "../../secret.txt")
	require.ErrorIs(t, err, ErrUnsafePath)
	_, err = safeArchiveTarget(root, "C:/Windows/secret.txt")
	require.Error(t, err)
}

func TestKeepBothDoesNotOverwrite(t *testing.T) {
	identity, _ := NewIdentity()
	recipient, _ := identity.Recipient()
	source := t.TempDir()
	file := filepath.Join(source, "report.txt")
	require.NoError(t, os.WriteFile(file, []byte("new"), 0o600))
	var encrypted bytes.Buffer
	manifest, err := BuildEncryptedPayload(context.Background(), "transfer-2", []string{file}, recipient, &encrypted)
	require.NoError(t, err)
	destination := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(destination, "report.txt"), []byte("old"), 0o600))
	require.NoError(t, ExtractEncryptedPayload(context.Background(), identity, &encrypted, destination, manifest, ConflictKeepBoth))
	content, err := os.ReadFile(filepath.Join(destination, "report (1).txt"))
	require.NoError(t, err)
	require.Equal(t, "new", string(content))
}
