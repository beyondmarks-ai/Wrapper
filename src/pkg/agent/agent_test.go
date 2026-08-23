package agent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/beyondmarks-ai/Wrapper/src/pkg/everything"
	"github.com/beyondmarks-ai/Wrapper/src/pkg/remote"
	"github.com/beyondmarks-ai/Wrapper/src/pkg/remoteclient"
)

type fakeSearcher struct{ results []everything.Result }

func (f fakeSearcher) Search(string, int) ([]everything.Result, error) { return f.results, nil }

type recordingSearcher struct {
	results []everything.Result
	queries []string
	limits  []int
}

func (f *recordingSearcher) Search(query string, limit int) ([]everything.Result, error) {
	f.queries = append(f.queries, query)
	f.limits = append(f.limits, limit)
	return f.results, nil
}

type fakeCloud struct {
	devices      []remote.Device
	sent         []remote.Envelope
	createCalled bool
	createErr    error
}

func (f *fakeCloud) ListDevices(context.Context) ([]remote.Device, error) { return f.devices, nil }
func (f *fakeCloud) PollEvents(context.Context, string, int) ([]remote.Envelope, error) {
	return nil, context.Canceled
}
func (f *fakeCloud) AckEvent(context.Context, string) error { return nil }
func (f *fakeCloud) SendEvent(_ context.Context, event remote.Envelope) error {
	f.sent = append(f.sent, event)
	return nil
}
func (f *fakeCloud) CreateTransfer(context.Context, string) (remote.Transfer, error) {
	f.createCalled = true
	return remote.Transfer{}, f.createErr
}
func (f *fakeCloud) GetTransfer(context.Context, string) (remote.Transfer, error) {
	return remote.Transfer{}, errors.New("unused")
}
func (f *fakeCloud) UpdateTransfer(context.Context, string, remote.TransferState, int64, string, string) (remote.Transfer, error) {
	return remote.Transfer{}, errors.New("unused")
}
func (f *fakeCloud) UploadSession(context.Context, string, int64) (string, time.Time, error) {
	return "", time.Time{}, errors.New("unused")
}
func (f *fakeCloud) DownloadURL(context.Context, string) (string, time.Time, error) {
	return "", time.Time{}, errors.New("unused")
}

func deviceFor(t *testing.T, id string, identity remote.Identity) remote.Device {
	t.Helper()
	recipient, err := identity.Recipient()
	require.NoError(t, err)
	signing, err := identity.SigningPublicKey()
	require.NoError(t, err)
	return remote.Device{ID: id, Name: id, AgeRecipient: recipient, SigningKey: signing}
}

func TestRemoteSearchOnlyReturnsSharedRoots(t *testing.T) {
	sourceIdentity, err := remote.NewIdentity()
	require.NoError(t, err)
	requesterIdentity, err := remote.NewIdentity()
	require.NoError(t, err)
	source := deviceFor(t, "source", sourceIdentity)
	requester := deviceFor(t, "requester", requesterIdentity)

	shared := t.TempDir()
	inside := filepath.Join(shared, "inside.txt")
	require.NoError(t, os.WriteFile(inside, []byte("inside"), 0o600))
	outside := filepath.Join(t.TempDir(), "outside.txt")
	require.NoError(t, os.WriteFile(outside, []byte("outside"), 0o600))
	cloud := &fakeCloud{devices: []remote.Device{source, requester}}
	searcher := &recordingSearcher{results: []everything.Result{{Path: inside}, {Path: outside}}}
	instance, err := New(Config{
		Client: cloud, Identity: sourceIdentity, Device: source,
		Searcher:    searcher,
		SharedRoots: []string{shared}, DownloadDir: t.TempDir(), CacheDir: t.TempDir(),
	})
	require.NoError(t, err)
	require.NoError(t, instance.refreshDevices(context.Background()))
	require.NoError(t, instance.handleSearch(context.Background(), requester.ID, remote.SearchRequest{
		RequestID: "search-1", Query: "txt", Mode: "file", Limit: 10,
	}))
	require.Len(t, cloud.sent, 1)
	require.Equal(t, "search.response", cloud.sent[0].Kind)
	var response remote.SearchResponse
	require.NoError(t, requesterIdentity.DecryptJSON(cloud.sent[0].Ciphertext, &response))
	require.Equal(t, "search-1", response.RequestID)
	require.Equal(t, []remote.SearchResult{{Path: inside, Size: 6, Modified: response.Results[0].Modified}}, response.Results)
	require.Equal(t, []int{10}, searcher.limits)
	require.Contains(t, searcher.queries[0], "file:")
	require.Contains(t, searcher.queries[0], "\""+filepath.Clean(shared)+string(os.PathSeparator)+"\"")
	require.Contains(t, searcher.queries[0], "nopath:nowildcards:\"txt\"")
}

func TestEncryptedEventLifetimeLeavesClockSkewMargin(t *testing.T) {
	sourceIdentity, err := remote.NewIdentity()
	require.NoError(t, err)
	targetIdentity, err := remote.NewIdentity()
	require.NoError(t, err)
	source := deviceFor(t, "source", sourceIdentity)
	target := deviceFor(t, "target", targetIdentity)
	cloud := &fakeCloud{devices: []remote.Device{source, target}}
	instance, err := New(Config{
		Client: cloud, Identity: sourceIdentity, Device: source, Searcher: fakeSearcher{},
		DownloadDir: t.TempDir(), CacheDir: t.TempDir(),
	})
	require.NoError(t, err)
	require.NoError(t, instance.refreshDevices(context.Background()))
	require.NoError(t, instance.sendEncryptedEvent(context.Background(), target.ID, "search.request", remote.SearchRequest{}))
	require.Len(t, cloud.sent, 1)
	event := cloud.sent[0]
	require.Equal(t, eventTTL, event.ExpiresAt.Sub(event.CreatedAt))
	require.Less(t, eventTTL, 5*time.Minute)
	require.Zero(t, event.CreatedAt.Nanosecond()%int(time.Microsecond))
	require.Zero(t, event.ExpiresAt.Nanosecond()%int(time.Microsecond))

	// Firestore truncates timestamps to microseconds. The stored envelope must
	// still verify after that round trip.
	event.CreatedAt = event.CreatedAt.Truncate(time.Microsecond)
	event.ExpiresAt = event.ExpiresAt.Truncate(time.Microsecond)
	require.NoError(t, remote.VerifyEnvelope(event, source.SigningKey))
}

func TestCryptographicEventFailuresAreNotRetriedForever(t *testing.T) {
	require.False(t, isTransient(remote.ErrInvalidSignature))
	require.True(t, isTransient(&remoteclient.APIError{Status: http.StatusServiceUnavailable}))
	require.False(t, isTransient(&remoteclient.APIError{Status: http.StatusBadRequest}))
}
func TestExplicitSendCanUseUnsharedPathButRemoteRequestCannot(t *testing.T) {
	sourceIdentity, err := remote.NewIdentity()
	require.NoError(t, err)
	targetIdentity, err := remote.NewIdentity()
	require.NoError(t, err)
	source := deviceFor(t, "source", sourceIdentity)
	target := deviceFor(t, "target", targetIdentity)
	selected := filepath.Join(t.TempDir(), "private.txt")
	require.NoError(t, os.WriteFile(selected, []byte("data"), 0o600))
	createErr := errors.New("create reached")
	cloud := &fakeCloud{devices: []remote.Device{source, target}, createErr: createErr}
	instance, err := New(Config{
		Client: cloud, Identity: sourceIdentity, Device: source, Searcher: fakeSearcher{},
		SharedRoots: []string{t.TempDir()}, DownloadDir: t.TempDir(), CacheDir: t.TempDir(),
	})
	require.NoError(t, err)
	require.NoError(t, instance.refreshDevices(context.Background()))

	_, err = instance.SendLocal(context.Background(), target.ID, []string{selected})
	require.ErrorIs(t, err, createErr)
	require.True(t, cloud.createCalled)

	cloud.createCalled = false
	_, err = instance.prepareAndUpload(context.Background(), target.ID, []string{selected}, "", "request", true)
	require.ErrorContains(t, err, "outside shared roots")
	require.False(t, cloud.createCalled)
}

func TestRemoteSearchRejectsOperatorsAndInvalidMode(t *testing.T) {
	sourceIdentity, err := remote.NewIdentity()
	require.NoError(t, err)
	requesterIdentity, err := remote.NewIdentity()
	require.NoError(t, err)
	source := deviceFor(t, "source", sourceIdentity)
	requester := deviceFor(t, "requester", requesterIdentity)
	cloud := &fakeCloud{devices: []remote.Device{source, requester}}
	searcher := &recordingSearcher{}
	instance, err := New(Config{
		Client: cloud, Identity: sourceIdentity, Device: source, Searcher: searcher,
		SharedRoots: []string{t.TempDir()}, DownloadDir: t.TempDir(), CacheDir: t.TempDir(),
	})
	require.NoError(t, err)
	require.NoError(t, instance.refreshDevices(context.Background()))

	require.NoError(t, instance.handleSearch(context.Background(), requester.ID, remote.SearchRequest{
		RequestID: "bad-query", Query: `x" | c:\`, Mode: "all", Limit: 0,
	}))
	require.NoError(t, instance.handleSearch(context.Background(), requester.ID, remote.SearchRequest{
		RequestID: "bad-mode", Query: "x", Mode: "regex", Limit: 99999,
	}))
	require.Empty(t, searcher.queries)
	require.Len(t, cloud.sent, 2)
	for _, event := range cloud.sent {
		var response remote.SearchResponse
		require.NoError(t, requesterIdentity.DecryptJSON(event.Ciphertext, &response))
		require.NotEmpty(t, response.Error)
	}
}

func TestTransferDestinationIsClaimedFromLocalState(t *testing.T) {
	instance := &Agent{pendingTransfers: map[string]string{"request-1": `C:\\trusted`}}
	require.Equal(t, `C:\\trusted`, instance.claimTransferDestination("request-1"))
	require.Empty(t, instance.claimTransferDestination("request-1"))
	require.Empty(t, instance.claimTransferDestination("peer-controlled"))
}

func TestTransferPathCountIsBounded(t *testing.T) {
	paths := make([]string, maxTransferPaths+1)
	for index := range paths {
		paths[index] = fmt.Sprintf("file-%d", index)
	}
	require.ErrorContains(t, validateTransferPaths(paths), "at most")
	require.Error(t, validateTransferPaths([]string{""}))
}
