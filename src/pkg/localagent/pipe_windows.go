//go:build windows

package localagent

import (
	"context"
	"fmt"
	"net"
	"net/http"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

func pipeName() (string, string, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", "", err
	}
	sid := user.User.Sid.String()
	return `\\.\pipe\wrapper-agent-` + sid, sid, nil
}

func servePipe(ctx context.Context, server *http.Server) error {
	name, sid, err := pipeName()
	if err != nil {
		return err
	}
	listener, err := winio.ListenPipe(name, &winio.PipeConfig{
		SecurityDescriptor: fmt.Sprintf("D:P(A;;GA;;;%s)(A;;GA;;;SY)", sid),
		InputBufferSize:    64 << 10, OutputBufferSize: 64 << 10,
	})
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		_ = server.Shutdown(context.Background())
		_ = listener.Close()
	}()
	err = server.Serve(listener)
	if err == http.ErrServerClosed || ctx.Err() != nil {
		return nil
	}
	return err
}

func pipeTransport() http.RoundTripper {
	return &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		name, _, err := pipeName()
		if err != nil {
			return nil, err
		}
		return winio.DialPipeContext(ctx, name)
	}}
}
