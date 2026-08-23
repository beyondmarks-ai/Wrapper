package everything

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/beyondmarks-ai/Wrapper/src/internal/common"
	"github.com/beyondmarks-ai/Wrapper/src/pkg/agent"
	everythingapi "github.com/beyondmarks-ai/Wrapper/src/pkg/everything"
	"github.com/beyondmarks-ai/Wrapper/src/pkg/localagent"
	"github.com/beyondmarks-ai/Wrapper/src/pkg/remote"
	"github.com/beyondmarks-ai/Wrapper/src/pkg/utils"
)

type fakeSearcher struct {
	results []everythingapi.Result
	err     error
	queries []string
}

func (f *fakeSearcher) Search(query string, _ int) ([]everythingapi.Result, error) {
	f.queries = append(f.queries, query)
	return f.results, f.err
}

func testModel(searcher everythingapi.Searcher) Model {
	return NewModel(20, 80, searcher, nil)
}

func TestQueryAndApply(t *testing.T) {
	searcher := &fakeSearcher{results: []everythingapi.Result{{Path: `C:\work\report.txt`}}}
	m := testModel(searcher)
	m.Open()
	m.justOpened = false

	_, cmd := m.HandleUpdate(utils.TeaRuneKeyMsg("r"))
	require.NotNil(t, cmd)
	assert.True(t, m.loading)

	queryMsg, ok := cmd().(QueryMsg)
	require.True(t, ok)
	searchCmd := queryMsg.Apply(&m)
	require.NotNil(t, searchCmd)
	msg, ok := searchCmd().(UpdateMsg)
	require.True(t, ok)
	msg.Apply(&m)

	assert.Equal(t, []string{"r"}, searcher.queries)
	assert.False(t, m.loading)
	require.Len(t, m.results, 1)
	assert.Equal(t, `C:\work\report.txt`, m.results[0].Path)
}

func TestApplyIgnoresStaleQuery(t *testing.T) {
	m := testModel(&fakeSearcher{})
	m.textInput.SetValue("new")
	m.results = []everythingapi.Result{{Path: "original"}}

	NewUpdateMsg("old", []everythingapi.Result{{Path: "stale"}}, nil, 1).Apply(&m)

	assert.Equal(t, "original", m.results[0].Path)
}

func TestConfirmReturnsRevealAction(t *testing.T) {
	m := testModel(&fakeSearcher{})
	m.results = []everythingapi.Result{{Path: `C:\work`, IsDir: true}}

	action, ok := m.confirm().(common.RevealPathAction)
	require.True(t, ok)
	assert.Equal(t, `C:\work`, action.Path)
	assert.True(t, action.IsDir)
}

func TestArrowKeysNavigateWithoutChangingQuery(t *testing.T) {
	originalListDown := common.Hotkeys.ListDown
	common.Hotkeys.ListDown = []string{"down"}
	defer func() { common.Hotkeys.ListDown = originalListDown }()

	m := testModel(&fakeSearcher{})
	m.Open()
	m.results = []everythingapi.Result{{Path: "one"}, {Path: "two"}}

	_, cmd := m.HandleUpdate(tea.KeyPressMsg{Code: tea.KeyDown})

	assert.Nil(t, cmd)
	assert.Equal(t, 1, m.cursor)
	assert.Empty(t, m.textInput.Value())
}

func TestTabCyclesSearchModeAndStrictlyFiltersMixedResults(t *testing.T) {
	searcher := &fakeSearcher{results: []everythingapi.Result{
		{Path: "report.txt"},
		{Path: "report-folder", IsDir: true},
	}}
	m := testModel(searcher)
	m.Open()
	m.textInput.SetValue("report")

	_, cmd := m.HandleUpdate(tea.KeyPressMsg{Code: tea.KeyTab})
	require.NotNil(t, cmd)
	assert.Equal(t, SearchFiles, m.searchMode)

	queryMsg, ok := cmd().(QueryMsg)
	require.True(t, ok)
	searchCmd := queryMsg.Apply(&m)
	require.NotNil(t, searchCmd)
	updateMsg, ok := searchCmd().(UpdateMsg)
	require.True(t, ok)
	updateMsg.Apply(&m)

	assert.Equal(t, []string{"file: report"}, searcher.queries)
	assert.Equal(t, []everythingapi.Result{{Path: "report.txt"}}, m.results)
}

func TestSearchModeStrictResultFiltering(t *testing.T) {
	mixed := []everythingapi.Result{
		{Path: "one.txt"},
		{Path: "folder", IsDir: true},
		{Path: "two.txt"},
	}

	assert.Equal(t, mixed, SearchAll.FilterResults(mixed))
	assert.Equal(t, []everythingapi.Result{{Path: "one.txt"}, {Path: "two.txt"}}, SearchFiles.FilterResults(mixed))
	assert.Equal(t, []everythingapi.Result{{Path: "folder", IsDir: true}}, SearchFolders.FilterResults(mixed))
}

func TestSearchModeCyclesThroughFoldersAndAll(t *testing.T) {
	m := testModel(&fakeSearcher{})
	m.cycleSearchMode()
	assert.Equal(t, SearchFiles, m.searchMode)
	m.cycleSearchMode()
	assert.Equal(t, SearchFolders, m.searchMode)
	m.cycleSearchMode()
	assert.Equal(t, SearchAll, m.searchMode)
}

func TestRenderStates(t *testing.T) {
	unavailable := NewModel(20, 80, nil, everythingapi.ErrUnavailable)
	assert.Contains(t, unavailable.Render(), "Everything search unavailable")

	m := testModel(&fakeSearcher{})
	m.Open()
	assert.Contains(t, m.Render(), "Type a file or folder name")
	m.textInput.SetValue("missing")
	assert.Contains(t, m.Render(), "No results")
	m.queryError = errors.New("IPC unavailable")
	assert.Contains(t, m.Render(), "Search failed")
}

func TestRenderShowsGreenSelectionMarkerAndControls(t *testing.T) {
	m := testModel(&fakeSearcher{})
	m.textInput.SetValue("txt")
	m.results = []everythingapi.Result{{Path: "one.txt"}, {Path: "two.txt"}}
	m.cursor = 1

	output := m.Render()

	assert.Contains(t, output, "Mode:")
	assert.Contains(t, output, "ALL")
	assert.Contains(t, output, "> FILE | two.txt")
	assert.Contains(t, output, "\x1b[1;92m")
	assert.NotContains(t, output, "\x1b[102m")
	assert.Contains(t, output, "Up/Down select | Enter open | Esc close")
}

type fakeRemoteClient struct {
	devices []remote.Device
	results []remote.SearchResult
	input   localagent.SearchInput
}

func (f *fakeRemoteClient) Devices(context.Context) ([]remote.Device, error) {
	return f.devices, nil
}

func (f *fakeRemoteClient) Search(_ context.Context, input localagent.SearchInput) ([]remote.SearchResult, error) {
	f.input = input
	return f.results, nil
}

func (f *fakeRemoteClient) Progress(context.Context) ([]agent.Progress, error) {
	return nil, nil
}

func TestRemoteDeviceSearchAndTransferSelection(t *testing.T) {
	remoteClient := &fakeRemoteClient{
		devices: []remote.Device{{ID: "laptop", Name: "Office Laptop"}},
		results: []remote.SearchResult{{Path: `D:\\Shared\\report.pdf`}},
	}
	m := NewModelWithRemote(20, 100, &fakeSearcher{}, nil, remoteClient)
	require.NotNil(t, m.Open())
	DevicesUpdateMsg{devices: remoteClient.devices}.Apply(&m)
	require.Len(t, m.devices, 1)

	m.deviceIndex = 1
	m.textInput.SetValue("report")
	queryCmd := m.queryCmd("report")
	require.NotNil(t, queryCmd)
	queryMsg, ok := queryCmd().(QueryMsg)
	require.True(t, ok)
	require.Equal(t, "laptop", queryMsg.remoteDeviceID)
	updateCmd := queryMsg.Apply(&m)
	updateMsg, ok := updateCmd().(UpdateMsg)
	require.True(t, ok)
	updateMsg.Apply(&m)
	require.Equal(t, "laptop", remoteClient.input.DeviceID)
	require.Equal(t, "all", remoteClient.input.Mode)
	require.Len(t, m.results, 1)

	_, _ = m.HandleUpdate(tea.KeyPressMsg{Code: tea.KeySpace})
	require.Contains(t, m.selected, `D:\\Shared\\report.pdf`)
	action, _ := m.HandleUpdate(tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
	transfer, ok := action.(common.RemoteTransferAction)
	require.True(t, ok)
	require.Equal(t, "laptop", transfer.DeviceID)
	require.Equal(t, []string{`D:\\Shared\\report.pdf`}, transfer.Paths)
	require.True(t, m.IsOpen(), "search stays open so transfer progress remains visible")
	require.True(t, m.monitoringTransfer, "transfer polling remains active if the search modal is closed")
}

func TestTransferProgressRendersBytesAndEmitsCompletionOnce(t *testing.T) {
	m := NewModelWithRemote(20, 80, &fakeSearcher{}, nil, nil)
	baseline := TransferProgressMsg{progress: []agent.Progress{{
		TransferID: "old", Stage: remote.TransferCompleted, Done: 1024, Total: 1024,
	}}}
	require.Nil(t, baseline.Apply(&m), "existing completed transfers are only used as the notification baseline")

	downloading := TransferProgressMsg{progress: []agent.Progress{{
		TransferID: "new", Stage: remote.TransferDownloading, Done: 512, Total: 1024,
		Destination: "C:\\Users\\Demo\\Downloads\\Wrapper",
	}}}
	require.Nil(t, downloading.Apply(&m))
	progress := strings.Join(m.progressLines(), "\n")
	require.Contains(t, progress, "50%")
	require.Contains(t, progress, "512 B / 1.0 KiB")
	require.Contains(t, progress, "█")
	require.Contains(t, progress, "░")

	m.monitoringTransfer = true
	completed := TransferProgressMsg{progress: []agent.Progress{{
		TransferID: "new", Stage: remote.TransferCompleted, Done: 1024, Total: 1024,
		Destination: "C:\\Users\\Demo\\Downloads\\Wrapper",
	}}}
	cmd := completed.Apply(&m)
	require.NotNil(t, cmd)
	completion, ok := cmd().(TransferCompletedMsg)
	require.True(t, ok)
	require.Equal(t, "C:\\Users\\Demo\\Downloads\\Wrapper", completion.Destination)
	require.False(t, m.monitoringTransfer, "completion stops background polling after a closed modal")
	require.Nil(t, completed.Apply(&m), "the same completed transfer must not notify twice")
	require.Contains(t, strings.Join(m.progressLines(), "\n"), "Saved to:")
}
func TestSendPickerReturnsSelectedDevice(t *testing.T) {
	remoteClient := &fakeRemoteClient{devices: []remote.Device{{ID: "desktop", Name: "Desktop"}}}
	m := NewModelWithRemote(20, 100, &fakeSearcher{}, nil, remoteClient)
	require.NotNil(t, m.OpenSend([]string{`C:\\Work\\draft.docx`}))
	DevicesUpdateMsg{devices: remoteClient.devices}.Apply(&m)

	action, _ := m.HandleUpdate(tea.KeyPressMsg{Code: tea.KeyEnter})
	send, ok := action.(common.SendTransferAction)
	require.True(t, ok)
	require.Equal(t, "desktop", send.DeviceID)
	require.Equal(t, []string{`C:\\Work\\draft.docx`}, send.Paths)
}

func TestRemoteSearchModesUseAgentProtocolValues(t *testing.T) {
	require.Equal(t, "all", SearchAll.APIValue())
	require.Equal(t, "file", SearchFiles.APIValue())
	require.Equal(t, "folder", SearchFolders.APIValue())
}

func TestVisibleResultsAdaptToTerminalHeight(t *testing.T) {
	m := NewModelWithRemote(10, 40, &fakeSearcher{}, nil, nil)
	require.Equal(t, 2, m.visibleResultCount())
	m.SetMaxHeight(30)
	require.Equal(t, maxVisibleResults, m.visibleResultCount())
}
