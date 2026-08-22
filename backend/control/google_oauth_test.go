package control

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type googleExchangeFunc func(context.Context, GoogleCodeExchange) (string, error)

func (function googleExchangeFunc) Exchange(ctx context.Context, exchange GoogleCodeExchange) (string, error) {
	return function(ctx, exchange)
}

type oauthRoundTripFunc func(*http.Request) (*http.Response, error)

func (function oauthRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestGoogleTokenExchangeEndpoint(t *testing.T) {
	var received GoogleCodeExchange
	exchanger := googleExchangeFunc(func(_ context.Context, exchange GoogleCodeExchange) (string, error) {
		received = exchange
		return "google-id-token", nil
	})
	server := NewServer(NewMemoryStore(), nil, nil, nil, nil, exchanger)
	body := `{"code":"authorization-code","codeVerifier":"` + strings.Repeat("a", 43) + `","redirectUri":"http://127.0.0.1:49152/callback"}`
	request := httptest.NewRequest(http.MethodPost, "/v1/auth/google/token", strings.NewReader(body))
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	require.JSONEq(t, `{"idToken":"google-id-token"}`, response.Body.String())
	require.Equal(t, "authorization-code", received.Code)
	require.Equal(t, "http://127.0.0.1:49152/callback", received.RedirectURI)
}

func TestGoogleTokenExchangeRejectsUnsafeInput(t *testing.T) {
	called := false
	exchanger := googleExchangeFunc(func(context.Context, GoogleCodeExchange) (string, error) {
		called = true
		return "", nil
	})
	server := NewServer(NewMemoryStore(), nil, nil, nil, nil, exchanger)
	redirects := []string{
		"http://localhost:49152/callback",
		"http://127.0.0.1:80/callback",
		"http://127.0.0.1:49152/other",
		"https://127.0.0.1:49152/callback",
		"http://127.0.0.1:49152/callback?code=leak",
	}
	for _, redirect := range redirects {
		t.Run(redirect, func(t *testing.T) {
			body := `{"code":"authorization-code","codeVerifier":"` + strings.Repeat("a", 43) + `","redirectUri":"` + redirect + `"}`
			request := httptest.NewRequest(http.MethodPost, "/v1/auth/google/token", strings.NewReader(body))
			response := httptest.NewRecorder()
			server.ServeHTTP(response, request)
			require.Equal(t, http.StatusBadRequest, response.Code)
		})
	}
	require.False(t, called)
}

func TestGoogleTokenExchangeMapsUpstreamRejection(t *testing.T) {
	exchanger := googleExchangeFunc(func(context.Context, GoogleCodeExchange) (string, error) {
		return "", ErrOAuthCodeRejected
	})
	server := NewServer(NewMemoryStore(), nil, nil, nil, nil, exchanger)
	body := `{"code":"authorization-code","codeVerifier":"` + strings.Repeat("a", 43) + `","redirectUri":"http://127.0.0.1:49152/callback"}`
	request := httptest.NewRequest(http.MethodPost, "/v1/auth/google/token", strings.NewReader(body))
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Contains(t, response.Body.String(), "oauth_exchange_failed")
}

func TestGoogleTokenExchangerUsesSecretAndPKCE(t *testing.T) {
	client := &http.Client{Transport: oauthRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, "https://oauth2.googleapis.com/token", request.URL.String())
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		values, err := url.ParseQuery(string(body))
		require.NoError(t, err)
		require.Equal(t, "client-id", values.Get("client_id"))
		require.Equal(t, "server-only-secret", values.Get("client_secret"))
		require.Equal(t, strings.Repeat("v", 43), values.Get("code_verifier"))
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"id_token":"google-token"}`))}, nil
	})}
	exchanger, err := NewGoogleTokenExchanger("client-id", "server-only-secret", client)
	require.NoError(t, err)
	token, err := exchanger.Exchange(t.Context(), GoogleCodeExchange{Code: "auth-code", CodeVerifier: strings.Repeat("v", 43), RedirectURI: "http://127.0.0.1:49152/callback"})
	require.NoError(t, err)
	require.Equal(t, "google-token", token)
}

func TestGoogleTokenExchangerHidesUpstreamError(t *testing.T) {
	client := &http.Client{Transport: oauthRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusBadRequest, Status: "400 Bad Request", Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":"invalid_grant","error_description":"sensitive detail"}`))}, nil
	})}
	exchanger, err := NewGoogleTokenExchanger("client-id", "server-only-secret", client)
	require.NoError(t, err)
	_, err = exchanger.Exchange(t.Context(), GoogleCodeExchange{})
	require.True(t, errors.Is(err, ErrOAuthCodeRejected))
	require.NotContains(t, err.Error(), "sensitive detail")
}
