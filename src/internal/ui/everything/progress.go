package everything

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/beyondmarks-ai/Wrapper/src/pkg/agent"
	"github.com/beyondmarks-ai/Wrapper/src/pkg/remote"
)

type TransferProgressMsg struct {
	progress []agent.Progress
	err      error
}

type TransferCompletedMsg struct {
	TransferID  string
	Destination string
}

func (msg TransferProgressMsg) Apply(m *Model) tea.Cmd {
	var completion tea.Cmd
	if !m.progressInitialized {
		for _, item := range msg.progress {
			if item.Stage == remote.TransferCompleted {
				m.completedTransfers[item.TransferID] = struct{}{}
			}
		}
		m.progressInitialized = true
	} else {
		for _, item := range msg.progress {
			if item.Stage != remote.TransferCompleted {
				continue
			}
			if _, seen := m.completedTransfers[item.TransferID]; seen {
				continue
			}
			m.completedTransfers[item.TransferID] = struct{}{}
			completed := TransferCompletedMsg{TransferID: item.TransferID, Destination: item.Destination}
			completion = func() tea.Msg { return completed }
			break
		}
	}
	m.progress = msg.progress
	m.progressError = msg.err
	return tea.Batch(completion, m.loadProgressAfter(time.Second))
}

func (m *Model) loadProgressCmd() tea.Cmd { return m.loadProgressAfter(0) }

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

func (m *Model) progressLines() []string {
	if len(m.progress) == 0 {
		return nil
	}
	current := m.progress[len(m.progress)-1]
	if current.Error != "" {
		return []string{" Transfer failed: " + current.Error}
	}
	if current.Total > 0 {
		percent := min(current.Done*100/current.Total, 100)
		barWidth := min(max(m.width-35, 12), 42)
		filled := int(percent) * barWidth / 100
		bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
		lines := []string{
			fmt.Sprintf(" Download %s  %3d%%", current.Stage, percent),
			fmt.Sprintf(" [%s]  %s / %s", bar, formatBytes(current.Done), formatBytes(current.Total)),
		}
		if current.Stage == remote.TransferCompleted && current.Destination != "" {
			lines = append(lines, " Saved to: "+current.Destination)
		}
		return lines
	}
	switch current.Stage {
	case remote.TransferCompleted:
		lines := []string{" Download completed and verified"}
		if current.Destination != "" {
			lines = append(lines, " Saved to: "+current.Destination)
		}
		return lines
	default:
		return []string{" Download " + string(current.Stage)}
	}
}

func formatBytes(value int64) string {
	const unit = int64(1024)
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}
	divisor, exponent := unit, 0
	for amount := value / unit; amount >= unit && exponent < 5; amount /= unit {
		divisor *= unit
		exponent++
	}
	return fmt.Sprintf("%.1f %ciB", float64(value)/float64(divisor), "KMGTPE"[exponent])
}
