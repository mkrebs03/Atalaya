// Package auth implements Atalaya administrator credential rules.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

const (
	argonVersion     = 19
	argonMemory      = 65536
	argonTime        = 3
	argonParallelism = 4
	passwordSaltSize = 16
	passwordHashSize = 32

	minimumPasswordBytes = 12
	maximumPasswordBytes = 256
)

var strictRawBase64 = base64.RawStdEncoding.Strict()

// ValidatePassword enforces the v1 administrator password input contract.
func ValidatePassword(password []byte) error {
	if !utf8.Valid(password) {
		return fmt.Errorf("password must be valid UTF-8")
	}
	if len(password) < minimumPasswordBytes || len(password) > maximumPasswordBytes {
		return fmt.Errorf("password must contain between 12 and 256 bytes")
	}
	return nil
}

// HashPassword validates and hashes a password with the v1 Argon2id profile.
func HashPassword(password []byte) (string, error) {
	return HashPasswordWithRandom(password, rand.Reader)
}

// HashPasswordWithRandom supports deterministic tests while preserving the v1 profile.
func HashPasswordWithRandom(password []byte, random io.Reader) (string, error) {
	if err := ValidatePassword(password); err != nil {
		return "", err
	}
	if random == nil {
		return "", fmt.Errorf("password salt source is required")
	}

	salt := make([]byte, passwordSaltSize)
	if _, err := io.ReadFull(random, salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}

	hash := argon2.IDKey(password, salt, argonTime, argonMemory, argonParallelism, passwordHashSize)
	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argonVersion,
		argonMemory,
		argonTime,
		argonParallelism,
		strictRawBase64.EncodeToString(salt),
		strictRawBase64.EncodeToString(hash),
	), nil
}

// VerifyPassword checks a candidate against a strict v1 PHC hash.
func VerifyPassword(encoded string, password []byte) (bool, error) {
	salt, expected, err := parsePasswordHash(encoded)
	if err != nil {
		return false, err
	}
	if err := ValidatePassword(password); err != nil {
		return false, nil
	}

	actual := argon2.IDKey(password, salt, argonTime, argonMemory, argonParallelism, passwordHashSize)
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}

// ValidatePasswordHash accepts only the persisted v1 Argon2id PHC profile.
func ValidatePasswordHash(encoded string) error {
	_, _, err := parsePasswordHash(encoded)
	return err
}

func parsePasswordHash(encoded string) ([]byte, []byte, error) {
	expectedLength := len("$argon2id$v=19$m=65536,t=3,p=4$") + strictRawBase64.EncodedLen(passwordSaltSize) + 1 + strictRawBase64.EncodedLen(passwordHashSize)
	if len(encoded) != expectedLength {
		return nil, nil, fmt.Errorf("invalid password hash format")
	}

	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v=19" || parts[3] != "m=65536,t=3,p=4" {
		return nil, nil, fmt.Errorf("invalid password hash format")
	}
	if len(parts[4]) != strictRawBase64.EncodedLen(passwordSaltSize) || len(parts[5]) != strictRawBase64.EncodedLen(passwordHashSize) {
		return nil, nil, fmt.Errorf("invalid password hash format")
	}

	salt, err := strictRawBase64.DecodeString(parts[4])
	if err != nil || len(salt) != passwordSaltSize {
		return nil, nil, fmt.Errorf("invalid password hash format")
	}
	expected, err := strictRawBase64.DecodeString(parts[5])
	if err != nil || len(expected) != passwordHashSize {
		return nil, nil, fmt.Errorf("invalid password hash format")
	}

	return salt, expected, nil
}
