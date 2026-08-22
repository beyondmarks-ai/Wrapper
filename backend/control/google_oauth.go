package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxOAuthExchangeBody = 16 << 10

var ErrOAuthCodeRejected = errors.New("Google rejected the authorization code")

type GoogleCodeExchange struct {
	Code         string `json:"code"`
	CodeVerifier string `json:"codeVerifier"`
	RedirectURI  string `json:"redirectUri"`
}

type GoogleTokenExchanger interface {
	Exchange(context.Context, GoogleCodeExchange) (string, error)
}

type GoogleTokenExchangeClient struct {
	clientID     string
	clientSecret string
	httpClient   *http.Client
}

func NewGoogleTokenExchanger(clientID, clientSecret string, httpClient *http.Client) (*GoogleTokenExchangeClient, error) {
	if strings.TrimSpace(clientID) == "" || strings.TrimSpace(clientSecret) == "" {
		return nil, errors.New("Google OAuth client ID and client secret are required")
	}
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 20 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	return &GoogleTokenExchangeClient{clientID: clientID, clientSecret: clientSecret, httpClient: httpClient}, nil
}

func (c *GoogleTokenExchangeClient) Exchange(ctx context.Context, exchange GoogleCodeExchange) (string, error) {
	values := url.Values{
		"client_id": {c.clientID}, "client_secret": {c.clientSecret}, "code": {exchange.Code},
		"code_verifier": {exchange.CodeVerifier}, "grant_type": {"authorization_code"},
		"redirect_uri": {exchange.RedirectURI},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://oauth2.googleapis.com/token", strings.NewReader(values.Encode()))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("contact Google OAuth: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if err != nil {
		return "", fmt.Errorf("read Google OAuth response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", ErrOAuthCodeRejected
	}
	var token struct {
		IDToken string `json:"id_token"`
	}
	if err = json.Unmarshal(body, &token); err != nil || token.IDToken == "" {
		return "", errors.New("Google OAuth returned an invalid token response")
	}
	return token.IDToken, nil
}

func (s *Server) exchangeGoogleToken(response http.ResponseWriter, request *http.Request) {
	if s.googleExchange == nil {
		writeError(response, http.StatusServiceUnavailable, "authentication_unavailable", "Google sign-in is temporarily unavailable.")
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, maxOAuthExchangeBody)
	var exchange GoogleCodeExchange
	if !decodeJSON(response, request, &exchange) {
		return
	}
	if !validOAuthCode(exchange.Code) || !validPKCEVerifier(exchange.CodeVerifier) || !validLoopbackRedirect(exchange.RedirectURI) {
		writeError(response, http.StatusBadRequest, "invalid_oauth_exchange", "The Google sign-in response is invalid.")
		return
	}
	idToken, err := s.googleExchange.Exchange(request.Context(), exchange)
	if errors.Is(err, ErrOAuthCodeRejected) {
		writeError(response, http.StatusBadRequest, "oauth_exchange_failed", "Google rejected or expired the sign-in response.")
		return
	}
	if err != nil {
		writeError(response, http.StatusBadGateway, "oauth_service_error", "Google sign-in could not be completed. Retry shortly.")
		return
	}
	writeJSON(response, http.StatusOK, map[string]string{"idToken": idToken})
}

func validOAuthCode(code string) bool {
	return len(code) >= 8 && len(code) <= 4096 && !strings.ContainsAny(code, "\r\n\x00")
}

func validPKCEVerifier(verifier string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	for _, character := range verifier {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || strings.ContainsRune("-._~", character)) {
			return false
		}
	}
	return true
}

func validLoopbackRedirect(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" || parsed.Path != "/callback" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	if net.ParseIP(parsed.Hostname()) == nil {
		return false
	}
	port, err := strconv.Atoi(parsed.Port())
	return err == nil && port >= 1024 && port <= 65535
}
