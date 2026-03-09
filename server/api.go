package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
)

// initRouter initializes the HTTP router for the plugin.
func (p *Plugin) initRouter() *mux.Router {
	router := mux.NewRouter()

	router.HandleFunc("/oauth2/login", p.OAuth2Login).Methods(http.MethodGet)
	router.HandleFunc("/oauth2/callback", p.OAuth2Callback).Methods(http.MethodGet)

	apiRouter := router.PathPrefix("/api/v1").Subrouter()
	apiRouter.Use(p.MattermostAuthorizationRequired)
	apiRouter.HandleFunc("/create", p.CreateMeeting).Methods(http.MethodPost)

	return router
}

// ServeHTTP demonstrates a plugin that handles HTTP requests.
func (p *Plugin) ServeHTTP(c *plugin.Context, w http.ResponseWriter, r *http.Request) {
	p.router.ServeHTTP(w, r)
}

func (p *Plugin) MattermostAuthorizationRequired(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := r.Header.Get("Mattermost-User-ID")
		if userID == "" {
			http.Error(w, "Not authorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (p *Plugin) OAuth2Login(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("Mattermost-User-ID")
	if userID == "" {
		http.Error(w, "Not authorized", http.StatusUnauthorized)
		return
	}

	url := getOAuthLoginURL(p.getOAuthConfig(), userID)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func (p *Plugin) OAuth2Callback(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")

	if state == "" || code == "" {
		http.Error(w, "Missing state or code", http.StatusBadRequest)
		return
	}

	token, err := p.getOAuthConfig().Exchange(context.Background(), code)
	if err != nil {
		p.API.LogError("Failed to exchange code", "error", err)
		http.Error(w, "Failed to exchange code", http.StatusInternalServerError)
		return
	}

	if err := p.saveGoogleToken(state, token); err != nil {
		p.API.LogError("Failed to save token", "error", err)
		http.Error(w, fmt.Sprintf("Failed to save token: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html lang="en">
<head>
	<meta charset="utf-8">
	<meta name="viewport" content="width=device-width, initial-scale=1">
	<title>Google Meet Connected</title>
	<style>
		body {
			font-family: sans-serif;
			margin: 0;
			padding: 24px;
			background: #f5f7fa;
			color: #1f2329;
		}
		.card {
			max-width: 520px;
			margin: 40px auto;
			padding: 24px;
			background: #ffffff;
			border: 1px solid #d9e0e6;
			border-radius: 8px;
			box-shadow: 0 4px 12px rgba(0, 0, 0, 0.08);
		}
		p {
			line-height: 1.5;
		}
	</style>
</head>
<body>
	<div class="card">
		<h1>Google Meet connected</h1>
		<p>You can return to Mattermost. If the meeting link does not appear automatically, click the Google Meet button once more.</p>
		<p>This tab will try to close automatically.</p>
	</div>
	<script>
		(function() {
			var pluginId = %q;
			var message = {type: 'google-meet-auth-complete', pluginId: pluginId};
			try {
				window.localStorage.setItem(pluginId + '_auth_complete', String(Date.now()));
			} catch (error) {}
			try {
				if (window.opener && !window.opener.closed) {
					window.opener.postMessage(message, window.location.origin);
				}
			} catch (error) {}
			window.setTimeout(function() {
				window.close();
			}, 300);
		})();
	</script>
</body>
</html>`, template.JSEscapeString(manifest.Id))
}

func (p *Plugin) CreateMeeting(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("Mattermost-User-ID")
	if userID == "" {
		http.Error(w, "Not authorized", http.StatusUnauthorized)
		return
	}

	channelID := r.URL.Query().Get("channel_id")
	if channelID == "" {
		http.Error(w, "Missing channel_id", http.StatusBadRequest)
		return
	}

	link, err := p.createMeeting(userID)
	if err != nil {
		p.API.LogError("Failed to create meeting", "error", err)
		if err.Error() == "no token found" || errors.Is(err, errGoogleReauthorizationRequired) {
			http.Error(w, "Authorization Required", http.StatusUnauthorized)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	post := &model.Post{
		UserId:    userID,
		ChannelId: channelID,
		Message:   fmt.Sprintf("I've created a Google Meet: %s", link),
		Type:      model.PostTypeDefault,
	}

	if _, err := p.API.CreatePost(post); err != nil {
		p.API.LogError("Failed to create post", "error", err)
		http.Error(w, "Failed to post meeting link", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	resp, _ := json.Marshal(map[string]string{"meet_url": link})
	w.Write(resp)
}
