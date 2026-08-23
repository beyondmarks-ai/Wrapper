package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/beyondmarks-ai/Wrapper/src/pkg/everything"
	"github.com/beyondmarks-ai/Wrapper/src/pkg/remote"
	"github.com/beyondmarks-ai/Wrapper/src/pkg/remoteclient"
)

type Progress struct {
	TransferID string
	Stage      remote.TransferState
	Done       int64
	Total      int64
	Err        error     `json:"-"`
	Error      string    `json:"error,omitempty"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type Config struct {
	Client      CloudClient
	Identity    remote.Identity
	Device      remote.Device
	Searcher    everything.Searcher
	SharedRoots []string
	DownloadDir string
	CacheDir    string
	HTTPClient  *http.Client
	Progress    func(Progress)
}

type CloudClient interface {
	ListDevices(context.Context) ([]remote.Device, error)
	PollEvents(context.Context, string, int) ([]remote.Envelope, error)
	AckEvent(context.Context, string) error
	SendEvent(context.Context, remote.Envelope) error
	CreateTransfer(context.Context, string) (remote.Transfer, error)
	GetTransfer(context.Context, string) (remote.Transfer, error)
	UpdateTransfer(context.Context, string, remote.TransferState, int64, string, string) (remote.Transfer, error)
	UploadSession(context.Context, string, int64) (string, time.Time, error)
	DownloadURL(context.Context, string) (string, time.Time, error)
}

type Agent struct {
	config           Config
	mu               sync.RWMutex
	devices          map[string]remote.Device
	pendingMu        sync.Mutex
	pendingSearch    map[string]chan remote.SearchResponse
	pendingTransfers map[string]string
	progress         map[string]Progress
	transferSlots    chan struct{}
}

func New(config Config) (*Agent, error) {
	if config.Client == nil || config.Device.ID == "" {
		return nil, errors.New("registered cloud client and device are required")
	}
	if config.Searcher == nil {
		return nil, errors.New("Everything search is required")
	}
	if config.DownloadDir == "" || config.CacheDir == "" {
		return nil, errors.New("download and cache directories are required")
	}
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{Timeout: 0}
	}
	for index, root := range config.SharedRoots {
		absolute, err := filepath.Abs(root)
		if err != nil {
			return nil, fmt.Errorf("resolve shared root: %w", err)
		}
		config.SharedRoots[index] = filepath.Clean(absolute)
	}
	return &Agent{
		config: config, devices: make(map[string]remote.Device),
		pendingSearch: make(map[string]chan remote.SearchResponse), pendingTransfers: make(map[string]string),
		progress:      make(map[string]Progress),
		transferSlots: make(chan struct{}, 2),
	}, nil
}

func (a *Agent) Run(ctx context.Context) error {
	if err := os.MkdirAll(a.config.CacheDir, 0o700); err != nil {
		return fmt.Errorf("create transfer cache: %w", err)
	}
	backoff := time.Second
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := a.refreshDevices(ctx); err != nil {
			a.report(Progress{Stage: remote.TransferFailed, Err: fmt.Errorf("refresh paired devices: %w", err)})
		}
		events, err := a.config.Client.PollEvents(ctx, "", 25)
		if err != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, 30*time.Second)
			continue
		}
		backoff = time.Second
		for _, event := range events {
			if err = a.handleEvent(ctx, event); err != nil {
				slog.Error("Remote event failed", "kind", event.Kind, "eventId", event.ID, "error", err)
				a.report(Progress{Stage: remote.TransferFailed, Err: err})
				if isTransient(err) {
					continue
				}
			}
			if err = a.config.Client.AckEvent(ctx, event.ID); err != nil {
				slog.Warn("Could not acknowledge remote event", "eventId", event.ID, "error", err)
			}
		}
	}
}

func (a *Agent) RequestSearch(ctx context.Context, targetDevice, query, mode string, limit int) (string, error) {
	requestID := uuid.NewString()
	request := remote.SearchRequest{RequestID: requestID, Query: query, Mode: mode, Limit: min(max(limit, 1), everything.DefaultMaxResults)}
	return requestID, a.sendEncryptedEvent(ctx, targetDevice, "search.request", request)
}

func (a *Agent) RequestTransfer(ctx context.Context, targetDevice string, paths []string, destination string) (string, error) {
	if err := validateTransferPaths(paths); err != nil {
		return "", err
	}
	if destination != "" {
		absolute, err := filepath.Abs(destination)
		if err != nil {
			return "", fmt.Errorf("resolve download destination: %w", err)
		}
		destination = filepath.Clean(absolute)
	}
	requestID := uuid.NewString()
	a.pendingMu.Lock()
	a.pendingTransfers[requestID] = destination
	a.pendingMu.Unlock()
	request := remote.TransferRequest{RequestID: requestID, Paths: paths}
	if err := a.sendEncryptedEvent(ctx, targetDevice, "transfer.request", request); err != nil {
		a.pendingMu.Lock()
		delete(a.pendingTransfers, requestID)
		a.pendingMu.Unlock()
		return "", err
	}
	return requestID, nil
}

func (a *Agent) SendLocal(ctx context.Context, targetDevice string, paths []string) (remote.Transfer, error) {
	return a.prepareAndUpload(ctx, targetDevice, paths, "", uuid.NewString(), false)
}

func (a *Agent) handleEvent(ctx context.Context, event remote.Envelope) error {
	device, err := a.getDevice(event.SourceDevice)
	if err != nil {
		return err
	}
	if err = remote.VerifyEnvelope(event, device.SigningKey); err != nil {
		return fmt.Errorf("verify source device: %w", err)
	}
	switch event.Kind {
	case "search.request":
		var request remote.SearchRequest
		if err = a.config.Identity.DecryptJSON(event.Ciphertext, &request); err != nil {
			return err
		}
		if _, err = uuid.Parse(request.RequestID); err != nil {
			return errors.New("invalid search request ID")
		}
		return a.handleSearch(ctx, event.SourceDevice, request)
	case "transfer.request":
		var request remote.TransferRequest
		if err = a.config.Identity.DecryptJSON(event.Ciphertext, &request); err != nil {
			return err
		}
		if _, err = uuid.Parse(request.RequestID); err != nil {
			return errors.New("invalid transfer request ID")
		}
		if err = validateTransferPaths(request.Paths); err != nil {
			return err
		}
		a.runBackgroundTransfer(ctx, func(runCtx context.Context) error {
			_, runErr := a.prepareAndUpload(runCtx, event.SourceDevice, request.Paths, "", request.RequestID, true)
			return runErr
		})
		return nil
	case "transfer.ready":
		var ready remote.TransferReady
		if err = a.config.Identity.DecryptJSON(event.Ciphertext, &ready); err != nil {
			return err
		}
		if _, err = uuid.Parse(ready.RequestID); err != nil {
			return errors.New("invalid transfer request ID")
		}
		if _, err = uuid.Parse(ready.TransferID); err != nil {
			return errors.New("invalid transfer ID")
		}
		// Only a destination recorded by this local agent is trusted. A paired source
		// cannot choose an arbitrary write location in a transfer.ready event.
		ready.DestinationPath = a.claimTransferDestination(ready.RequestID)
		a.runBackgroundTransfer(ctx, func(runCtx context.Context) error {
			return a.download(runCtx, ready)
		})
		return nil
	case "search.response":
		var response remote.SearchResponse
		if err = a.config.Identity.DecryptJSON(event.Ciphertext, &response); err != nil {
			return err
		}
		a.pendingMu.Lock()
		channel := a.pendingSearch[response.RequestID]
		a.pendingMu.Unlock()
		if channel != nil {
			select {
			case channel <- response:
			default:
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported remote event kind %q", event.Kind)
	}
}

const maxRemoteQueryLength = 512

func (a *Agent) handleSearch(ctx context.Context, target string, request remote.SearchRequest) error {
	response := remote.SearchResponse{RequestID: request.RequestID}
	query := strings.TrimSpace(request.Query)
	limit := min(max(request.Limit, 1), everything.DefaultMaxResults)
	if len(query) > maxRemoteQueryLength || strings.ContainsAny(query, "\"\r\n") {
		response.Error = "Search query is invalid or too long."
		return a.sendEncryptedEvent(ctx, target, "search.response", response)
	}
	if request.Mode != "" && request.Mode != "all" && request.Mode != "file" && request.Mode != "folder" {
		response.Error = "Search mode must be all, file, or folder."
		return a.sendEncryptedEvent(ctx, target, "search.response", response)
	}

	seen := make(map[string]struct{})
	for _, root := range a.config.SharedRoots {
		terms := make([]string, 0, 3)
		switch request.Mode {
		case "file":
			terms = append(terms, "file:")
		case "folder":
			terms = append(terms, "folder:")
		}
		// A quoted path ending in a separator limits Everything to this folder tree.
		terms = append(terms, "\""+filepath.Clean(root)+string(os.PathSeparator)+"\"")
		if query != "" {
			// Treat remote input as a literal filename term, never as Everything operators.
			terms = append(terms, "nopath:nowildcards:\""+query+"\"")
		}
		results, err := a.config.Searcher.Search(strings.Join(terms, " "), limit)
		if err != nil {
			response.Error = "Everything search failed on the source PC."
			break
		}
		for _, result := range results {
			if !a.isShared(result.Path) {
				continue
			}
			key := strings.ToLower(filepath.Clean(result.Path))
			if _, exists := seen[key]; exists {
				continue
			}
			info, statErr := os.Stat(result.Path)
			if statErr != nil {
				continue
			}
			seen[key] = struct{}{}
			response.Results = append(response.Results, remote.SearchResult{
				Path: result.Path, IsDir: info.IsDir(), Size: info.Size(), Modified: info.ModTime().UTC(),
			})
		}
	}
	sort.Slice(response.Results, func(i, j int) bool {
		return strings.ToLower(response.Results[i].Path) < strings.ToLower(response.Results[j].Path)
	})
	if len(response.Results) > limit {
		response.Results = response.Results[:limit]
	}
	return a.sendEncryptedEvent(ctx, target, "search.response", response)
}

const (
	maxTransferPaths = 100
	eventTTL         = 4 * time.Minute
)

func validateTransferPaths(paths []string) error {
	if len(paths) == 0 {
		return errors.New("at least one path is required")
	}
	if len(paths) > maxTransferPaths {
		return fmt.Errorf("at most %d paths may be transferred at once", maxTransferPaths)
	}
	for _, path := range paths {
		if strings.TrimSpace(path) == "" || len(path) > 32767 {
			return errors.New("transfer contains an invalid path")
		}
	}
	return nil
}

func (a *Agent) claimTransferDestination(requestID string) string {
	a.pendingMu.Lock()
	defer a.pendingMu.Unlock()
	destination := a.pendingTransfers[requestID]
	delete(a.pendingTransfers, requestID)
	return destination
}

func (a *Agent) prepareAndUpload(ctx context.Context, target string, paths []string, destination, requestID string, enforceShared bool) (transfer remote.Transfer, err error) {
	if len(paths) == 0 {
		return remote.Transfer{}, errors.New("at least one path is required")
	}
	for _, path := range paths {
		if enforceShared && !a.isShared(path) {
			return remote.Transfer{}, fmt.Errorf("selected path is outside shared roots")
		}
	}
	targetDevice, err := a.lookupDevice(ctx, target)
	if err != nil {
		return remote.Transfer{}, err
	}
	transfer, err = a.config.Client.CreateTransfer(ctx, target)
	if err != nil {
		return remote.Transfer{}, err
	}
	defer func() {
		if err == nil || transfer.ID == "" {
			return
		}
		a.report(Progress{TransferID: transfer.ID, Stage: remote.TransferFailed, Err: err})
		updateCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = a.config.Client.UpdateTransfer(updateCtx, transfer.ID, remote.TransferFailed, transfer.CiphertextSize, transfer.EncryptedManifest, "transfer_failed")
	}()
	a.report(Progress{TransferID: transfer.ID, Stage: remote.TransferPreparing})
	if _, err = a.config.Client.UpdateTransfer(ctx, transfer.ID, remote.TransferPreparing, 0, "", ""); err != nil {
		return transfer, err
	}
	tempFile, err := os.CreateTemp(a.config.CacheDir, "wrapper-upload-*.age")
	if err != nil {
		return transfer, err
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath) //nolint:errcheck
	manifest, buildErr := remote.BuildEncryptedPayload(ctx, transfer.ID, paths, targetDevice.AgeRecipient, tempFile)
	closeErr := tempFile.Close()
	if buildErr != nil {
		return transfer, buildErr
	}
	if closeErr != nil {
		return transfer, closeErr
	}
	info, err := os.Stat(tempPath)
	if err != nil {
		return transfer, err
	}
	encryptedManifest, err := a.config.Identity.EncryptJSON(targetDevice.AgeRecipient, manifest)
	if err != nil {
		return transfer, err
	}
	a.report(Progress{TransferID: transfer.ID, Stage: remote.TransferUploading, Total: info.Size()})
	if _, err = a.config.Client.UpdateTransfer(ctx, transfer.ID, remote.TransferUploading, info.Size(), encryptedManifest, ""); err != nil {
		return transfer, err
	}
	session, _, err := a.config.Client.UploadSession(ctx, transfer.ID, info.Size())
	if err != nil {
		return transfer, err
	}
	if err = remoteclient.UploadResumable(ctx, a.config.HTTPClient, session, tempPath, func(done, total int64) {
		a.report(Progress{TransferID: transfer.ID, Stage: remote.TransferUploading, Done: done, Total: total})
	}); err != nil {
		return transfer, err
	}
	transfer, err = a.config.Client.UpdateTransfer(ctx, transfer.ID, remote.TransferWaiting, info.Size(), encryptedManifest, "")
	if err != nil {
		return transfer, err
	}
	a.report(Progress{TransferID: transfer.ID, Stage: remote.TransferWaiting, Done: info.Size(), Total: info.Size()})
	err = a.sendEncryptedEvent(ctx, target, "transfer.ready", remote.TransferReady{
		RequestID: requestID, TransferID: transfer.ID, DestinationPath: destination,
	})
	return transfer, err
}

func (a *Agent) download(ctx context.Context, ready remote.TransferReady) (err error) {
	transfer, err := a.config.Client.GetTransfer(ctx, ready.TransferID)
	if err != nil {
		return err
	}
	defer func() {
		if err == nil {
			return
		}
		a.report(Progress{TransferID: transfer.ID, Stage: remote.TransferFailed, Err: err})
		updateCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = a.config.Client.UpdateTransfer(updateCtx, transfer.ID, remote.TransferFailed, transfer.CiphertextSize, transfer.EncryptedManifest, "transfer_failed")
	}()
	transfer, err = a.config.Client.UpdateTransfer(ctx, transfer.ID, remote.TransferDownloading,
		transfer.CiphertextSize, transfer.EncryptedManifest, "")
	if err != nil {
		return err
	}
	url, _, err := a.config.Client.DownloadURL(ctx, transfer.ID)
	if err != nil {
		return err
	}
	tempPath := filepath.Join(a.config.CacheDir, "wrapper-download-"+transfer.ID+".age")
	defer os.Remove(tempPath) //nolint:errcheck
	if err = remoteclient.DownloadResumable(ctx, a.config.HTTPClient, url, tempPath, transfer.CiphertextSize,
		func(done, total int64) {
			a.report(Progress{TransferID: transfer.ID, Stage: remote.TransferDownloading, Done: done, Total: total})
		}); err != nil {
		return err
	}
	if _, err = a.config.Client.UpdateTransfer(ctx, transfer.ID, remote.TransferVerifying,
		transfer.CiphertextSize, transfer.EncryptedManifest, ""); err != nil {
		return err
	}
	a.report(Progress{TransferID: transfer.ID, Stage: remote.TransferVerifying})
	var manifest remote.Manifest
	if err = a.config.Identity.DecryptJSON(transfer.EncryptedManifest, &manifest); err != nil {
		return err
	}
	if manifest.TransferID != transfer.ID {
		return remote.ErrIntegrity
	}
	payload, err := os.Open(tempPath)
	if err != nil {
		return err
	}
	destination := ready.DestinationPath
	if destination == "" {
		destination = a.config.DownloadDir
	}
	err = remote.ExtractEncryptedPayload(ctx, a.config.Identity, payload, destination, manifest, remote.ConflictKeepBoth)
	closeErr := payload.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	_, err = a.config.Client.UpdateTransfer(ctx, transfer.ID, remote.TransferCompleted,
		transfer.CiphertextSize, transfer.EncryptedManifest, "")
	if err == nil {
		a.report(Progress{TransferID: transfer.ID, Stage: remote.TransferCompleted, Done: transfer.CiphertextSize, Total: transfer.CiphertextSize})
	}
	return err
}

func (a *Agent) sendEncryptedEvent(ctx context.Context, target, kind string, value any) error {
	device, err := a.lookupDevice(ctx, target)
	if err != nil {
		return err
	}
	ciphertext, err := a.config.Identity.EncryptJSON(device.AgeRecipient, value)
	if err != nil {
		return err
	}
	// Firestore timestamps have microsecond precision. Sign the canonical value
	// that will survive storage so receivers verify the same envelope bytes.
	now := time.Now().UTC().Truncate(time.Microsecond)
	event := remote.Envelope{
		Version: remote.ProtocolVersion, ID: uuid.NewString(), Kind: kind,
		SourceDevice: a.config.Device.ID, TargetDevice: target, Ciphertext: ciphertext,
		CreatedAt: now, ExpiresAt: now.Add(eventTTL),
	}
	if err = a.config.Identity.SignEnvelope(&event); err != nil {
		return err
	}
	return a.config.Client.SendEvent(ctx, event)
}

func (a *Agent) refreshDevices(ctx context.Context) error {
	devices, err := a.config.Client.ListDevices(ctx)
	if err != nil {
		return err
	}
	next := make(map[string]remote.Device, len(devices))
	for _, device := range devices {
		next[device.ID] = device
	}
	a.mu.Lock()
	a.devices = next
	a.mu.Unlock()
	return nil
}

func (a *Agent) getDevice(id string) (remote.Device, error) {
	a.mu.RLock()
	device, ok := a.devices[id]
	a.mu.RUnlock()
	if !ok || !device.RevokedAt.IsZero() {
		return remote.Device{}, fmt.Errorf("paired device is unavailable")
	}
	return device, nil
}

func (a *Agent) lookupDevice(ctx context.Context, id string) (remote.Device, error) {
	device, err := a.getDevice(id)
	if err == nil {
		return device, nil
	}
	if refreshErr := a.refreshDevices(ctx); refreshErr != nil {
		return remote.Device{}, err
	}
	return a.getDevice(id)
}

func (a *Agent) isShared(path string) bool {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	absolute = filepath.Clean(absolute)
	for _, root := range a.config.SharedRoots {
		relative, relErr := filepath.Rel(root, absolute)
		if relErr == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative) {
			return true
		}
	}
	return false
}

func (a *Agent) runBackgroundTransfer(ctx context.Context, operation func(context.Context) error) {
	go func() {
		select {
		case a.transferSlots <- struct{}{}:
			defer func() { <-a.transferSlots }()
		case <-ctx.Done():
			return
		}
		if err := operation(ctx); err != nil {
			a.report(Progress{Stage: remote.TransferFailed, Err: err})
		}
	}()
}

func (a *Agent) report(progress Progress) {
	if progress.UpdatedAt.IsZero() {
		progress.UpdatedAt = time.Now().UTC()
	}
	if progress.Err != nil && progress.Error == "" {
		progress.Error = progress.Err.Error()
	}
	a.pendingMu.Lock()
	if progress.TransferID != "" {
		a.progress[progress.TransferID] = progress
	}
	a.pendingMu.Unlock()
	if a.config.Progress != nil {
		a.config.Progress(progress)
	}
}

func (a *Agent) SearchRemote(ctx context.Context, targetDevice, query, mode string, limit int) ([]remote.SearchResult, error) {
	requestID := uuid.NewString()
	channel := make(chan remote.SearchResponse, 1)
	a.pendingMu.Lock()
	a.pendingSearch[requestID] = channel
	a.pendingMu.Unlock()
	defer func() {
		a.pendingMu.Lock()
		delete(a.pendingSearch, requestID)
		a.pendingMu.Unlock()
	}()
	request := remote.SearchRequest{RequestID: requestID, Query: query, Mode: mode, Limit: min(max(limit, 1), everything.DefaultMaxResults)}
	if err := a.sendEncryptedEvent(ctx, targetDevice, "search.request", request); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case response := <-channel:
		if response.Error != "" {
			return nil, errors.New(response.Error)
		}
		return response.Results, nil
	}
}

func (a *Agent) Devices() []remote.Device {
	a.mu.RLock()
	defer a.mu.RUnlock()
	result := make([]remote.Device, 0, len(a.devices))
	for _, device := range a.devices {
		if device.ID != a.config.Device.ID && device.RevokedAt.IsZero() && device.Paired && device.Online {
			result = append(result, device)
		}
	}
	return result
}

func (a *Agent) Progress() []Progress {
	a.pendingMu.Lock()
	defer a.pendingMu.Unlock()
	result := make([]Progress, 0, len(a.progress))
	for _, progress := range a.progress {
		result = append(result, progress)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].UpdatedAt.Before(result[j].UpdatedAt) })
	return result
}
func isTransient(err error) bool {
	var apiError *remoteclient.APIError
	if errors.As(err, &apiError) {
		return apiError.Status >= 500 || apiError.Status == http.StatusTooManyRequests
	}
	var networkError net.Error
	return errors.As(err, &networkError) && (networkError.Timeout() || networkError.Temporary())
}
