// Package igdb adapts the IGDB API to catalog source games.
package igdb

import (
	"errors"
	"strings"
	"unicode"
)

const maxCredentialLength = 256

// Config holds the Twitch application credentials used for IGDB.
type Config struct {
	clientID     string
	clientSecret string
}

func (Config) String() string   { return "IGDB configuration" }
func (Config) GoString() string { return "IGDB configuration" }

// Enabled reports whether IGDB credentials are configured.
func (c Config) Enabled() bool { return c.clientID != "" }

// Load disables the integration when no credentials are set and fails closed on
// partial or malformed credentials without including their values in errors.
func Load(env func(string) string) (Config, error) {
	clientID, clientSecret := env("IGDB_CLIENT_ID"), env("IGDB_CLIENT_SECRET")
	if clientID == "" && clientSecret == "" {
		return Config{}, nil
	}
	if !validCredential(clientID) || !validCredential(clientSecret) {
		return Config{}, errors.New("IGDB_CLIENT_ID and IGDB_CLIENT_SECRET must both be set, without whitespace and at most 256 bytes")
	}
	return Config{clientID: clientID, clientSecret: clientSecret}, nil
}

func validCredential(value string) bool {
	return value != "" && len(value) <= maxCredentialLength && !strings.ContainsFunc(value, unicode.IsSpace)
}
