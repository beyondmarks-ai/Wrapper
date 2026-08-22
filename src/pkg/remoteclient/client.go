package remoteclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/beyondmarks-ai/Wrapper/src/pkg/remote"
)

type TokenProvider interface {
	Token(context.Context) (string, error)
}

type Client struct {
	baseURL    string
	httpClient *http.Client
	tokens     TokenProvider
	deviceID   string
	identity   remote.Identity
	now        func() time.Time
}

type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string { return e.Message }

func New(baseURL string, httpClient *http.Client, tokens TokenProvider, deviceID string, identity remote.Identity) (*Client, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return nil, errors.New("Wrapper Cloud API URL must be an absolute HTTPS URL")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 35 * time.Second}
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"), httpClient: httpClient, tokens: tokens,
		deviceID: deviceID, identity: identity, now: func() time.Time { return time.Now().UTC() },
	}, nil
}

func (c *Client) RegisterDevice(ctx context.Context, name, recipient, signingKey string) (remote.Device, error) {
	var result remote.Device
	err := c.do(ctx, http.MethodPost, "/v1/devices", map[string]string{
		"name": name, "ageRecipient": recipient, "signingKey": signingKey,
	}, &result, false)
	return result, err
}

func (c *Client) ListDevices(ctx context.Context) ([]remote.Device, error) {
	var result []remote.Device
	err := c.do(ctx, http.MethodGet, "/v1/devices", nil, &result, true)
	return result, err
}

func (c *Client) RevokeDevice(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/devices/"+url.PathEscape(id), nil, nil, true)
}

func (c *Client) CreatePairingCode(ctx context.Context) (string, time.Time, error) {
	var result struct {
		Code      string    `json:"code"`
		ExpiresAt time.Time `json:"expiresAt"`
	}
	err := c.do(ctx, http.MethodPost, "/v1/pairings/code", nil, &result, true)
	return result.Code, result.ExpiresAt, err
}

func (c *Client) ClaimPairing(ctx context.Context, code string) (remote.Pairing, error) {
	var result remote.Pairing
	err := c.do(ctx, http.MethodPost, "/v1/pairings/claim", map[string]string{"code": code}, &result, true)
	return result, err
}

func (c *Client) ConfirmPairing(ctx context.Context, id string) (remote.Pairing, error) {
	var result remote.Pairing
	err := c.do(ctx, http.MethodPost, "/v1/pairings/"+url.PathEscape(id)+"/confirm", nil, &result, true)
	return result, err
}

func (c *Client) SendEvent(ctx context.Context, event remote.Envelope) error {
	return c.do(ctx, http.MethodPost, "/v1/events", event, nil, true)
}

func (c *Client) PollEvents(ctx context.Context, cursor string, wait int) ([]remote.Envelope, error) {
	query := url.Values{"cursor": {cursor}, "wait": {fmt.Sprint(min(max(wait, 0), 25))}}
	var result []remote.Envelope
	err := c.do(ctx, http.MethodGet, "/v1/events?"+query.Encode(), nil, &result, true)
	return result, err
}

func (c *Client) AckEvent(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/events/"+url.PathEscape(id), nil, nil, true)
}

func (c *Client) CreateTransfer(ctx context.Context, target string) (remote.Transfer, error) {
	var result remote.Transfer
	err := c.do(ctx, http.MethodPost, "/v1/transfers", map[string]string{"targetDevice": target}, &result, true)
	return result, err
}

func (c *Client) ListTransfers(ctx context.Context) ([]remote.Transfer, error) {
	var result []remote.Transfer
	err := c.do(ctx, http.MethodGet, "/v1/transfers", nil, &result, true)
	return result, err
}

func (c *Client) GetTransfer(ctx context.Context, id string) (remote.Transfer, error) {
	var result remote.Transfer
	err := c.do(ctx, http.MethodGet, "/v1/transfers/"+url.PathEscape(id), nil, &result, true)
	return result, err
}

func (c *Client) UpdateTransfer(ctx context.Context, id string, state remote.TransferState, size int64,
	manifest, errorCode string,
) (remote.Transfer, error) {
	var result remote.Transfer
	err := c.do(ctx, http.MethodPatch, "/v1/transfers/"+url.PathEscape(id), map[string]any{
		"state": state, "ciphertextSize": size, "encryptedManifest": manifest, "errorCode": errorCode,
	}, &result, true)
	return result, err
}

func (c *Client) UploadSession(ctx context.Context, id string, size int64) (string, time.Time, error) {
	var result struct {
		URL       string    `json:"url"`
		ExpiresAt time.Time `json:"expiresAt"`
	}
	err := c.do(ctx, http.MethodPost, "/v1/transfers/"+url.PathEscape(id)+"/upload", map[string]int64{"size": size}, &result, true)
	return result.URL, result.ExpiresAt, err
}

func (c *Client) DownloadURL(ctx context.Context, id string) (string, time.Time, error) {
	var result struct {
		URL       string    `json:"url"`
		ExpiresAt time.Time `json:"expiresAt"`
	}
	err := c.do(ctx, http.MethodPost, "/v1/transfers/"+url.PathEscape(id)+"/download", nil, &result, true)
	return result.URL, result.ExpiresAt, err
}

func (c *Client) do(ctx context.Context, method, path string, value, target any, signed bool) error {
	body, err := encodeBody(value)
	if err != nil {
		return err
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(1<<attempt) * 250 * time.Millisecond):
			}
		}
		request, requestErr := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
		if requestErr != nil {
			return requestErr
		}
		request.Header.Set("Content-Type", "application/json")
		token, tokenErr := c.tokens.Token(ctx)
		if tokenErr != nil {
			return tokenErr
		}
		request.Header.Set("Authorization", "Bearer "+token)
		if signed {
			if err = c.signRequest(request, body); err != nil {
				return err
			}
		}
		response, requestErr := c.httpClient.Do(request)
		if requestErr != nil {
			lastErr = requestErr
			continue
		}
		err = decodeResponse(response, target)
		if err == nil {
			return nil
		}
		var apiError *APIError
		if !errors.As(err, &apiError) || apiError.Status < 500 {
			return err
		}
		lastErr = err
	}
	return fmt.Errorf("Wrapper Cloud request failed after retries: %w", lastErr)
}

func (c *Client) signRequest(request *http.Request, body []byte) error {
	if c.deviceID == "" {
		return errors.New("device is not registered")
	}
	timestamp := c.now().Truncate(time.Second)
	nonce := uuid.NewString()
	digest := sha256.Sum256(body)
	canonical := []byte(strings.Join([]string{
		request.Method, request.URL.EscapedPath(), request.URL.RawQuery,
		timestamp.Format(time.RFC3339), nonce, hex.EncodeToString(digest[:]),
	}, "\n"))
	signature, err := c.identity.Sign(canonical)
	if err != nil {
		return err
	}
	request.Header.Set("X-Wrapper-Device", c.deviceID)
	request.Header.Set("X-Wrapper-Timestamp", timestamp.Format(time.RFC3339))
	request.Header.Set("X-Wrapper-Nonce", nonce)
	request.Header.Set("X-Wrapper-Signature", signature)
	return nil
}

func encodeBody(value any) ([]byte, error) {
	if value == nil {
		return nil, nil
	}
	return json.Marshal(value)
}

func decodeResponse(response *http.Response, target any) error {
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var payload struct {
			Error struct {
				Code, Message string
			} `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&payload)
		if payload.Error.Message == "" {
			payload.Error.Message = "Wrapper Cloud returned " + response.Status
		}
		return &APIError{Status: response.StatusCode, Code: payload.Error.Code, Message: payload.Error.Message}
	}
	if target == nil || response.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(target)
}
