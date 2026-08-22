package remoteclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/beyondmarks-ai/Wrapper/src/pkg/remote"
)

type staticTokens struct{}

func (staticTokens) Token(context.Context) (string, error) { return "firebase-token", nil }

func TestSignedRequestRetriesWithFreshNonce(t *testing.T) {
	identity, err := remote.NewIdentity()
	require.NoError(t, err)
	publicKey, err := identity.SigningPublicKey()
	require.NoError(t, err)

	var mu sync.Mutex
	nonces := make(map[string]bool)
	attempts := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer firebase-token", r.Header.Get("Authorization"))
		require.Equal(t, "device-a", r.Header.Get("X-Wrapper-Device"))
		digest := sha256.Sum256(nil)
		canonical := strings.Join([]string{
			r.Method,
			r.URL.EscapedPath(),
			r.URL.RawQuery,
			r.Header.Get("X-Wrapper-Timestamp"),
			r.Header.Get("X-Wrapper-Nonce"),
			hex.EncodeToString(digest[:]),
		}, "\n")
		require.NoError(t, remote.VerifySignature([]byte(canonical), r.Header.Get("X-Wrapper-Signature"), publicKey))
		mu.Lock()
		require.False(t, nonces[r.Header.Get("X-Wrapper-Nonce")], "retry reused a nonce")
		nonces[r.Header.Get("X-Wrapper-Nonce")] = true
		attempts++
		current := attempts
		mu.Unlock()
		if current == 1 {
			http.Error(w, "retry", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, "[]")
	}))
	defer server.Close()

	client, err := New(server.URL, server.Client(), staticTokens{}, "device-a", identity)
	require.NoError(t, err)
	client.now = func() time.Time { return time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC) }

	devices, err := client.ListDevices(context.Background())
	require.NoError(t, err)
	require.Empty(t, devices)
	require.Equal(t, 2, attempts)
}

func TestClientAndTransferURLsRequireHTTPS(t *testing.T) {
	identity, err := remote.NewIdentity()
	require.NoError(t, err)
	_, err = New("http://api.example.com", nil, staticTokens{}, "device-a", identity)
	require.ErrorContains(t, err, "HTTPS")
	require.ErrorContains(t, UploadResumable(context.Background(), http.DefaultClient, "http://upload.example.com", "missing", nil), "HTTPS")
	require.ErrorContains(t, DownloadResumable(context.Background(), http.DefaultClient, "http://download.example.com", "missing", 0, nil), "HTTPS")
}
