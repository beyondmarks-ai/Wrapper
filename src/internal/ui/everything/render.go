package everything

import (
	"fmt"

	"charm.land/lipgloss/v2"

	"github.com/beyondmarks-ai/Wrapper/src/internal/common"
	"github.com/beyondmarks-ai/Wrapper/src/internal/ui"
	"github.com/beyondmarks-ai/Wrapper/src/internal/ui/rendering"
)

func selectedResultStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.BrightGreen).
		Bold(true)
}

func (m *Model) Render() string {
	r := ui.ZoxideRenderer(m.maxHeight, m.width)
	r.SetBorderTitle(m.headline)
	if status := m.progressLines(); len(status) > 0 {
		for _, line := range status {
			r.AddLines(selectedResultStyle().Render(common.TruncateText(line, max(m.width-2, 1), "...")))
		}
		r.AddSection()
	}

	if len(m.sendPaths) > 0 {
		m.renderDevicePicker(r)
		return r.Render()
	}

	_, remoteDevice := m.activeDevice()
	if m.searcher == nil && !remoteDevice {
		r.AddLines(" Everything search unavailable on This PC")
		r.AddSection()
		r.AddLines(" Install/run Everything and place its SDK DLL beside wrap.exe.")
		if m.startupError != nil {
			r.AddLines(" " + common.TruncateText(m.startupError.Error(), m.width-4, "..."))
		}
		r.AddSection()
		r.AddLines(" Shift+Tab remote PC | Esc close")
		return r.Render()
	}

	r.AddLines(" Search on: " + selectedResultStyle().Render(m.deviceLabel()) + "  (Shift+Tab changes PC)")
	r.AddLines(" Mode: " + selectedResultStyle().Render(m.searchMode.Label()) + "  (Tab changes type)")
	if m.deviceError != nil {
		r.AddLines(" Agent: " + common.TruncateText(m.deviceError.Error(), m.width-10, "..."))
	}
	r.AddLines(" " + m.textInput.View())
	r.AddSection()
	switch {
	case m.queryError != nil:
		r.AddLines(" Search failed: " + common.TruncateText(m.queryError.Error(), m.width-17, "..."))
	case m.loading:
		r.AddLines(" Searching " + m.deviceLabel() + "...")
	case m.textInput.Value() == "":
		r.AddLines(" Type a file or folder name")
	case len(m.results) == 0:
		r.AddLines(" No results. Try another name or check the remote PC.")
	default:
		m.renderResults(r, remoteDevice)
	}
	return r.Render()
}

func (m *Model) renderDevicePicker(r *rendering.Renderer) {
	r.AddLines(fmt.Sprintf(" Send %d selected item(s) to:", len(m.sendPaths)))
	r.AddSection()
	if m.deviceError != nil {
		r.AddLines(" Wrapper Agent unavailable: " + common.TruncateText(m.deviceError.Error(), max(m.width-30, 1), "..."))
	} else if len(m.devices) == 0 {
		r.AddLines(" No paired online devices. Pair one with wrap device code.")
	} else {
		for i, device := range m.devices {
			marker := " "
			line := fmt.Sprintf("   %s", device.Name)
			if i == m.cursor {
				marker = ">"
				line = selectedResultStyle().Render(line)
			}
			r.AddLines(" " + marker + line)
		}
	}
	r.AddSection()
	r.AddLines(" Up/Down select | Enter send | Esc cancel")
}

func (m *Model) renderResults(r *rendering.Renderer, remoteDevice bool) {
	end := min(m.renderIndex+m.visibleResultCount(), len(m.results))
	for i := m.renderIndex; i < end; i++ {
		result := m.results[i]
		kind := "FILE"
		if result.IsDir {
			kind = "DIR "
		}
		cursorMarker := " "
		if i == m.cursor {
			cursorMarker = ">"
		}
		selectedMarker := " "
		if _, selected := m.selected[result.Path]; selected {
			selectedMarker = "x"
		}
		path := common.TruncateTextBeginning(result.Path, max(m.width-17, 1), "...")
		line := fmt.Sprintf(" %s %s | %s", cursorMarker, kind, path)
		if remoteDevice {
			line = fmt.Sprintf(" %s [%s] %s | %s", cursorMarker, selectedMarker, kind, path)
		}
		if i == m.cursor {
			line = selectedResultStyle().Render(line)
		}
		r.AddLines(line)
	}
	r.AddSection()
	if remoteDevice {
		r.AddLines(" Space mark | Ctrl+T request | Tab type | Shift+Tab PC | Esc close")
	} else {
		r.AddLines(" Tab type | Shift+Tab PC | Up/Down select | Enter open | Esc close")
	}
	if len(m.results) > m.visibleResultCount() {
		r.AddLines(fmt.Sprintf(" Showing %d-%d of %d", m.renderIndex+1, end, len(m.results)))
	}
}
