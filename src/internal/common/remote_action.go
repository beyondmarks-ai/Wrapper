package common

type RemoteTransferAction struct {
	DeviceID string
	Paths    []string
}

func (RemoteTransferAction) String() string { return "RemoteTransferAction" }

type SendTransferAction struct {
	DeviceID string
	Paths    []string
}

func (SendTransferAction) String() string { return "SendTransferAction" }
