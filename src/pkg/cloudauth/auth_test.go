package cloudauth

import (
	"encoding/json"
	"io"
	"net/http"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/beyondmarks-ai/Wrapper/src/pkg/securestore"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestManagerRefreshesAndPersistsFirebaseToken(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("DPAPI credential persistence is covered by the Windows CI job")
	}
	tokenPath := t.TempDir() + "/tokens.bin"
	initial := TokenSet{RefreshToken: "refresh-old", ExpiresAt: time.Now().Add(-time.Minute), Email: "test@example.com"}
	data, err := jsonMarshal(initial)
	require.NoError(t, err)
	require.NoError(t, securestore.Write(tokenPath, data))
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		require.Contains(t, request.URL.String(), "securetoken.googleapis.com")
		return &http.Response{
			StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader(`{"id_token":"id-new","refresh_token":"refresh-new","expires_in":"3600","user_id":"user-1"}`)),
		}, nil
	})}
	manager, err := NewManager(Config{
		GoogleClientID: "client", GoogleExchangeURL: "https://cloud.example/v1/auth/google/token", FirebaseAPIKey: "api-key", TokenPath: tokenPath, HTTPClient: client,
	})
	require.NoError(t, err)
	token, err := manager.Token(t.Context())
	require.NoError(t, err)
	require.Equal(t, "id-new", token)
	require.Equal(t, "user-1", manager.Status().UserID)

	reloaded, err := NewManager(Config{GoogleClientID: "client", GoogleExchangeURL: "https://cloud.example/v1/auth/google/token", FirebaseAPIKey: "api-key", TokenPath: tokenPath, HTTPClient: client})
	require.NoError(t, err)
	require.Equal(t, "user-1", reloaded.Status().UserID)
}

func jsonMarshal(value any) ([]byte, error) {
	return json.Marshal(value)
}

func TestGoogleCodeExchangeUsesWrapperBackend(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, "https://cloud.example/v1/auth/google/token", request.URL.String())
		require.Equal(t, "application/json", request.Header.Get("Content-Type"))
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		var exchange map[string]string
		require.NoError(t, json.Unmarshal(body, &exchange))
		require.Equal(t, "authorization-code", exchange["code"])
		require.Equal(t, "verifier", exchange["codeVerifier"])
		return &http.Response{
			StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader(`{"idToken":"google-id-token"}`)),
		}, nil
	})}
	manager, err := NewManager(Config{
		GoogleClientID: "client", GoogleExchangeURL: "https://cloud.example/v1/auth/google/token",
		FirebaseAPIKey: "api-key", TokenPath: t.TempDir() + "/tokens.bin", HTTPClient: client,
	})
	require.NoError(t, err)
	token, err := manager.exchangeGoogleCode(t.Context(), "authorization-code", "verifier", "http://127.0.0.1:49152/callback")
	require.NoError(t, err)
	require.Equal(t, "google-id-token", token.IDToken)
}

func TestManagerRejectsInsecureExchangeURL(t *testing.T) {
	_, err := NewManager(Config{
		GoogleClientID: "client", GoogleExchangeURL: "http://cloud.example/v1/auth/google/token",
		FirebaseAPIKey: "api-key", TokenPath: t.TempDir() + "/tokens.bin",
	})
	require.ErrorContains(t, err, "absolute HTTPS URL")
}
