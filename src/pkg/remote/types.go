package remote

import "time"

const (
	ProtocolVersion = "1"
	MaxTransferSize = int64(20 << 30)
)

type Device struct {
	ID           string    `json:"id" firestore:"id"`
	UserID       string    `json:"-" firestore:"userId"`
	Name         string    `json:"name" firestore:"name"`
	AgeRecipient string    `json:"ageRecipient" firestore:"ageRecipient"`
	SigningKey   string    `json:"signingKey" firestore:"signingKey"`
	Online       bool      `json:"online" firestore:"online"`
	Paired       bool      `json:"paired" firestore:"-"`
	LastSeen     time.Time `json:"lastSeen" firestore:"lastSeen"`
	CreatedAt    time.Time `json:"createdAt" firestore:"createdAt"`
	RevokedAt    time.Time `json:"revokedAt,omitempty" firestore:"revokedAt,omitempty"`
}

type PairingCode struct {
	Hash      string    `firestore:"hash"`
	UserID    string    `firestore:"userId"`
	DeviceID  string    `firestore:"deviceId"`
	ExpiresAt time.Time `firestore:"expiresAt"`
	ClaimedBy string    `firestore:"claimedBy,omitempty"`
	Confirmed bool      `firestore:"confirmed"`
	CreatedAt time.Time `firestore:"createdAt"`
}

type Envelope struct {
	Version      string         `json:"version" firestore:"version"`
	ID           string         `json:"id" firestore:"id"`
	Kind         string         `json:"kind" firestore:"kind"`
	SourceDevice string         `json:"sourceDevice" firestore:"sourceDevice"`
	TargetDevice string         `json:"targetDevice" firestore:"targetDevice"`
	Ciphertext   string         `json:"ciphertext" firestore:"ciphertext"`
	Signature    string         `json:"signature" firestore:"signature"`
	CreatedAt    time.Time      `json:"createdAt" firestore:"createdAt"`
	ExpiresAt    time.Time      `json:"expiresAt" firestore:"expiresAt"`
	Metadata     map[string]any `json:"metadata,omitempty" firestore:"metadata,omitempty"`
}

type SearchRequest struct {
	RequestID string `json:"requestId"`
	Query     string `json:"query"`
	Mode      string `json:"mode"`
	Limit     int    `json:"limit"`
}

type SearchResult struct {
	Path     string    `json:"path"`
	IsDir    bool      `json:"isDir"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
}

type SearchResponse struct {
	RequestID string         `json:"requestId"`
	Results   []SearchResult `json:"results"`
	Error     string         `json:"error,omitempty"`
}

type TransferState string

const (
	TransferRequested   TransferState = "requested"
	TransferPreparing   TransferState = "preparing"
	TransferUploading   TransferState = "uploading"
	TransferWaiting     TransferState = "waiting"
	TransferDownloading TransferState = "downloading"
	TransferVerifying   TransferState = "verifying"
	TransferCompleted   TransferState = "completed"
	TransferFailed      TransferState = "failed"
	TransferCancelled   TransferState = "cancelled"
	TransferExpired     TransferState = "expired"
)

type Transfer struct {
	ID                string        `json:"id" firestore:"id"`
	UserID            string        `json:"-" firestore:"userId"`
	SourceDevice      string        `json:"sourceDevice" firestore:"sourceDevice"`
	TargetDevice      string        `json:"targetDevice" firestore:"targetDevice"`
	ObjectName        string        `json:"objectName,omitempty" firestore:"objectName,omitempty"`
	EncryptedManifest string        `json:"encryptedManifest,omitempty" firestore:"encryptedManifest,omitempty"`
	CiphertextSize    int64         `json:"ciphertextSize" firestore:"ciphertextSize"`
	State             TransferState `json:"state" firestore:"state"`
	ErrorCode         string        `json:"errorCode,omitempty" firestore:"errorCode,omitempty"`
	CreatedAt         time.Time     `json:"createdAt" firestore:"createdAt"`
	UpdatedAt         time.Time     `json:"updatedAt" firestore:"updatedAt"`
	ExpiresAt         time.Time     `json:"expiresAt" firestore:"expiresAt"`
}

type Manifest struct {
	TransferID string         `json:"transferId"`
	Archive    bool           `json:"archive"`
	Name       string         `json:"name"`
	PlainSize  int64          `json:"plainSize"`
	SHA256     string         `json:"sha256"`
	Entries    []ManifestItem `json:"entries"`
}

type ManifestItem struct {
	Path     string    `json:"path"`
	Size     int64     `json:"size"`
	Mode     uint32    `json:"mode"`
	Modified time.Time `json:"modified"`
	SHA256   string    `json:"sha256,omitempty"`
	IsDir    bool      `json:"isDir"`
}
