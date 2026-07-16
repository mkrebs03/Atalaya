package auth

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

func TestHashPasswordProducesStrictGoldenPHC(t *testing.T) {
	password := []byte("correct horse battery staple")
	salt := sequentialBytes(passwordSaltSize)

	encoded, err := HashPasswordWithRandom(password, bytes.NewReader(salt))
	if err != nil {
		t.Fatalf("HashPasswordWithRandom() error = %v", err)
	}

	const expected = "$argon2id$v=19$m=65536,t=3,p=4$AAECAwQFBgcICQoLDA0ODw$hTsnKkTbFCHAKWJmmlXrCZTzyrOF7RxMeSU+7hm6tJ4"
	if encoded != expected {
		t.Fatalf("HashPasswordWithRandom() = %q, want %q", encoded, expected)
	}
}

func TestVerifyPasswordAcceptsCorrectAndRejectsIncorrectPassword(t *testing.T) {
	password := []byte("correct horse battery staple")
	encoded, err := HashPasswordWithRandom(password, bytes.NewReader(bytes.Repeat([]byte{0x42}, passwordSaltSize)))
	if err != nil {
		t.Fatalf("HashPasswordWithRandom() error = %v", err)
	}

	valid, err := VerifyPassword(encoded, password)
	if err != nil {
		t.Fatalf("VerifyPassword(correct) error = %v", err)
	}
	if !valid {
		t.Fatal("VerifyPassword(correct) = false")
	}

	valid, err = VerifyPassword(encoded, []byte("incorrect horse battery staple"))
	if err != nil {
		t.Fatalf("VerifyPassword(incorrect) error = %v", err)
	}
	if valid {
		t.Fatal("VerifyPassword(incorrect) = true")
	}
}

func TestHashPasswordUsesInjectedSalt(t *testing.T) {
	password := []byte("valid password")
	first, err := HashPasswordWithRandom(password, bytes.NewReader(bytes.Repeat([]byte{0x01}, passwordSaltSize)))
	if err != nil {
		t.Fatalf("HashPasswordWithRandom(first) error = %v", err)
	}
	second, err := HashPasswordWithRandom(password, bytes.NewReader(bytes.Repeat([]byte{0x02}, passwordSaltSize)))
	if err != nil {
		t.Fatalf("HashPasswordWithRandom(second) error = %v", err)
	}
	if first == second {
		t.Fatal("HashPasswordWithRandom() produced equal hashes for distinct salts")
	}
}

func TestValidatePasswordEnforcesByteAndUTF8Limits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		password []byte
		valid    bool
	}{
		{name: "minimum", password: bytes.Repeat([]byte("a"), 12), valid: true},
		{name: "maximum", password: bytes.Repeat([]byte("a"), 256), valid: true},
		{name: "unicode measured as bytes", password: []byte(strings.Repeat("á", 6)), valid: true},
		{name: "spaces are not trimmed", password: []byte("            "), valid: true},
		{name: "too short", password: bytes.Repeat([]byte("a"), 11), valid: false},
		{name: "too long", password: bytes.Repeat([]byte("a"), 257), valid: false},
		{name: "invalid UTF-8", password: append(bytes.Repeat([]byte("a"), 12), 0xff), valid: false},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := ValidatePassword(test.password)
			if test.valid && err != nil {
				t.Fatalf("ValidatePassword() error = %v", err)
			}
			if !test.valid && err == nil {
				t.Fatal("ValidatePassword() error = nil")
			}
		})
	}
}

func TestHashPasswordRejectsInvalidInputAndRandomFailure(t *testing.T) {
	t.Parallel()

	if _, err := HashPasswordWithRandom([]byte("short"), bytes.NewReader(make([]byte, 16))); err == nil {
		t.Fatal("HashPasswordWithRandom(short password) error = nil")
	}
	if _, err := HashPasswordWithRandom([]byte("valid password"), bytes.NewReader(nil)); err == nil || !strings.Contains(err.Error(), "generate password salt") {
		t.Fatalf("HashPasswordWithRandom(failed random) error = %v", err)
	}
	if _, err := HashPasswordWithRandom([]byte("valid password"), nil); err == nil {
		t.Fatal("HashPasswordWithRandom(nil random) error = nil")
	}
}

func TestVerifyPasswordRejectsNonV1PHCWithoutDeriving(t *testing.T) {
	t.Parallel()

	salt := base64.RawStdEncoding.EncodeToString(make([]byte, passwordSaltSize))
	nonCanonicalSalt := salt[:len(salt)-1] + "B"
	hash := base64.RawStdEncoding.EncodeToString(make([]byte, passwordHashSize))
	valid := "$argon2id$v=19$m=65536,t=3,p=4$" + salt + "$" + hash
	tests := []string{
		"",
		strings.Replace(valid, "argon2id", "argon2i", 1),
		strings.Replace(valid, "v=19", "v=16", 1),
		strings.Replace(valid, "m=65536", "m=4294967295", 1),
		strings.Replace(valid, "m=65536,t=3,p=4", "t=3,m=65536,p=4", 1),
		valid + "$extra",
		strings.Replace(valid, salt, salt+"=", 1),
		strings.Replace(valid, salt, "!!!!!!!!!!!!!!!!!!!!!!", 1),
		strings.Replace(valid, salt, nonCanonicalSalt, 1),
		strings.Replace(valid, salt, base64.RawStdEncoding.EncodeToString(make([]byte, 15)), 1),
		strings.Replace(valid, hash, base64.RawStdEncoding.EncodeToString(make([]byte, 31)), 1),
	}

	for _, encoded := range tests {
		encoded := encoded
		t.Run(encoded, func(t *testing.T) {
			t.Parallel()
			matched, err := VerifyPassword(encoded, []byte("valid password"))
			if err == nil {
				t.Fatal("VerifyPassword(malformed) error = nil")
			}
			if matched {
				t.Fatal("VerifyPassword(malformed) = true")
			}
		})
	}
}

func TestValidatePasswordHashUsesStrictV1Parser(t *testing.T) {
	t.Parallel()

	if err := ValidatePasswordHash(testPasswordPHC); err != nil {
		t.Fatalf("ValidatePasswordHash(valid) error = %v", err)
	}
	if err := ValidatePasswordHash(strings.Replace(testPasswordPHC, "p=4", "p=8", 1)); err == nil {
		t.Fatal("ValidatePasswordHash(non-v1 profile) error = nil")
	}
}

func sequentialBytes(length int) []byte {
	result := make([]byte, length)
	for index := range result {
		result[index] = byte(index)
	}
	return result
}
