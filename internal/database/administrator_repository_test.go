package database

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mkrebs03/atalaya/internal/auth"
)

func TestAdministratorRepositoryPersistsUseCaseResultWithoutPlaintext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database.db")
	database := openTestDatabase(t, path)
	passwordHash := testPasswordPHC
	administrator := testAdministrator("00000000-0000-4000-8000-000000000011", "admin", passwordHash)

	if err := database.CreateAdministrator(context.Background(), administrator); err != nil {
		t.Fatalf("CreateAdministrator() error = %v", err)
	}

	var id, username, storedHash, createdAt, updatedAt, changedAt string
	var disabledAt *string
	var version int
	if err := database.sql.QueryRow(`
		SELECT id, username, password_hash, created_at, updated_at,
		       password_changed_at, disabled_at, version
		FROM administrators`,
	).Scan(&id, &username, &storedHash, &createdAt, &updatedAt, &changedAt, &disabledAt, &version); err != nil {
		t.Fatal(err)
	}
	if id != administrator.ID || username != "admin" || storedHash != passwordHash {
		t.Fatalf("stored identity = %q, %q, %q", id, username, storedHash)
	}
	if strings.Contains(storedHash, "correct horse battery staple") {
		t.Fatal("stored credential contains plaintext")
	}
	if createdAt != testInstant.Format(time.RFC3339Nano) || createdAt != updatedAt || createdAt != changedAt {
		t.Fatalf("stored timestamps = %q, %q, %q", createdAt, updatedAt, changedAt)
	}
	if disabledAt != nil || version != 1 {
		t.Fatalf("stored state = disabled %v, version %d", disabledAt, version)
	}
	count, err := database.CountActiveAdministrators(context.Background())
	if err != nil || count != 1 {
		t.Fatalf("CountActiveAdministrators() = %d, %v", count, err)
	}
}

func TestAdministratorRepositoryEnforcesSingletonForDistinctUsernames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database.db")
	database := openTestDatabase(t, path)
	database.sql.SetMaxOpenConns(2)

	start := make(chan struct{})
	errorsByAttempt := make(chan error, 2)
	var wait sync.WaitGroup
	for index, username := range []string{"first", "second"} {
		index, username := index, username
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			administrator := testAdministrator(
				"00000000-0000-4000-8000-00000000000"+string(rune('1'+index)),
				username,
				testPasswordPHC,
			)
			err := database.CreateAdministrator(context.Background(), administrator)
			errorsByAttempt <- err
		}()
	}
	close(start)
	wait.Wait()
	close(errorsByAttempt)

	successes, alreadyExists := 0, 0
	for err := range errorsByAttempt {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrAdministratorExists):
			alreadyExists++
		default:
			t.Fatalf("Create() unexpected error = %v", err)
		}
	}
	if successes != 1 || alreadyExists != 1 {
		t.Fatalf("results = %d successes, %d singleton errors", successes, alreadyExists)
	}
}

func TestAdministratorInsertFailureLeavesMigratedDatabaseWithoutActiveAdmin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database.db")
	database := openTestDatabase(t, path)
	if _, err := database.sql.Exec(`
		CREATE TRIGGER reject_administrator
		BEFORE INSERT ON administrators
		BEGIN
			SELECT RAISE(ABORT, 'test insert failure');
		END`); err != nil {
		t.Fatal(err)
	}

	if err := database.CreateAdministrator(
		context.Background(), testAdministrator("00000000-0000-4000-8000-000000000033", "admin", testPasswordPHC),
	); err == nil {
		t.Fatal("Create() error = nil")
	}
	if _, err := database.sql.Exec("DROP TRIGGER reject_administrator"); err != nil {
		t.Fatal(err)
	}
	result, err := Preflight(context.Background(), path, testMasterKey())
	if err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
	if result.SchemaVersion != len(embeddedMigrations) || result.HasActiveAdministrator {
		t.Fatalf("Preflight() = %+v", result)
	}
}

func TestAdministratorRepositoryRejectsMalformedBoundaryValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database.db")
	database := openTestDatabase(t, path)
	valid := testAdministrator("00000000-0000-4000-8000-000000000044", "admin", testPasswordPHC)

	tests := []struct {
		name   string
		mutate func(*auth.Administrator)
	}{
		{name: "non UUIDv4 ID", mutate: func(value *auth.Administrator) { value.ID = "not-a-uuid" }},
		{name: "non-normalized username", mutate: func(value *auth.Administrator) { value.Username = "Admin" }},
		{name: "invalid PHC", mutate: func(value *auth.Administrator) { value.PasswordHash = "hash" }},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			administrator := valid
			test.mutate(&administrator)
			if err := database.CreateAdministrator(context.Background(), administrator); err == nil {
				t.Fatal("CreateAdministrator() error = nil")
			}
		})
	}

	count, err := database.CountActiveAdministrators(context.Background())
	if err != nil || count != 0 {
		t.Fatalf("CountActiveAdministrators() = %d, %v", count, err)
	}
}

func testAdministrator(id, username, passwordHash string) auth.Administrator {
	timestamp := testInstant.Format(time.RFC3339Nano)
	return auth.Administrator{
		ID:                id,
		Username:          username,
		PasswordHash:      passwordHash,
		CreatedAt:         timestamp,
		UpdatedAt:         timestamp,
		PasswordChangedAt: timestamp,
		Version:           1,
	}
}
