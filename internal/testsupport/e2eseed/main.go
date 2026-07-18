// Command e2eseed initializes an isolated Phase 1 browser-test database.
// It is intentionally outside cmd/ and is never linked into production binaries.
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/mkrebs03/atalaya/internal/agent"
	"github.com/mkrebs03/atalaya/internal/auth"
	"github.com/mkrebs03/atalaya/internal/collection"
	startupconfig "github.com/mkrebs03/atalaya/internal/config"
	atalayacrypto "github.com/mkrebs03/atalaya/internal/crypto"
	"github.com/mkrebs03/atalaya/internal/database"
	"github.com/mkrebs03/atalaya/internal/hosts"
)

type seedCollectionRandom struct{ next int }

func (*seedCollectionRandom) Intn(int) int { return 0 }
func (random *seedCollectionRandom) ID() (string, error) {
	random.next++
	return fmt.Sprintf("00000000-0000-4000-8000-%012d", random.next), nil
}

func stableSeedTime(now time.Time) time.Time {
	return now.UTC().Truncate(time.Hour).Add(24 * time.Hour)
}

func main() {
	configPath := flag.String("config", "", "isolated E2E server config")
	flag.Parse()
	password := os.Getenv("ATALAYA_E2E_PASSWORD")
	if *configPath == "" || password == "" {
		fmt.Fprintln(os.Stderr, "e2eseed: config and ATALAYA_E2E_PASSWORD are required")
		os.Exit(2)
	}
	config, err := startupconfig.LoadServer(*configPath)
	if err != nil {
		fail(err)
	}
	key, err := atalayacrypto.LoadMasterKey(config.MasterKeyPath)
	if err != nil {
		fail(err)
	}
	defer clear(key)
	db, err := database.Open(context.Background(), config.DatabasePath, key)
	if err != nil {
		fail(err)
	}
	defer db.Close()
	hash, err := auth.HashPassword([]byte(password))
	if err != nil {
		fail(err)
	}
	_, err = auth.NewAdministratorCreator(db).Create(context.Background(), "admin", hash)
	if err != nil {
		fail(err)
	}
	random := bytes.NewReader(bytes.Repeat([]byte{0x42}, 4096))
	// Keep the first collection slot beyond the browser run so screenshots cannot
	// race scheduler failures against the intentionally offline fake agent.
	stableAt := stableSeedTime(time.Now())
	host, err := db.PrepareLocalHost(context.Background(), hosts.DefaultDisplayName, "https://oracle.tailnet.ts.net:9474", stableAt, random)
	if err != nil {
		fail(err)
	}
	clear(host.Credential)
	if err := db.CompleteLocalHost(context.Background(), host.ID, stableAt, random); err != nil {
		fail(err)
	}
	claim, err := db.ClaimDue(context.Background(), stableAt.Add(10*time.Second))
	if err != nil {
		fail(err)
	}
	defer claim.Clear()
	bootID := "11111111-2222-3333-4444-555555555555"
	uptime := uint64(172800)
	cpu := 12.5
	memoryUsed, memoryTotal := uint64(8<<30), uint64(16<<30)
	tailscaleHostname := "oracle.tailnet.ts.net"
	filesystemUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	snapshot := agent.Snapshot{
		ProtocolVersion:    agent.ProtocolVersion,
		AgentVersion:       "dev",
		Host:               agent.Host{Hostname: "oracle", ObservedAt: stableAt.Add(10 * time.Second).Format(time.RFC3339), BootID: &bootID, UptimeSeconds: &uptime, CPUUsedPercent: &cpu, MemoryUsedBytes: &memoryUsed, MemoryTotalBytes: &memoryTotal},
		Tailscale:          agent.Tailscale{State: "connected", Hostname: &tailscaleHostname, Addresses: []string{"100.64.0.10"}},
		Filesystems:        []agent.Filesystem{{MountPoint: "/", Device: "/dev/sda1", MountRoot: "/", MajorMinor: "8:1", FSType: "ext4", FSUUID: &filesystemUUID, UsedBytes: 32 << 30, TotalBytes: 128 << 30, AvailableBytes: 96 << 30}},
		CollectionWarnings: []string{"filesystems_partial"},
	}
	if err := db.Finish(context.Background(), claim, collection.Result{Snapshot: &snapshot, ReceivedAt: stableAt.Add(10 * time.Second)}, &seedCollectionRandom{}); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "e2eseed: %v\n", err)
	os.Exit(1)
}
