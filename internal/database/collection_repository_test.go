package database

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"testing"
	"time"

	"github.com/mkrebs03/atalaya/internal/agent"
	"github.com/mkrebs03/atalaya/internal/collection"
)

type collectionRandom struct{ next int }

type integrationClock struct{ at time.Time }

func (c integrationClock) Now() time.Time { return c.at }

type integrationResolver struct{}

func (integrationResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return []netip.Addr{netip.MustParseAddr("100.64.0.10")}, nil
}

type integrationDialer struct{ target string }

func (d integrationDialer) DialContext(ctx context.Context, network, _ string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, network, d.target)
}

func (r *collectionRandom) Intn(n int) int { return 0 }
func (r *collectionRandom) ID() (string, error) {
	r.next++
	return fmt.Sprintf("00000000-0000-4000-8000-%012d", r.next), nil
}

func TestCollectionRepositoryPersistsCurrentSnapshotAndFilesystemSemantics(t *testing.T) {
	db := openTestDatabase(t, filepath.Join(t.TempDir(), "atalaya.db"))
	defer db.Close()
	ctx := context.Background()
	bootstrapRandom := bytes.NewReader(bytes.Repeat([]byte{0x2a}, 512))
	host, err := db.PrepareLocalHost(ctx, "Oracle", "https://oracle.example.ts.net:9474", testInstant, bootstrapRandom)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(host.Credential)
	if err = db.CompleteLocalHost(ctx, host.ID, testInstant, bootstrapRandom); err != nil {
		t.Fatal(err)
	}
	claim, err := db.ClaimDue(ctx, testInstant)
	if err != nil {
		t.Fatal(err)
	}
	random := &collectionRandom{}
	uptime := uint64(10)
	cpu := 12.5
	used := uint64(100)
	total := uint64(200)
	uuid := "deadbeef"
	snapshot := agent.Snapshot{ProtocolVersion: 1, AgentVersion: "dev", Host: agent.Host{Hostname: "oracle", ObservedAt: testInstant.Format(time.RFC3339Nano), UptimeSeconds: &uptime, CPUUsedPercent: &cpu, MemoryUsedBytes: &used, MemoryTotalBytes: &total}, Tailscale: agent.Tailscale{State: "connected", Addresses: []string{"100.64.0.1"}}, Filesystems: []agent.Filesystem{{MountPoint: "/", MountRoot: "/", Device: "/dev/sda1", MajorMinor: "8:1", FSType: "ext4", FSUUID: &uuid, UsedBytes: 100, TotalBytes: 200, AvailableBytes: 100}}, CollectionWarnings: []string{}}
	if err = db.Finish(ctx, claim, collection.Result{Snapshot: &snapshot, ReceivedAt: testInstant}, random); err != nil {
		t.Fatal(err)
	}
	var administrative, observed string
	var failures, stale, filesystems int
	if err = db.sql.QueryRow(`SELECT administrative_state,observed_state,consecutive_failures,stale FROM hosts WHERE id=?`, host.ID).Scan(&administrative, &observed, &failures, &stale); err != nil {
		t.Fatal(err)
	}
	db.sql.QueryRow(`SELECT count(*) FROM filesystems WHERE host_id=? AND present=1 AND selected=1`, host.ID).Scan(&filesystems)
	if administrative != "active" || observed != "healthy" || failures != 0 || stale != 0 || filesystems != 1 {
		t.Fatalf("host=%s/%s failures=%d stale=%d filesystems=%d", administrative, observed, failures, stale, filesystems)
	}
	var generation int64
	if err := db.sql.QueryRow(`SELECT collection_generation FROM hosts WHERE id=?`, host.ID).Scan(&generation); err != nil || generation != 1 {
		t.Fatalf("generation=%d err=%v", generation, err)
	}
	claim, err = db.ClaimDue(ctx, testInstant.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Filesystems = nil
	snapshot.CollectionWarnings = []string{"filesystems_partial"}
	snapshot.Host.ObservedAt = testInstant.Add(time.Minute).Format(time.RFC3339Nano)
	if err = db.Finish(ctx, claim, collection.Result{Snapshot: &snapshot, ReceivedAt: testInstant.Add(time.Minute)}, random); err != nil {
		t.Fatal(err)
	}
	db.sql.QueryRow(`SELECT count(*) FROM filesystems WHERE host_id=? AND present=1`, host.ID).Scan(&filesystems)
	if filesystems != 1 {
		t.Fatalf("partial snapshot removed filesystem")
	}
	var versionBefore int
	if err := db.sql.QueryRow(`SELECT version FROM hosts WHERE id=?`, host.ID).Scan(&versionBefore); err != nil {
		t.Fatal(err)
	}
	if err := db.Maintain(ctx, testInstant.Add(3*time.Minute), random); err != nil {
		t.Fatal(err)
	}
	var versionAfter int
	if err := db.sql.QueryRow(`SELECT stale,version FROM hosts WHERE id=?`, host.ID).Scan(&stale, &versionAfter); err != nil {
		t.Fatal(err)
	}
	if stale != 1 || versionAfter != versionBefore+1 {
		t.Fatalf("stale=%d versions=%d->%d", stale, versionBefore, versionAfter)
	}
}

func TestProtocolFailureLeavesCurrentSnapshotAndFilesystemsIntact(t *testing.T) {
	db := openTestDatabase(t, filepath.Join(t.TempDir(), "atalaya.db"))
	defer db.Close()
	ctx := context.Background()
	source := bytes.NewReader(bytes.Repeat([]byte{0x2a}, 512))
	host, err := db.PrepareLocalHost(ctx, "Oracle", "https://oracle.example.ts.net:9474", testInstant, source)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(host.Credential)
	if err = db.CompleteLocalHost(ctx, host.ID, testInstant, source); err != nil {
		t.Fatal(err)
	}
	claim, err := db.ClaimDue(ctx, testInstant)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := minimalSnapshot(testInstant)
	snapshot.Filesystems = []agent.Filesystem{validDatabaseFilesystem()}
	random := &collectionRandom{}
	if err = db.Finish(ctx, claim, collection.Result{Snapshot: &snapshot, ReceivedAt: testInstant}, random); err != nil {
		t.Fatal(err)
	}
	var receivedBefore string
	var filesystemVersionBefore int
	var metricSamplesBefore int
	var observedBefore, successBefore, attemptBefore string
	var hostVersionBefore, failuresBefore int
	if err = db.sql.QueryRow(`SELECT received_at FROM host_snapshots WHERE host_id=?`, host.ID).Scan(&receivedBefore); err != nil {
		t.Fatal(err)
	}
	if err = db.sql.QueryRow(`SELECT version FROM filesystems WHERE host_id=?`, host.ID).Scan(&filesystemVersionBefore); err != nil {
		t.Fatal(err)
	}
	if err = db.sql.QueryRow(`SELECT count(*) FROM metric_samples WHERE host_id=?`, host.ID).Scan(&metricSamplesBefore); err != nil {
		t.Fatal(err)
	}
	if err = db.sql.QueryRow(`SELECT observed_state,version,consecutive_failures,COALESCE(last_success_received_at,''),COALESCE(last_attempt_at,'') FROM hosts WHERE id=?`, host.ID).Scan(&observedBefore, &hostVersionBefore, &failuresBefore, &successBefore, &attemptBefore); err != nil {
		t.Fatal(err)
	}
	agentServer, roots := duplicatePayloadServer(t)
	defer agentServer.Close()
	client, err := collection.NewClient(integrationResolver{}, integrationDialer{target: agentServer.Listener.Addr().String()}, &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatal(err)
	}

	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	failures := make(chan collection.FailureKind, 1)
	go func() {
		done <- (collection.Scheduler{Repository: db, Puller: client, Clock: integrationClock{at: testInstant.Add(time.Minute)}, Random: random, PollInterval: time.Millisecond, OnPullError: func(kind collection.FailureKind) { failures <- kind }}).Run(runCtx)
	}()
	if kind := <-failures; kind != collection.FailureProtocol {
		t.Fatalf("failure=%s", kind)
	}
	deadline := time.Now().Add(time.Second)
	for {
		var failed int
		_ = db.sql.QueryRow(`SELECT count(*) FROM schedule_slots WHERE target_id=? AND state='failed'`, host.ID).Scan(&failed)
		if failed > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err = <-done; err != nil {
		t.Fatalf("scheduler stopped on protocol failure: %v", err)
	}
	var receivedAfter string
	var filesystemVersionAfter int
	var metricSamplesAfter int
	var observedAfter, successAfter, attemptAfter string
	var hostVersionAfter, failuresAfter int
	if err = db.sql.QueryRow(`SELECT received_at FROM host_snapshots WHERE host_id=?`, host.ID).Scan(&receivedAfter); err != nil {
		t.Fatal(err)
	}
	if err = db.sql.QueryRow(`SELECT version FROM filesystems WHERE host_id=?`, host.ID).Scan(&filesystemVersionAfter); err != nil {
		t.Fatal(err)
	}
	if err = db.sql.QueryRow(`SELECT count(*) FROM metric_samples WHERE host_id=?`, host.ID).Scan(&metricSamplesAfter); err != nil {
		t.Fatal(err)
	}
	if err = db.sql.QueryRow(`SELECT observed_state,version,consecutive_failures,COALESCE(last_success_received_at,''),COALESCE(last_attempt_at,'') FROM hosts WHERE id=?`, host.ID).Scan(&observedAfter, &hostVersionAfter, &failuresAfter, &successAfter, &attemptAfter); err != nil {
		t.Fatal(err)
	}
	if receivedAfter != receivedBefore || filesystemVersionAfter != filesystemVersionBefore || metricSamplesAfter != metricSamplesBefore || observedAfter != observedBefore || hostVersionAfter != hostVersionBefore || failuresAfter != failuresBefore || successAfter != successBefore || attemptAfter != attemptBefore {
		t.Fatalf("current state mutated: snapshot %q->%q fs=%d->%d metric_samples=%d->%d observed=%s->%s host_version=%d->%d failures=%d->%d success=%q->%q attempt=%q->%q", receivedBefore, receivedAfter, filesystemVersionBefore, filesystemVersionAfter, metricSamplesBefore, metricSamplesAfter, observedBefore, observedAfter, hostVersionBefore, hostVersionAfter, failuresBefore, failuresAfter, successBefore, successAfter, attemptBefore, attemptAfter)
	}
}

func duplicatePayloadServer(t *testing.T) (*httptest.Server, *x509.CertPool) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "oracle.example.ts.net"}, DNSNames: []string{"oracle.example.ts.net"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, public, private)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"protocol_version":1,"protocol_version":2,"agent_version":"dev","host":{"hostname":"oracle","observed_at":"2026-07-17T00:00:00Z","boot_id":null,"uptime_seconds":null,"cpu_used_percent":null,"memory_used_bytes":null,"memory_total_bytes":null},"tailscale":{"state":"connected","hostname":null,"addresses":[]},"filesystems":[],"collection_warnings":[]}`))
	}))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: private}}, MinVersion: tls.VersionTLS12}
	server.StartTLS()
	roots := x509.NewCertPool()
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots.AddCert(certificate)
	return server, roots
}

func validDatabaseFilesystem() agent.Filesystem {
	uuid := "deadbeef"
	return agent.Filesystem{MountPoint: "/", MountRoot: "/", Device: "/dev/sda1", MajorMinor: "8:1", FSType: "ext4", FSUUID: &uuid, UsedBytes: 100, TotalBytes: 200, AvailableBytes: 100}
}

func TestCollectionFinishDiscardsChangedTargetAndReanchors(t *testing.T) {
	db := openTestDatabase(t, filepath.Join(t.TempDir(), "atalaya.db"))
	defer db.Close()
	ctx := context.Background()
	source := bytes.NewReader(bytes.Repeat([]byte{0x2a}, 512))
	host, err := db.PrepareLocalHost(ctx, "Oracle", "https://oracle.example.ts.net:9474", testInstant, source)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(host.Credential)
	if err = db.CompleteLocalHost(ctx, host.ID, testInstant, source); err != nil {
		t.Fatal(err)
	}
	claim, err := db.ClaimDue(ctx, testInstant)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.sql.Exec(`UPDATE hosts SET interval_seconds=120,timeout_seconds=20,collection_generation=collection_generation+1 WHERE id=?`, host.ID); err != nil {
		t.Fatal(err)
	}
	snapshot := minimalSnapshot(testInstant)
	if err = db.Finish(ctx, claim, collection.Result{Snapshot: &snapshot, ReceivedAt: testInstant.Add(time.Second)}, &collectionRandom{}); err != nil {
		t.Fatal(err)
	}
	var changed, snapshots, scheduled int
	db.sql.QueryRow(`SELECT count(*) FROM schedule_slots WHERE target_id=? AND cancellation_reason='target_changed'`, host.ID).Scan(&changed)
	db.sql.QueryRow(`SELECT count(*) FROM host_snapshots WHERE host_id=?`, host.ID).Scan(&snapshots)
	db.sql.QueryRow(`SELECT count(*) FROM schedule_slots WHERE target_id=? AND state='scheduled' AND effective_interval_seconds=120 AND effective_timeout_seconds=20`, host.ID).Scan(&scheduled)
	if changed != 1 || snapshots != 0 || scheduled != 1 {
		t.Fatalf("changed=%d snapshots=%d scheduled=%d", changed, snapshots, scheduled)
	}
}

func TestCollectionMaintainAdministrativeCancellationWinsOverExpiry(t *testing.T) {
	db := openTestDatabase(t, filepath.Join(t.TempDir(), "atalaya.db"))
	defer db.Close()
	ctx := context.Background()
	source := bytes.NewReader(bytes.Repeat([]byte{0x2a}, 512))
	host, err := db.PrepareLocalHost(ctx, "Oracle", "https://oracle.example.ts.net:9474", testInstant, source)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(host.Credential)
	if err = db.CompleteLocalHost(ctx, host.ID, testInstant, source); err != nil {
		t.Fatal(err)
	}
	if _, err = db.sql.Exec(`UPDATE hosts SET administrative_state='disabled' WHERE id=?`, host.ID); err != nil {
		t.Fatal(err)
	}
	if err = db.Maintain(ctx, testInstant.Add(10*time.Minute), &collectionRandom{}); err != nil {
		t.Fatal(err)
	}
	var disabled, scheduled, open int
	db.sql.QueryRow(`SELECT count(*) FROM schedule_slots WHERE target_id=? AND cancellation_reason='target_disabled'`, host.ID).Scan(&disabled)
	db.sql.QueryRow(`SELECT count(*) FROM schedule_slots WHERE target_id=? AND state='scheduled'`, host.ID).Scan(&scheduled)
	db.sql.QueryRow(`SELECT count(*) FROM schedule_periods WHERE target_id=? AND ends_at IS NULL`, host.ID).Scan(&open)
	if disabled != 1 || scheduled != 0 || open != 0 {
		t.Fatalf("disabled=%d scheduled=%d open=%d", disabled, scheduled, open)
	}
}

func TestCollectionClockSkewWarningIsServerDerivedAndReplaced(t *testing.T) {
	db := openTestDatabase(t, filepath.Join(t.TempDir(), "atalaya.db"))
	defer db.Close()
	ctx := context.Background()
	source := bytes.NewReader(bytes.Repeat([]byte{0x2a}, 512))
	host, err := db.PrepareLocalHost(ctx, "Oracle", "https://oracle.example.ts.net:9474", testInstant, source)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(host.Credential)
	if err = db.CompleteLocalHost(ctx, host.ID, testInstant, source); err != nil {
		t.Fatal(err)
	}
	random := &collectionRandom{}
	claim, err := db.ClaimDue(ctx, testInstant)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := minimalSnapshot(testInstant.Add(6 * time.Minute))
	if err = db.Finish(ctx, claim, collection.Result{Snapshot: &snapshot, ReceivedAt: testInstant}, random); err != nil {
		t.Fatal(err)
	}
	var warnings int
	var state string
	db.sql.QueryRow(`SELECT count(*) FROM host_snapshot_warnings WHERE host_id=? AND code='clock_skew'`, host.ID).Scan(&warnings)
	db.sql.QueryRow(`SELECT observed_state FROM hosts WHERE id=?`, host.ID).Scan(&state)
	if warnings != 1 || state != "degraded" {
		t.Fatalf("warnings=%d state=%s", warnings, state)
	}
	claim, err = db.ClaimDue(ctx, testInstant.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	snapshot = minimalSnapshot(testInstant.Add(time.Minute))
	if err = db.Finish(ctx, claim, collection.Result{Snapshot: &snapshot, ReceivedAt: testInstant.Add(time.Minute)}, random); err != nil {
		t.Fatal(err)
	}
	db.sql.QueryRow(`SELECT count(*) FROM host_snapshot_warnings WHERE host_id=? AND code='clock_skew'`, host.ID).Scan(&warnings)
	db.sql.QueryRow(`SELECT observed_state FROM hosts WHERE id=?`, host.ID).Scan(&state)
	if warnings != 0 || state != "healthy" {
		t.Fatalf("replacement warnings=%d state=%s", warnings, state)
	}
}

func minimalSnapshot(at time.Time) agent.Snapshot {
	return agent.Snapshot{ProtocolVersion: 1, AgentVersion: "dev", Host: agent.Host{Hostname: "oracle", ObservedAt: at.Format(time.RFC3339Nano)}, Tailscale: agent.Tailscale{State: "connected", Addresses: []string{}}, Filesystems: []agent.Filesystem{}, CollectionWarnings: []string{}}
}

func TestCollectionMaintainReanchorsExpiredSlotWithoutCatchup(t *testing.T) {
	db := openTestDatabase(t, filepath.Join(t.TempDir(), "atalaya.db"))
	defer db.Close()
	ctx := context.Background()
	source := bytes.NewReader(bytes.Repeat([]byte{0x2a}, 512))
	host, err := db.PrepareLocalHost(ctx, "Oracle", "https://oracle.example.ts.net:9474", testInstant, source)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(host.Credential)
	if err = db.CompleteLocalHost(ctx, host.ID, testInstant, source); err != nil {
		t.Fatal(err)
	}
	now := testInstant.Add(10 * time.Minute)
	if err = db.Maintain(ctx, now, &collectionRandom{}); err != nil {
		t.Fatal(err)
	}
	var cancelled, scheduled, periods int
	db.sql.QueryRow(`SELECT count(*) FROM schedule_slots WHERE target_id=? AND state='cancelled' AND cancellation_reason='missed_deadline'`, host.ID).Scan(&cancelled)
	db.sql.QueryRow(`SELECT count(*) FROM schedule_slots WHERE target_id=? AND state='scheduled'`, host.ID).Scan(&scheduled)
	db.sql.QueryRow(`SELECT count(*) FROM schedule_periods WHERE target_id=?`, host.ID).Scan(&periods)
	if cancelled != 1 || scheduled != 1 || periods != 2 {
		t.Fatalf("cancelled=%d scheduled=%d periods=%d", cancelled, scheduled, periods)
	}
}

func TestCollectionUnreachableOnlyAtThirdFailedDeadline(t *testing.T) {
	db := openTestDatabase(t, filepath.Join(t.TempDir(), "atalaya.db"))
	defer db.Close()
	ctx := context.Background()
	source := bytes.NewReader(bytes.Repeat([]byte{0x2a}, 512))
	host, err := db.PrepareLocalHost(ctx, "Oracle", "https://oracle.example.ts.net:9474", testInstant, source)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(host.Credential)
	if err = db.CompleteLocalHost(ctx, host.ID, testInstant, source); err != nil {
		t.Fatal(err)
	}
	random := &collectionRandom{}
	for index := 0; index < 3; index++ {
		at := testInstant.Add(time.Duration(index) * time.Minute)
		claim, claimErr := db.ClaimDue(ctx, at)
		if claimErr != nil {
			t.Fatal(claimErr)
		}
		if err = db.Finish(ctx, claim, collection.Result{Failure: collection.FailureConnect, ReceivedAt: at}, random); err != nil {
			t.Fatal(err)
		}
	}
	var state string
	if err = db.sql.QueryRow(`SELECT observed_state FROM hosts WHERE id=?`, host.ID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "unknown" {
		t.Fatalf("before deadline state=%s", state)
	}
	if err = db.Maintain(ctx, testInstant.Add(2*time.Minute+14*time.Second), random); err != nil {
		t.Fatal(err)
	}
	db.sql.QueryRow(`SELECT observed_state FROM hosts WHERE id=?`, host.ID).Scan(&state)
	if state != "unknown" {
		t.Fatalf("early state=%s", state)
	}
	if err = db.Maintain(ctx, testInstant.Add(2*time.Minute+15*time.Second), random); err != nil {
		t.Fatal(err)
	}
	db.sql.QueryRow(`SELECT observed_state FROM hosts WHERE id=?`, host.ID).Scan(&state)
	if state != "unreachable" {
		t.Fatalf("deadline state=%s", state)
	}
	var versionAtThreshold int
	if err = db.sql.QueryRow(`SELECT version FROM hosts WHERE id=?`, host.ID).Scan(&versionAtThreshold); err != nil {
		t.Fatal(err)
	}
	for index := 3; index < 5; index++ {
		at := testInstant.Add(time.Duration(index) * time.Minute)
		claim, claimErr := db.ClaimDue(ctx, at)
		if claimErr != nil {
			t.Fatal(claimErr)
		}
		if err = db.Finish(ctx, claim, collection.Result{Failure: collection.FailureConnect, ReceivedAt: at}, random); err != nil {
			t.Fatal(err)
		}
		var versionAfterFailure int
		if err = db.sql.QueryRow(`SELECT observed_state,version FROM hosts WHERE id=?`, host.ID).Scan(&state, &versionAfterFailure); err != nil {
			t.Fatal(err)
		}
		if state != "unreachable" {
			t.Fatalf("failure %d downgraded state=%s", index+1, state)
		}
		if versionAfterFailure != versionAtThreshold+1 {
			t.Fatalf("failure %d version=%d want=%d", index+1, versionAfterFailure, versionAtThreshold+1)
		}
		deadline := at.Add(15 * time.Second)
		if err = db.Maintain(ctx, deadline, random); err != nil {
			t.Fatal(err)
		}
		var versionAfterDeadline int
		if err = db.sql.QueryRow(`SELECT observed_state,version FROM hosts WHERE id=?`, host.ID).Scan(&state, &versionAfterDeadline); err != nil {
			t.Fatal(err)
		}
		if state != "unreachable" || versionAfterDeadline != versionAfterFailure {
			t.Fatalf("deadline %d state=%s version=%d want=%d", index+1, state, versionAfterDeadline, versionAfterFailure)
		}
		versionAtThreshold = versionAfterDeadline
	}
}

func TestThreeProtocolFailuresDoNotContributeToUnreachable(t *testing.T) {
	db := openTestDatabase(t, filepath.Join(t.TempDir(), "atalaya.db"))
	defer db.Close()
	ctx := context.Background()
	source := bytes.NewReader(bytes.Repeat([]byte{0x2a}, 512))
	host, err := db.PrepareLocalHost(ctx, "Oracle", "https://oracle.example.ts.net:9474", testInstant, source)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(host.Credential)
	if err = db.CompleteLocalHost(ctx, host.ID, testInstant, source); err != nil {
		t.Fatal(err)
	}
	var stateBefore string
	var versionBefore int
	if err = db.sql.QueryRow(`SELECT observed_state,version FROM hosts WHERE id=?`, host.ID).Scan(&stateBefore, &versionBefore); err != nil {
		t.Fatal(err)
	}
	random := &collectionRandom{}
	for range 3 {
		var due string
		if err = db.sql.QueryRow(`SELECT not_before_at FROM schedule_slots WHERE target_id=? AND state='scheduled' ORDER BY nominal_at LIMIT 1`, host.ID).Scan(&due); err != nil {
			t.Fatal(err)
		}
		at, parseErr := parseDBTime(due)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		claim, claimErr := db.ClaimDue(ctx, at)
		if claimErr != nil {
			t.Fatal(claimErr)
		}
		if err = db.Finish(ctx, claim, collection.Result{Failure: collection.FailureProtocol, ReceivedAt: at}, random); err != nil {
			t.Fatal(err)
		}
	}
	var stateAfter string
	var versionAfter, failures int
	if err = db.sql.QueryRow(`SELECT observed_state,version,consecutive_failures FROM hosts WHERE id=?`, host.ID).Scan(&stateAfter, &versionAfter, &failures); err != nil {
		t.Fatal(err)
	}
	if stateAfter != stateBefore || versionAfter != versionBefore || failures != 0 {
		t.Fatalf("protocol failures affected availability: state=%s->%s version=%d->%d failures=%d", stateBefore, stateAfter, versionBefore, versionAfter, failures)
	}
}

func TestCollectionLeaseRecoveryRetryCapAndUniqueClaim(t *testing.T) {
	db := openTestDatabase(t, filepath.Join(t.TempDir(), "atalaya.db"))
	defer db.Close()
	ctx := context.Background()
	source := bytes.NewReader(bytes.Repeat([]byte{0x2a}, 512))
	host, err := db.PrepareLocalHost(ctx, "Oracle", "https://oracle.example.ts.net:9474", testInstant, source)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(host.Credential)
	if err = db.CompleteLocalHost(ctx, host.ID, testInstant, source); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	claims := make(chan collection.Claim, 2)
	for index := 0; index < 2; index++ {
		go func() {
			claim, claimErr := db.ClaimDue(ctx, testInstant)
			if claimErr == nil {
				claims <- claim
			}
			results <- claimErr
		}()
	}
	success := 0
	for index := 0; index < 2; index++ {
		claimErr := <-results
		if claimErr == nil {
			success++
		} else if !errors.Is(claimErr, collection.ErrNoDueSlot) {
			t.Fatal(claimErr)
		}
	}
	if success != 1 {
		t.Fatalf("successful claims=%d", success)
	}
	first := <-claims
	if err = db.Maintain(ctx, testInstant.Add(15*time.Second), &collectionRandom{}); err != nil {
		t.Fatal(err)
	}
	second, err := db.ClaimDue(ctx, testInstant.Add(15*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if second.SlotID != first.SlotID {
		t.Fatal("restart changed slot identity")
	}
	if err = db.Release(ctx, second, testInstant.Add(15*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ClaimDue(ctx, testInstant.Add(15*time.Second)); !errors.Is(err, collection.ErrNoDueSlot) {
		t.Fatalf("retry cap err=%v", err)
	}
	if err = db.Maintain(ctx, testInstant.Add(16*time.Second), &collectionRandom{}); err != nil {
		t.Fatal(err)
	}
	var missed int
	db.sql.QueryRow(`SELECT count(*) FROM schedule_slots WHERE id=? AND cancellation_reason='missed_deadline'`, first.SlotID).Scan(&missed)
	if missed != 1 {
		t.Fatalf("missed=%d", missed)
	}
	first.Clear()
	second.Clear()
}
