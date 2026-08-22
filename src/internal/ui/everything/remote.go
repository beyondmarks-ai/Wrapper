package everything

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/beyondmarks-ai/Wrapper/src/pkg/agent"
	"github.com/beyondmarks-ai/Wrapper/src/pkg/localagent"
	"github.com/beyondmarks-ai/Wrapper/src/pkg/remote"
)

type RemoteClient interface {
	Devices(context.Context) ([]remote.Device, error)
	Search(context.Context, localagent.SearchInput) ([]remote.SearchResult, error)
	Progress(context.Context) ([]agent.Progress, error)
}

type DevicesUpdateMsg struct {
	devices []remote.Device
	err     error
}

func (msg DevicesUpdateMsg) Apply(m *Model) tea.Cmd {
	m.devices = msg.devices
	m.deviceError = msg.err
	if m.deviceIndex > len(m.devices) {
		m.deviceIndex = 0
	}
	return nil
}

func (m *Model) loadDevicesCmd() tea.Cmd {
	if m.remoteClient == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		devices, err := m.remoteClient.Devices(ctx)
		return DevicesUpdateMsg{devices: devices, err: err}
	}
}

func (m *Model) activeDevice() (remote.Device, bool) {
	if m.deviceIndex <= 0 || m.deviceIndex > len(m.devices) {
		return remote.Device{}, false
	}
	return m.devices[m.deviceIndex-1], true
}

func (m *Model) deviceLabel() string {
	if device, ok := m.activeDevice(); ok {
		return device.Name
	}
	return "This PC"
}

func (m *Model) cycleDevice() tea.Cmd {
	if len(m.devices) == 0 {
		return m.loadDevicesCmd()
	}
	m.deviceIndex = (m.deviceIndex + 1) % (len(m.devices) + 1)
	m.selected = make(map[string]struct{})
	m.results = nil
	m.cursor = 0
	m.renderIndex = 0
	return m.queryCmd(m.textInput.Value())
}

func (m *Model) OpenSend(paths []string) tea.Cmd {
	m.open = true
	m.justOpened = false
	m.sendPaths = append([]string(nil), paths...)
	m.deviceIndex = 0
	m.cursor = 0
	m.renderIndex = 0
	m.results = nil
	m.selected = make(map[string]struct{})
	return m.loadDevicesCmd()
}
