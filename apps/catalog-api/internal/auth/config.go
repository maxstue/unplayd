// Package auth owns catalog service authentication configuration.
package auth

import (
	"errors"
	"strings"
	"unicode"
)

type Config struct{ token string }

func (Config) String() string   { return "catalog authentication configuration" }
func (Config) GoString() string { return "catalog authentication configuration" }

func (c Config) Token() string { return c.token }

// Load fails closed without including credential values in errors.
func Load(env func(string) string) (Config, error) {
	token := env("CATALOG_SERVICE_TOKEN")
	if len(token) < 32 || strings.ContainsFunc(token, unicode.IsSpace) {
		return Config{}, errors.New("CATALOG_SERVICE_TOKEN must contain at least 32 bytes and no whitespace")
	}
	return Config{token: token}, nil
}
