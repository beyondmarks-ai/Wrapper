package internal

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/beyondmarks-ai/Wrapper/src/internal/common"
	everythingui "github.com/beyondmarks-ai/Wrapper/src/internal/ui/everything"
	"github.com/beyondmarks-ai/Wrapper/src/internal/ui/notify"
	"github.com/beyondmarks-ai/Wrapper/src/pkg/localagent"
)

type remoteTransferResultMsg struct {
	id      string
	sending bool
	err     error
}

func (m *model) openRemoteSendPicker() tea.Cmd {
	panel := m.getFocusedFilePanel()
	if panel.EmptyOrInvalid() {
		m.notifyModel = notify.New(true, "Nothing to transfer", "Select a file or folder first.", notify.NoAction)
		return nil
	}
	paths := panel.GetSelectedLocationsSortedAsVisible()
	if len(paths) == 0 {
		paths = []string{panel.GetFocusedItem().Location}
	}
	return m.everythingModal.OpenSend(paths)
}

func (m *model) remoteTransferCmd(action common.ModelAction) tea.Cmd {
	client := localagent.NewClient()
	switch action := action.(type) {
	case common.RemoteTransferAction:
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			id, err := client.RequestTransfer(ctx, localagent.TransferInput{
				DeviceID: action.DeviceID,
				Paths:    action.Paths,
			})
			return remoteTransferResultMsg{id: id, err: err}
		}
	case common.SendTransferAction:
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			id, err := client.Send(ctx, localagent.TransferInput{
				DeviceID: action.DeviceID,
				Paths:    action.Paths,
			})
			return remoteTransferResultMsg{id: id, sending: true, err: err}
		}
	default:
		return nil
	}
}

func (m *model) applyRemoteTransferResult(msg remoteTransferResultMsg) {
	if msg.err != nil {
		m.notifyModel = notify.New(true, "Transfer could not start", msg.err.Error(), notify.NoAction)
		return
	}
	title := "Transfer requested"
	content := fmt.Sprintf("Request %s was sent. Wrapper Agent will download and verify it automatically.", msg.id)
	if msg.sending {
		title = "Transfer started"
		content = fmt.Sprintf("Job %s is encrypting and uploading in the background.", msg.id)
	}
	m.notifyModel = notify.New(true, title, content, notify.NoAction)
}

func (m *model) applyTransferCompleted(msg everythingui.TransferCompletedMsg) {
	location := msg.Destination
	if location == "" {
		location = "your configured Wrapper download folder"
	}
	content := fmt.Sprintf("The file or folder was downloaded and verified.\n\nSaved to: %s", location)
	m.notifyModel = notify.New(true, "Download complete", content, notify.NoAction)
}
