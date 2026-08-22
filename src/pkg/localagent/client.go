package localagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/beyondmarks-ai/Wrapper/src/pkg/agent"
	"github.com/beyondmarks-ai/Wrapper/src/pkg/remote"
)

type Client struct{ http *http.Client }

func NewClient() *Client {
	return &Client{http: &http.Client{Transport: pipeTransport(), Timeout: 35 * time.Second}}
}

func (c *Client) Status(ctx context.Context) error {
	return c.do(ctx, http.MethodGet, "/v1/status", nil, nil)
}

type StatusInfo struct {
	Status   string `json:"status"`
	Protocol string `json:"protocol"`
	PID      int    `json:"pid"`
}

func (c *Client) StatusInfo(ctx context.Context) (StatusInfo, error) {
	var result StatusInfo
	err := c.do(ctx, http.MethodGet, "/v1/status", nil, &result)
	return result, err
}

func (c *Client) Devices(ctx context.Context) ([]remote.Device, error) {
	var result []remote.Device
	err := c.do(ctx, http.MethodGet, "/v1/devices", nil, &result)
	return result, err
}

func (c *Client) Search(ctx context.Context, input SearchInput) ([]remote.SearchResult, error) {
	var result []remote.SearchResult
	err := c.do(ctx, http.MethodPost, "/v1/search", input, &result)
	return result, err
}

func (c *Client) RequestTransfer(ctx context.Context, input TransferInput) (string, error) {
	var result map[string]string
	err := c.do(ctx, http.MethodPost, "/v1/transfers/request", input, &result)
	return result["requestId"], err
}

func (c *Client) Send(ctx context.Context, input TransferInput) (string, error) {
	var result map[string]string
	err := c.do(ctx, http.MethodPost, "/v1/transfers/send", input, &result)
	return result["jobId"], err
}

func (c *Client) Progress(ctx context.Context) ([]agent.Progress, error) {
	var result []agent.Progress
	err := c.do(ctx, http.MethodGet, "/v1/progress", nil, &result)
	return result, err
}

func (c *Client) do(ctx context.Context, method, path string, value, target any) error {
	var body []byte
	var err error
	if value != nil {
		body, err = json.Marshal(value)
		if err != nil {
			return err
		}
	}
	request, err := http.NewRequestWithContext(ctx, method, "http://wrapper-agent"+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("Wrapper Agent is unavailable: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var failure map[string]string
		_ = json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&failure)
		return fmt.Errorf("%s", failure["error"])
	}
	if target == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(target)
}
