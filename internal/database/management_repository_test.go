package database

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mkrebs03/atalaya/internal/auth"
	"github.com/mkrebs03/atalaya/internal/hosts"
)

func TestEnrollmentTokenIsHashedAndExactReplayIsIdempotent(t *testing.T) {
	database := openTestDatabase(t, t.TempDir()+"/management.db")
	admin := auth.Administrator{
		ID: "00000000-0000-4000-8000-000000000009", Username: "admin", PasswordHash: testPasswordPHC,
		CreatedAt: testInstant.Format(time.RFC3339Nano), UpdatedAt: testInstant.Format(time.RFC3339Nano),
		PasswordChangedAt: testInstant.Format(time.RFC3339Nano), Version: 1,
	}
	if err := database.CreateAdministrator(context.Background(), admin); err != nil {
		t.Fatal(err)
	}
	token, secret, err := database.CreateEnrollmentToken(context.Background(), "remote", admin.ID, testInstant, bytes.NewReader(bytes.Repeat([]byte{4}, 256)))
	if err != nil {
		t.Fatal(err)
	}
	if len(secret) != 43 {
		t.Fatalf("secret length = %d", len(secret))
	}
	var stored []byte
	if err := database.sql.QueryRow("SELECT token_hash FROM enrollment_tokens WHERE id=?", token.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stored, []byte(secret)) || bytes.Equal(stored, []byte(secret)) {
		t.Fatal("enrollment token was stored in plaintext")
	}

	request := hosts.EnrollmentRequest{Token: secret, RequestID: "request-1", DisplayName: "Remote", AgentURL: "https://remote.example.ts.net:9474", AgentVersion: "dev", ProtocolVersion: 1}
	first, err := database.EnrollAgent(context.Background(), request, testInstant, bytes.NewReader(bytes.Repeat([]byte{8}, 512)))
	if err != nil {
		t.Fatal(err)
	}
	if first.HostID == "" || len(first.AgentCredential) != 32 {
		t.Fatalf("enrollment response = %+v", first)
	}
	replay, err := database.EnrollAgent(context.Background(), request, testInstant.Add(time.Minute), bytes.NewReader(bytes.Repeat([]byte{9}, 512)))
	if err != nil {
		t.Fatal(err)
	}
	if replay.HostID != first.HostID || !bytes.Equal(replay.AgentCredential, first.AgentCredential) {
		t.Fatal("exact replay did not return the original enrollment")
	}
	var hostsCount int
	if err := database.sql.QueryRow("SELECT count(*) FROM hosts WHERE source='enrolled'").Scan(&hostsCount); err != nil {
		t.Fatal(err)
	}
	if hostsCount != 1 {
		t.Fatalf("enrolled hosts = %d", hostsCount)
	}
	request.RequestID = "request-2"
	if _, err := database.EnrollAgent(context.Background(), request, testInstant.Add(time.Minute), bytes.NewReader(bytes.Repeat([]byte{9}, 512))); !errors.Is(err, hosts.ErrEnrollmentConsumed) {
		t.Fatalf("different replay error = %v", err)
	}
}

func TestHostManagementChangesScheduleAndPreservesETagSemantics(t *testing.T) {
	database := openTestDatabase(t, t.TempDir()+"/management.db")
	admin := auth.Administrator{ID: "00000000-0000-4000-8000-000000000010", Username: "admin", PasswordHash: testPasswordPHC, CreatedAt: testInstant.Format(time.RFC3339Nano), UpdatedAt: testInstant.Format(time.RFC3339Nano), PasswordChangedAt: testInstant.Format(time.RFC3339Nano), Version: 1}
	if err := database.CreateAdministrator(context.Background(), admin); err != nil {
		t.Fatal(err)
	}
	token, secret, err := database.CreateEnrollmentToken(context.Background(), "", admin.ID, testInstant, bytes.NewReader(bytes.Repeat([]byte{5}, 256)))
	if err != nil {
		t.Fatal(err)
	}
	request := hosts.EnrollmentRequest{Token: secret, RequestID: "request-2", DisplayName: "Remote", AgentURL: "https://remote-2.example.ts.net:9474", AgentVersion: "dev", ProtocolVersion: 1}
	if _, err := database.EnrollAgent(context.Background(), request, testInstant, bytes.NewReader(bytes.Repeat([]byte{7}, 512))); err != nil {
		t.Fatal(err)
	}
	var hostID string
	if err := database.sql.QueryRow("SELECT host_id FROM enrollment_tokens WHERE id=?", token.ID).Scan(&hostID); err != nil {
		t.Fatal(err)
	}
	detail, err := database.GetHost(context.Background(), hostID)
	if err != nil {
		t.Fatal(err)
	}
	oldVersion := detail.Version
	interval, timeout := 120, 10
	if err := database.PatchHost(context.Background(), hostID, oldVersion, hosts.HostPatch{IntervalSeconds: &interval, TimeoutSeconds: &timeout}, testInstant.Add(time.Minute), bytes.NewReader(bytes.Repeat([]byte{3}, 256))); err != nil {
		t.Fatal(err)
	}
	var currentInterval int
	if err := database.sql.QueryRow("SELECT interval_seconds FROM hosts WHERE id=?", hostID).Scan(&currentInterval); err != nil {
		t.Fatal(err)
	}
	if currentInterval != interval {
		t.Fatalf("interval = %d", currentInterval)
	}
	if err := database.PatchHost(context.Background(), hostID, oldVersion, hosts.HostPatch{DisplayName: stringPointerValue("Other")}, testInstant, bytes.NewReader(bytes.Repeat([]byte{3}, 256))); !errors.Is(err, hosts.ErrHostVersionConflict) {
		t.Fatalf("stale patch = %v", err)
	}
}

func stringPointerValue(value string) *string { return &value }
