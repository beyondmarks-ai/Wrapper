package everything

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/beyondmarks-ai/Wrapper/src/config/icon"
	"github.com/beyondmarks-ai/Wrapper/src/internal/common"
	everythingapi "github.com/beyondmarks-ai/Wrapper/src/pkg/everything"
	"github.com/beyondmarks-ai/Wrapper/src/pkg/localagent"
)

func DefaultModel(maxHeight, width int) Model {
	searcher, err := everythingapi.New()
	model := NewModel(maxHeight, width, searcher, err)
	model.remoteClient = localagent.NewClient()
	return model
}

func NewModel(maxHeight, width int, searcher everythingapi.Searcher, startupErr error) Model {
	return NewModelWithRemote(maxHeight, width, searcher, startupErr, nil)
}

func NewModelWithRemote(maxHeight, width int, searcher everythingapi.Searcher, startupErr error, remoteClient RemoteClient) Model {
	m := Model{
		headline: icon.Search + icon.Space + everythingHeadlineText, searcher: searcher,
		startupError: startupErr, remoteClient: remoteClient, textInput: common.GeneratePromptTextInput(),
		results: []everythingapi.Result{}, selected: make(map[string]struct{}),
		completedTransfers: make(map[string]struct{}),
	}
	m.textInput.Prompt = ""
	m.SetMaxHeight(maxHeight)
	m.SetWidth(width)
	return m
}

func (m *Model) HandleUpdate(msg tea.Msg) (common.ModelAction, tea.Cmd) {
	if !m.open {
		return common.NoAction{}, nil
	}
	if key, ok := msg.(tea.KeyPressMsg); ok {
		if len(m.sendPaths) > 0 {
			return m.handleSendPickerKey(key)
		}
		switch {
		case slices.Contains(common.Hotkeys.CancelTyping, key.String()):
			m.Close()
			return common.NoAction{}, nil
		case key.String() == "shift+tab":
			return common.NoAction{}, m.cycleDevice()
		case m.searcher == nil && m.deviceIndex == 0:
			if slices.Contains(common.Hotkeys.ConfirmTyping, key.String()) || slices.Contains(common.Hotkeys.Quit, key.String()) {
				m.Close()
			}
			return common.NoAction{}, nil
		case key.String() == "ctrl+t" || slices.Contains(common.Hotkeys.TransferItems, key.String()):
			return m.transferAction(), nil
		case key.String() == " " || key.String() == "space":
			m.toggleSelected()
			return common.NoAction{}, nil
		case slices.Contains(common.Hotkeys.ConfirmTyping, key.String()):
			if m.loading {
				return common.NoAction{}, nil
			}
			action := m.confirm()
			if _, remoteDevice := m.activeDevice(); !remoteDevice {
				m.Close()
			}
			return action, nil
		case key.String() == "tab":
			m.cycleSearchMode()
			return common.NoAction{}, m.queryCmd(m.textInput.Value())
		case slices.Contains(common.Hotkeys.ListUp, key.String()) && !isAlphaNumeric(key):
			m.navigateUp()
			return common.NoAction{}, nil
		case slices.Contains(common.Hotkeys.ListDown, key.String()) && !isAlphaNumeric(key):
			m.navigateDown()
			return common.NoAction{}, nil
		case slices.Contains(common.Hotkeys.OpenEverything, key.String()) && m.justOpened:
			m.justOpened = false
			return common.NoAction{}, nil
		default:
			m.textInput, _ = m.textInput.Update(key)
			return common.NoAction{}, m.queryCmd(m.textInput.Value())
		}
	}
	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return common.NoAction{}, cmd
}

func (m *Model) handleSendPickerKey(key tea.KeyPressMsg) (common.ModelAction, tea.Cmd) {
	switch {
	case key.String() == "esc" || slices.Contains(common.Hotkeys.CancelTyping, key.String()) || slices.Contains(common.Hotkeys.Quit, key.String()):
		m.Close()
	case slices.Contains(common.Hotkeys.ListUp, key.String()):
		if m.cursor > 0 {
			m.cursor--
		}
	case slices.Contains(common.Hotkeys.ListDown, key.String()):
		if m.cursor+1 < len(m.devices) {
			m.cursor++
		}
	case key.String() == "enter" || slices.Contains(common.Hotkeys.ConfirmTyping, key.String()):
		if m.cursor >= 0 && m.cursor < len(m.devices) {
			action := common.SendTransferAction{DeviceID: m.devices[m.cursor].ID, Paths: append([]string(nil), m.sendPaths...)}
			m.Close()
			return action, nil
		}
	}
	return common.NoAction{}, nil
}

func (m *Model) confirm() common.ModelAction {
	if m.cursor < 0 || m.cursor >= len(m.results) {
		return common.NoAction{}
	}
	if _, remoteDevice := m.activeDevice(); remoteDevice {
		m.toggleSelected()
		return common.NoAction{}
	}
	result := m.results[m.cursor]
	return common.RevealPathAction{Path: filepath.Clean(result.Path), IsDir: result.IsDir}
}

func (m *Model) transferAction() common.ModelAction {
	device, remoteDevice := m.activeDevice()
	if !remoteDevice || m.cursor < 0 || m.cursor >= len(m.results) {
		return common.NoAction{}
	}
	paths := make([]string, 0, len(m.selected))
	for _, result := range m.results {
		if _, selected := m.selected[result.Path]; selected {
			paths = append(paths, result.Path)
		}
	}
	if len(paths) == 0 {
		paths = []string{m.results[m.cursor].Path}
	}
	m.monitoringTransfer = true
	return common.RemoteTransferAction{DeviceID: device.ID, Paths: paths}
}

func (m *Model) toggleSelected() {
	if _, remoteDevice := m.activeDevice(); !remoteDevice || m.cursor < 0 || m.cursor >= len(m.results) {
		return
	}
	path := m.results[m.cursor].Path
	if _, selected := m.selected[path]; selected {
		delete(m.selected, path)
	} else {
		m.selected[path] = struct{}{}
	}
}

func (m *Model) queryCmd(query string) tea.Cmd {
	query = strings.TrimSpace(query)
	if query == "" {
		m.loading = false
		m.queryError = nil
		m.results = nil
		m.cursor = 0
		m.renderIndex = 0
		return nil
	}
	m.loading = true
	reqID := m.reqCnt
	m.reqCnt++
	deviceID := ""
	if device, remoteDevice := m.activeDevice(); remoteDevice {
		deviceID = device.ID
	}
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg {
		return QueryMsg{query: query, searchQuery: m.searchMode.EverythingQuery(query), mode: m.searchMode, reqID: reqID, remoteDeviceID: deviceID}
	})
}

func (msg UpdateMsg) Apply(m *Model) tea.Cmd {
	deviceID := ""
	if device, remoteDevice := m.activeDevice(); remoteDevice {
		deviceID = device.ID
	}
	if msg.query != strings.TrimSpace(m.textInput.Value()) || msg.searchQuery != m.searchMode.EverythingQuery(msg.query) || msg.remoteDeviceID != deviceID {
		return nil
	}
	m.loading = false
	m.results = msg.results
	m.queryError = msg.err
	m.selected = make(map[string]struct{})
	m.cursor = 0
	m.renderIndex = 0
	return nil
}

func (m *Model) cycleSearchMode() {
	m.searchMode = (m.searchMode + 1) % 3
	m.loading = false
	m.queryError = nil
	m.results = nil
	m.selected = make(map[string]struct{})
	m.cursor = 0
	m.renderIndex = 0
}

func (m *Model) remoteSearch(ctx context.Context, input localagent.SearchInput) ([]everythingapi.Result, error) {
	results, err := m.remoteClient.Search(ctx, input)
	converted := make([]everythingapi.Result, 0, len(results))
	for _, result := range results {
		converted = append(converted, everythingapi.Result{Path: result.Path, IsDir: result.IsDir})
	}
	return converted, err
}

func isAlphaNumeric(msg tea.KeyPressMsg) bool {
	runes := []rune(msg.String())
	return len(runes) == 1 && (unicode.IsLetter(runes[0]) || unicode.IsNumber(runes[0]))
}
