package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/mattermost/mattermost/server/public/plugin"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

type staticTokenSource struct {
	token *oauth2.Token
	err   error
}

func (s staticTokenSource) Token() (*oauth2.Token, error) {
	if s.err != nil {
		return nil, s.err
	}

	return s.token, nil
}

func TestServeHTTPRejectsUnauthorizedCreate(t *testing.T) {
	p := Plugin{}
	p.router = p.initRouter()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/create?channel_id=test-channel", nil)

	p.ServeHTTP(&plugin.Context{}, w, r)

	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.Equal(t, "Not authorized\n", w.Body.String())
}

func TestGoogleTokenEncryptedRoundTrip(t *testing.T) {
	api := &plugintest.API{}
	p := &Plugin{MattermostPlugin: plugin.MattermostPlugin{API: api}}
	p.setConfiguration(&configuration{EncryptionKey: "demo-encryption-key"})

	token := &oauth2.Token{
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		TokenType:    "Bearer",
		Expiry:       time.Unix(1700000000, 0).UTC(),
	}

	var stored []byte
	api.On("KVSet", "token_test-user", mock.AnythingOfType("[]uint8")).Run(func(args mock.Arguments) {
		stored = append([]byte(nil), args.Get(1).([]byte)...)
	}).Return(nil).Once()

	err := p.saveGoogleToken("test-user", token)
	require.NoError(t, err)
	require.NotEmpty(t, stored)
	require.NotContains(t, string(stored), token.AccessToken)

	api.On("KVGet", "token_test-user").Return(stored, nil).Once()

	decoded, err := p.getGoogleToken("test-user")
	require.NoError(t, err)
	require.Equal(t, token.AccessToken, decoded.AccessToken)
	require.Equal(t, token.RefreshToken, decoded.RefreshToken)
	require.Equal(t, token.TokenType, decoded.TokenType)
	require.True(t, token.Expiry.Equal(decoded.Expiry))

	api.AssertExpectations(t)
}

func TestGoogleTokenLegacyPayloadMigratesToEncryptedStorage(t *testing.T) {
	api := &plugintest.API{}
	p := &Plugin{MattermostPlugin: plugin.MattermostPlugin{API: api}}
	p.setConfiguration(&configuration{EncryptionKey: "demo-encryption-key"})

	legacyToken := &oauth2.Token{
		AccessToken:  "legacy-access",
		RefreshToken: "legacy-refresh",
		TokenType:    "Bearer",
		Expiry:       time.Unix(1700001000, 0).UTC(),
	}
	legacyData, err := json.Marshal(legacyToken)
	require.NoError(t, err)

	var migrated []byte
	api.On("KVGet", "token_test-user").Return(legacyData, nil).Once()
	api.On("KVSet", "token_test-user", mock.AnythingOfType("[]uint8")).Run(func(args mock.Arguments) {
		migrated = append([]byte(nil), args.Get(1).([]byte)...)
	}).Return(nil).Once()

	decoded, err := p.getGoogleToken("test-user")
	require.NoError(t, err)
	require.Equal(t, legacyToken.AccessToken, decoded.AccessToken)
	require.Equal(t, legacyToken.RefreshToken, decoded.RefreshToken)
	require.NotEmpty(t, migrated)
	require.NotEqual(t, string(legacyData), string(migrated))
	require.NotContains(t, string(migrated), legacyToken.AccessToken)

	api.AssertExpectations(t)
}

func TestGetOAuthLoginURLRequestsOfflineAccess(t *testing.T) {
	config := &oauth2.Config{
		ClientID:    "client-id",
		RedirectURL: "https://mattermost.example/plugins/com.mattermost.google-meet/oauth2/callback",
		Scopes:      []string{"scope-a"},
		Endpoint: oauth2.Endpoint{
			AuthURL: "https://accounts.example/auth",
		},
	}

	loginURL := getOAuthLoginURL(config, "test-user")
	parsedURL, err := url.Parse(loginURL)
	require.NoError(t, err)

	query := parsedURL.Query()
	require.Equal(t, "test-user", query.Get("state"))
	require.Equal(t, "offline", query.Get("access_type"))
	require.Equal(t, "consent", query.Get("prompt"))
}

func TestGoogleTokenNeedsReauthorization(t *testing.T) {
	t.Run("expired token without refresh token", func(t *testing.T) {
		token := &oauth2.Token{
			AccessToken: "access-token",
			Expiry:      time.Now().Add(-1 * time.Minute),
		}

		require.True(t, googleTokenNeedsReauthorization(token))
	})

	t.Run("expired token with refresh token", func(t *testing.T) {
		token := &oauth2.Token{
			AccessToken:  "access-token",
			RefreshToken: "refresh-token",
			Expiry:       time.Now().Add(-1 * time.Minute),
		}

		require.False(t, googleTokenNeedsReauthorization(token))
	})

	t.Run("valid token without refresh token", func(t *testing.T) {
		token := &oauth2.Token{
			AccessToken: "access-token",
			Expiry:      time.Now().Add(1 * time.Hour),
		}

		require.False(t, googleTokenNeedsReauthorization(token))
	})
}

func TestIsGoogleReauthorizationError(t *testing.T) {
	require.True(t, isGoogleReauthorizationError(errors.New("oauth2: token expired and refresh token is not set")))
	require.False(t, isGoogleReauthorizationError(errors.New("some other error")))
	require.False(t, isGoogleReauthorizationError(nil))
}

func TestRefreshGoogleToken(t *testing.T) {
	t.Run("returns current token when still fresh", func(t *testing.T) {
		api := &plugintest.API{}
		p := &Plugin{MattermostPlugin: plugin.MattermostPlugin{API: api}}
		p.setConfiguration(&configuration{EncryptionKey: "demo-encryption-key"})

		token := &oauth2.Token{
			AccessToken:  "access-token",
			RefreshToken: "refresh-token",
			TokenType:    "Bearer",
			Expiry:       time.Now().Add(2 * googleTokenRefreshBuffer),
		}

		originalFactory := googleTokenSourceFactory
		googleTokenSourceFactory = func(context.Context, *Plugin, *oauth2.Token) oauth2.TokenSource {
			t.Fatalf("token source should not be used for a fresh token")
			return nil
		}
		defer func() {
			googleTokenSourceFactory = originalFactory
		}()

		refreshed, err := p.refreshGoogleToken(context.Background(), "test-user", token)
		require.NoError(t, err)
		require.Same(t, token, refreshed)
	})

	t.Run("stores refreshed token when credentials change", func(t *testing.T) {
		api := &plugintest.API{}
		p := &Plugin{MattermostPlugin: plugin.MattermostPlugin{API: api}}
		p.setConfiguration(&configuration{EncryptionKey: "demo-encryption-key"})

		token := &oauth2.Token{
			AccessToken:  "old-access-token",
			RefreshToken: "refresh-token",
			TokenType:    "Bearer",
			Expiry:       time.Now().Add(1 * time.Minute),
		}
		updatedToken := &oauth2.Token{
			AccessToken:  "new-access-token",
			RefreshToken: "new-refresh-token",
			TokenType:    "Bearer",
			Expiry:       time.Now().Add(1 * time.Hour),
		}

		originalFactory := googleTokenSourceFactory
		googleTokenSourceFactory = func(context.Context, *Plugin, *oauth2.Token) oauth2.TokenSource {
			return staticTokenSource{token: updatedToken}
		}
		defer func() {
			googleTokenSourceFactory = originalFactory
		}()

		api.On("KVSet", "token_test-user", mock.AnythingOfType("[]uint8")).Return(nil).Once()

		refreshed, err := p.refreshGoogleToken(context.Background(), "test-user", token)
		require.NoError(t, err)
		require.Equal(t, updatedToken, refreshed)

		api.AssertExpectations(t)
	})

	t.Run("maps missing refresh token to reauthorization", func(t *testing.T) {
		api := &plugintest.API{}
		p := &Plugin{MattermostPlugin: plugin.MattermostPlugin{API: api}}
		p.setConfiguration(&configuration{EncryptionKey: "demo-encryption-key"})

		token := &oauth2.Token{
			AccessToken:  "access-token",
			RefreshToken: "refresh-token",
			TokenType:    "Bearer",
			Expiry:       time.Now().Add(1 * time.Minute),
		}

		originalFactory := googleTokenSourceFactory
		googleTokenSourceFactory = func(context.Context, *Plugin, *oauth2.Token) oauth2.TokenSource {
			return staticTokenSource{err: errors.New("oauth2: token expired and refresh token is not set")}
		}
		defer func() {
			googleTokenSourceFactory = originalFactory
		}()

		refreshed, err := p.refreshGoogleToken(context.Background(), "test-user", token)
		require.Nil(t, refreshed)
		require.ErrorIs(t, err, errGoogleReauthorizationRequired)
	})
}
