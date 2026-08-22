package control

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/beyondmarks-ai/Wrapper/src/pkg/remote"
)

type MemoryStore struct {
	mu           sync.Mutex
	devices      map[string]remote.Device
	nonces       map[string]time.Time
	pairingCodes map[string]remote.PairingCode
	pairings     map[string]remote.Pairing
	events       map[string]remote.Envelope
	transfers    map[string]remote.Transfer
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		devices: make(map[string]remote.Device), nonces: make(map[string]time.Time),
		pairingCodes: make(map[string]remote.PairingCode), pairings: make(map[string]remote.Pairing),
		events: make(map[string]remote.Envelope), transfers: make(map[string]remote.Transfer),
	}
}

func scoped(userID, id string) string { return userID + "/" + id }

func pairingKey(userID, first, second string) string {
	if first > second {
		first, second = second, first
	}
	return userID + "/" + first + "/" + second
}

func (s *MemoryStore) CreateDevice(_ context.Context, device remote.Device) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := scoped(device.UserID, device.ID)
	if _, exists := s.devices[key]; exists {
		return ErrConflict
	}
	s.devices[key] = device
	return nil
}

func (s *MemoryStore) GetDevice(_ context.Context, userID, id string) (remote.Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	device, exists := s.devices[scoped(userID, id)]
	if !exists {
		return remote.Device{}, ErrNotFound
	}
	return device, nil
}

func (s *MemoryStore) ListDevices(_ context.Context, userID string) ([]remote.Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]remote.Device, 0)
	for _, device := range s.devices {
		if device.UserID == userID && device.RevokedAt.IsZero() {
			result = append(result, device)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func (s *MemoryStore) UpdateDevice(_ context.Context, device remote.Device) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := scoped(device.UserID, device.ID)
	if _, exists := s.devices[key]; !exists {
		return ErrNotFound
	}
	s.devices[key] = device
	return nil
}

func (s *MemoryStore) UseNonce(_ context.Context, deviceID, nonce string, expires time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	for key, expiry := range s.nonces {
		if expiry.Before(now) {
			delete(s.nonces, key)
		}
	}
	key := deviceID + "/" + nonce
	if _, used := s.nonces[key]; used {
		return false, nil
	}
	s.nonces[key] = expires
	return true, nil
}

func (s *MemoryStore) CreatePairingCode(_ context.Context, hash string, code remote.PairingCode) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.pairingCodes[hash]; exists {
		return ErrConflict
	}
	s.pairingCodes[hash] = code
	return nil
}

func (s *MemoryStore) ClaimPairingCode(_ context.Context, userID, hash, target string, now time.Time) (remote.Pairing, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	code, exists := s.pairingCodes[hash]
	if !exists || code.UserID != userID || !code.ExpiresAt.After(now) || code.ClaimedBy != "" || code.DeviceID == target {
		return remote.Pairing{}, ErrPairingExpired
	}
	if _, exists = s.devices[scoped(userID, target)]; !exists {
		return remote.Pairing{}, ErrNotFound
	}
	code.ClaimedBy = target
	s.pairingCodes[hash] = code
	pairing := remote.Pairing{
		ID: uuid.NewString(), UserID: userID, SourceDevice: code.DeviceID, TargetDevice: target,
		TargetConfirmed: true, CreatedAt: now,
	}
	s.pairings[pairingKey(userID, pairing.SourceDevice, pairing.TargetDevice)] = pairing
	return pairing, nil
}

func (s *MemoryStore) ConfirmPairing(_ context.Context, userID, pairingID, source string, now time.Time) (remote.Pairing, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, pairing := range s.pairings {
		if pairing.UserID == userID && pairing.ID == pairingID && pairing.SourceDevice == source {
			if now.Sub(pairing.CreatedAt) > pairingCodeTTL {
				return remote.Pairing{}, ErrPairingExpired
			}
			pairing.SourceConfirmed = true
			pairing.ConfirmedAt = now
			s.pairings[key] = pairing
			return pairing, nil
		}
	}
	return remote.Pairing{}, ErrNotFound
}

func (s *MemoryStore) GetPairing(_ context.Context, userID, first, second string) (remote.Pairing, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pairing, exists := s.pairings[pairingKey(userID, first, second)]
	if !exists || !pairing.Active() {
		return remote.Pairing{}, ErrForbidden
	}
	return pairing, nil
}

func (s *MemoryStore) AppendEvent(_ context.Context, userID string, event remote.Envelope) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := scoped(userID, event.ID)
	if _, exists := s.events[key]; exists {
		return ErrConflict
	}
	s.events[key] = event
	return nil
}

func (s *MemoryStore) ListEvents(_ context.Context, userID, deviceID, _ string, limit int) ([]remote.Envelope, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	result := make([]remote.Envelope, 0)
	for key, event := range s.events {
		if strings.HasPrefix(key, userID+"/") && event.TargetDevice == deviceID && event.ExpiresAt.After(now) {
			result = append(result, event)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].ID < result[j].ID
		}
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (s *MemoryStore) AckEvent(_ context.Context, userID, deviceID, eventID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := scoped(userID, eventID)
	event, exists := s.events[key]
	if !exists || event.TargetDevice != deviceID {
		return ErrNotFound
	}
	delete(s.events, key)
	return nil
}

func (s *MemoryStore) CreateTransfer(_ context.Context, transfer remote.Transfer) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := scoped(transfer.UserID, transfer.ID)
	if _, exists := s.transfers[key]; exists {
		return ErrConflict
	}
	s.transfers[key] = transfer
	return nil
}

func (s *MemoryStore) GetTransfer(_ context.Context, userID, id string) (remote.Transfer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	transfer, exists := s.transfers[scoped(userID, id)]
	if !exists {
		return remote.Transfer{}, ErrNotFound
	}
	return transfer, nil
}

func (s *MemoryStore) UpdateTransfer(_ context.Context, transfer remote.Transfer, expected remote.TransferState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := scoped(transfer.UserID, transfer.ID)
	current, exists := s.transfers[key]
	if !exists {
		return ErrNotFound
	}
	if current.State != expected {
		return ErrConflict
	}
	s.transfers[key] = transfer
	return nil
}

func (s *MemoryStore) ListTransfers(_ context.Context, userID, deviceID string) ([]remote.Transfer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]remote.Transfer, 0)
	for _, transfer := range s.transfers {
		if transfer.UserID == userID && (transfer.SourceDevice == deviceID || transfer.TargetDevice == deviceID) {
			result = append(result, transfer)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.After(result[j].CreatedAt) })
	return result, nil
}

func (s *MemoryStore) DailyTransferCount(_ context.Context, userID string, since time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, transfer := range s.transfers {
		if transfer.UserID == userID && !transfer.CreatedAt.Before(since) {
			count++
		}
	}
	return count, nil
}

func (s *MemoryStore) DailyTransferBytes(_ context.Context, userID string, since time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var total int64
	for _, transfer := range s.transfers {
		if transfer.UserID == userID && transfer.CreatedAt.After(since) {
			total += transfer.CiphertextSize
		}
	}
	return total, nil
}
