package localagent

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/beyondmarks-ai/Wrapper/src/pkg/agent"
	"github.com/beyondmarks-ai/Wrapper/src/pkg/remote"
)

type fakeService struct {
	requestDevice string
	requestPaths  []string
}

func (f *fakeService) Devices() []remote.Device {
	return []remote.Device{{ID: "b", Name: "Zulu"}, {ID: "a", Name: "Alpha"}}
}
func (f *fakeService) SearchRemote(context.Context, string, string, string, int) ([]remote.SearchResult, error) {
	return []remote.SearchResult{{Path: "C:\\Shared\\report.pdf"}}, nil
}
func (f *fakeService) RequestTransfer(_ context.Context, device string, paths []string, _ string) (string, error) {
	f.requestDevice, f.requestPaths = device, paths
	return "request-1", nil
}
func (f *fakeService) SendLocal(context.Context, string, []string) (remote.Transfer, error) {
	return remote.Transfer{ID: "transfer-1"}, nil
}
func (f *fakeService) Progress() []agent.Progress {
	return []agent.Progress{{TransferID: "transfer-1", Stage: remote.TransferUploading, Done: 5, Total: 10}}
}

func TestLocalAPIValidatesAndRoutesRequests(t *testing.T) {
	service := &fakeService{}
	server := httptest.NewServer(NewServer(service).http.Handler)
	defer server.Close()

	statusResponse, err := http.Get(server.URL + "/v1/status")
	require.NoError(t, err)
	defer statusResponse.Body.Close()
	var status StatusInfo
	require.NoError(t, json.NewDecoder(statusResponse.Body).Decode(&status))
	require.Equal(t, "ready", status.Status)
	require.Equal(t, remote.ProtocolVersion, status.Protocol)
	require.Equal(t, os.Getpid(), status.PID)
	response, err := http.Get(server.URL + "/v1/devices")
	require.NoError(t, err)
	defer response.Body.Close()
	var devices []remote.Device
	require.NoError(t, json.NewDecoder(response.Body).Decode(&devices))
	require.Equal(t, []string{"Alpha", "Zulu"}, []string{devices[0].Name, devices[1].Name})

	body := []byte(`{"deviceId":"a","paths":["C:\\\\Shared\\\\report.pdf"]}`)
	response, err = http.Post(server.URL+"/v1/transfers/request", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusAccepted, response.StatusCode)
	require.Equal(t, "a", service.requestDevice)
	require.Len(t, service.requestPaths, 1)

	response, err = http.Post(server.URL+"/v1/search", "application/json", bytes.NewBufferString(`{"deviceId":"a","query":"x","unknown":true}`))
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusBadRequest, response.StatusCode)
	require.Equal(t, "no-store", response.Header.Get("Cache-Control"))
}
