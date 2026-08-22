package remote

type TransferRequest struct {
	RequestID       string   `json:"requestId"`
	Paths           []string `json:"paths"`
	DestinationPath string   `json:"destinationPath,omitempty"`
}

type TransferReady struct {
	RequestID       string `json:"requestId"`
	TransferID      string `json:"transferId"`
	DestinationPath string `json:"destinationPath,omitempty"`
}

type PairingClaimed struct {
	PairingID   string `json:"pairingId"`
	DeviceID    string `json:"deviceId"`
	DeviceName  string `json:"deviceName"`
	Fingerprint string `json:"fingerprint"`
}
