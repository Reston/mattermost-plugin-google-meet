package main

import (
	"context"
	"fmt"
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

	url := p.getOAuthConfig().AuthCodeURL(userID)
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
		http.Error(w, "Failed to save token", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, "Authenticated successfully! You can now close this window.")
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
		if err.Error() == "no token found" {
			http.Error(w, "Authorization Required", http.StatusUnauthorized)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	post := &model.Post{
		UserId:    manifest.Id, // Use plugin ID or set up a bot account
		ChannelId: channelID,
		Message:   fmt.Sprintf("I've created a Google Meet for you: %s", link),
		Type:      model.PostTypeDefault,
	}

	if _, err := p.API.CreatePost(post); err != nil {
		p.API.LogError("Failed to create post", "error", err)
		http.Error(w, "Failed to post meeting link", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"meet_url": "%s"}`, link)
}

