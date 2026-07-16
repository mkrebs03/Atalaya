package auth

import (
	"strings"
	"testing"
)

func TestNormalizeUsernameCanonicalizesDocumentedGrammar(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"admin":                 "admin",
		"  AdMiN  ":             "admin",
		"A.user_name-1":         "a.user_name-1",
		"\tAdministrator-1\n":   "administrator-1",
		strings.Repeat("a", 64): strings.Repeat("a", 64),
	}

	for raw, want := range tests {
		raw, want := raw, want
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			got, err := NormalizeUsername(raw)
			if err != nil {
				t.Fatalf("NormalizeUsername() error = %v", err)
			}
			if got != want {
				t.Fatalf("NormalizeUsername() = %q, want %q", got, want)
			}
		})
	}
}

func TestNormalizeUsernameRejectsUnsupportedValues(t *testing.T) {
	t.Parallel()

	values := []string{
		"",
		"   ",
		"_admin",
		"-admin",
		".admin",
		"admin space",
		"admin@example",
		"admín",
		"admin\x00name",
		strings.Repeat("a", 65),
	}

	for _, raw := range values {
		raw := raw
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			if _, err := NormalizeUsername(raw); err == nil {
				t.Fatal("NormalizeUsername() error = nil")
			}
		})
	}
}
