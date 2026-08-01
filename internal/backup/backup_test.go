package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	startupconfig "github.com/mkrebs03/atalaya/internal/config"
	"github.com/mkrebs03/atalaya/internal/database"
)

func TestCreateUsesConsistentSnapshotAndExcludesMasterKey(t *testing.T) {
	directory := t.TempDir()
	key := bytes.Repeat([]byte{0x2a}, 32)
	databasePath := filepath.Join(directory, "atalaya.db")
	keyPath := filepath.Join(directory, "master.key")
	configPath := filepath.Join(directory, "server.yaml")
	output := filepath.Join(directory, "backup.tar.gz")
	if err := os.WriteFile(keyPath, key, 0o600); err != nil {
		t.Fatal(err)
	}
	config := startupconfig.ServerConfig{
		Listen: "127.0.0.1:8080", Mode: startupconfig.ModeDevelopment,
		PublicURL: "http://127.0.0.1:8080", DatabasePath: databasePath, MasterKeyPath: keyPath,
	}
	if err := os.WriteFile(configPath, []byte("listen: 127.0.0.1:8080\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	owner, err := database.Open(context.Background(), databasePath, key)
	if err != nil {
		t.Fatal(err)
	}
	// Keep a WAL-producing connection open while the backup is made.
	if _, err := owner.CountActiveAdministrators(context.Background()); err != nil {
		t.Fatal(err)
	}
	manifest, err := Create(context.Background(), Options{
		ConfigPath: configPath, Config: config, Key: key, Output: output, Version: "test",
	})
	if err != nil {
		owner.Close()
		t.Fatalf("Create() error = %v", err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if manifest.ContainsMasterKey || manifest.SchemaVersion != 8 {
		t.Fatalf("manifest = %+v", manifest)
	}
	info, err := os.Stat(output)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("backup mode = %o, want 600", info.Mode().Perm())
	}
	entries := readArchive(t, output)
	if _, ok := entries["master.key"]; ok {
		t.Fatal("backup contains master.key")
	}
	for _, name := range []string{"manifest.json", "server.yaml", "atalaya.db"} {
		if len(entries[name]) == 0 {
			t.Fatalf("archive entry %q is missing or empty", name)
		}
	}
	archiveHash := sha256.Sum256(entries["atalaya.db"])
	if got := hex.EncodeToString(archiveHash[:]); got != manifest.DatabaseSHA256 {
		t.Fatalf("database hash = %q, manifest = %q", got, manifest.DatabaseSHA256)
	}
	if _, err := os.Stat(keyPath); err != nil {
		t.Fatal(err)
	}
	if _, err := Create(context.Background(), Options{
		ConfigPath: configPath, Config: config, Key: key, Output: output, Version: "test",
	}); err == nil {
		t.Fatal("Create() overwrote an existing backup")
	}
}

func TestExportKeyIs0600AndNeverOverwrites(t *testing.T) {
	directory := t.TempDir()
	key := bytes.Repeat([]byte{0x5c}, 32)
	source := filepath.Join(directory, "master.key")
	output := filepath.Join(directory, "backup.key")
	if err := os.WriteFile(source, key, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ExportKey(source, output); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(output)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 600", info.Mode().Perm())
	}
	if got, err := os.ReadFile(output); err != nil || !bytes.Equal(got, key) {
		t.Fatalf("exported key = %x, err = %v", got, err)
	}
	if err := ExportKey(source, output); err == nil {
		t.Fatal("ExportKey() overwrote existing output")
	}
}

func TestVerifyRejectsWrongKeyAndCorruptSchema(t *testing.T) {
	directory := t.TempDir()
	key := bytes.Repeat([]byte{0x41}, 32)
	path := filepath.Join(directory, "atalaya.db")
	owner, err := database.Open(context.Background(), path, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Verify(context.Background(), path, bytes.Repeat([]byte{0x42}, 32)); !errors.Is(err, database.ErrWrongMasterKey) {
		t.Fatalf("wrong key error = %v", err)
	}
	raw, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.WriteAt([]byte("corrupt"), 0); err != nil {
		raw.Close()
		t.Fatal(err)
	}
	raw.Close()
	if _, err := database.Verify(context.Background(), path, key); err == nil {
		t.Fatal("Verify() accepted a corrupt SQLite file")
	}
}

func readArchive(t *testing.T, path string) map[string][]byte {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	result := make(map[string][]byte)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(tarReader)
		if err != nil {
			t.Fatal(err)
		}
		result[header.Name] = data
	}
	return result
}
