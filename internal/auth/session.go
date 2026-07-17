package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	SessionIdleDuration     = 24 * time.Hour
	SessionAbsoluteDuration = 7 * 24 * time.Hour
	sessionTokenBytes       = 32
)

var (
	ErrInvalidCredentials           = errors.New("invalid credentials")
	ErrAuthenticationRequired       = errors.New("authentication required")
	ErrSessionExpired               = errors.New("session expired")
	ErrCurrentPasswordInvalid       = errors.New("current password is invalid")
	ErrNewPasswordInvalid           = errors.New("new password is invalid")
	ErrAdministratorNotFound        = errors.New("administrator not found")
	ErrAdministratorVersionConflict = errors.New("administrator version conflict")
)

type Session struct {
	ID                         string
	AdministratorID            string
	TokenHash                  []byte
	CreatedAt                  time.Time
	LastActivityAt             time.Time
	IdleExpiresAt              time.Time
	AbsoluteExpiresAt          time.Time
	RevokedAt                  *time.Time
	RemoteAddr                 string
	UserAgent                  string
	TailscaleLogin             *string
	TailscaleName              *string
	ProxyIdentityInformational bool
}

type SessionMetadata struct {
	RemoteAddr                 string
	UserAgent                  string
	TailscaleLogin             *string
	TailscaleName              *string
	ProxyIdentityInformational bool
}

type AuthenticatedSession struct {
	Administrator Administrator
	Session       Session
	Token         string
}

type Repository interface {
	FindActiveAdministrator(context.Context, string) (Administrator, error)
	CreateSessionIfAdministratorVersion(context.Context, Session, int) error
	ValidateAndTouchSession(context.Context, []byte, time.Time, time.Time) (Administrator, Session, error)
	RevokeSession(context.Context, []byte, time.Time) error
	ChangePasswordAndReplaceSessions(context.Context, string, int, string, time.Time, Session) error
	ResetPasswordAndRevokeSessions(context.Context, string, time.Time) error
}

type Service struct {
	repository Repository
	clock      func() time.Time
	random     io.Reader
	dummyHash  string
}

func NewService(repository Repository) (*Service, error) {
	dummy, err := HashPasswordWithRandom([]byte("atalaya-dummy-password"), strings.NewReader(strings.Repeat("d", passwordSaltSize)))
	if err != nil {
		return nil, err
	}
	return newService(repository, time.Now, rand.Reader, dummy), nil
}

func newService(repository Repository, clock func() time.Time, random io.Reader, dummyHash string) *Service {
	return &Service{repository: repository, clock: clock, random: random, dummyHash: dummyHash}
}

func (service *Service) Login(ctx context.Context, username string, password []byte, metadata SessionMetadata) (AuthenticatedSession, error) {
	if err := service.ready(); err != nil {
		return AuthenticatedSession{}, err
	}
	normalized, normalizeErr := NormalizeUsername(username)
	administrator, findErr := service.repository.FindActiveAdministrator(ctx, normalized)
	if normalizeErr != nil || errors.Is(findErr, ErrAdministratorNotFound) {
		_, _ = VerifyPassword(service.dummyHash, password)
		return AuthenticatedSession{}, ErrInvalidCredentials
	}
	if findErr != nil {
		return AuthenticatedSession{}, fmt.Errorf("find administrator: %w", findErr)
	}
	valid, err := VerifyPassword(administrator.PasswordHash, password)
	if err != nil {
		return AuthenticatedSession{}, fmt.Errorf("verify administrator password: %w", err)
	}
	if !valid {
		return AuthenticatedSession{}, ErrInvalidCredentials
	}
	created, err := service.newSession(administrator.ID, metadata)
	if err != nil {
		return AuthenticatedSession{}, err
	}
	if err := service.repository.CreateSessionIfAdministratorVersion(ctx, created.Session, administrator.Version); err != nil {
		if errors.Is(err, ErrAdministratorVersionConflict) {
			return AuthenticatedSession{}, ErrInvalidCredentials
		}
		return AuthenticatedSession{}, fmt.Errorf("create session: %w", err)
	}
	created.Administrator = publicAdministrator(administrator)
	return created, nil
}

func (service *Service) Authenticate(ctx context.Context, token string) (AuthenticatedSession, error) {
	if err := service.ready(); err != nil {
		return AuthenticatedSession{}, err
	}
	raw, err := decodeSessionToken(token)
	if err != nil {
		return AuthenticatedSession{}, ErrAuthenticationRequired
	}
	now := service.clock().UTC()
	administrator, session, err := service.repository.ValidateAndTouchSession(ctx, tokenDigest(raw), now, now.Add(SessionIdleDuration))
	if err != nil {
		if errors.Is(err, ErrSessionExpired) || errors.Is(err, ErrAuthenticationRequired) {
			return AuthenticatedSession{}, err
		}
		return AuthenticatedSession{}, fmt.Errorf("validate session: %w", err)
	}
	return AuthenticatedSession{Administrator: publicAdministrator(administrator), Session: session, Token: token}, nil
}

func (service *Service) Logout(ctx context.Context, token string) error {
	raw, err := decodeSessionToken(token)
	if err != nil {
		return ErrAuthenticationRequired
	}
	return service.repository.RevokeSession(ctx, tokenDigest(raw), service.clock().UTC())
}

func (service *Service) ChangePassword(ctx context.Context, currentToken string, currentPassword, newPassword []byte, metadata SessionMetadata) (AuthenticatedSession, error) {
	authenticated, err := service.Authenticate(ctx, currentToken)
	if err != nil {
		return AuthenticatedSession{}, err
	}
	administrator, err := service.repository.FindActiveAdministrator(ctx, authenticated.Administrator.Username)
	if errors.Is(err, ErrAdministratorNotFound) {
		return AuthenticatedSession{}, ErrAuthenticationRequired
	}
	if err != nil {
		return AuthenticatedSession{}, fmt.Errorf("find administrator: %w", err)
	}
	valid, err := VerifyPassword(administrator.PasswordHash, currentPassword)
	if err != nil {
		return AuthenticatedSession{}, err
	}
	if !valid {
		return AuthenticatedSession{}, ErrCurrentPasswordInvalid
	}
	if err := ValidatePassword(newPassword); err != nil {
		return AuthenticatedSession{}, fmt.Errorf("%w: %v", ErrNewPasswordInvalid, err)
	}
	newHash, err := HashPasswordWithRandom(newPassword, service.random)
	if err != nil {
		return AuthenticatedSession{}, err
	}
	replacement, err := service.newSession(administrator.ID, metadata)
	if err != nil {
		return AuthenticatedSession{}, err
	}
	now := service.clock().UTC()
	if err := service.repository.ChangePasswordAndReplaceSessions(ctx, administrator.ID, administrator.Version, newHash, now, replacement.Session); err != nil {
		if errors.Is(err, ErrAdministratorVersionConflict) {
			return AuthenticatedSession{}, ErrCurrentPasswordInvalid
		}
		return AuthenticatedSession{}, fmt.Errorf("change password: %w", err)
	}
	replacement.Administrator = publicAdministrator(administrator)
	replacement.Administrator.Version++
	replacement.Administrator.PasswordHash = ""
	return replacement, nil
}

func (service *Service) ResetPassword(ctx context.Context, newHash string) error {
	if err := ValidatePasswordHash(newHash); err != nil {
		return err
	}
	return service.repository.ResetPasswordAndRevokeSessions(ctx, newHash, service.clock().UTC())
}

func (service *Service) newSession(administratorID string, metadata SessionMetadata) (AuthenticatedSession, error) {
	raw := make([]byte, sessionTokenBytes)
	if _, err := io.ReadFull(service.random, raw); err != nil {
		return AuthenticatedSession{}, fmt.Errorf("generate session token: %w", err)
	}
	id, err := randomUUIDv4(service.random)
	if err != nil {
		return AuthenticatedSession{}, fmt.Errorf("generate session ID: %w", err)
	}
	now := service.clock().UTC()
	metadata.UserAgent = truncateUTF8(metadata.UserAgent, 512)
	session := Session{ID: id, AdministratorID: administratorID, TokenHash: tokenDigest(raw), CreatedAt: now, LastActivityAt: now, IdleExpiresAt: now.Add(SessionIdleDuration), AbsoluteExpiresAt: now.Add(SessionAbsoluteDuration), RemoteAddr: metadata.RemoteAddr, UserAgent: metadata.UserAgent, TailscaleLogin: metadata.TailscaleLogin, TailscaleName: metadata.TailscaleName, ProxyIdentityInformational: metadata.ProxyIdentityInformational}
	return AuthenticatedSession{Session: session, Token: base64.RawURLEncoding.EncodeToString(raw)}, nil
}

func CSRFToken(sessionToken string) (string, error) {
	raw, err := decodeSessionToken(sessionToken)
	if err != nil {
		return "", ErrAuthenticationRequired
	}
	mac := hmac.New(sha256.New, raw)
	_, _ = mac.Write([]byte("atalaya\x00csrf\x00v1"))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func VerifyCSRF(sessionToken, candidate string) bool {
	expected, err := CSRFToken(sessionToken)
	return err == nil && hmac.Equal([]byte(expected), []byte(candidate))
}

func decodeSessionToken(token string) ([]byte, error) {
	raw, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || len(raw) != sessionTokenBytes {
		return nil, fmt.Errorf("invalid session token")
	}
	return raw, nil
}

func tokenDigest(raw []byte) []byte { digest := sha256.Sum256(raw); return digest[:] }

func truncateUTF8(value string, maximum int) string {
	value = strings.ToValidUTF8(value, "�")
	if len(value) <= maximum {
		return value
	}
	value = value[:maximum]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func publicAdministrator(value Administrator) Administrator { value.PasswordHash = ""; return value }
func (service *Service) ready() error {
	if service == nil || service.repository == nil || service.clock == nil || service.random == nil || service.dummyHash == "" {
		return fmt.Errorf("authentication service dependencies are required")
	}
	return nil
}
