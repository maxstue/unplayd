package database

import (
	"crypto/rand"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

func TestConnectionConfigurationEncodesCredentialsAndOwnSchema(t *testing.T) {
	password := rand.Text() + "#?@$"
	values := map[string]string{"DATABASE_URL": "postgresql://localhost:5432/unplayd?sslmode=disable", "DATABASE_PASSWORD": password}
	config, err := Load(func(key string) string { return values[key] })
	if err != nil {
		t.Fatal(err)
	}
	connection, err := url.Parse(config.connectionString())
	if err != nil {
		t.Fatal(err)
	}
	decoded, _ := connection.User.Password()
	if decoded != password || connection.User.Username() != "catalog" || connection.Query().Get("search_path") != "catalog" {
		t.Fatal("credentials or owned schema were not preserved")
	}
	if strings.Contains(fmt.Sprint(config), password) {
		t.Fatal("configuration diagnostics expose credentials")
	}
	for key, want := range map[string]string{"connect_timeout": "3", "statement_timeout": "2000", "lock_timeout": "1000"} {
		if got := connection.Query().Get(key); got != want {
			t.Fatalf("%s=%s, want %s", key, got, want)
		}
	}
}

func TestMigrationTimeoutsPermitDDLWithoutChangingApplicationTimeouts(t *testing.T) {
	values := map[string]string{"DATABASE_URL": "postgresql://localhost/unplayd?statement_timeout=750&lock_timeout=500&connect_timeout=2", "DATABASE_PASSWORD": rand.Text()}
	config, err := Load(func(key string) string { return values[key] })
	if err != nil {
		t.Fatal(err)
	}
	application, _ := url.Parse(config.connectionString())
	migration, _ := url.Parse(config.migrationConfig().connectionString())
	if application.Query().Get("statement_timeout") != "750" || application.Query().Get("lock_timeout") != "500" {
		t.Fatal("explicit application settings were overwritten")
	}
	if migration.Query().Get("statement_timeout") != "0" || migration.Query().Get("lock_timeout") != "10000" || migration.Query().Get("connect_timeout") != "2" {
		t.Fatal("migration bounds must allow long DDL but retain connection and lock limits")
	}
}

func TestInvalidDatabaseConfigurationIsRejected(t *testing.T) {
	for _, tc := range []struct{ name, key, value string }{
		{"missing URL", "DATABASE_URL", ""},
		{"wrong protocol", "DATABASE_URL", "http://localhost/unplayd"},
		{"missing database", "DATABASE_URL", "postgresql://localhost"},
		{"credentials in URL", "DATABASE_URL", "postgresql://user:" + rand.Text() + "@localhost/unplayd"},
		{"foreign schema", "DATABASE_URL", "postgresql://localhost/unplayd?search_path=user_data"},
		{"admin role", "DATABASE_USER", "postgres"},
		{"missing password", "DATABASE_PASSWORD", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			password := rand.Text()
			values := map[string]string{"DATABASE_URL": "postgresql://localhost/unplayd", "DATABASE_PASSWORD": password}
			values[tc.key] = tc.value
			_, err := Load(func(key string) string { return values[key] })
			if err == nil || strings.Contains(err.Error(), password) {
				t.Fatal("invalid configuration accepted or secret exposed")
			}
		})
	}
}
