package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

const (
	googleOAuthCallbackRelPath = "/oauth2/callback"
)

type storedGoogleToken struct {
	EncryptedToken string        `json:"encrypted_token,omitempty"`
	Token          *oauth2.Token `json:"token,omitempty"`
}

func (p *Plugin) getOAuthConfig() *oauth2.Config {
	config := p.getConfiguration()
	return &oauth2.Config{
		ClientID:     config.GoogleClientID,
		ClientSecret: config.GoogleClientSecret,
		Endpoint:     google.Endpoint,
		RedirectURL:  fmt.Sprintf("%s/plugins/%s%s", *p.client.Configuration.GetConfig().ServiceSettings.SiteURL, manifest.Id, googleOAuthCallbackRelPath),
		Scopes:       []string{calendar.CalendarEventsScope},
	}
}

func (p *Plugin) getGoogleClient(ctx context.Context, token *oauth2.Token) *http.Client {
	return p.getOAuthConfig().Client(ctx, token)
}

func (p *Plugin) createMeeting(userID string) (string, error) {
	token, err := p.getGoogleToken(userID)
	if err != nil {
		return "", err
	}

	ctx := context.Background()
	client := p.getGoogleClient(ctx, token)

	srv, err := calendar.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return "", fmt.Errorf("unable to retrieve Calendar client: %v", err)
	}

	now := time.Now().UTC()
	event := &calendar.Event{
		Summary:     "Mattermost Meeting",
		Description: "Meeting created from Mattermost",
		Start: &calendar.EventDateTime{
			DateTime: now.Format(time.RFC3339),
			TimeZone: "UTC",
		},
		End: &calendar.EventDateTime{
			DateTime: now.Add(1 * time.Hour).Format(time.RFC3339),
			TimeZone: "UTC",
		},
		ConferenceData: &calendar.ConferenceData{
			CreateRequest: &calendar.CreateConferenceRequest{
				RequestId: fmt.Sprintf("mm-%s-%d", userID, now.UnixMilli()),
				ConferenceSolutionKey: &calendar.ConferenceSolutionKey{
					Type: "hangoutsMeet",
				},
			},
		},
	}

	event, err = srv.Events.Insert("primary", event).ConferenceDataVersion(1).Do()
	if err != nil {
		return "", fmt.Errorf("unable to create event: %v", err)
	}

	return event.HangoutLink, nil
}

func (p *Plugin) saveGoogleToken(userID string, token *oauth2.Token) error {
	data, err := p.encodeStoredGoogleToken(token)
	if err != nil {
		return err
	}
	if appErr := p.API.KVSet(fmt.Sprintf("token_%s", userID), data); appErr != nil {
		return fmt.Errorf("kv set token_%s: %s", userID, appErr.Error())
	}
	return nil
}

func (p *Plugin) getGoogleToken(userID string) (*oauth2.Token, error) {
	data, appErr := p.API.KVGet(fmt.Sprintf("token_%s", userID))
	if appErr != nil {
		return nil, fmt.Errorf("kv get token_%s: %s", userID, appErr.Error())
	}
	if data == nil {
		return nil, fmt.Errorf("no token found")
	}

	token, shouldMigrate, err := p.decodeStoredGoogleToken(data)
	if err != nil {
		return nil, err
	}

	if shouldMigrate {
		if saveErr := p.saveGoogleToken(userID, token); saveErr != nil {
			p.API.LogWarn("Failed to migrate legacy Google token to encrypted storage", "user_id", userID, "error", saveErr.Error())
		}
	}

	return token, nil
}

func (p *Plugin) encodeStoredGoogleToken(token *oauth2.Token) ([]byte, error) {
	if token == nil {
		return nil, fmt.Errorf("missing token")
	}

	serializedToken, err := json.Marshal(token)
	if err != nil {
		return nil, fmt.Errorf("marshal token: %w", err)
	}

	encryptionKey, err := p.getTokenEncryptionKey()
	if err != nil {
		return nil, err
	}

	encryptedToken, err := encryptToken(encryptionKey, serializedToken)
	if err != nil {
		return nil, fmt.Errorf("encrypt token: %w", err)
	}

	data, err := json.Marshal(storedGoogleToken{EncryptedToken: encryptedToken})
	if err != nil {
		return nil, fmt.Errorf("marshal stored token: %w", err)
	}

	return data, nil
}

func (p *Plugin) decodeStoredGoogleToken(data []byte) (*oauth2.Token, bool, error) {
	var stored storedGoogleToken
	if err := json.Unmarshal(data, &stored); err == nil {
		switch {
		case stored.EncryptedToken != "":
			encryptionKey, keyErr := p.getTokenEncryptionKey()
			if keyErr != nil {
				return nil, false, keyErr
			}

			decryptedToken, decryptErr := decryptToken(encryptionKey, stored.EncryptedToken)
			if decryptErr != nil {
				return nil, false, fmt.Errorf("decrypt token: %w", decryptErr)
			}

			var token oauth2.Token
			if err := json.Unmarshal(decryptedToken, &token); err != nil {
				return nil, false, fmt.Errorf("decode decrypted token: %w", err)
			}
			return &token, false, nil
		case stored.Token != nil:
			return stored.Token, true, nil
		}
	}

	var legacyToken oauth2.Token
	if err := json.Unmarshal(data, &legacyToken); err != nil {
		return nil, false, fmt.Errorf("decode token: %w", err)
	}

	if legacyToken.AccessToken == "" && legacyToken.RefreshToken == "" && legacyToken.TokenType == "" && legacyToken.Expiry.IsZero() {
		return nil, false, fmt.Errorf("decode token: empty token payload")
	}

	return &legacyToken, true, nil
}

func (p *Plugin) getTokenEncryptionKey() ([]byte, error) {
	secret := p.getConfiguration().EncryptionKey
	if secret == "" {
		return nil, fmt.Errorf("encryption key is not configured")
	}

	sum := sha256.Sum256([]byte(secret))
	key := make([]byte, len(sum))
	copy(key, sum[:])
	return key, nil
}

func encryptToken(key []byte, data []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, data, nil)
	return fmt.Sprintf("v1:%s", base64.RawURLEncoding.EncodeToString(ciphertext)), nil
}

func decryptToken(key []byte, encoded string) ([]byte, error) {
	const prefix = "v1:"
	if !strings.HasPrefix(encoded, prefix) {
		return nil, fmt.Errorf("unsupported token format")
	}

	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(encoded, prefix))
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(decoded) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := decoded[:nonceSize], decoded[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}
