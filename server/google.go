package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

const (
	googleOAuthCallbackRelPath = "/oauth2/callback"
)

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

	event := &calendar.Event{
		Summary:     "Mattermost Meeting",
		Description: "Meeting created from Mattermost",
		Start: &calendar.EventDateTime{
			DateTime: "2026-03-06T17:00:00Z", // Example, should probably be "now"
			TimeZone: "UTC",
		},
		End: &calendar.EventDateTime{
			DateTime: "2026-03-06T18:00:00Z",
			TimeZone: "UTC",
		},
		ConferenceData: &calendar.ConferenceData{
			CreateRequest: &calendar.CreateConferenceRequest{
				RequestId: "mattermost-" + userID,
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
	data, err := json.Marshal(token)
	if err != nil {
		return err
	}
	return p.API.KVSet(fmt.Sprintf("token_%s", userID), data)
}

func (p *Plugin) getGoogleToken(userID string) (*oauth2.Token, error) {
	data, appErr := p.API.KVGet(fmt.Sprintf("token_%s", userID))
	if appErr != nil {
		return nil, appErr
	}
	if data == nil {
		return nil, fmt.Errorf("no token found")
	}

	var token oauth2.Token
	err := json.Unmarshal(data, &token)
	if err != nil {
		return nil, err
	}
	return &token, nil
}
