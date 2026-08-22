package localagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/beyondmarks-ai/Wrapper/src/pkg/agent"
	"github.com/beyondmarks-ai/Wrapper/src/pkg/remote"
)

type Service interface {
	Devices() []remote.Device
	SearchRemote(context.Context, string, string, string, int) ([]remote.SearchResult, error)
	RequestTransfer(context.Context, string, []string, string) (string, error)
	SendLocal(context.Context, string, []string) (remote.Transfer, error)
	Progress() []agent.Progress
}

type Server struct {
	service Service
	http    *http.Server
}

type SearchInput struct {
	DeviceID string `json:"deviceId"`
	Query    string `json:"query"`
	Mode     string `json:"mode"`
	Limit    int    `json:"limit"`
}

type TransferInput struct {
	DeviceID    string   `json:"deviceId"`
	Paths       []string `json:"paths"`
	Destination string   `json:"destination,omitempty"`
}

func NewServer(service Service) *Server {
	server := &Server{service: service}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/status", server.status)
	mux.HandleFunc("GET /v1/devices", server.devices)
	mux.HandleFunc("POST /v1/search", server.search)
	mux.HandleFunc("POST /v1/transfers/request", server.requestTransfer)
	mux.HandleFunc("POST /v1/transfers/send", server.sendTransfer)
	mux.HandleFunc("GET /v1/progress", server.progress)
	server.http = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 35 * time.Second, WriteTimeout: 35 * time.Second}
	return server
}

func (s *Server) Serve(ctx context.Context) error { return servePipe(ctx, s.http) }

func (s *Server) status(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, map[string]any{"status": "ready", "protocol": remote.ProtocolVersion, "pid": os.Getpid()})
}

func (s *Server) devices(response http.ResponseWriter, _ *http.Request) {
	devices := s.service.Devices()
	sort.Slice(devices, func(i, j int) bool { return devices[i].Name < devices[j].Name })
	writeJSON(response, http.StatusOK, devices)
}

func (s *Server) search(response http.ResponseWriter, request *http.Request) {
	var input SearchInput
	if !decode(response, request, &input) {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 30*time.Second)
	defer cancel()
	results, err := s.service.SearchRemote(ctx, input.DeviceID, input.Query, input.Mode, min(max(input.Limit, 1), 100))
	if err != nil {
		writeFailure(response, err)
		return
	}
	writeJSON(response, http.StatusOK, results)
}

func (s *Server) requestTransfer(response http.ResponseWriter, request *http.Request) {
	var input TransferInput
	if !decode(response, request, &input) {
		return
	}
	requestID, err := s.service.RequestTransfer(request.Context(), input.DeviceID, input.Paths, input.Destination)
	if err != nil {
		writeFailure(response, err)
		return
	}
	writeJSON(response, http.StatusAccepted, map[string]string{"requestId": requestID})
}

func (s *Server) sendTransfer(response http.ResponseWriter, request *http.Request) {
	var input TransferInput
	if !decode(response, request, &input) {
		return
	}
	jobID := uuid.NewString()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 48*time.Hour)
		defer cancel()
		_, _ = s.service.SendLocal(ctx, input.DeviceID, input.Paths)
	}()
	writeJSON(response, http.StatusAccepted, map[string]string{"jobId": jobID})
}

func (s *Server) progress(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, s.service.Progress())
}

func decode(response http.ResponseWriter, request *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "Invalid local agent request."})
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "Only one JSON request is allowed."})
		return false
	}
	return true
}

func writeFailure(response http.ResponseWriter, err error) {
	writeJSON(response, http.StatusBadGateway, map[string]string{
		"error": fmt.Sprintf("%s Retry the action or check 'wrap agent status'.", err),
	})
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}
