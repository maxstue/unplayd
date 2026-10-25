package igdb

import (
	"crypto/rand"
	"fmt"
	"strings"
	"testing"
)

func TestLoadWithoutCredentialsDisablesIntegration(t *testing.T) {
	config, err := Load(func(string) string { return "" })
	if err != nil || config.Enabled() {
		t.Fatalf("missing credentials must disable IGDB without error, got enabled=%v err=%v", config.Enabled(), err)
	}
	if _, err := NewClient(config); err == nil {
		t.Fatal("client was created without credentials")
	}
}

func TestLoadAcceptsCredentialsAndRedactsDiagnostics(t *testing.T) {
	id, secret := rand.Text(), rand.Text()
	config, err := Load(credentials(id, secret))
	if err != nil || !config.Enabled() {
		t.Fatalf("valid credentials rejected: %v", err)
	}
	for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
		if formatted := fmt.Sprintf(format, config); strings.Contains(formatted, id) || strings.Contains(formatted, secret) {
			t.Errorf("configuration exposes credentials using %s", format)
		}
	}
	if _, err := NewClient(config); err != nil {
		t.Fatal(err)
	}
}

func TestLoadRejectsPartialOrMalformedCredentialsWithoutLeakingValues(t *testing.T) {
	id, secret := rand.Text(), rand.Text()
	for _, tc := range []struct{ name, id, secret string }{
		{"missing secret", id, ""},
		{"missing client ID", "", secret},
		{"whitespace in client ID", id + " x", secret},
		{"newline in secret", id, secret + "\n"},
		{"unicode whitespace", id, secret + " "},
		{"oversized secret", id, strings.Repeat("a", maxCredentialLength+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config, err := Load(credentials(tc.id, tc.secret))
			if err == nil || config.Enabled() {
				t.Fatal("invalid credentials were accepted")
			}
			for _, value := range []string{tc.id, tc.secret} {
				if len(value) > 8 && strings.Contains(err.Error(), strings.TrimSpace(value)) {
					t.Error("configuration error exposes a credential")
				}
			}
		})
	}
}

func credentials(id, secret string) func(string) string {
	return func(name string) string {
		return map[string]string{"IGDB_CLIENT_ID": id, "IGDB_CLIENT_SECRET": secret}[name]
	}
}
