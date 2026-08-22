package control

import (
	"context"
	"errors"
	"time"

	"github.com/beyondmarks-ai/Wrapper/src/pkg/remote"
)

var (
	ErrNotFound       = errors.New("resource not found")
	ErrConflict       = errors.New("resource already exists")
	ErrForbidden      = errors.New("operation is not allowed")
	ErrPairingExpired = errors.New("pairing code is invalid or expired")
)

type Store interface {
	CreateDevice(context.Context, remote.Device) error
	GetDevice(context.Context, string, string) (remote.Device, error)
	ListDevices(context.Context, string) ([]remote.Device, error)
	UpdateDevice(context.Context, remote.Device) error
	UseNonce(context.Context, string, string, time.Time) (bool, error)

	CreatePairingCode(context.Context, string, remote.PairingCode) error
	ClaimPairingCode(context.Context, string, string, string, time.Time) (remote.Pairing, error)
	ConfirmPairing(context.Context, string, string, string, time.Time) (remote.Pairing, error)
	GetPairing(context.Context, string, string, string) (remote.Pairing, error)

	AppendEvent(context.Context, string, remote.Envelope) error
	ListEvents(context.Context, string, string, string, int) ([]remote.Envelope, error)
	AckEvent(context.Context, string, string, string) error

	CreateTransfer(context.Context, remote.Transfer) error
	GetTransfer(context.Context, string, string) (remote.Transfer, error)
	UpdateTransfer(context.Context, remote.Transfer, remote.TransferState) error
	ListTransfers(context.Context, string, string) ([]remote.Transfer, error)
	DailyTransferBytes(context.Context, string, time.Time) (int64, error)
	DailyTransferCount(context.Context, string, time.Time) (int, error)
}

type BlobStore interface {
	CreateUploadSession(context.Context, string, int64, time.Time) (string, error)
	DownloadURL(context.Context, string, time.Time) (string, error)
	DeleteURL(context.Context, string, time.Time) (string, error)
	Delete(context.Context, string) error
}

type ExpirationScheduler interface {
	ScheduleExpiration(context.Context, string, string, time.Time) error
}

type User struct {
	ID    string
	Email string
}

type TokenVerifier interface {
	Verify(context.Context, string) (User, error)
}

type InviteChecker interface {
	Allowed(context.Context, User) (bool, error)
}
