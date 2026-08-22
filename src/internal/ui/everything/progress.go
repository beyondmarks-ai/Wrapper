package everything

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/beyondmarks-ai/Wrapper/src/pkg/agent"
	"github.com/beyondmarks-ai/Wrapper/src/pkg/remote"
)

type TransferProgressMsg struct {
	progress []agent.Progress
	err      error
}

func (msg TransferProgressMsg) Apply(m *Model) tea.Cmd {
	m.progress = msg.progress
	m.progressError = msg.err
	if !m.open {
		return nil
	}
	return m.loadProgressAfter(time.Second)
}

func (m *Model) loadProgressCmd() tea.Cmd {
	return m.loadProgressAfter(0)
}

func (m *Model) loadProgressAfter(delay time.Duration) tea.Cmd {
	if m.remoteClient == nil {
		return nil
	}
	load := func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		progress, err := m.remoteClient.Progress(ctx)
		return TransferProgressMsg{progress: progress, err: err}
	}
	if delay == 0 {
		return load
	}
	return tea.Tick(delay, func(time.Time) tea.Msg { return load() })
}

func (m *Model) progressLine() string {
	if len(m.progress) == 0 {
		return ""
	}
	current := m.progress[len(m.progress)-1]
	if current.Error != "" {
		return " Transfer failed: " + current.Error
	}
	if current.Total > 0 {
		percent := min(current.Done*100/current.Total, 100)
		return fmt.Sprintf(" Transfer %s: %d%%", current.Stage, percent)
	}
	switch current.Stage {
	case remote.TransferCompleted:
		return " Transfer completed and verified"
	default:
		return " Transfer " + string(current.Stage)
	}
}
