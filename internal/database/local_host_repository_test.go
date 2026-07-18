package database

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"github.com/mkrebs03/atalaya/internal/hosts"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestLocalHostRepositoryPersistsEncryptedSingletonAndSchedule(t *testing.T) {
	database := openTestDatabase(t, filepath.Join(t.TempDir(), "atalaya.db"))
	defer database.Close()
	random := bytes.NewReader(bytes.Repeat([]byte{0x2a}, 256))
	host, err := database.PrepareLocalHost(context.Background(), "Oracle", "https://oracle.example.ts.net:9474", testInstant, random)
	if err != nil {
		t.Fatal(err)
	}
	if len(host.Credential) != 32 {
		t.Fatalf("credential length=%d", len(host.Credential))
	}
	var envelope []byte
	if err := database.sql.QueryRow("SELECT agent_credential FROM hosts WHERE id=?", host.ID).Scan(&envelope); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(envelope, host.Credential) || bytes.Contains(envelope, []byte(base64.RawURLEncoding.EncodeToString(host.Credential))) {
		t.Fatal("credential persisted in plaintext")
	}
	if err := database.CompleteLocalHost(context.Background(), host.ID, testInstant, random); err != nil {
		t.Fatal(err)
	}
	if err := database.CompleteLocalHost(context.Background(), host.ID, testInstant.Add(time.Hour), random); err != nil {
		t.Fatal(err)
	}
	var completed string
	var periods, slots int
	if err := database.sql.QueryRow("SELECT bootstrap_completed_at FROM hosts WHERE id=?", host.ID).Scan(&completed); err != nil {
		t.Fatal(err)
	}
	if err := database.sql.QueryRow("SELECT count(*) FROM schedule_periods WHERE target_id=?", host.ID).Scan(&periods); err != nil {
		t.Fatal(err)
	}
	if err := database.sql.QueryRow("SELECT count(*) FROM schedule_slots WHERE target_id=?", host.ID).Scan(&slots); err != nil {
		t.Fatal(err)
	}
	if completed != testInstant.Format(time.RFC3339Nano) || periods != 1 || slots != 1 {
		t.Fatalf("completed=%s periods=%d slots=%d", completed, periods, slots)
	}
	recovered, err := database.PrepareLocalHost(context.Background(), "Oracle", "https://oracle.example.ts.net:9474", testInstant, random)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.ID != host.ID || !bytes.Equal(recovered.Credential, host.Credential) || !recovered.BootstrapCompleted {
		t.Fatal("rerun changed identity, credential, or completion")
	}
}

func TestUniformJitter6RejectsBiasedTailAndCoversBoundaries(t *testing.T) {
	for input, want := range map[byte]int{0: 0, 5: 5, 251: 5} {
		got, err := uniformJitter6(bytes.NewReader([]byte{input}))
		if err != nil || got != want {
			t.Fatalf("input %d got=%d err=%v", input, got, err)
		}
	}
	got, err := uniformJitter6(bytes.NewReader([]byte{252, 255, 6}))
	if err != nil || got != 0 {
		t.Fatalf("rejection got=%d err=%v", got, err)
	}
	if _, err := uniformJitter6(nil); err == nil {
		t.Fatal("nil reader accepted")
	}
	if _, err := uniformJitter6(bytes.NewReader([]byte{252})); !errors.Is(err, io.EOF) {
		t.Fatalf("short reader err=%v", err)
	}
}

func TestExistingLocalHostCredentialOwnershipClearsEveryError(t *testing.T) {
	for _, name := range []string{"parameter query", "semantic mismatch", "commit"} {
		t.Run(name, func(t *testing.T) {
			credential := bytes.Repeat([]byte{0x7b}, 32)
			_, err := transferLocalHostCredential(hosts.LocalHost{ID: "host", Credential: credential}, func() error { return errors.New(name) })
			if err == nil {
				t.Fatal("error swallowed")
			}
			if !bytes.Equal(credential, make([]byte, len(credential))) {
				t.Fatal("credential was not cleared")
			}
		})
	}
}

func TestExistingLocalHostCredentialOwnershipTransfersOnlyOnSuccess(t *testing.T) {
	credential := bytes.Repeat([]byte{0x5a}, 32)
	got, err := transferLocalHostCredential(hosts.LocalHost{ID: "host", Credential: credential}, func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Credential, bytes.Repeat([]byte{0x5a}, 32)) {
		t.Fatal("successful transfer cleared credential")
	}
	clear(got.Credential)
}

func TestConcurrentFullBootstrapUsesOneHostScheduleAndFiles(t *testing.T) {
	dir := t.TempDir()
	database := openTestDatabase(t, filepath.Join(dir, "atalaya.db"))
	defer database.Close()
	database.sql.SetMaxOpenConns(2)
	req := hosts.BootstrapRequest{AgentConfig: filepath.Join(dir, "agent.yaml"), AgentURL: "https://ORACLE.EXAMPLE.TS.NET:9474", ServerURL: "HTTP://LOCALHOST:80"}
	var wait sync.WaitGroup
	wait.Add(2)
	errs := make(chan error, 2)
	ids := make(chan string, 2)
	for seed := byte(3); seed <= 4; seed++ {
		seed := seed
		go func() {
			defer wait.Done()
			id, err := (hosts.Bootstrapper{Repository: database, Random: bytes.NewReader(bytes.Repeat([]byte{seed}, 256)), Clock: func() time.Time { return testInstant }}).Run(context.Background(), req)
			if err != nil {
				errs <- err
			} else {
				ids <- id
			}
		}()
	}
	wait.Wait()
	close(errs)
	close(ids)
	for err := range errs {
		t.Fatal(err)
	}
	var first string
	for id := range ids {
		if first == "" {
			first = id
		} else if first != id {
			t.Fatal("different host IDs")
		}
	}
	for _, path := range []string{filepath.Join(dir, "agent.yaml"), filepath.Join(dir, "agent-credential")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
	}
	var hostsCount, periods, slots int
	database.sql.QueryRow("SELECT count(*) FROM hosts").Scan(&hostsCount)
	database.sql.QueryRow("SELECT count(*) FROM schedule_periods").Scan(&periods)
	database.sql.QueryRow("SELECT count(*) FROM schedule_slots").Scan(&slots)
	if hostsCount != 1 || periods != 1 || slots != 1 {
		t.Fatalf("hosts=%d periods=%d slots=%d", hostsCount, periods, slots)
	}
}

func TestBootstrapRecoversWithRealSQLiteAtEveryBoundary(t *testing.T) {
	for _, boundary := range []string{"database_prepared", "credential_ready", "configuration_ready", "before_completion", "after_completion"} {
		t.Run(boundary, func(t *testing.T) {
			dir := t.TempDir()
			database := openTestDatabase(t, filepath.Join(dir, "atalaya.db"))
			defer database.Close()
			req := hosts.BootstrapRequest{AgentConfig: filepath.Join(dir, "agent.yaml"), AgentURL: "https://oracle.example.ts.net:9474", ServerURL: "http://localhost"}
			failed := false
			b := hosts.Bootstrapper{Repository: database, Random: bytes.NewReader(bytes.Repeat([]byte{8}, 512)), Clock: func() time.Time { return testInstant }, Boundary: func(name string) error {
				if name == boundary && !failed {
					failed = true
					return io.ErrUnexpectedEOF
				}
				return nil
			}}
			if _, err := b.Run(context.Background(), req); err == nil {
				t.Fatal("injection did not fail")
			}
			var envelope []byte
			if err := database.sql.QueryRow("SELECT agent_credential FROM hosts").Scan(&envelope); err != nil {
				t.Fatal(err)
			}
			b.Boundary = nil
			if _, err := b.Run(context.Background(), req); err != nil {
				t.Fatal(err)
			}
			var envelope2 []byte
			database.sql.QueryRow("SELECT agent_credential FROM hosts").Scan(&envelope2)
			if !bytes.Equal(envelope, envelope2) {
				t.Fatal("encrypted secret changed")
			}
			var p, s int
			database.sql.QueryRow("SELECT count(*) FROM schedule_periods").Scan(&p)
			database.sql.QueryRow("SELECT count(*) FROM schedule_slots").Scan(&s)
			if p != 1 || s != 1 {
				t.Fatalf("periods=%d slots=%d", p, s)
			}
		})
	}
}

func TestConcurrentLocalHostPreparationReturnsOneIdentity(t *testing.T) {
	database := openTestDatabase(t, filepath.Join(t.TempDir(), "atalaya.db"))
	defer database.Close()
	database.sql.SetMaxOpenConns(2)
	var wait sync.WaitGroup
	wait.Add(2)
	results := make(chan string, 2)
	errors := make(chan error, 2)
	for seed := byte(1); seed <= 2; seed++ {
		seed := seed
		go func() {
			defer wait.Done()
			host, err := database.PrepareLocalHost(context.Background(), "Oracle", "https://oracle.example.ts.net:9474", testInstant, bytes.NewReader(bytes.Repeat([]byte{seed}, 128)))
			if err != nil {
				errors <- err
				return
			}
			results <- host.ID
		}()
	}
	wait.Wait()
	close(results)
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
	var first string
	for id := range results {
		if first == "" {
			first = id
		} else if id != first {
			t.Fatalf("IDs differ: %s != %s", id, first)
		}
	}
	var count int
	if err := database.sql.QueryRow("SELECT count(*) FROM hosts WHERE source='local'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("local host count=%d", count)
	}
}

func TestIncompleteLocalHostIsExcludedBySchedulablePredicate(t *testing.T) {
	database := openTestDatabase(t, filepath.Join(t.TempDir(), "atalaya.db"))
	defer database.Close()
	host, err := database.PrepareLocalHost(context.Background(), "Oracle", "https://oracle.example.ts.net:9474", testInstant, bytes.NewReader(bytes.Repeat([]byte{1}, 128)))
	if err != nil {
		t.Fatal(err)
	}
	var visible int
	if err := database.sql.QueryRow("SELECT count(*) FROM hosts WHERE id=? AND bootstrap_completed_at IS NOT NULL AND administrative_state IN ('pending','active') AND archived_at IS NULL", host.ID).Scan(&visible); err != nil {
		t.Fatal(err)
	}
	if visible != 0 {
		t.Fatal("incomplete host is schedulable")
	}
}

func TestScheduleSchemaRejectsMismatchedAndIncompleteHostTargets(t *testing.T) {
	database := openTestDatabase(t, filepath.Join(t.TempDir(), "atalaya.db"))
	defer database.Close()
	host, err := database.PrepareLocalHost(context.Background(), "Oracle", "https://oracle.example.ts.net:9474", testInstant, bytes.NewReader(bytes.Repeat([]byte{1}, 128)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.sql.Exec(`INSERT INTO schedule_periods(id,target_type,target_id,starts_at,effective_interval_seconds,effective_timeout_seconds) VALUES('p','host',?, ?,60,10)`, host.ID, testInstant.Format(time.RFC3339Nano)); err == nil {
		t.Fatal("incomplete host period accepted")
	}
	if err := database.CompleteLocalHost(context.Background(), host.ID, testInstant, bytes.NewReader(bytes.Repeat([]byte{2}, 128))); err != nil {
		t.Fatal(err)
	}
	if _, err := database.sql.Exec(`INSERT INTO schedule_slots(id,target_type,target_id,schedule_period_id,nominal_at,not_before_at,deadline_at,effective_interval_seconds,effective_timeout_seconds,state,process_attempts) VALUES('bad','host','other',(SELECT id FROM schedule_periods LIMIT 1),?,?,?,60,10,'scheduled',0)`, testInstant.Format(time.RFC3339Nano), testInstant.Format(time.RFC3339Nano), testInstant.Add(15*time.Second).Format(time.RFC3339Nano)); err == nil {
		t.Fatal("mismatched composite target accepted")
	}
}
