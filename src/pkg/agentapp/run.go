package agentapp

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	variable "github.com/beyondmarks-ai/Wrapper/src/config"
	"github.com/beyondmarks-ai/Wrapper/src/pkg/agent"
	"github.com/beyondmarks-ai/Wrapper/src/pkg/cloudauth"
	"github.com/beyondmarks-ai/Wrapper/src/pkg/cloudstate"
	"github.com/beyondmarks-ai/Wrapper/src/pkg/everything"
	"github.com/beyondmarks-ai/Wrapper/src/pkg/localagent"
	"github.com/beyondmarks-ai/Wrapper/src/pkg/remote"
	"github.com/beyondmarks-ai/Wrapper/src/pkg/remoteclient"
)

func Run(ctx context.Context) error {
	state, err := cloudstate.Load(variable.CloudStateFile)
	if err != nil {
		return fmt.Errorf("load cloud device: %w", err)
	}
	identity, err := remote.LoadIdentity(state.IdentityPath)
	if err != nil {
		return fmt.Errorf("load protected device identity: %w", err)
	}
	authManager, err := cloudauth.NewManager(cloudauth.Config{
		GoogleClientID: variable.GoogleClientID(), GoogleExchangeURL: strings.TrimRight(state.APIURL, "/") + "/v1/auth/google/token",
		FirebaseAPIKey: variable.FirebaseAPIKey(), TokenPath: variable.CloudTokensFile,
	})
	if err != nil {
		return err
	}
	controlClient, err := remoteclient.New(state.APIURL, controlHTTPClient(), authManager, state.Device.ID, identity)
	if err != nil {
		return err
	}
	searcher, err := everything.New()
	if err != nil {
		return fmt.Errorf("start Everything search: %w", err)
	}
	backgroundAgent, err := agent.New(agent.Config{
		Client: controlClient, Identity: identity, Device: state.Device, Searcher: searcher,
		SharedRoots: state.SharedRoots, DownloadDir: state.DownloadDir, CacheDir: variable.CloudCacheDir,
		HTTPClient: transferHTTPClient(), Progress: logProgress,
	})
	if err != nil {
		return err
	}
	slog.Info("Wrapper agent started", "deviceId", state.Device.ID, "sharedRootCount", len(state.SharedRoots))
	group, runCtx := errgroup.WithContext(ctx)
	group.Go(func() error { return backgroundAgent.Run(runCtx) })
	group.Go(func() error { return localagent.NewServer(backgroundAgent).Serve(runCtx) })
	return group.Wait()
}

func controlHTTPClient() *http.Client {
	return &http.Client{Timeout: 35 * time.Second, Transport: hardenedTransport()}
}

func transferHTTPClient() *http.Client {
	return &http.Client{Transport: hardenedTransport()}
}

func hardenedTransport() *http.Transport {
	return &http.Transport{
		Proxy:             http.ProxyFromEnvironment,
		DialContext:       (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2: true, MaxIdleConns: 20, MaxIdleConnsPerHost: 10,
		IdleConnTimeout: 90 * time.Second, TLSHandshakeTimeout: 10 * time.Second,
		ExpectContinueTimeout: time.Second, ResponseHeaderTimeout: 35 * time.Second,
	}
}

func logProgress(progress agent.Progress) {
	attributes := []any{"transferId", progress.TransferID, "stage", progress.Stage}
	if progress.Total > 0 {
		attributes = append(attributes, "done", progress.Done, "total", progress.Total)
	}
	if progress.Err != nil {
		attributes = append(attributes, "error", progress.Err)
	}
	slog.Info("Transfer progress", attributes...)
}
