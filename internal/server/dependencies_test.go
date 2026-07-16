package server_test

import (
	"database/sql"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
	"golang.org/x/crypto/argon2"
	_ "modernc.org/sqlite"
)

func TestFoundationDependenciesWorkOnTheSupportedPlatform(t *testing.T) {
	t.Run("SQLite", func(t *testing.T) {
		database, err := sql.Open("sqlite", ":memory:")
		if err != nil {
			t.Fatalf("sql.Open() error = %v", err)
		}
		defer database.Close()

		if _, err := database.Exec(`CREATE TABLE bootstrap (id INTEGER PRIMARY KEY, status TEXT NOT NULL)`); err != nil {
			t.Fatalf("CREATE TABLE error = %v", err)
		}
		if _, err := database.Exec(`INSERT INTO bootstrap (status) VALUES (?)`, "ready"); err != nil {
			t.Fatalf("INSERT error = %v", err)
		}

		var status string
		if err := database.QueryRow(`SELECT status FROM bootstrap WHERE id = 1`).Scan(&status); err != nil {
			t.Fatalf("SELECT error = %v", err)
		}
		if status != "ready" {
			t.Fatalf("status = %q, want ready", status)
		}
	})

	t.Run("Argon2id", func(t *testing.T) {
		derived := argon2.IDKey([]byte("phase-zero-password"), []byte("deterministic-salt"), 1, 8*1024, 1, 32)
		if len(derived) != 32 {
			t.Fatalf("derived key length = %d, want 32", len(derived))
		}
	})

	t.Run("strict YAML", func(t *testing.T) {
		type bootstrapConfig struct {
			Listen string `yaml:"listen"`
		}

		decoder := yaml.NewDecoder(strings.NewReader("listen: 127.0.0.1:8080\n"))
		decoder.KnownFields(true)
		var valid bootstrapConfig
		if err := decoder.Decode(&valid); err != nil {
			t.Fatalf("Decode(valid) error = %v", err)
		}

		decoder = yaml.NewDecoder(strings.NewReader("listen: 127.0.0.1:8080\nunknown: true\n"))
		decoder.KnownFields(true)
		var invalid bootstrapConfig
		if err := decoder.Decode(&invalid); err == nil {
			t.Fatal("Decode(unknown field) error = nil, want strict rejection")
		}
	})
}
