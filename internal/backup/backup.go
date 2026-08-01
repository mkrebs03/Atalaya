// Package backup creates and verifies the offline data artifacts used by the
// installer and upgrade rollback flow.
package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	startupconfig "github.com/mkrebs03/atalaya/internal/config"
	atalayacrypto "github.com/mkrebs03/atalaya/internal/crypto"
	"github.com/mkrebs03/atalaya/internal/database"
	"go.yaml.in/yaml/v3"
)

// Manifest is stored inside every data backup. It explicitly records that the
// master key is not part of the archive.
type Manifest struct {
	Format             string `json:"format"`
	ApplicationVersion string `json:"application_version"`
	SchemaVersion      int    `json:"schema_version"`
	CreatedAt          string `json:"created_at"`
	DatabaseName       string `json:"database"`
	ConfigName         string `json:"config"`
	DatabaseSHA256     string `json:"database_sha256"`
	ContainsMasterKey  bool   `json:"contains_master_key"`
}

// Options controls creation of a data-only backup.
type Options struct {
	ConfigPath string
	Config     startupconfig.ServerConfig
	Key        []byte
	Output     string
	Version    string
}

// Create validates the live instance and writes an exclusive 0600 tar.gz
// containing only the consistent database snapshot, non-secret config, and
// manifest. The master key is never added.
func Create(ctx context.Context, options Options) (Manifest, error) {
	if options.ConfigPath == "" || options.Config.DatabasePath == "" || options.Output == "" {
		return Manifest{}, fmt.Errorf("backup configuration and output are required")
	}
	if len(options.Key) != 32 {
		return Manifest{}, fmt.Errorf("master key must contain exactly 32 bytes")
	}
	output, err := filepath.Abs(options.Output)
	if err != nil {
		return Manifest{}, fmt.Errorf("resolve backup output: %w", err)
	}
	if _, err := os.Lstat(output); err == nil {
		return Manifest{}, fmt.Errorf("backup output already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return Manifest{}, fmt.Errorf("inspect backup output: %w", err)
	}
	if samePath(output, options.Config.MasterKeyPath) || samePath(output, options.Config.DatabasePath) || samePath(output, options.ConfigPath) {
		return Manifest{}, fmt.Errorf("backup output must be separate from configuration, database, and master key")
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o700); err != nil {
		return Manifest{}, fmt.Errorf("prepare backup directory: %w", err)
	}

	verification, err := database.Verify(ctx, options.Config.DatabasePath, options.Key)
	if err != nil {
		return Manifest{}, fmt.Errorf("verify database before backup: %w", err)
	}
	snapshotPath := filepath.Join(filepath.Dir(output), ".atalaya-backup-snapshot-"+filepath.Base(output))
	if _, err := os.Lstat(snapshotPath); err == nil {
		return Manifest{}, fmt.Errorf("temporary backup snapshot already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return Manifest{}, fmt.Errorf("inspect temporary backup snapshot: %w", err)
	}
	defer os.Remove(snapshotPath)
	if err := database.Snapshot(ctx, options.Config.DatabasePath, snapshotPath); err != nil {
		return Manifest{}, err
	}
	digest, err := sha256File(snapshotPath)
	if err != nil {
		return Manifest{}, err
	}
	config, err := nonSecretConfig(options.Config)
	if err != nil {
		return Manifest{}, err
	}
	manifest := Manifest{
		Format:             "atalaya-backup-v1",
		ApplicationVersion: options.Version,
		SchemaVersion:      verification.SchemaVersion,
		CreatedAt:          time.Now().UTC().Format(time.RFC3339Nano),
		DatabaseName:       "atalaya.db",
		ConfigName:         "server.yaml",
		DatabaseSHA256:     digest,
		ContainsMasterKey:  false,
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return Manifest{}, fmt.Errorf("encode backup manifest: %w", err)
	}
	manifestBytes = append(manifestBytes, '\n')
	if err := writeArchive(output, manifestBytes, config, snapshotPath); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func nonSecretConfig(config startupconfig.ServerConfig) ([]byte, error) {
	// ServerConfig contains only bootstrap paths and transport settings. The
	// key's bytes are never represented here.
	data, err := yamlMarshal(config)
	if err != nil {
		return nil, fmt.Errorf("encode backup configuration: %w", err)
	}
	return data, nil
}

func writeArchive(output string, manifest, config []byte, snapshot string) error {
	temporary, err := os.OpenFile(filepath.Join(filepath.Dir(output), ".atalaya-backup-"+filepath.Base(output)), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create backup temporary: %w", err)
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		_ = temporary.Close()
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	gzipWriter := gzip.NewWriter(temporary)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := addBytes(tarWriter, "manifest.json", manifest); err != nil {
		return err
	}
	if err := addBytes(tarWriter, "server.yaml", config); err != nil {
		return err
	}
	file, err := os.Open(snapshot)
	if err != nil {
		return fmt.Errorf("open SQLite snapshot for archive: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return fmt.Errorf("inspect SQLite snapshot for archive: %w", err)
	}
	header := &tar.Header{Name: "atalaya.db", Mode: 0o600, Size: info.Size(), ModTime: time.Unix(0, 0).UTC()}
	if err := tarWriter.WriteHeader(header); err != nil {
		file.Close()
		return fmt.Errorf("write SQLite archive header: %w", err)
	}
	if _, err := io.Copy(tarWriter, file); err != nil {
		file.Close()
		return fmt.Errorf("write SQLite archive: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close SQLite snapshot: %w", err)
	}
	if err := tarWriter.Close(); err != nil {
		return fmt.Errorf("close backup tar: %w", err)
	}
	if err := gzipWriter.Close(); err != nil {
		return fmt.Errorf("close backup gzip: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync backup: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close backup: %w", err)
	}
	if err := os.Link(temporaryPath, output); err != nil {
		return fmt.Errorf("install backup without overwrite: %w", err)
	}
	removeTemporary = false
	return nil
}

func addBytes(writer *tar.Writer, name string, data []byte) error {
	if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(data)), ModTime: time.Unix(0, 0).UTC()}); err != nil {
		return fmt.Errorf("write %s archive header: %w", name, err)
	}
	if _, err := writer.Write(data); err != nil {
		return fmt.Errorf("write %s archive: %w", name, err)
	}
	return nil
}

func sha256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open snapshot for checksum: %w", err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("hash snapshot: %w", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func samePath(left, right string) bool {
	if right == "" {
		return false
	}
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	return leftErr == nil && rightErr == nil && filepath.Clean(leftAbs) == filepath.Clean(rightAbs)
}

// ExportKey copies only the validated raw master key to a new 0600 file.
func ExportKey(keyPath, output string) error {
	if keyPath == "" || output == "" {
		return fmt.Errorf("master key and output paths are required")
	}
	if samePath(keyPath, output) {
		return fmt.Errorf("master key output must differ from source")
	}
	if _, err := os.Lstat(output); err == nil {
		return fmt.Errorf("master key output already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect master key output: %w", err)
	}
	key, err := loadMasterKey(keyPath)
	if err != nil {
		return err
	}
	defer clear(key)
	return writeExclusive(output, key, 0o600)
}

var yamlMarshal = func(value startupconfig.ServerConfig) ([]byte, error) {
	return yaml.Marshal(value)
}

var loadMasterKey = atalayacrypto.LoadMasterKey

func writeExclusive(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("prepare key output directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".atalaya-key-*")
	if err != nil {
		return fmt.Errorf("create key output: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Link(temporaryPath, path); err != nil {
		return fmt.Errorf("install key output without overwrite: %w", err)
	}
	return nil
}

func clear(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
