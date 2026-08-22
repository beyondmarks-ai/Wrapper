package everything

func (m *Model) navigateUp() {
	if len(m.results) == 0 {
		return
	}
	if m.cursor > 0 {
		m.cursor--
	} else {
		m.cursor = len(m.results) - 1
	}
	m.updateRenderIndex()
}

func (m *Model) navigateDown() {
	if len(m.results) == 0 {
		return
	}
	if m.cursor < len(m.results)-1 {
		m.cursor++
	} else {
		m.cursor = 0
	}
	m.updateRenderIndex()
}

func (m *Model) visibleResultCount() int {
	return max(min(maxVisibleResults, m.maxHeight-8), 1)
}

func (m *Model) updateRenderIndex() {
	if m.cursor < m.renderIndex {
		m.renderIndex = m.cursor
	}
	if m.cursor >= m.renderIndex+m.visibleResultCount() {
		m.renderIndex = m.cursor - m.visibleResultCount() + 1
	}
	maxIndex := max(len(m.results)-m.visibleResultCount(), 0)
	m.renderIndex = min(max(m.renderIndex, 0), maxIndex)
}
