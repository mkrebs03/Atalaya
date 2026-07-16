package auth

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

const testPasswordPHC = "$argon2id$v=19$m=65536,t=3,p=4$AAECAwQFBgcICQoLDA0ODw$hTsnKkTbFCHAKWJmmlXrCZTzyrOF7RxMeSU+7hm6tJ4"

type recordingAdministratorRepository struct {
	administrator Administrator
	err           error
}

func (repository *recordingAdministratorRepository) CreateAdministrator(_ context.Context, administrator Administrator) error {
	repository.administrator = administrator
	return repository.err
}

func TestAdministratorCreatorUsesDeterministicUUIDAndTimestamp(t *testing.T) {
	t.Parallel()

	repository := &recordingAdministratorRepository{}
	instant := time.Date(2026, time.July, 16, 8, 9, 10, 123, time.FixedZone("offset", -4*60*60))
	creator := newAdministratorCreator(repository, func() time.Time { return instant }, bytes.NewReader(make([]byte, 16)))

	administrator, err := creator.Create(context.Background(), "admin", testPasswordPHC)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if administrator.ID != "00000000-0000-4000-8000-000000000000" {
		t.Fatalf("ID = %q", administrator.ID)
	}
	if administrator.CreatedAt != "2026-07-16T12:09:10.000000123Z" {
		t.Fatalf("CreatedAt = %q", administrator.CreatedAt)
	}
	if administrator.CreatedAt != administrator.UpdatedAt || administrator.CreatedAt != administrator.PasswordChangedAt {
		t.Fatal("creation timestamps differ")
	}
	if repository.administrator != administrator {
		t.Fatal("repository did not receive created administrator")
	}
	if administrator.Version != 1 || administrator.DisabledAt != nil {
		t.Fatalf("creation state = version %d, disabled %v", administrator.Version, administrator.DisabledAt)
	}
}

func TestAdministratorCreatorRejectsInputsAndPropagatesRepositoryFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		username     string
		passwordHash string
		random       *bytes.Reader
		repository   *recordingAdministratorRepository
		want         string
	}{
		{name: "username not normalized", username: "Admin", passwordHash: testPasswordPHC, random: bytes.NewReader(make([]byte, 16)), repository: &recordingAdministratorRepository{}, want: "already be normalized"},
		{name: "empty hash", username: "admin", random: bytes.NewReader(make([]byte, 16)), repository: &recordingAdministratorRepository{}, want: "v1 Argon2id profile"},
		{name: "short entropy", username: "admin", passwordHash: testPasswordPHC, random: bytes.NewReader(nil), repository: &recordingAdministratorRepository{}, want: "generate administrator ID"},
		{name: "repository", username: "admin", passwordHash: testPasswordPHC, random: bytes.NewReader(make([]byte, 16)), repository: &recordingAdministratorRepository{err: errors.New("write failed")}, want: "write failed"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			creator := newAdministratorCreator(test.repository, time.Now, test.random)
			if _, err := creator.Create(context.Background(), test.username, test.passwordHash); err == nil {
				t.Fatal("Create() error = nil")
			} else if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Create() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestValidateNewAdministratorRejectsMalformedPersistenceValues(t *testing.T) {
	t.Parallel()

	timestamp := "2026-07-16T12:09:10.000000123Z"
	valid := Administrator{
		ID:                "00000000-0000-4000-8000-000000000011",
		Username:          "admin",
		PasswordHash:      testPasswordPHC,
		CreatedAt:         timestamp,
		UpdatedAt:         timestamp,
		PasswordChangedAt: timestamp,
		Version:           1,
	}
	disabledAt := timestamp
	tests := []struct {
		name   string
		mutate func(*Administrator)
	}{
		{name: "non UUIDv4 ID", mutate: func(value *Administrator) { value.ID = "00000000-0000-3000-8000-000000000011" }},
		{name: "non-normalized username", mutate: func(value *Administrator) { value.Username = "Admin" }},
		{name: "invalid PHC", mutate: func(value *Administrator) { value.PasswordHash = "$argon2id$redacted" }},
		{name: "non-canonical timestamp", mutate: func(value *Administrator) { value.CreatedAt = "2026-07-16T08:09:10-04:00" }},
		{name: "unequal timestamps", mutate: func(value *Administrator) { value.UpdatedAt = "2026-07-16T12:09:11Z" }},
		{name: "disabled", mutate: func(value *Administrator) { value.DisabledAt = &disabledAt }},
		{name: "wrong version", mutate: func(value *Administrator) { value.Version = 2 }},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			administrator := valid
			test.mutate(&administrator)
			if err := ValidateNewAdministrator(administrator); err == nil {
				t.Fatal("ValidateNewAdministrator() error = nil")
			}
		})
	}
}
