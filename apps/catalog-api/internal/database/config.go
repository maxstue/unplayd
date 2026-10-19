// Package database owns catalog connection configuration and versioned migrations.
package database

import (
	"errors"
	"net/url"
)

// Config keeps credentials out of default formatted output.
type Config struct {
	endpoint url.URL
	user     string
	password string
}

func (Config) String() string { return "catalog database configuration" }

// Load validates configuration without exposing connection values in errors.
func Load(env func(string) string) (Config, error) {
	parsed, err := url.Parse(env("DATABASE_URL"))
	if err != nil || parsed.Hostname() == "" || parsed.Path == "" || parsed.Path == "/" || (parsed.Scheme != "postgresql" && parsed.Scheme != "postgres") {
		return Config{}, errors.New("DATABASE_URL must be a PostgreSQL URL with a host and database")
	}
	if parsed.User != nil || parsed.Query().Has("user") || parsed.Query().Has("password") {
		return Config{}, errors.New("DATABASE_URL must not contain credentials; use separate settings")
	}
	if schema := parsed.Query().Get("search_path"); schema != "" && schema != "catalog" {
		return Config{}, errors.New("DATABASE_URL must target the catalog schema")
	}
	user := env("DATABASE_USER")
	if user == "" {
		user = "catalog"
	}
	if user != "catalog" {
		return Config{}, errors.New("DATABASE_USER must be catalog")
	}
	password := env("DATABASE_PASSWORD")
	if password == "" {
		return Config{}, errors.New("DATABASE_PASSWORD is required")
	}
	return Config{endpoint: *parsed, user: user, password: password}, nil
}

func (c Config) connectionString() string {
	endpoint := c.endpoint
	endpoint.User = url.UserPassword(c.user, c.password)
	query := endpoint.Query()
	query.Set("search_path", "catalog")
	endpoint.RawQuery = query.Encode()
	return endpoint.String()
}
