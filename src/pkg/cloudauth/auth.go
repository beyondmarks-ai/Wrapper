package cloudauth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/beyondmarks-ai/Wrapper/src/pkg/securestore"
)

type Config struct {
	GoogleClientID    string
	GoogleExchangeURL string
	FirebaseAPIKey    string
	TokenPath         string
	HTTPClient        *http.Client
	OpenBrowser       func(string) error
}

type TokenSet struct {
	IDToken      string    `json:"idToken"`
	RefreshToken string    `json:"refreshToken"`
	ExpiresAt    time.Time `json:"expiresAt"`
	UserID       string    `json:"userId"`
	Email        string    `json:"email"`
}

type Manager struct {
	config Config
	mu     sync.Mutex
	tokens TokenSet
}

func NewManager(config Config) (*Manager, error) {
	if config.GoogleClientID == "" || config.GoogleExchangeURL == "" || config.FirebaseAPIKey == "" || config.TokenPath == "" {
		return nil, errors.New("Google OAuth client ID, exchange URL, Firebase API key, and token path are required")
	}
	exchangeURL, err := url.Parse(config.GoogleExchangeURL)
	if err != nil || exchangeURL.Scheme != "https" || exchangeURL.Host == "" || exchangeURL.User != nil || exchangeURL.RawQuery != "" || exchangeURL.Fragment != "" {
		return nil, errors.New("Google exchange URL must be an absolute HTTPS URL")
	}
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{
			Timeout:       20 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	}
	if config.OpenBrowser == nil {
		config.OpenBrowser = openBrowser
	}
	manager := &Manager{config: config}
	if data, err := securestore.Read(config.TokenPath); err == nil {
		_ = json.Unmarshal(data, &manager.tokens)
	}
	return manager, nil
}

func (m *Manager) Login(ctx context.Context) (TokenSet, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return TokenSet{}, fmt.Errorf("start local sign-in callback: %w", err)
	}
	defer listener.Close()
	redirectURI := "http://" + listener.Addr().String() + "/callback"
	state, err := randomURLToken(24)
	if err != nil {
		return TokenSet{}, err
	}
	verifier, err := randomURLToken(48)
	if err != nil {
		return TokenSet{}, err
	}
	challengeDigest := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(challengeDigest[:])
	result := make(chan loginResult, 1)
	sendResult := func(login loginResult) {
		select {
		case result <- login:
		default:
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /callback", func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("state") != state {
			http.Error(response, "The Wrapper sign-in state is invalid. Return to Wrapper and retry.", http.StatusBadRequest)
			return
		}
		if oauthError := request.URL.Query().Get("error"); oauthError != "" {
			http.Error(response, "Google sign-in was cancelled. You may close this tab.", http.StatusBadRequest)
			sendResult(loginResult{err: fmt.Errorf("Google sign-in: %s", oauthError)})
			return
		}
		code := request.URL.Query().Get("code")
		if code == "" {
			http.Error(response, "Google did not return an authorization code.", http.StatusBadRequest)
			sendResult(loginResult{err: errors.New("Google did not return an authorization code")})
			return
		}
		_, _ = io.WriteString(response, "Wrapper is connected. You may close this tab and return to the terminal.")
		sendResult(loginResult{code: code})
	})
	callbackServer := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go callbackServer.Serve(listener)                   //nolint:errcheck // Shutdown and callback result handle lifecycle.
	defer callbackServer.Shutdown(context.Background()) //nolint:errcheck

	authorizeURL := "https://accounts.google.com/o/oauth2/v2/auth?" + url.Values{
		"client_id": {m.config.GoogleClientID}, "redirect_uri": {redirectURI}, "response_type": {"code"},
		"scope": {"openid email profile"}, "state": {state}, "code_challenge": {challenge},
		"code_challenge_method": {"S256"}, "access_type": {"offline"}, "prompt": {"consent"},
	}.Encode()
	if err = m.config.OpenBrowser(authorizeURL); err != nil {
		return TokenSet{}, fmt.Errorf("open Google sign-in: %w", err)
	}
	select {
	case <-ctx.Done():
		return TokenSet{}, ctx.Err()
	case login := <-result:
		if login.err != nil {
			return TokenSet{}, login.err
		}
		googleToken, exchangeErr := m.exchangeGoogleCode(ctx, login.code, verifier, redirectURI)
		if exchangeErr != nil {
			return TokenSet{}, exchangeErr
		}
		tokens, firebaseErr := m.exchangeFirebaseToken(ctx, googleToken.IDToken, redirectURI)
		if firebaseErr != nil {
			return TokenSet{}, firebaseErr
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		m.tokens = tokens
		if err = m.saveLocked(); err != nil {
			return TokenSet{}, err
		}
		return tokens, nil
	}
}

func (m *Manager) Token(ctx context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.tokens.RefreshToken == "" {
		return "", errors.New("not signed in; run 'wrap auth login'")
	}
	if m.tokens.IDToken != "" && time.Until(m.tokens.ExpiresAt) > 2*time.Minute {
		return m.tokens.IDToken, nil
	}
	refreshed, err := m.refresh(ctx, m.tokens.RefreshToken)
	if err != nil {
		return "", err
	}
	if refreshed.RefreshToken == "" {
		refreshed.RefreshToken = m.tokens.RefreshToken
	}
	m.tokens = refreshed
	if err = m.saveLocked(); err != nil {
		return "", err
	}
	return refreshed.IDToken, nil
}

func (m *Manager) Status() TokenSet {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := m.tokens
	result.IDToken = ""
	result.RefreshToken = ""
	return result
}

func (m *Manager) Logout() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tokens = TokenSet{}
	return securestore.Write(m.config.TokenPath, []byte("{}"))
}

type loginResult struct {
	code string
	err  error
}

type googleTokenResponse struct {
	IDToken string `json:"idToken"`
	Error   string `json:"error"`
}

func (m *Manager) exchangeGoogleCode(ctx context.Context, code, verifier, redirectURI string) (googleTokenResponse, error) {
	payload, err := json.Marshal(map[string]string{
		"code": code, "codeVerifier": verifier, "redirectUri": redirectURI,
	})
	if err != nil {
		return googleTokenResponse{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, m.config.GoogleExchangeURL, bytes.NewReader(payload))
	if err != nil {
		return googleTokenResponse{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	var response googleTokenResponse
	if err = m.doJSON(request, &response); err != nil {
		return response, fmt.Errorf("exchange Google authorization: %w", err)
	}
	if response.Error != "" || response.IDToken == "" {
		return response, fmt.Errorf("Google token exchange failed: %s", response.Error)
	}
	return response, nil
}

func (m *Manager) exchangeFirebaseToken(ctx context.Context, googleIDToken, redirectURI string) (TokenSet, error) {
	payload, err := json.Marshal(map[string]any{
		"postBody":   "id_token=" + url.QueryEscape(googleIDToken) + "&providerId=google.com",
		"requestUri": redirectURI, "returnIdpCredential": true, "returnSecureToken": true,
	})
	if err != nil {
		return TokenSet{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://identitytoolkit.googleapis.com/v1/accounts:signInWithIdp?key="+url.QueryEscape(m.config.FirebaseAPIKey), bytes.NewReader(payload))
	if err != nil {
		return TokenSet{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	var response struct {
		IDToken      string         `json:"idToken"`
		RefreshToken string         `json:"refreshToken"`
		ExpiresIn    string         `json:"expiresIn"`
		LocalID      string         `json:"localId"`
		Email        string         `json:"email"`
		Error        map[string]any `json:"error"`
	}
	if err = m.doJSON(request, &response); err != nil {
		return TokenSet{}, fmt.Errorf("sign in to Firebase: %w", err)
	}
	expires, _ := strconv.Atoi(response.ExpiresIn)
	return TokenSet{
		IDToken: response.IDToken, RefreshToken: response.RefreshToken,
		ExpiresAt: time.Now().UTC().Add(time.Duration(expires) * time.Second),
		UserID:    response.LocalID, Email: response.Email,
	}, nil
}

func (m *Manager) refresh(ctx context.Context, refreshToken string) (TokenSet, error) {
	values := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}}
	var response struct {
		IDToken      string `json:"id_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    string `json:"expires_in"`
		UserID       string `json:"user_id"`
	}
	if err := m.postForm(ctx, "https://securetoken.googleapis.com/v1/token?key="+url.QueryEscape(m.config.FirebaseAPIKey), values, &response); err != nil {
		return TokenSet{}, fmt.Errorf("refresh Firebase sign-in: %w", err)
	}
	expires, _ := strconv.Atoi(response.ExpiresIn)
	return TokenSet{
		IDToken: response.IDToken, RefreshToken: response.RefreshToken,
		ExpiresAt: time.Now().UTC().Add(time.Duration(expires) * time.Second), UserID: response.UserID,
		Email: m.tokens.Email,
	}, nil
}

func (m *Manager) postForm(ctx context.Context, endpoint string, values url.Values, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return m.doJSON(request, target)
}

func (m *Manager) doJSON(request *http.Request, target any) error {
	response, err := m.config.HTTPClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("authentication service returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(response.Body).Decode(target)
}

func (m *Manager) saveLocked() error {
	data, err := json.Marshal(m.tokens)
	if err != nil {
		return err
	}
	return securestore.Write(m.config.TokenPath, data)
}

func randomURLToken(size int) (string, error) {
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}
