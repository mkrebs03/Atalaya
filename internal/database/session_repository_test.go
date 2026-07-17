package database

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mkrebs03/atalaya/internal/auth"
)

func TestMigration002UpgradesV1AndValidatesExactSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database.db")
	v1, err := open(context.Background(), path, testMasterKey(), openOptions{clock: func() time.Time { return testInstant }, random: bytes.NewReader(make([]byte, 12)), migrations: embeddedMigrations[:1]})
	if err != nil {
		t.Fatal(err)
	}
	if err := v1.Close(); err != nil {
		t.Fatal(err)
	}
	upgraded, err := Open(context.Background(), path, testMasterKey())
	if err != nil {
		t.Fatal(err)
	}
	if err := upgraded.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := Preflight(context.Background(), path, testMasterKey())
	if err != nil || result.SchemaVersion != 2 {
		t.Fatalf("preflight=%+v err=%v", result, err)
	}
	raw := openRawDatabase(t, path)
	if _, err := raw.Exec("DROP INDEX sessions_administrator_active"); err != nil {
		t.Fatal(err)
	}
	raw.Close()
	if _, err := Preflight(context.Background(), path, testMasterKey()); !errors.Is(err, ErrCorruptDatabase) {
		t.Fatalf("corrupt schema error=%v", err)
	}
}

func TestMigration002RejectsAlteredSessionConstraintAndForeignKey(t *testing.T) {
	tests := []struct {
		name   string
		oldSQL string
		newSQL string
	}{
		{name: "token hash constraint", oldSQL: "CHECK(length(token_hash) = 32)", newSQL: "CHECK(length(token_hash) >= 16)"},
		{name: "foreign key action", oldSQL: "REFERENCES administrators(id)", newSQL: "REFERENCES administrators(id) ON DELETE CASCADE"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "database.db")
			database := openTestDatabase(t, path)
			if err := database.Close(); err != nil {
				t.Fatal(err)
			}
			raw := openRawDatabase(t, path)
			if _, err := raw.Exec("PRAGMA writable_schema=ON"); err != nil {
				t.Fatal(err)
			}
			result, err := raw.Exec("UPDATE sqlite_schema SET sql=replace(sql, ?, ?) WHERE type='table' AND name='sessions'", test.oldSQL, test.newSQL)
			if err != nil {
				t.Fatal(err)
			}
			if affected, err := result.RowsAffected(); err != nil || affected != 1 {
				t.Fatalf("schema update affected=%d err=%v", affected, err)
			}
			if _, err := raw.Exec("PRAGMA writable_schema=OFF"); err != nil {
				t.Fatal(err)
			}
			if err := raw.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := Preflight(context.Background(), path, testMasterKey()); !errors.Is(err, ErrCorruptDatabase) {
				t.Fatalf("Preflight() error = %v", err)
			}
		})
	}
}

func TestSessionRepositoryTouchExpiryAndPasswordRevocation(t *testing.T) {
	database := openTestDatabase(t, filepath.Join(t.TempDir(), "database.db"))
	defer database.Close()
	administrator := testAdministrator("00000000-0000-4000-8000-000000000077", "admin", testPasswordPHC)
	if err := database.CreateAdministrator(context.Background(), administrator); err != nil {
		t.Fatal(err)
	}
	now := testInstant
	session := auth.Session{ID: "00000000-0000-4000-8000-000000000088", AdministratorID: administrator.ID, TokenHash: bytes.Repeat([]byte{7}, 32), CreatedAt: now, LastActivityAt: now, IdleExpiresAt: now.Add(time.Hour), AbsoluteExpiresAt: now.Add(2 * time.Hour), RemoteAddr: "127.0.0.1:1", UserAgent: "test"}
	if err := database.CreateSessionIfAdministratorVersion(context.Background(), session, 1); err != nil {
		t.Fatal(err)
	}
	_, touched, err := database.ValidateAndTouchSession(context.Background(), session.TokenHash, now.Add(30*time.Minute), now.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !touched.IdleExpiresAt.Equal(session.AbsoluteExpiresAt) {
		t.Fatalf("idle=%v absolute=%v", touched.IdleExpiresAt, session.AbsoluteExpiresAt)
	}
	if err := database.ResetPasswordAndRevokeSessions(context.Background(), testPasswordPHC, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := database.ValidateAndTouchSession(context.Background(), session.TokenHash, now.Add(time.Hour), now.Add(2*time.Hour)); !errors.Is(err, auth.ErrSessionExpired) {
		t.Fatalf("revoked session error=%v", err)
	}
}

func TestSessionSurvivesRestartAndResetCannotRaceInActiveOldSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database.db")
	database := openTestDatabase(t, path)
	administrator := testAdministrator("00000000-0000-4000-8000-000000000099", "admin", testPasswordPHC)
	if err := database.CreateAdministrator(context.Background(), administrator); err != nil {
		t.Fatal(err)
	}
	now := testInstant
	session := auth.Session{ID: "00000000-0000-4000-8000-000000000100", AdministratorID: administrator.ID, TokenHash: bytes.Repeat([]byte{8}, 32), CreatedAt: now, LastActivityAt: now, IdleExpiresAt: now.Add(time.Hour), AbsoluteExpiresAt: now.Add(2 * time.Hour), RemoteAddr: "127.0.0.1:1", UserAgent: "test"}
	if err := database.CreateSessionIfAdministratorVersion(context.Background(), session, 1); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(context.Background(), path, testMasterKey())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, _, err := reopened.ValidateAndTouchSession(context.Background(), session.TokenHash, now.Add(time.Minute), now.Add(time.Hour)); err != nil {
		t.Fatalf("session after restart: %v", err)
	}
	racingSession := session
	racingSession.ID = "00000000-0000-4000-8000-000000000101"
	racingSession.TokenHash = bytes.Repeat([]byte{9}, 32)
	start := make(chan struct{})
	results := make(chan error, 2)
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		<-start
		results <- reopened.CreateSessionIfAdministratorVersion(context.Background(), racingSession, 1)
	}()
	go func() {
		defer wait.Done()
		<-start
		results <- reopened.ResetPasswordAndRevokeSessions(context.Background(), testPasswordPHC, now.Add(2*time.Minute))
	}()
	close(start)
	wait.Wait()
	close(results)
	for result := range results {
		if result != nil && !errors.Is(result, auth.ErrAdministratorVersionConflict) {
			t.Fatalf("race error=%v", result)
		}
	}
	if _, _, err := reopened.ValidateAndTouchSession(context.Background(), racingSession.TokenHash, now.Add(3*time.Minute), now.Add(time.Hour)); !errors.Is(err, auth.ErrAuthenticationRequired) && !errors.Is(err, auth.ErrSessionExpired) {
		t.Fatalf("racing session remained active: %v", err)
	}
}
