package control

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/beyondmarks-ai/Wrapper/src/pkg/remote"
)

type staticVerifier struct{ user User }

func (v staticVerifier) Verify(_ context.Context, token string) (User, error) {
	if token != "valid-token" {
		return User{}, ErrForbidden
	}
	return v.user, nil
}

type allowAllInvites struct{}

func (allowAllInvites) Allowed(context.Context, User) (bool, error) { return true, nil }

type testDevice struct {
	device   remote.Device
	identity remote.Identity
}

func TestHealthEndpoint(t *testing.T) {
	server := NewServer(NewMemoryStore(), nil, nil, nil, nil)
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	require.JSONEq(t, `{"protocol":"1","status":"ok"}`, response.Body.String())
}

func TestPairingAndEncryptedEventFlow(t *testing.T) {
	store := NewMemoryStore()
	server := NewServer(store, nil, nil, staticVerifier{User{ID: "user-1", Email: "test@example.com"}}, allowAllInvites{})
	first := registerTestDevice(t, server, "Office PC")
	second := registerTestDevice(t, server, "Laptop")

	codeResponse := signedRequest(t, server, first, http.MethodPost, "/v1/pairings/code", nil, uuid.NewString())
	require.Equal(t, http.StatusCreated, codeResponse.Code)
	var code struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.NewDecoder(codeResponse.Body).Decode(&code))
	require.Len(t, code.Code, 8)

	claimResponse := signedRequest(t, server, second, http.MethodPost, "/v1/pairings/claim", map[string]string{"code": code.Code}, uuid.NewString())
	require.Equal(t, http.StatusCreated, claimResponse.Code)
	var pairing remote.Pairing
	require.NoError(t, json.NewDecoder(claimResponse.Body).Decode(&pairing))
	require.False(t, pairing.Active())

	confirmResponse := signedRequest(t, server, first, http.MethodPost, "/v1/pairings/"+pairing.ID+"/confirm", nil, uuid.NewString())
	require.Equal(t, http.StatusOK, confirmResponse.Code)
	require.NoError(t, json.NewDecoder(confirmResponse.Body).Decode(&pairing))
	require.True(t, pairing.Active())

	devicesResponse := signedRequest(t, server, first, http.MethodGet, "/v1/devices", nil, uuid.NewString())
	require.Equal(t, http.StatusOK, devicesResponse.Code)
	var devices []remote.Device
	require.NoError(t, json.NewDecoder(devicesResponse.Body).Decode(&devices))
	for _, listed := range devices {
		if listed.ID == second.device.ID {
			require.True(t, listed.Paired)
		}
	}
	recipient, err := second.identity.Recipient()
	require.NoError(t, err)
	ciphertext, err := first.identity.EncryptJSON(recipient, remote.SearchRequest{RequestID: "search-1", Query: "report", Mode: "file", Limit: 50})
	require.NoError(t, err)
	now := time.Now().UTC()
	event := remote.Envelope{
		Version: remote.ProtocolVersion, ID: uuid.NewString(), Kind: "search.request",
		SourceDevice: first.device.ID, TargetDevice: second.device.ID, Ciphertext: ciphertext,
		CreatedAt: now, ExpiresAt: now.Add(time.Minute),
	}
	require.NoError(t, first.identity.SignEnvelope(&event))
	eventResponse := signedRequest(t, server, first, http.MethodPost, "/v1/events", event, uuid.NewString())
	require.Equal(t, http.StatusAccepted, eventResponse.Code)

	listResponse := signedRequest(t, server, second, http.MethodGet, "/v1/events", nil, uuid.NewString())
	require.Equal(t, http.StatusOK, listResponse.Code)
	var events []remote.Envelope
	require.NoError(t, json.NewDecoder(listResponse.Body).Decode(&events))
	require.Len(t, events, 1)
	require.NoError(t, remote.VerifyEnvelope(events[0], first.device.SigningKey))
	var query remote.SearchRequest
	require.NoError(t, second.identity.DecryptJSON(events[0].Ciphertext, &query))
	require.Equal(t, "report", query.Query)
}

func TestSignedRequestCannotBeReplayed(t *testing.T) {
	server := NewServer(NewMemoryStore(), nil, nil, staticVerifier{User{ID: "user-1"}}, allowAllInvites{})
	device := registerTestDevice(t, server, "Office PC")
	nonce := uuid.NewString()
	first := signedRequest(t, server, device, http.MethodGet, "/v1/devices", nil, nonce)
	require.Equal(t, http.StatusOK, first.Code)
	second := signedRequest(t, server, device, http.MethodGet, "/v1/devices", nil, nonce)
	require.Equal(t, http.StatusUnauthorized, second.Code)
	require.Contains(t, second.Body.String(), "replayed_request")
}

func TestInvalidSignatureDoesNotConsumeNonce(t *testing.T) {
	server := NewServer(NewMemoryStore(), nil, nil, staticVerifier{User{ID: "user-1"}}, allowAllInvites{})
	device := registerTestDevice(t, server, "Office PC")
	wrongIdentity, err := remote.NewIdentity()
	require.NoError(t, err)
	attacker := device
	attacker.identity = wrongIdentity
	nonce := uuid.NewString()

	invalid := signedRequest(t, server, attacker, http.MethodGet, "/v1/devices", nil, nonce)
	require.Equal(t, http.StatusUnauthorized, invalid.Code)
	require.Contains(t, invalid.Body.String(), "invalid_device_signature")
	valid := signedRequest(t, server, device, http.MethodGet, "/v1/devices", nil, nonce)
	require.Equal(t, http.StatusOK, valid.Code)
}

func TestUnpairedDevicesCannotExchangeEvents(t *testing.T) {
	server := NewServer(NewMemoryStore(), nil, nil, staticVerifier{User{ID: "user-1"}}, allowAllInvites{})
	first := registerTestDevice(t, server, "Office PC")
	second := registerTestDevice(t, server, "Laptop")
	now := time.Now().UTC()
	event := remote.Envelope{
		Version: remote.ProtocolVersion, ID: uuid.NewString(), Kind: "search.request",
		SourceDevice: first.device.ID, TargetDevice: second.device.ID, Ciphertext: "opaque",
		CreatedAt: now, ExpiresAt: now.Add(time.Minute),
	}
	require.NoError(t, first.identity.SignEnvelope(&event))
	response := signedRequest(t, server, first, http.MethodPost, "/v1/events", event, uuid.NewString())
	require.Equal(t, http.StatusForbidden, response.Code)
}

func registerTestDevice(t *testing.T, server http.Handler, name string) testDevice {
	t.Helper()
	identity, err := remote.NewIdentity()
	require.NoError(t, err)
	recipient, err := identity.Recipient()
	require.NoError(t, err)
	signingKey, err := identity.SigningPublicKey()
	require.NoError(t, err)
	body, err := json.Marshal(map[string]string{"name": name, "ageRecipient": recipient, "signingKey": signingKey})
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/v1/devices", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer valid-token")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
	var device remote.Device
	require.NoError(t, json.NewDecoder(response.Body).Decode(&device))
	return testDevice{device: device, identity: identity}
}

func signedRequest(t *testing.T, server http.Handler, device testDevice, method, path string, value any, nonce string) *httptest.ResponseRecorder {
	t.Helper()
	var body []byte
	var err error
	if value != nil {
		body, err = json.Marshal(value)
		require.NoError(t, err)
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer valid-token")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Wrapper-Device", device.device.ID)
	timestamp := time.Now().UTC().Truncate(time.Second)
	request.Header.Set("X-Wrapper-Timestamp", timestamp.Format(time.RFC3339))
	request.Header.Set("X-Wrapper-Nonce", nonce)
	signature, err := device.identity.Sign(requestSignatureInput(request, body, timestamp, nonce))
	require.NoError(t, err)
	request.Header.Set("X-Wrapper-Signature", signature)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	return response
}

type fakeBlobs struct {
	uploadSize int64
	downloads  int
}

func (f *fakeBlobs) CreateUploadSession(_ context.Context, _ string, size int64, _ time.Time) (string, error) {
	f.uploadSize = size
	return "https://storage.example/upload", nil
}
func (f *fakeBlobs) DownloadURL(context.Context, string, time.Time) (string, error) {
	f.downloads++
	return "https://storage.example/download", nil
}
func (f *fakeBlobs) DeleteURL(context.Context, string, time.Time) (string, error) {
	return "https://storage.example/delete", nil
}
func (f *fakeBlobs) Delete(context.Context, string) error { return nil }

type fakeScheduler struct{ scheduled int }

func (f *fakeScheduler) ScheduleExpiration(context.Context, string, string, time.Time) error {
	f.scheduled++
	return nil
}

func pairTestDevices(t *testing.T, server http.Handler, first, second testDevice) {
	t.Helper()
	codeResponse := signedRequest(t, server, first, http.MethodPost, "/v1/pairings/code", nil, uuid.NewString())
	require.Equal(t, http.StatusCreated, codeResponse.Code)
	var code struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.NewDecoder(codeResponse.Body).Decode(&code))
	claimResponse := signedRequest(t, server, second, http.MethodPost, "/v1/pairings/claim", map[string]string{"code": code.Code}, uuid.NewString())
	require.Equal(t, http.StatusCreated, claimResponse.Code)
	var pairing remote.Pairing
	require.NoError(t, json.NewDecoder(claimResponse.Body).Decode(&pairing))
	confirmResponse := signedRequest(t, server, first, http.MethodPost, "/v1/pairings/"+pairing.ID+"/confirm", nil, uuid.NewString())
	require.Equal(t, http.StatusOK, confirmResponse.Code)
}

func updateTestTransfer(t *testing.T, server http.Handler, device testDevice, id string, state remote.TransferState, size int64, manifest string) *httptest.ResponseRecorder {
	t.Helper()
	return signedRequest(t, server, device, http.MethodPatch, "/v1/transfers/"+id, map[string]any{
		"state": state, "ciphertextSize": size, "encryptedManifest": manifest, "errorCode": "",
	}, uuid.NewString())
}

func TestTransferStateMachineAndMetadataAuthorization(t *testing.T) {
	store := NewMemoryStore()
	blobs := &fakeBlobs{}
	scheduler := &fakeScheduler{}
	server := NewServer(store, blobs, scheduler, staticVerifier{User{ID: "user-1"}}, allowAllInvites{})
	source := registerTestDevice(t, server, "Source")
	target := registerTestDevice(t, server, "Target")
	pairTestDevices(t, server, source, target)

	created := signedRequest(t, server, source, http.MethodPost, "/v1/transfers", map[string]string{
		"targetDevice": target.device.ID,
	}, uuid.NewString())
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	require.Equal(t, 1, scheduler.scheduled)
	var transfer remote.Transfer
	require.NoError(t, json.NewDecoder(created.Body).Decode(&transfer))

	response := updateTestTransfer(t, server, target, transfer.ID, remote.TransferPreparing, 0, "")
	require.Equal(t, http.StatusConflict, response.Code, response.Body.String())

	response = updateTestTransfer(t, server, source, transfer.ID, remote.TransferPreparing, 0, "")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	response = updateTestTransfer(t, server, source, transfer.ID, remote.TransferUploading, 10, "encrypted-manifest")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())

	wrongUpload := signedRequest(t, server, source, http.MethodPost, "/v1/transfers/"+transfer.ID+"/upload", map[string]int64{"size": 9}, uuid.NewString())
	require.Equal(t, http.StatusConflict, wrongUpload.Code, wrongUpload.Body.String())
	upload := signedRequest(t, server, source, http.MethodPost, "/v1/transfers/"+transfer.ID+"/upload", map[string]int64{"size": 10}, uuid.NewString())
	require.Equal(t, http.StatusOK, upload.Code, upload.Body.String())
	require.Equal(t, int64(10), blobs.uploadSize)

	earlyDownload := signedRequest(t, server, target, http.MethodPost, "/v1/transfers/"+transfer.ID+"/download", nil, uuid.NewString())
	require.Equal(t, http.StatusForbidden, earlyDownload.Code, earlyDownload.Body.String())

	response = updateTestTransfer(t, server, source, transfer.ID, remote.TransferWaiting, 10, "encrypted-manifest")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	response = updateTestTransfer(t, server, source, transfer.ID, remote.TransferDownloading, 10, "encrypted-manifest")
	require.Equal(t, http.StatusConflict, response.Code, response.Body.String())

	download := signedRequest(t, server, target, http.MethodPost, "/v1/transfers/"+transfer.ID+"/download", nil, uuid.NewString())
	require.Equal(t, http.StatusOK, download.Code, download.Body.String())
	require.Equal(t, 1, blobs.downloads)

	response = updateTestTransfer(t, server, target, transfer.ID, remote.TransferDownloading, 10, "tampered")
	require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
	response = updateTestTransfer(t, server, target, transfer.ID, remote.TransferDownloading, 10, "encrypted-manifest")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	response = updateTestTransfer(t, server, target, transfer.ID, remote.TransferVerifying, 10, "encrypted-manifest")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	response = updateTestTransfer(t, server, target, transfer.ID, remote.TransferCompleted, 10, "encrypted-manifest")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	response = updateTestTransfer(t, server, target, transfer.ID, remote.TransferFailed, 10, "encrypted-manifest")
	require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
}

func TestUnconfirmedPairingExpires(t *testing.T) {
	store := NewMemoryStore()
	created := time.Now().UTC().Add(-pairingCodeTTL - time.Second)
	pairing := remote.Pairing{
		ID: "pairing-1", UserID: "user-1", SourceDevice: "source", TargetDevice: "target",
		TargetConfirmed: true, CreatedAt: created,
	}
	store.pairings[pairingKey(pairing.UserID, pairing.SourceDevice, pairing.TargetDevice)] = pairing
	_, err := store.ConfirmPairing(context.Background(), pairing.UserID, pairing.ID, pairing.SourceDevice, time.Now().UTC())
	require.ErrorIs(t, err, ErrPairingExpired)
}

func TestDailyTransferCountUsesRollingWindow(t *testing.T) {
	store := NewMemoryStore()
	now := time.Now().UTC()
	require.NoError(t, store.CreateTransfer(t.Context(), remote.Transfer{ID: "recent", UserID: "user-1", CreatedAt: now.Add(-time.Hour)}))
	require.NoError(t, store.CreateTransfer(t.Context(), remote.Transfer{ID: "old", UserID: "user-1", CreatedAt: now.Add(-25 * time.Hour)}))
	count, err := store.DailyTransferCount(t.Context(), "user-1", now.Add(-24*time.Hour))
	require.NoError(t, err)
	require.Equal(t, 1, count)
}
