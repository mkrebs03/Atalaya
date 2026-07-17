// Command e2eseed initializes an isolated Phase 1 browser-test database.
// It is intentionally outside cmd/ and is never linked into production binaries.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/mkrebs03/atalaya/internal/auth"
	startupconfig "github.com/mkrebs03/atalaya/internal/config"
	atalayacrypto "github.com/mkrebs03/atalaya/internal/crypto"
	"github.com/mkrebs03/atalaya/internal/database"
)

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
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "e2eseed: %v\n", err)
	os.Exit(1)
}
