package auth

import (
	"crypto/rand"
	"fmt"
	"strings"
	"testing"
)

func TestLoadAcceptsTokenBoundaryAndRedactsDiagnostics(t *testing.T) {
	for _, length := range []int{32, 33, 128} {
		t.Run(fmt.Sprint(length), func(t *testing.T) {
			token := (rand.Text() + rand.Text() + rand.Text() + rand.Text() + rand.Text())[:length]
			config, err := Load(func(name string) string {
				if name != "CATALOG_SERVICE_TOKEN" {
					t.Fatalf("unexpected configuration lookup: %s", name)
				}
				return token
			})
			if err != nil || config.Token() != token {
				t.Fatal("valid service token was rejected or changed")
			}
			assertRedactedDiagnostics(t, config, token)
		})
	}
}

func TestLoadRejectsMissingShortAndWhitespaceTokensWithoutLeakingValues(t *testing.T) {
	token := rand.Text() + rand.Text()
	for _, tc := range []struct{ name, token string }{
		{"missing", ""},
		{"below minimum", token[:31]},
		{"leading space", " " + token},
		{"trailing space", token + " "},
		{"embedded space", token[:20] + " " + token[20:]},
		{"tab", token + "\t"},
		{"newline", token + "\n"},
		{"unicode whitespace", token + "\u2003"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config, err := Load(func(string) string { return tc.token })
			if err == nil {
				t.Fatal("invalid service token was accepted")
			}
			if config.Token() != "" {
				t.Error("invalid configuration retained service token")
			}
			if tc.token != "" && strings.Contains(err.Error(), tc.token) {
				t.Error("configuration error exposes service token")
			}
		})
	}
}

func assertRedactedDiagnostics(t *testing.T, config Config, token string) {
	t.Helper()
	for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
		if strings.Contains(fmt.Sprintf(format, config), token) {
			t.Errorf("configuration exposes service token using %s", format)
		}
	}
}
