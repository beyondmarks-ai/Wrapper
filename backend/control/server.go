package control

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"filippo.io/age"
	"github.com/google/uuid"

	"github.com/beyondmarks-ai/Wrapper/src/pkg/remote"
)

const (
	maxRequestBody     = 1 << 20
	pairingCodeTTL     = 10 * time.Minute
	eventMaxTTL        = 10 * time.Minute
	transferTTL        = 24 * time.Hour
	requestClockSkew   = 5 * time.Minute
	defaultEventLimit  = 100
	dailyTransferLimit = int64(50 << 30)
	maxDailyTransfers  = 100
)

type Server struct {
	store          Store
	blobs          BlobStore
	scheduler      ExpirationScheduler
	verifier       TokenVerifier
	invites        InviteChecker
	googleExchange GoogleTokenExchanger
	now            func() time.Time
	handler        http.Handler
}

type contextKey int

const (
	userContextKey contextKey = iota
	deviceContextKey
)

func NewServer(store Store, blobs BlobStore, scheduler ExpirationScheduler, verifier TokenVerifier,
	invites InviteChecker, googleExchanges ...GoogleTokenExchanger,
) *Server {
	var googleExchange GoogleTokenExchanger
	if len(googleExchanges) > 0 {
		googleExchange = googleExchanges[0]
	}
	server := &Server{
		store: store, blobs: blobs, scheduler: scheduler, verifier: verifier, invites: invites, googleExchange: googleExchange,
		now: func() time.Time { return time.Now().UTC() },
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", server.health)
	mux.HandleFunc("/health", server.health)
	mux.HandleFunc("POST /v1/auth/google/token", server.exchangeGoogleToken)
	mux.Handle("POST /v1/devices", server.authenticate(http.HandlerFunc(server.registerDevice)))
	mux.Handle("GET /v1/devices", server.authenticate(server.deviceAuthenticate(http.HandlerFunc(server.listDevices))))
	mux.Handle("DELETE /v1/devices/{id}", server.authenticate(server.deviceAuthenticate(http.HandlerFunc(server.revokeDevice))))
	mux.Handle("POST /v1/pairings/code", server.authenticate(server.deviceAuthenticate(http.HandlerFunc(server.createPairingCode))))
	mux.Handle("POST /v1/pairings/claim", server.authenticate(server.deviceAuthenticate(http.HandlerFunc(server.claimPairing))))
	mux.Handle("POST /v1/pairings/{id}/confirm", server.authenticate(server.deviceAuthenticate(http.HandlerFunc(server.confirmPairing))))
	mux.Handle("POST /v1/events", server.authenticate(server.deviceAuthenticate(http.HandlerFunc(server.appendEvent))))
	mux.Handle("GET /v1/events", server.authenticate(server.deviceAuthenticate(http.HandlerFunc(server.listEvents))))
	mux.Handle("DELETE /v1/events/{id}", server.authenticate(server.deviceAuthenticate(http.HandlerFunc(server.ackEvent))))
	mux.Handle("POST /v1/transfers", server.authenticate(server.deviceAuthenticate(http.HandlerFunc(server.createTransfer))))
	mux.Handle("GET /v1/transfers", server.authenticate(server.deviceAuthenticate(http.HandlerFunc(server.listTransfers))))
	mux.Handle("GET /v1/transfers/{id}", server.authenticate(server.deviceAuthenticate(http.HandlerFunc(server.getTransfer))))
	mux.Handle("PATCH /v1/transfers/{id}", server.authenticate(server.deviceAuthenticate(http.HandlerFunc(server.updateTransfer))))
	mux.Handle("POST /v1/transfers/{id}/upload", server.authenticate(server.deviceAuthenticate(http.HandlerFunc(server.createUpload))))
	mux.Handle("POST /v1/transfers/{id}/download", server.authenticate(server.deviceAuthenticate(http.HandlerFunc(server.createDownload))))
	server.handler = securityHeaders(requestIDMiddleware(recoverMiddleware(mux)))
	return server
}

func (s *Server) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	s.handler.ServeHTTP(response, request)
}

func (s *Server) health(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, map[string]string{"status": "ok", "protocol": remote.ProtocolVersion})
}

func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		authorization := request.Header.Get("Authorization")
		if !strings.HasPrefix(authorization, "Bearer ") {
			writeError(response, http.StatusUnauthorized, "authentication_required", "Sign in to Wrapper and try again.")
			return
		}
		user, err := s.verifier.Verify(request.Context(), strings.TrimPrefix(authorization, "Bearer "))
		if err != nil || user.ID == "" {
			writeError(response, http.StatusUnauthorized, "invalid_token", "Your sign-in expired. Sign in again.")
			return
		}
		if s.invites != nil {
			allowed, inviteErr := s.invites.Allowed(request.Context(), user)
			if inviteErr != nil {
				writeError(response, http.StatusServiceUnavailable, "invite_check_failed", "The beta allowlist could not be checked. Retry shortly.")
				return
			}
			if !allowed {
				writeError(response, http.StatusForbidden, "beta_invite_required", "This account is not invited to the private beta.")
				return
			}
		}
		next.ServeHTTP(response, request.WithContext(context.WithValue(request.Context(), userContextKey, user)))
	})
}

func (s *Server) deviceAuthenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		user := request.Context().Value(userContextKey).(User)
		deviceID := request.Header.Get("X-Wrapper-Device")
		device, err := s.store.GetDevice(request.Context(), user.ID, deviceID)
		if err != nil || !device.RevokedAt.IsZero() {
			writeError(response, http.StatusUnauthorized, "unknown_device", "This device is not registered or has been revoked.")
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(response, request.Body, maxRequestBody))
		if err != nil {
			writeError(response, http.StatusRequestEntityTooLarge, "request_too_large", "The control request is too large.")
			return
		}
		request.Body = io.NopCloser(strings.NewReader(string(body)))
		timestamp, err := time.Parse(time.RFC3339, request.Header.Get("X-Wrapper-Timestamp"))
		if err != nil || absDuration(s.now().Sub(timestamp)) > requestClockSkew {
			writeError(response, http.StatusUnauthorized, "stale_request", "The device clock is out of sync or the request expired.")
			return
		}
		nonce := request.Header.Get("X-Wrapper-Nonce")
		if len(nonce) < 16 || len(nonce) > 128 {
			writeError(response, http.StatusUnauthorized, "invalid_nonce", "The request nonce is invalid.")
			return
		}
		canonical := requestSignatureInput(request, body, timestamp, nonce)
		if err = remote.VerifySignature(canonical, request.Header.Get("X-Wrapper-Signature"), device.SigningKey); err != nil {
			writeError(response, http.StatusUnauthorized, "invalid_device_signature", "The device signature could not be verified.")
			return
		}
		used, err := s.store.UseNonce(request.Context(), device.ID, nonce, s.now().Add(requestClockSkew))
		if err != nil || !used {
			writeError(response, http.StatusUnauthorized, "replayed_request", "This request was already used. Retry the action.")
			return
		}
		device.LastSeen = s.now()
		device.Online = true
		_ = s.store.UpdateDevice(request.Context(), device)
		next.ServeHTTP(response, request.WithContext(context.WithValue(request.Context(), deviceContextKey, device)))
	})
}

func requestSignatureInput(request *http.Request, body []byte, timestamp time.Time, nonce string) []byte {
	digest := sha256.Sum256(body)
	return []byte(strings.Join([]string{
		request.Method, request.URL.EscapedPath(), request.URL.RawQuery,
		timestamp.UTC().Format(time.RFC3339), nonce, hex.EncodeToString(digest[:]),
	}, "\n"))
}

func (s *Server) registerDevice(response http.ResponseWriter, request *http.Request) {
	user := request.Context().Value(userContextKey).(User)
	var input struct {
		Name         string `json:"name"`
		AgeRecipient string `json:"ageRecipient"`
		SigningKey   string `json:"signingKey"`
	}
	if !decodeJSON(response, request, &input) {
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if len(input.Name) < 1 || len(input.Name) > 64 {
		writeError(response, http.StatusBadRequest, "invalid_device_name", "Device name must contain 1-64 characters.")
		return
	}
	if _, err := age.ParseX25519Recipient(input.AgeRecipient); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_encryption_key", "The device encryption key is invalid.")
		return
	}
	if decoded, err := base64.RawStdEncoding.DecodeString(input.SigningKey); err != nil || len(decoded) != 32 {
		writeError(response, http.StatusBadRequest, "invalid_signing_key", "The device signing key is invalid.")
		return
	}
	now := s.now()
	device := remote.Device{
		ID: uuid.NewString(), UserID: user.ID, Name: input.Name, AgeRecipient: input.AgeRecipient,
		SigningKey: input.SigningKey, Online: true, LastSeen: now, CreatedAt: now,
	}
	if err := s.store.CreateDevice(request.Context(), device); err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, device)
}

func (s *Server) listDevices(response http.ResponseWriter, request *http.Request) {
	user := request.Context().Value(userContextKey).(User)
	devices, err := s.store.ListDevices(request.Context(), user.ID)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	requestingDevice := request.Context().Value(deviceContextKey).(remote.Device)
	for index := range devices {
		devices[index].Online = devices[index].RevokedAt.IsZero() && s.now().Sub(devices[index].LastSeen) < 90*time.Second
		if devices[index].ID == requestingDevice.ID {
			devices[index].Paired = true
			continue
		}
		_, pairingErr := s.store.GetPairing(request.Context(), user.ID, requestingDevice.ID, devices[index].ID)
		devices[index].Paired = pairingErr == nil
	}
	writeJSON(response, http.StatusOK, devices)
}

func (s *Server) revokeDevice(response http.ResponseWriter, request *http.Request) {
	user := request.Context().Value(userContextKey).(User)
	device, err := s.store.GetDevice(request.Context(), user.ID, request.PathValue("id"))
	if err != nil {
		writeStoreError(response, err)
		return
	}
	device.RevokedAt = s.now()
	device.Online = false
	if err = s.store.UpdateDevice(request.Context(), device); err != nil {
		writeStoreError(response, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (s *Server) createPairingCode(response http.ResponseWriter, request *http.Request) {
	user := request.Context().Value(userContextKey).(User)
	device := request.Context().Value(deviceContextKey).(remote.Device)
	code, err := randomCode(8)
	if err != nil {
		writeError(response, http.StatusInternalServerError, "code_generation_failed", "A pairing code could not be generated.")
		return
	}
	hash := hashPairingCode(code)
	pairingCode := remote.PairingCode{
		Hash: hash, UserID: user.ID, DeviceID: device.ID,
		ExpiresAt: s.now().Add(pairingCodeTTL), CreatedAt: s.now(),
	}
	if err = s.store.CreatePairingCode(request.Context(), hash, pairingCode); err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, map[string]any{"code": code, "expiresAt": pairingCode.ExpiresAt})
}

func (s *Server) claimPairing(response http.ResponseWriter, request *http.Request) {
	user := request.Context().Value(userContextKey).(User)
	device := request.Context().Value(deviceContextKey).(remote.Device)
	var input struct {
		Code string `json:"code"`
	}
	if !decodeJSON(response, request, &input) {
		return
	}
	pairing, err := s.store.ClaimPairingCode(request.Context(), user.ID, hashPairingCode(input.Code), device.ID, s.now())
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, pairing)
}

func (s *Server) confirmPairing(response http.ResponseWriter, request *http.Request) {
	user := request.Context().Value(userContextKey).(User)
	device := request.Context().Value(deviceContextKey).(remote.Device)
	pairing, err := s.store.ConfirmPairing(request.Context(), user.ID, request.PathValue("id"), device.ID, s.now())
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, pairing)
}

func (s *Server) appendEvent(response http.ResponseWriter, request *http.Request) {
	user := request.Context().Value(userContextKey).(User)
	device := request.Context().Value(deviceContextKey).(remote.Device)
	var event remote.Envelope
	if !decodeJSON(response, request, &event) {
		return
	}
	now := s.now()
	_, eventIDErr := uuid.Parse(event.ID)
	if event.Version != remote.ProtocolVersion || eventIDErr != nil || !validEventKind(event.Kind) ||
		event.SourceDevice != device.ID || event.TargetDevice == device.ID || event.Ciphertext == "" ||
		event.CreatedAt.Before(now.Add(-requestClockSkew)) || event.CreatedAt.After(now.Add(requestClockSkew)) ||
		!event.ExpiresAt.After(now) || !event.ExpiresAt.After(event.CreatedAt) || event.ExpiresAt.After(now.Add(eventMaxTTL)) {
		writeError(response, http.StatusBadRequest, "invalid_event", "The encrypted device event is invalid or expired.")
		return
	}
	if _, err := s.store.GetPairing(request.Context(), user.ID, event.SourceDevice, event.TargetDevice); err != nil {
		writeError(response, http.StatusForbidden, "devices_not_paired", "The target device is not paired with this device.")
		return
	}
	if err := remote.VerifyEnvelope(event, device.SigningKey); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_event_signature", "The encrypted event signature is invalid.")
		return
	}
	if err := s.store.AppendEvent(request.Context(), user.ID, event); err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusAccepted, event)
}

func (s *Server) listEvents(response http.ResponseWriter, request *http.Request) {
	user := request.Context().Value(userContextKey).(User)
	device := request.Context().Value(deviceContextKey).(remote.Device)
	cursor := request.URL.Query().Get("cursor")
	waitSeconds, _ := strconv.Atoi(request.URL.Query().Get("wait"))
	waitSeconds = min(max(waitSeconds, 0), 25)
	deadline := s.now().Add(time.Duration(waitSeconds) * time.Second)
	for {
		events, err := s.store.ListEvents(request.Context(), user.ID, device.ID, cursor, defaultEventLimit)
		if err != nil {
			writeStoreError(response, err)
			return
		}
		if len(events) > 0 || !s.now().Before(deadline) {
			writeJSON(response, http.StatusOK, events)
			return
		}
		select {
		case <-request.Context().Done():
			return
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func (s *Server) ackEvent(response http.ResponseWriter, request *http.Request) {
	user := request.Context().Value(userContextKey).(User)
	device := request.Context().Value(deviceContextKey).(remote.Device)
	if err := s.store.AckEvent(request.Context(), user.ID, device.ID, request.PathValue("id")); err != nil {
		writeStoreError(response, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (s *Server) createTransfer(response http.ResponseWriter, request *http.Request) {
	user := request.Context().Value(userContextKey).(User)
	device := request.Context().Value(deviceContextKey).(remote.Device)
	var input struct {
		TargetDevice string `json:"targetDevice"`
	}
	if !decodeJSON(response, request, &input) {
		return
	}
	if _, err := s.store.GetPairing(request.Context(), user.ID, device.ID, input.TargetDevice); err != nil {
		writeError(response, http.StatusForbidden, "devices_not_paired", "The target device is not paired with this device.")
		return
	}
	now := s.now()
	transferCount, err := s.store.DailyTransferCount(request.Context(), user.ID, now.Add(-24*time.Hour))
	if err != nil {
		writeStoreError(response, err)
		return
	}
	if transferCount >= maxDailyTransfers {
		writeError(response, http.StatusTooManyRequests, "daily_transfer_count_exceeded", "The daily beta transfer-count limit has been reached.")
		return
	}
	transfer := remote.Transfer{
		ID: uuid.NewString(), UserID: user.ID, SourceDevice: device.ID, TargetDevice: input.TargetDevice,
		ObjectName: uuid.NewString(), State: remote.TransferRequested, CreatedAt: now, UpdatedAt: now,
		ExpiresAt: now.Add(transferTTL),
	}
	if err := s.store.CreateTransfer(request.Context(), transfer); err != nil {
		writeStoreError(response, err)
		return
	}
	if s.scheduler != nil && s.blobs != nil {
		deleteURL, deleteErr := s.blobs.DeleteURL(request.Context(), transfer.ObjectName, transfer.ExpiresAt.Add(time.Hour))
		if deleteErr == nil {
			deleteErr = s.scheduler.ScheduleExpiration(request.Context(), transfer.ID, deleteURL, transfer.ExpiresAt)
		}
		if deleteErr != nil {
			transfer.State = remote.TransferFailed
			transfer.ErrorCode = "expiration_schedule_failed"
			_ = s.store.UpdateTransfer(request.Context(), transfer, remote.TransferRequested)
			writeError(response, http.StatusServiceUnavailable, transfer.ErrorCode, "Secure cleanup could not be scheduled. Retry the transfer.")
			return
		}
	}
	writeJSON(response, http.StatusCreated, transfer)
}

func (s *Server) listTransfers(response http.ResponseWriter, request *http.Request) {
	user := request.Context().Value(userContextKey).(User)
	device := request.Context().Value(deviceContextKey).(remote.Device)
	transfers, err := s.store.ListTransfers(request.Context(), user.ID, device.ID)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, transfers)
}

func (s *Server) getTransfer(response http.ResponseWriter, request *http.Request) {
	transfer, ok := s.authorizedTransfer(response, request)
	if ok {
		writeJSON(response, http.StatusOK, transfer)
	}
}

func (s *Server) updateTransfer(response http.ResponseWriter, request *http.Request) {
	transfer, ok := s.authorizedTransfer(response, request)
	if !ok {
		return
	}
	device := request.Context().Value(deviceContextKey).(remote.Device)
	var input struct {
		State             remote.TransferState `json:"state"`
		CiphertextSize    int64                `json:"ciphertextSize"`
		EncryptedManifest string               `json:"encryptedManifest"`
		ErrorCode         string               `json:"errorCode"`
	}
	if !decodeJSON(response, request, &input) {
		return
	}
	if !validTransition(transfer.State, input.State, device.ID == transfer.SourceDevice) {
		writeError(response, http.StatusConflict, "invalid_transfer_state", "The transfer changed on another device. Refresh and retry.")
		return
	}
	if input.CiphertextSize < 0 || input.CiphertextSize > remote.MaxTransferSize+(1<<30) {
		writeError(response, http.StatusBadRequest, "transfer_too_large", "The encrypted transfer exceeds the 20 GB limit.")
		return
	}
	switch input.State {
	case remote.TransferUploading:
		if input.CiphertextSize <= 0 || input.EncryptedManifest == "" {
			writeError(response, http.StatusBadRequest, "invalid_transfer_metadata", "Encrypted transfer metadata is required before upload.")
			return
		}
		transfer.CiphertextSize = input.CiphertextSize
		transfer.EncryptedManifest = input.EncryptedManifest
	case remote.TransferFailed, remote.TransferCancelled:
		// Preserve the last authenticated size and manifest when either device reports failure.
	default:
		if input.CiphertextSize != transfer.CiphertextSize || input.EncryptedManifest != transfer.EncryptedManifest {
			writeError(response, http.StatusConflict, "immutable_transfer_metadata", "Encrypted transfer metadata cannot change after upload starts.")
			return
		}
	}
	previousState := transfer.State
	transfer.State = input.State
	if input.State == remote.TransferFailed || input.State == remote.TransferCancelled {
		transfer.ErrorCode = sanitizeErrorCode(input.ErrorCode)
	} else {
		transfer.ErrorCode = ""
	}
	transfer.UpdatedAt = s.now()
	if err := s.store.UpdateTransfer(request.Context(), transfer, previousState); err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, transfer)
}

func (s *Server) createUpload(response http.ResponseWriter, request *http.Request) {
	transfer, ok := s.authorizedTransfer(response, request)
	if !ok {
		return
	}
	device := request.Context().Value(deviceContextKey).(remote.Device)
	if device.ID != transfer.SourceDevice || s.blobs == nil || transfer.State != remote.TransferUploading {
		writeError(response, http.StatusForbidden, "upload_not_allowed", "Only the source device may upload a prepared transfer.")
		return
	}
	var input struct {
		Size int64 `json:"size"`
	}
	if !decodeJSON(response, request, &input) {
		return
	}
	if input.Size <= 0 || input.Size > remote.MaxTransferSize+(1<<30) {
		writeError(response, http.StatusBadRequest, "transfer_too_large", "The encrypted transfer exceeds the supported limit.")
		return
	}
	if input.Size != transfer.CiphertextSize {
		writeError(response, http.StatusConflict, "transfer_size_mismatch", "The upload size does not match the signed transfer metadata.")
		return
	}
	user := request.Context().Value(userContextKey).(User)
	usage, err := s.store.DailyTransferBytes(request.Context(), user.ID, s.now().Add(-24*time.Hour))
	if usage >= transfer.CiphertextSize {
		usage -= transfer.CiphertextSize
	}
	if err != nil || usage+input.Size > dailyTransferLimit {
		writeError(response, http.StatusTooManyRequests, "daily_quota_exceeded", "The 50 GB daily beta transfer quota has been reached.")
		return
	}
	url, err := s.blobs.CreateUploadSession(request.Context(), transfer.ObjectName, input.Size, s.now().Add(15*time.Minute))
	if err != nil {
		writeError(response, http.StatusServiceUnavailable, "upload_session_failed", "The resumable upload could not start. Retry shortly.")
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"url": url, "expiresAt": s.now().Add(15 * time.Minute)})
}

func (s *Server) createDownload(response http.ResponseWriter, request *http.Request) {
	transfer, ok := s.authorizedTransfer(response, request)
	if !ok {
		return
	}
	device := request.Context().Value(deviceContextKey).(remote.Device)
	if device.ID != transfer.TargetDevice || s.blobs == nil || !transfer.ExpiresAt.After(s.now()) ||
		!downloadableState(transfer.State) {
		writeError(response, http.StatusForbidden, "download_not_allowed", "This encrypted transfer is unavailable or expired.")
		return
	}
	url, err := s.blobs.DownloadURL(request.Context(), transfer.ObjectName, s.now().Add(10*time.Minute))
	if err != nil {
		writeError(response, http.StatusServiceUnavailable, "download_url_failed", "The secure download could not start. Retry shortly.")
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"url": url, "expiresAt": s.now().Add(10 * time.Minute)})
}

func downloadableState(state remote.TransferState) bool {
	return state == remote.TransferWaiting || state == remote.TransferDownloading || state == remote.TransferVerifying
}

func (s *Server) authorizedTransfer(response http.ResponseWriter, request *http.Request) (remote.Transfer, bool) {
	user := request.Context().Value(userContextKey).(User)
	device := request.Context().Value(deviceContextKey).(remote.Device)
	transfer, err := s.store.GetTransfer(request.Context(), user.ID, request.PathValue("id"))
	if err != nil {
		writeStoreError(response, err)
		return remote.Transfer{}, false
	}
	if transfer.SourceDevice != device.ID && transfer.TargetDevice != device.ID {
		writeError(response, http.StatusForbidden, "transfer_not_allowed", "This device is not part of the transfer.")
		return remote.Transfer{}, false
	}
	return transfer, true
}

func validTransition(from, to remote.TransferState, source bool) bool {
	if to == remote.TransferCancelled || to == remote.TransferFailed {
		return from != remote.TransferCompleted && from != remote.TransferExpired
	}
	if source {
		return from == remote.TransferRequested && to == remote.TransferPreparing ||
			from == remote.TransferPreparing && to == remote.TransferUploading ||
			from == remote.TransferUploading && to == remote.TransferWaiting
	}
	return from == remote.TransferWaiting && to == remote.TransferDownloading ||
		from == remote.TransferDownloading && to == remote.TransferVerifying ||
		from == remote.TransferVerifying && to == remote.TransferCompleted
}
func validEventKind(kind string) bool {
	switch kind {
	case "search.request", "search.response", "transfer.request", "transfer.ready":
		return true
	default:
		return false
	}
}

func randomCode(length int) (string, error) {
	const alphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	data := make([]byte, length)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	for index := range data {
		data[index] = alphabet[int(data[index])%len(alphabet)]
	}
	return string(data), nil
}

func hashPairingCode(code string) string {
	normalized := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(code), "-", ""))
	hash := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(hash[:])
}

func decodeJSON(response http.ResponseWriter, request *http.Request, target any) bool {
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_json", "The request body is invalid.")
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(response, http.StatusBadRequest, "invalid_json", "The request must contain one JSON value.")
		return false
	}
	return true
}

func writeStoreError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeError(response, http.StatusNotFound, "not_found", "The requested item no longer exists.")
	case errors.Is(err, ErrConflict):
		writeError(response, http.StatusConflict, "already_exists", "This request was already processed.")
	case errors.Is(err, ErrForbidden), errors.Is(err, ErrPairingExpired):
		writeError(response, http.StatusForbidden, "not_allowed", "The request is not allowed or has expired.")
	default:
		writeError(response, http.StatusInternalServerError, "internal_error", "Wrapper Cloud could not complete the request. Retry shortly.")
	}
}

func writeError(response http.ResponseWriter, status int, code, message string) {
	writeJSON(response, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Cache-Control", "no-store")
		response.Header().Set("X-Content-Type-Options", "nosniff")
		response.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(response, request)
	})
}

func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requestID := request.Header.Get("X-Request-ID")
		if _, err := uuid.Parse(requestID); err != nil {
			requestID = uuid.NewString()
		}
		response.Header().Set("X-Request-ID", requestID)
		next.ServeHTTP(response, request)
	})
}

func recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		defer func() {
			if recover() != nil {
				writeError(response, http.StatusInternalServerError, "internal_error", "Wrapper Cloud could not complete the request.")
			}
		}()
		next.ServeHTTP(response, request)
	})
}

func sanitizeErrorCode(code string) string {
	if len(code) > 64 {
		code = code[:64]
	}
	for _, character := range code {
		if !(character == '_' || character == '-' || character >= 'a' && character <= 'z' || character >= '0' && character <= '9') {
			return "transfer_failed"
		}
	}
	return code
}

func absDuration(value time.Duration) time.Duration {
	if value < 0 {
		return -value
	}
	return value
}
