package database

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	atalayacrypto "github.com/mkrebs03/atalaya/internal/crypto"
)

func TestVerifyDecryptsPersistedAgentAndNotificationSecrets(t *testing.T) {
	t.Parallel()

	key := bytes.Repeat([]byte{0x34}, 32)
	path := filepath.Join(t.TempDir(), "atalaya.db")
	owner, err := Open(context.Background(), path, key)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()

	agentEnvelope, err := atalayacrypto.Seal(key, "agent", "host-1", []byte("agent credential"))
	if err != nil {
		t.Fatal(err)
	}
	channelEnvelope, err := atalayacrypto.Seal(key, "notification-channel", "channel-1", []byte("{\"webhook\":\"https://discord.example\"}"))
	if err != nil {
		t.Fatal(err)
	}
	createdAt := "2026-08-01T00:00:00Z"
	_, err = owner.sql.ExecContext(context.Background(), `
		INSERT INTO hosts(
			id,display_name,source,agent_url,agent_credential,administrative_state,
			observed_state,interval_seconds,timeout_seconds,consecutive_failures,stale,
			created_at,updated_at,collection_generation,version
		) VALUES(?,?,?,?,?,'active','unknown',60,10,0,0,?,?,1,1)
	`, "host-1", "Oracle", "enrolled", "https://oracle.example:9474", agentEnvelope, createdAt, createdAt)
	if err != nil {
		t.Fatal(err)
	}
	_, err = owner.sql.ExecContext(context.Background(), `
		INSERT INTO notification_channels(
			id,name,provider,encrypted_config,enabled,created_at,updated_at,version
		) VALUES(?,?,?, ?,1,?,?,1)
	`, "channel-1", "Discord", "discord", channelEnvelope, createdAt, createdAt)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := Verify(context.Background(), path, key); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}

	if _, err := owner.sql.ExecContext(context.Background(), "UPDATE hosts SET agent_credential=x'01' WHERE id='host-1'"); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(context.Background(), path, key); !errors.Is(err, ErrCorruptDatabase) {
		t.Fatalf("corrupt encrypted credential error = %v", err)
	}
}

func TestSystemStatusIsReadOnlyAndUsesStableWorkerStates(t *testing.T) {
	t.Parallel()

	key := bytes.Repeat([]byte{0x45}, 32)
	path := filepath.Join(t.TempDir(), "atalaya.db")
	owner, err := Open(context.Background(), path, key)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()

	status, err := owner.SystemStatus(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	if status.Version != "test" || status.SchemaVersion != len(embeddedMigrations) || status.DatabaseSizeBytes <= 0 {
		t.Fatalf("status = %+v", status)
	}
	if status.Collection.State != "idle" || status.Collection.ErrorCode != "no_targets" {
		t.Fatalf("collection = %+v", status.Collection)
	}
	if status.Retention.State != "unknown" || status.Retention.ErrorCode != "not_started" {
		t.Fatalf("retention = %+v", status.Retention)
	}
	if status.Checks.State != "idle" || status.Checks.ErrorCode != "no_checks" {
		t.Fatalf("checks = %+v", status.Checks)
	}
	if status.Notifications.State != "idle" || status.Notifications.ErrorCode != "no_channels" {
		t.Fatalf("notifications = %+v", status.Notifications)
	}
}

func TestSystemStatusDoesNotTreatHistoricalCollectionSuccessAsHealthy(t *testing.T) {
	key := bytes.Repeat([]byte{0x46}, 32)
	path := filepath.Join(t.TempDir(), "atalaya.db")
	owner, err := Open(context.Background(), path, key)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()

	now := time.Now().UTC()
	oldSuccess := dbTime(now.Add(-2 * time.Hour))
	oldDeadline := dbTime(now.Add(-time.Hour))
	_, err = owner.sql.ExecContext(context.Background(), `
		INSERT INTO hosts(
			id,display_name,source,agent_url,agent_credential,administrative_state,
			observed_state,interval_seconds,timeout_seconds,last_attempt_at,last_success_received_at,
			freshness_deadline_at,consecutive_failures,stale,created_at,updated_at,collection_generation,version
		) VALUES('stale-host','Stale host','enrolled','https://stale.example:9474',x'01','active',
			'healthy',60,10,?,?,?,0,0,?,?,1,1)`, oldSuccess, oldSuccess, oldDeadline, oldSuccess, oldSuccess)
	if err != nil {
		t.Fatal(err)
	}

	status, err := owner.SystemStatus(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	if status.Collection.State != "degraded" || status.Collection.ErrorCode != "target_unavailable" {
		t.Fatalf("stale collection = %+v", status.Collection)
	}
}

func TestSystemStatusEvaluatesCurrentCheckState(t *testing.T) {
	key := bytes.Repeat([]byte{0x47}, 32)
	path := filepath.Join(t.TempDir(), "atalaya.db")
	owner, err := Open(context.Background(), path, key)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()

	now := time.Now().UTC()
	oldResult := dbTime(now.Add(-2 * time.Hour))
	oldDeadline := dbTime(now.Add(-time.Hour))
	created := dbTime(now.Add(-3 * time.Hour))
	_, err = owner.sql.ExecContext(context.Background(), `
		INSERT INTO service_checks(
			id,name,type,target_url,interval_seconds,timeout_seconds,expected_status_min,expected_status_max,
			follow_redirects,max_redirects,enabled,observed_state,last_attempt_at,last_result_at,
			freshness_deadline_at,consecutive_failures,created_at,updated_at,configuration_generation,version
		) VALUES('stale-check','Stale check','http','https://check.example',60,10,200,299,0,1,1,'up',?,?,?,1,?,?,1,1)`, oldResult, oldResult, oldDeadline, created, created)
	if err != nil {
		t.Fatal(err)
	}

	status, err := owner.SystemStatus(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	if status.Checks.State != "degraded" || status.Checks.ErrorCode != "stale" {
		t.Fatalf("stale checks = %+v", status.Checks)
	}

	freshResult := dbTime(now)
	freshDeadline := dbTime(now.Add(time.Hour))
	if _, err := owner.sql.ExecContext(context.Background(), `UPDATE service_checks SET observed_state='down',last_attempt_at=?,last_result_at=?,freshness_deadline_at=?,updated_at=? WHERE id='stale-check'`, freshResult, freshResult, freshDeadline, freshResult); err != nil {
		t.Fatal(err)
	}
	status, err = owner.SystemStatus(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	if status.Checks.State != "degraded" || status.Checks.ErrorCode != "check_failed" {
		t.Fatalf("failed checks = %+v", status.Checks)
	}
}
