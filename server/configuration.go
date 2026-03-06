package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"reflect"

	"github.com/pkg/errors"
)

// configuration captures the plugin's external configuration as exposed in the Mattermost server
// configuration, as well as values computed from the configuration. Any public fields will be
// deserialized from the Mattermost server configuration in OnConfigurationChange.
//
// As plugins are inherently concurrent (hooks being called asynchronously), and the plugin
// configuration can change at any time, access to the configuration must be synchronized. The
// strategy used in this plugin is to guard a pointer to the configuration, and clone the entire
// struct whenever it changes. You may replace this with whatever strategy you choose.
//
// If you add non-reference types to your configuration struct, be sure to rewrite Clone as a deep
// copy appropriate for your types.
type configuration struct {
	GoogleClientID     string
	GoogleClientSecret string
	EncryptionKey      string
}

func (c *configuration) ToMap() (map[string]any, error) {
	var out map[string]any
	data, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}

	return out, nil
}

// Clone shallow copies the configuration. Your implementation may require a deep copy if
// your configuration has reference types.
func (c *configuration) Clone() *configuration {
	clone := *c
	return &clone
}

// getConfiguration retrieves the active configuration under lock, making it safe to use
// concurrently. The active configuration may change underneath the client of this method, but
// the struct returned by this API call is considered immutable.
func (p *Plugin) getConfiguration() *configuration {
	p.configurationLock.RLock()
	defer p.configurationLock.RUnlock()

	if p.configuration == nil {
		return &configuration{}
	}

	return p.configuration
}

// setConfiguration replaces the active configuration under lock.
//
// Do not call setConfiguration while holding the configurationLock, as sync.Mutex is not
// reentrant. In particular, avoid using the plugin API entirely, as this may in turn trigger a
// hook back into the plugin. If that hook attempts to acquire this lock, a deadlock may occur.
//
// This method panics if setConfiguration is called with the existing configuration. This almost
// certainly means that the configuration was modified without being cloned and may result in
// an unsafe access.
func (p *Plugin) setConfiguration(configuration *configuration) {
	p.configurationLock.Lock()
	defer p.configurationLock.Unlock()

	if configuration != nil && p.configuration == configuration {
		// Ignore assignment if the configuration struct is empty. Go will optimize the
		// allocation for same to point at the same memory address, breaking the check
		// above.
		if reflect.ValueOf(*configuration).NumField() == 0 {
			return
		}

		panic("setConfiguration called with the existing configuration")
	}

	p.configuration = configuration
}

// OnConfigurationChange is invoked when configuration changes may have been made.
func (p *Plugin) OnConfigurationChange() error {
	previous := p.getConfiguration()
	configuration := new(configuration)

	// Load the public configuration fields from the Mattermost server configuration.
	if err := p.API.LoadPluginConfiguration(configuration); err != nil {
		return errors.Wrap(err, "failed to load plugin configuration")
	}

	generatedEncryptionKey := false
	resetStoredTokens := previous != nil && previous.EncryptionKey != "" && configuration.EncryptionKey != previous.EncryptionKey
	if configuration.EncryptionKey == "" {
		secret, err := generateSecret()
		if err != nil {
			return errors.Wrap(err, "failed to generate encryption key")
		}
		configuration.EncryptionKey = secret
		generatedEncryptionKey = true
		p.API.LogInfo("Auto-generated encryption key for Google Meet plugin")
	}

	p.setConfiguration(configuration)

	if generatedEncryptionKey {
		go p.storeConfiguration(configuration)
	}

	if resetStoredTokens {
		go p.resetStoredGoogleTokens()
	}

	return nil
}

func (p *Plugin) storeConfiguration(configuration *configuration) {
	configMap, err := configuration.ToMap()
	if err != nil {
		p.API.LogError("Failed to serialize updated plugin configuration", "error", err.Error())
		return
	}

	if appErr := p.API.SavePluginConfig(configMap); appErr != nil {
		p.API.LogError("Failed to store updated plugin configuration", "error", appErr.Error())
	}
}

func generateSecret() (string, error) {
	b := make([]byte, 256)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}

	secret := base64.RawURLEncoding.EncodeToString(b)
	return secret[:32], nil
}

func (p *Plugin) resetStoredGoogleTokens() {
	p.API.LogInfo("Encryption key changed. Resetting stored Google Meet tokens; users will need to reconnect.")
	if appErr := p.API.KVDeleteAll(); appErr != nil {
		p.API.LogError("Failed to reset stored Google Meet tokens", "error", appErr.Error())
	}
}
