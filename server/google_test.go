package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mattermost/mattermost/server/public/plugin"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

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