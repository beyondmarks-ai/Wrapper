package remoteclient

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type transferRoundTripFunc func(*http.Request) (*http.Response, error)

func (function transferRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestUploadResumableChunksAndProgress(t *testing.T) {
	data := make([]byte, uploadChunkSize+37)
	for index := range data {
		data[index] = byte(index % 251)
	}
	path := filepath.Join(t.TempDir(), "payload.age")
	require.NoError(t, os.WriteFile(path, data, 0o600))

	var received []byte
	requests := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		chunk, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		received = append(received, chunk...)
		if len(received) < len(data) {
			w.Header().Set("Range", fmt.Sprintf("bytes=0-%d", len(received)-1))
			w.WriteHeader(http.StatusPermanentRedirect)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	var done int64
	err := UploadResumable(context.Background(), server.Client(), server.URL, path, func(current, total int64) {
		done = current
		require.Equal(t, int64(len(data)), total)
	})
	require.NoError(t, err)
	require.Equal(t, data, received)
	require.Equal(t, int64(len(data)), done)
	require.Equal(t, 2, requests)
}

func TestDownloadResumesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "download.age")
	require.NoError(t, os.WriteFile(path, []byte("abc"), 0o600))
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "bytes=3-", r.Header.Get("Range"))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("def"))
	}))
	defer server.Close()

	require.NoError(t, DownloadResumable(context.Background(), server.Client(), server.URL, path, 6, nil))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, []byte("abcdef"), data)
}

func TestDownloadAlreadyCompleteSkipsNetwork(t *testing.T) {
	path := filepath.Join(t.TempDir(), "download.age")
	require.NoError(t, os.WriteFile(path, []byte("done"), 0o600))
	called := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer server.Close()

	require.NoError(t, DownloadResumable(context.Background(), server.Client(), server.URL, path, 4, nil))
	require.False(t, called)
}

func TestDownloadRetriesShortResponseWithRange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "download.age")
	requests := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			require.Empty(t, r.Header.Get("Range"))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte("abc"))
			return
		}
		require.Equal(t, "bytes=3-", r.Header.Get("Range"))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("def"))
	}))
	defer server.Close()

	require.NoError(t, DownloadResumable(t.Context(), server.Client(), server.URL, path, 6, nil))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, []byte("abcdef"), data)
	require.Equal(t, 2, requests)
}

func TestDownloadRejectsHTTPSDowngradeRedirect(t *testing.T) {
	called := false
	insecure := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer insecure.Close()
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, insecure.URL, http.StatusTemporaryRedirect)
	}))
	defer secure.Close()

	err := DownloadResumable(t.Context(), secure.Client(), secure.URL, filepath.Join(t.TempDir(), "download.age"), 1, nil)
	require.ErrorContains(t, err, "HTTPS")
	require.False(t, called)
}

func TestUploadStopsWhenResumeOffsetNeverAdvances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "payload.age")
	require.NoError(t, os.WriteFile(path, []byte("payload"), 0o600))
	client := &http.Client{Transport: transferRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("Content-Range") == "bytes */7" {
			return &http.Response{
				StatusCode: http.StatusPermanentRedirect, Status: "308 Permanent Redirect",
				Header: make(http.Header), Body: io.NopCloser(strings.NewReader("")), Request: request,
			}, nil
		}
		return nil, io.ErrUnexpectedEOF
	})}

	err := UploadResumable(t.Context(), client, "https://upload.example/session", path, nil)
	require.ErrorContains(t, err, "no progress")
}
