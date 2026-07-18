package database

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/mkrebs03/atalaya/internal/hostquery"
)

func TestHostQueryRepositoryProjectsCurrentStateAndHidesIncompleteHosts(t *testing.T) {
	database := openTestDatabase(t, filepath.Join(t.TempDir(), "atalaya.db"))
	defer database.Close()
	ctx := context.Background()
	insertHost := func(id, name string, completed any) {
		t.Helper()
		_, err := database.sql.ExecContext(ctx, `INSERT INTO hosts(id,display_name,source,agent_url,agent_credential,administrative_state,observed_state,interval_seconds,timeout_seconds,last_attempt_at,last_success_received_at,freshness_deadline_at,consecutive_failures,stale,archived_at,bootstrap_completed_at,created_at,updated_at,collection_generation,version) VALUES(?,?,'enrolled',?,x'01','active','degraded',60,10,'2026-07-17T12:00:00Z','2026-07-17T12:00:00Z','2026-07-17T12:01:20Z',0,0,NULL,?,'2026-07-17T11:00:00Z','2026-07-17T12:00:00Z',1,7)`, id, name, "https://"+id+":9474", completed)
		if err != nil {
			t.Fatal(err)
		}
	}
	insertHost("host-1", "Oracle", "2026-07-17T11:00:00Z")
	insertHost("incomplete", "Incomplete", nil)
	_, err := database.sql.ExecContext(ctx, `INSERT INTO host_snapshots(host_id,received_at,observed_at,hostname,boot_id,uptime_seconds,cpu_used_percent,memory_used_bytes,memory_total_bytes,tailscale_state,tailscale_hostname,agent_version,protocol_version) VALUES('host-1','2026-07-17T12:00:00Z','2026-07-17T11:50:00Z','oracle','boot',123,12.5,1024,4096,'connected','oracle.tailnet.ts.net','dev',1)`)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO host_snapshot_warnings(host_id,code) VALUES('host-1','clock_skew'),('host-1','boot_id_unavailable')`,
		`INSERT INTO host_snapshot_addresses(host_id,address) VALUES('host-1','100.64.0.1')`,
		`INSERT INTO filesystems(id,host_id,identity_kind,identity_key,fs_uuid,mount_root,mount_point,major_minor,fs_type,device,total_bytes,used_bytes,available_bytes,discovered_at,selected,present,last_observed_at,version) VALUES('fs-1','host-1','uuid',x'0175756964006100620063006400','abcd','/','/','8:1','ext4','/dev/sda1',4096,3072,1024,'2026-07-17T12:00:00Z',1,1,'2026-07-17T12:00:00Z',2)`,
	} {
		if _, err := database.sql.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}

	hosts, err := database.ListHosts(ctx, 50, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 1 || hosts[0].ID != "host-1" || hosts[0].Tailscale == nil || len(hosts[0].Warnings) != 2 || hosts[0].MostConstrainedFilesystem == nil || hosts[0].MostConstrainedFilesystem.UsedPercent != 75 {
		t.Fatalf("hosts = %+v", hosts)
	}
	detail, err := database.GetHost(ctx, "host-1")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Snapshot == nil || len(detail.Snapshot.CollectionWarnings) != 1 || detail.Snapshot.CollectionWarnings[0] != "boot_id_unavailable" || detail.AgentURL != "https://host-1:9474" {
		t.Fatalf("detail = %+v", detail)
	}
	filesystems, err := database.ListFilesystems(ctx, "host-1", 50, nil)
	if err != nil || len(filesystems) != 1 || filesystems[0].UsedBytes == nil || *filesystems[0].UsedBytes != 3072 {
		t.Fatalf("filesystems=%+v err=%v", filesystems, err)
	}
	if _, err := database.GetHost(ctx, "incomplete"); !errors.Is(err, hostquery.ErrNotFound) {
		t.Fatalf("incomplete err=%v", err)
	}
	if _, err := database.ListFilesystems(ctx, "incomplete", 50, nil); !errors.Is(err, hostquery.ErrNotFound) {
		t.Fatalf("incomplete filesystems err=%v", err)
	}
}

func TestGetHostNeverMixesVersionAndWarningsAcrossConcurrentPulls(t *testing.T) {
	database := openTestDatabase(t, filepath.Join(t.TempDir(), "atalaya.db"))
	database.sql.SetMaxOpenConns(4)
	defer database.Close()
	ctx := context.Background()
	_, err := database.sql.ExecContext(ctx, `INSERT INTO hosts(id,display_name,source,agent_url,agent_credential,administrative_state,observed_state,interval_seconds,timeout_seconds,consecutive_failures,stale,bootstrap_completed_at,created_at,updated_at,collection_generation,version) VALUES('host-1','Oracle','enrolled','https://host-1',x'01','active','healthy',60,10,0,0,'2026-07-17T00:00:00Z','2026-07-17T00:00:00Z','2026-07-17T00:00:00Z',1,1)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.sql.ExecContext(ctx, `INSERT INTO host_snapshots(host_id,received_at,observed_at,hostname,tailscale_state,agent_version,protocol_version) VALUES('host-1','2026-07-17T00:00:00Z','2026-07-17T00:00:00Z','oracle','connected','dev',1); INSERT INTO host_snapshot_warnings(host_id,code) VALUES('host-1','state_1')`)
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	errorsSeen := make(chan error, 2)
	wait.Add(2)
	go func() {
		defer wait.Done()
		for iteration := 0; iteration < 60; iteration++ {
			state := iteration%2 + 1
			tx, err := database.sql.BeginTx(ctx, nil)
			if err == nil {
				_, err = tx.ExecContext(ctx, `UPDATE hosts SET version=? WHERE id='host-1'`, state)
			}
			if err == nil {
				_, err = tx.ExecContext(ctx, `DELETE FROM host_snapshot_warnings WHERE host_id='host-1'`)
			}
			if err == nil {
				_, err = tx.ExecContext(ctx, `INSERT INTO host_snapshot_warnings(host_id,code) VALUES('host-1',?)`, "state_"+string(rune('0'+state)))
			}
			if err == nil {
				err = tx.Commit()
			} else if tx != nil {
				_ = tx.Rollback()
			}
			if err != nil {
				errorsSeen <- err
				return
			}
		}
	}()
	go func() {
		defer wait.Done()
		for iteration := 0; iteration < 60; iteration++ {
			detail, err := database.GetHost(ctx, "host-1")
			if err != nil {
				errorsSeen <- err
				return
			}
			if len(detail.Warnings) != 1 || detail.Warnings[0] != "state_"+string(rune('0'+detail.Version)) {
				errorsSeen <- errors.New("mixed host representation")
				return
			}
		}
	}()
	wait.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		t.Fatal(err)
	}
}

func TestHostQueryRepositoryUsesDocumentedKeysets(t *testing.T) {
	database := openTestDatabase(t, filepath.Join(t.TempDir(), "atalaya.db"))
	defer database.Close()
	ctx := context.Background()
	for _, value := range [][2]string{{"2", "alpha"}, {"1", "Alpha"}, {"3", "Beta"}} {
		_, err := database.sql.ExecContext(ctx, `INSERT INTO hosts(id,display_name,source,agent_url,agent_credential,administrative_state,observed_state,interval_seconds,timeout_seconds,consecutive_failures,stale,bootstrap_completed_at,created_at,updated_at,collection_generation,version) VALUES(?,?,'enrolled',?,x'01','pending','unknown',60,10,0,0,'2026-07-17T00:00:00Z','2026-07-17T00:00:00Z','2026-07-17T00:00:00Z',1,1)`, value[0], value[1], "https://"+value[0])
		if err != nil {
			t.Fatal(err)
		}
	}
	items, err := database.ListHosts(ctx, 10, &hostquery.HostCursor{Name: "alpha", ID: "1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ID != "2" || items[1].ID != "3" {
		t.Fatalf("items=%+v", items)
	}
}
