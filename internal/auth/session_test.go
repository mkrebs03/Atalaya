package auth

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"
)

type sessionRepositoryStub struct {
	administrator Administrator
	session       Session
	createVersion int
}

func (stub *sessionRepositoryStub) FindActiveAdministrator(context.Context, string) (Administrator, error) {
	if stub.administrator.ID == "" {
		return Administrator{}, errors.New("missing")
	}
	return stub.administrator, nil
}
func (stub *sessionRepositoryStub) CreateSessionIfAdministratorVersion(_ context.Context, session Session, version int) error {
	stub.session = session
	stub.createVersion = version
	return nil
}
func (stub *sessionRepositoryStub) ValidateAndTouchSession(_ context.Context, _ []byte, now, idle time.Time) (Administrator, Session, error) {
	if stub.session.ID == "" {
		return Administrator{}, Session{}, ErrAuthenticationRequired
	}
	if !now.Before(stub.session.IdleExpiresAt) || !now.Before(stub.session.AbsoluteExpiresAt) {
		return Administrator{}, Session{}, ErrSessionExpired
	}
	if idle.After(stub.session.AbsoluteExpiresAt) {
		idle = stub.session.AbsoluteExpiresAt
	}
	stub.session.LastActivityAt = now
	stub.session.IdleExpiresAt = idle
	return stub.administrator, stub.session, nil
}
func (*sessionRepositoryStub) RevokeSession(context.Context, []byte, time.Time) error { return nil }
func (*sessionRepositoryStub) ChangePasswordAndReplaceSessions(context.Context, string, int, string, time.Time, Session) error {
	return nil
}
func (*sessionRepositoryStub) ResetPasswordAndRevokeSessions(context.Context, string, time.Time) error {
	return nil
}

func TestSessionServiceCreatesHashedTokenAndUsesExactExpirations(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	hash, err := HashPasswordWithRandom([]byte("correct horse battery staple"), bytes.NewReader(bytes.Repeat([]byte{1}, 16)))
	if err != nil {
		t.Fatal(err)
	}
	repository := &sessionRepositoryStub{administrator: Administrator{ID: "00000000-0000-4000-8000-000000000001", Username: "admin", PasswordHash: hash, Version: 7}}
	service := newService(repository, func() time.Time { return now }, bytes.NewReader(bytes.Repeat([]byte{2}, 48)), hash)
	result, err := service.Login(context.Background(), "admin", []byte("correct horse battery staple"), SessionMetadata{RemoteAddr: "127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	if repository.createVersion != 7 || len(repository.session.TokenHash) != 32 {
		t.Fatalf("persisted session/version = %d/%d", repository.createVersion, len(repository.session.TokenHash))
	}
	if bytes.Contains(repository.session.TokenHash, []byte(result.Token)) {
		t.Fatal("persisted hash contains token")
	}
	if !result.Session.IdleExpiresAt.Equal(now.Add(24*time.Hour)) || !result.Session.AbsoluteExpiresAt.Equal(now.Add(7*24*time.Hour)) {
		t.Fatalf("expirations = %v / %v", result.Session.IdleExpiresAt, result.Session.AbsoluteExpiresAt)
	}
}

func TestSessionExpiryBoundaryAndCSRFAreDeterministic(t *testing.T) {
	raw := bytes.Repeat([]byte{3}, 32)
	token := base64.RawURLEncoding.EncodeToString(raw)
	first, err := CSRFToken(token)
	if err != nil {
		t.Fatal(err)
	}
	second, _ := CSRFToken(token)
	if first != second || !VerifyCSRF(token, first) || VerifyCSRF(token, first+"x") {
		t.Fatal("CSRF contract differs")
	}
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	repository := &sessionRepositoryStub{administrator: Administrator{ID: "id", Username: "admin"}, session: Session{ID: "session", IdleExpiresAt: now, AbsoluteExpiresAt: now.Add(time.Hour)}}
	service := newService(repository, func() time.Time { return now }, bytes.NewReader(nil), "dummy")
	_, err = service.Authenticate(context.Background(), token)
	if !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("Authenticate() error = %v", err)
	}
}
