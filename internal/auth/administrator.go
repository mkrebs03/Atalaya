package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"time"
)

// Administrator is the persistence-neutral v1 administrator aggregate.
type Administrator struct {
	ID                string
	Username          string
	PasswordHash      string
	CreatedAt         string
	UpdatedAt         string
	PasswordChangedAt string
	DisabledAt        *string
	Version           int
}

// AdministratorRepository persists the single v1 administrator.
type AdministratorRepository interface {
	CreateAdministrator(context.Context, Administrator) error
}

// AdministratorCreator creates administrator identity and timestamps without
// depending on a persistence implementation.
type AdministratorCreator struct {
	repository AdministratorRepository
	clock      func() time.Time
	random     io.Reader
}

// NewAdministratorCreator builds the production administrator creation use case.
func NewAdministratorCreator(repository AdministratorRepository) *AdministratorCreator {
	return newAdministratorCreator(repository, time.Now, rand.Reader)
}

func newAdministratorCreator(repository AdministratorRepository, clock func() time.Time, random io.Reader) *AdministratorCreator {
	return &AdministratorCreator{repository: repository, clock: clock, random: random}
}

// Create persists one administrator from an already normalized username and PHC hash.
func (creator *AdministratorCreator) Create(ctx context.Context, username, passwordHash string) (Administrator, error) {
	if creator == nil || creator.repository == nil || creator.clock == nil || creator.random == nil {
		return Administrator{}, fmt.Errorf("administrator creator dependencies are required")
	}
	normalized, err := NormalizeUsername(username)
	if err != nil || normalized != username {
		return Administrator{}, fmt.Errorf("username must already be normalized")
	}
	if err := ValidatePasswordHash(passwordHash); err != nil {
		return Administrator{}, fmt.Errorf("password hash must use the v1 Argon2id profile: %w", err)
	}

	id, err := randomUUIDv4(creator.random)
	if err != nil {
		return Administrator{}, err
	}
	now := creator.clock().UTC().Format(time.RFC3339Nano)
	administrator := Administrator{
		ID:                id,
		Username:          username,
		PasswordHash:      passwordHash,
		CreatedAt:         now,
		UpdatedAt:         now,
		PasswordChangedAt: now,
		Version:           1,
	}
	if err := creator.repository.CreateAdministrator(ctx, administrator); err != nil {
		return Administrator{}, err
	}
	return administrator, nil
}

// ValidateNewAdministrator enforces the complete persistence boundary for a
// newly-created active v1 administrator.
func ValidateNewAdministrator(administrator Administrator) error {
	if !validUUIDv4(administrator.ID) {
		return fmt.Errorf("administrator ID must be a UUIDv4")
	}
	normalized, err := NormalizeUsername(administrator.Username)
	if err != nil || normalized != administrator.Username {
		return fmt.Errorf("administrator username must already be normalized")
	}
	if err := ValidatePasswordHash(administrator.PasswordHash); err != nil {
		return fmt.Errorf("administrator password hash must use the v1 Argon2id profile: %w", err)
	}
	if !validCreationTimestamp(administrator.CreatedAt) ||
		administrator.CreatedAt != administrator.UpdatedAt ||
		administrator.CreatedAt != administrator.PasswordChangedAt {
		return fmt.Errorf("administrator creation timestamps must be equal canonical RFC3339 UTC values")
	}
	if administrator.DisabledAt != nil || administrator.Version != 1 {
		return fmt.Errorf("new administrator must be active at version 1")
	}
	return nil
}

func validUUIDv4(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	compact := strings.NewReplacer("-", "").Replace(value)
	decoded, err := hex.DecodeString(compact)
	if err != nil || len(decoded) != 16 {
		return false
	}
	return decoded[6]>>4 == 4 && decoded[8]>>6 == 2
}

func validCreationTimestamp(value string) bool {
	if !strings.HasSuffix(value, "Z") {
		return false
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	return err == nil && parsed.UTC().Format(time.RFC3339Nano) == value
}

func randomUUIDv4(random io.Reader) (string, error) {
	bytes := make([]byte, 16)
	if _, err := io.ReadFull(random, bytes); err != nil {
		return "", fmt.Errorf("generate administrator ID: %w", err)
	}
	bytes[6] = bytes[6]&0x0f | 0x40
	bytes[8] = bytes[8]&0x3f | 0x80

	encoded := make([]byte, 36)
	hex.Encode(encoded[0:8], bytes[0:4])
	encoded[8] = '-'
	hex.Encode(encoded[9:13], bytes[4:6])
	encoded[13] = '-'
	hex.Encode(encoded[14:18], bytes[6:8])
	encoded[18] = '-'
	hex.Encode(encoded[19:23], bytes[8:10])
	encoded[23] = '-'
	hex.Encode(encoded[24:36], bytes[10:16])
	return string(encoded), nil
}
