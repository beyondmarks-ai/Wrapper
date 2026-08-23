package everything

import (
	"log/slog"

	tea "charm.land/bubbletea/v2"
)

func (m *Model) Open() tea.Cmd {
	m.open = true
	m.justOpened = true
	m.loading = false
	m.queryError = nil
	m.results = nil
	m.cursor = 0
	m.renderIndex = 0
	m.searchMode = SearchAll
	m.deviceIndex = 0
	m.sendPaths = nil
	m.selected = make(map[string]struct{})
	m.textInput.SetValue("")
	_ = m.textInput.Focus()
	return tea.Batch(m.loadDevicesCmd(), m.loadProgressCmd())
}

func (m *Model) CancelTransferMonitoring() { m.monitoringTransfer = false }

func (m *Model) Close() {
	m.open = false
	m.loading = false
	m.textInput.Blur()
	m.textInput.SetValue("")
	m.results = nil
	m.queryError = nil
	m.cursor = 0
	m.renderIndex = 0
	m.searchMode = SearchAll
	m.deviceIndex = 0
	m.sendPaths = nil
	m.selected = make(map[string]struct{})
}

func (m *Model) IsOpen() bool      { return m.open }
func (m *Model) GetWidth() int     { return m.width }
func (m *Model) GetMaxHeight() int { return m.maxHeight }

func (m *Model) SetWidth(width int) {
	if width < MinWidth {
		slog.Warn("Everything search initialized with too little width", "width", width)
		width = MinWidth
	}
	m.width = width
	m.textInput.SetWidth(width - modalInputPadding)
}

func (m *Model) SetMaxHeight(height int) {
	if height < MinHeight {
		height = MinHeight
	}
	m.maxHeight = height
}

func (m *Model) GetResults() []string {
	paths := make([]string, len(m.results))
	for i, result := range m.results {
		paths[i] = result.Path
	}
	return paths
}
