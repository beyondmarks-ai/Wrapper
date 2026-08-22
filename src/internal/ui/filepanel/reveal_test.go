package filepanel

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/beyondmarks-ai/Wrapper/src/internal/common"
)

func TestRevealFile(t *testing.T) {
	originalSearchBar := common.Hotkeys.SearchBar
	common.Hotkeys.SearchBar = []string{"/"}
	defer func() { common.Hotkeys.SearchBar = originalSearchBar }()

	root := t.TempDir()
	targetDir := filepath.Join(root, "target")
	require.NoError(t, os.Mkdir(targetDir, 0o755))
	target := filepath.Join(targetDir, "report.txt")
	require.NoError(t, os.WriteFile(target, []byte("report"), 0o600))

	panel := defaultFilePanel(root, true)
	require.NoError(t, panel.RevealFile(target))
	assert.Equal(t, targetDir, panel.Location)
	assert.Equal(t, "report.txt", panel.TargetFile)

	panel.UpdateElementsIfNeeded(true, true)
	assert.Equal(t, target, panel.GetFocusedItem().Location)
}
