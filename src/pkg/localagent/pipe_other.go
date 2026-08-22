//go:build !windows

package localagent

import (
	"context"
	"fmt"
	"net/http"
)

func servePipe(context.Context, *http.Server) error {
	return fmt.Errorf("Wrapper Agent local IPC is currently supported only on Windows")
}

func pipeTransport() http.RoundTripper { return unsupportedTransport{} }

type unsupportedTransport struct{}

func (unsupportedTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("Wrapper Agent local IPC is currently supported only on Windows")
}
