package control

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/beyondmarks-ai/Wrapper/src/pkg/remote"
)

type FirestoreStore struct{ client *firestore.Client }

func NewFirestoreStore(client *firestore.Client) *FirestoreStore {
	return &FirestoreStore{client: client}
}

func (s *FirestoreStore) user(userID string) *firestore.DocumentRef {
	return s.client.Collection("users").Doc(userID)
}

func (s *FirestoreStore) CreateDevice(ctx context.Context, device remote.Device) error {
	_, err := s.user(device.UserID).Collection("devices").Doc(device.ID).Create(ctx, device)
	return translateFirestoreError(err)
}

func (s *FirestoreStore) GetDevice(ctx context.Context, userID, id string) (remote.Device, error) {
	snapshot, err := s.user(userID).Collection("devices").Doc(id).Get(ctx)
	if err != nil {
		return remote.Device{}, translateFirestoreError(err)
	}
	var device remote.Device
	if err = snapshot.DataTo(&device); err != nil {
		return remote.Device{}, err
	}
	return device, nil
}

func (s *FirestoreStore) ListDevices(ctx context.Context, userID string) ([]remote.Device, error) {
	documents := s.user(userID).Collection("devices").Documents(ctx)
	defer documents.Stop()
	var devices []remote.Device
	for {
		snapshot, err := documents.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, err
		}
		var device remote.Device
		if err = snapshot.DataTo(&device); err != nil {
			return nil, err
		}
		if device.RevokedAt.IsZero() {
			devices = append(devices, device)
		}
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].Name < devices[j].Name })
	return devices, nil
}

func (s *FirestoreStore) UpdateDevice(ctx context.Context, device remote.Device) error {
	_, err := s.user(device.UserID).Collection("devices").Doc(device.ID).Set(ctx, device)
	return translateFirestoreError(err)
}

func (s *FirestoreStore) UseNonce(ctx context.Context, deviceID, nonce string, expires time.Time) (bool, error) {
	digest := sha256.Sum256([]byte(deviceID + "\x00" + nonce))
	reference := s.client.Collection("requestNonces").Doc(hex.EncodeToString(digest[:]))
	err := s.client.RunTransaction(ctx, func(ctx context.Context, transaction *firestore.Transaction) error {
		_, err := transaction.Get(reference)
		if err == nil {
			return ErrConflict
		}
		if status.Code(err) != codes.NotFound {
			return err
		}
		return transaction.Create(reference, map[string]any{"deviceId": deviceID, "expiresAt": expires})
	})
	if errors.Is(err, ErrConflict) {
		return false, nil
	}
	return err == nil, translateFirestoreError(err)
}

func (s *FirestoreStore) CreatePairingCode(ctx context.Context, hash string, code remote.PairingCode) error {
	_, err := s.user(code.UserID).Collection("pairingCodes").Doc(hash).Create(ctx, code)
	return translateFirestoreError(err)
}

func (s *FirestoreStore) ClaimPairingCode(ctx context.Context, userID, hash, target string, now time.Time) (remote.Pairing, error) {
	codeReference := s.user(userID).Collection("pairingCodes").Doc(hash)
	var pairing remote.Pairing
	err := s.client.RunTransaction(ctx, func(ctx context.Context, transaction *firestore.Transaction) error {
		snapshot, err := transaction.Get(codeReference)
		if err != nil {
			return ErrPairingExpired
		}
		var code remote.PairingCode
		if err = snapshot.DataTo(&code); err != nil {
			return err
		}
		if code.UserID != userID || !code.ExpiresAt.After(now) || code.ClaimedBy != "" || code.DeviceID == target {
			return ErrPairingExpired
		}
		if _, err = transaction.Get(s.user(userID).Collection("devices").Doc(target)); err != nil {
			return ErrNotFound
		}
		code.ClaimedBy = target
		pairing = remote.Pairing{
			ID: cloudPairingID(code.DeviceID, target), UserID: userID, SourceDevice: code.DeviceID,
			TargetDevice: target, TargetConfirmed: true, CreatedAt: now,
		}
		if err = transaction.Set(codeReference, code); err != nil {
			return err
		}
		return transaction.Create(s.user(userID).Collection("pairings").Doc(pairing.ID), pairing)
	})
	return pairing, translateFirestoreError(err)
}

func (s *FirestoreStore) ConfirmPairing(ctx context.Context, userID, pairingID, source string, now time.Time) (remote.Pairing, error) {
	reference := s.user(userID).Collection("pairings").Doc(pairingID)
	var pairing remote.Pairing
	err := s.client.RunTransaction(ctx, func(ctx context.Context, transaction *firestore.Transaction) error {
		snapshot, err := transaction.Get(reference)
		if err != nil {
			return err
		}
		if err = snapshot.DataTo(&pairing); err != nil {
			return err
		}
		if pairing.SourceDevice != source || now.Sub(pairing.CreatedAt) > pairingCodeTTL {
			return ErrPairingExpired
		}
		pairing.SourceConfirmed = true
		pairing.ConfirmedAt = now
		return transaction.Set(reference, pairing)
	})
	return pairing, translateFirestoreError(err)
}

func (s *FirestoreStore) GetPairing(ctx context.Context, userID, first, second string) (remote.Pairing, error) {
	snapshot, err := s.user(userID).Collection("pairings").Doc(cloudPairingID(first, second)).Get(ctx)
	if err != nil {
		return remote.Pairing{}, ErrForbidden
	}
	var pairing remote.Pairing
	if err = snapshot.DataTo(&pairing); err != nil || !pairing.Active() {
		return remote.Pairing{}, ErrForbidden
	}
	return pairing, nil
}

func (s *FirestoreStore) AppendEvent(ctx context.Context, userID string, event remote.Envelope) error {
	_, err := s.user(userID).Collection("events").Doc(event.ID).Create(ctx, event)
	return translateFirestoreError(err)
}

func (s *FirestoreStore) ListEvents(ctx context.Context, userID, deviceID, _ string, limit int) ([]remote.Envelope, error) {
	now := time.Now().UTC()
	query := s.user(userID).Collection("events").Where("targetDevice", "==", deviceID).
		Where("expiresAt", ">", now).OrderBy("expiresAt", firestore.Asc).Limit(limit)
	documents := query.Documents(ctx)
	defer documents.Stop()
	var events []remote.Envelope
	for {
		snapshot, err := documents.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, err
		}
		var event remote.Envelope
		if err = snapshot.DataTo(&event); err != nil {
			return nil, err
		}
		if event.ExpiresAt.After(now) {
			events = append(events, event)
		}
	}
	sort.Slice(events, func(i, j int) bool { return events[i].ID < events[j].ID })
	if len(events) > limit {
		events = events[:limit]
	}
	return events, nil
}

func (s *FirestoreStore) AckEvent(ctx context.Context, userID, deviceID, eventID string) error {
	reference := s.user(userID).Collection("events").Doc(eventID)
	return translateFirestoreError(s.client.RunTransaction(ctx, func(ctx context.Context, transaction *firestore.Transaction) error {
		snapshot, err := transaction.Get(reference)
		if err != nil {
			return err
		}
		if snapshot.Data()["targetDevice"] != deviceID {
			return ErrForbidden
		}
		return transaction.Delete(reference)
	}))
}

func (s *FirestoreStore) CreateTransfer(ctx context.Context, transfer remote.Transfer) error {
	_, err := s.user(transfer.UserID).Collection("transfers").Doc(transfer.ID).Create(ctx, transfer)
	return translateFirestoreError(err)
}

func (s *FirestoreStore) GetTransfer(ctx context.Context, userID, id string) (remote.Transfer, error) {
	snapshot, err := s.user(userID).Collection("transfers").Doc(id).Get(ctx)
	if err != nil {
		return remote.Transfer{}, translateFirestoreError(err)
	}
	var transfer remote.Transfer
	if err = snapshot.DataTo(&transfer); err != nil {
		return remote.Transfer{}, err
	}
	return transfer, nil
}

func (s *FirestoreStore) UpdateTransfer(ctx context.Context, transfer remote.Transfer, expected remote.TransferState) error {
	reference := s.user(transfer.UserID).Collection("transfers").Doc(transfer.ID)
	err := s.client.RunTransaction(ctx, func(ctx context.Context, transaction *firestore.Transaction) error {
		snapshot, err := transaction.Get(reference)
		if err != nil {
			return err
		}
		var current remote.Transfer
		if err = snapshot.DataTo(&current); err != nil {
			return err
		}
		if current.State != expected {
			return ErrConflict
		}
		return transaction.Set(reference, transfer)
	})
	return translateFirestoreError(err)
}

func (s *FirestoreStore) ListTransfers(ctx context.Context, userID, deviceID string) ([]remote.Transfer, error) {
	documents := s.user(userID).Collection("transfers").OrderBy("createdAt", firestore.Desc).Limit(100).Documents(ctx)
	defer documents.Stop()
	var transfers []remote.Transfer
	for {
		snapshot, err := documents.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, err
		}
		var transfer remote.Transfer
		if err = snapshot.DataTo(&transfer); err != nil {
			return nil, err
		}
		if transfer.SourceDevice == deviceID || transfer.TargetDevice == deviceID {
			transfers = append(transfers, transfer)
		}
	}
	return transfers, nil
}

func (s *FirestoreStore) DailyTransferCount(ctx context.Context, userID string, since time.Time) (int, error) {
	documents := s.user(userID).Collection("transfers").Where("createdAt", ">=", since).Documents(ctx)
	defer documents.Stop()
	count := 0
	for {
		_, err := documents.Next()
		if errors.Is(err, iterator.Done) {
			return count, nil
		}
		if err != nil {
			return 0, err
		}
		count++
	}
}

func (s *FirestoreStore) DailyTransferBytes(ctx context.Context, userID string, since time.Time) (int64, error) {
	documents := s.user(userID).Collection("transfers").Where("createdAt", ">=", since).Documents(ctx)
	defer documents.Stop()
	var total int64
	for {
		snapshot, err := documents.Next()
		if errors.Is(err, iterator.Done) {
			return total, nil
		}
		if err != nil {
			return 0, err
		}
		if size, ok := snapshot.Data()["ciphertextSize"].(int64); ok {
			total += size
		}
	}
}

func cloudPairingID(first, second string) string {
	items := []string{first, second}
	sort.Strings(items)
	digest := sha256.Sum256([]byte(strings.Join(items, "\x00")))
	return hex.EncodeToString(digest[:])
}

func translateFirestoreError(err error) error {
	switch status.Code(err) {
	case codes.NotFound:
		return ErrNotFound
	case codes.AlreadyExists:
		return ErrConflict
	default:
		return err
	}
}
