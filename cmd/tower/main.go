// Command tower is the Tower control-plane server.
package main

import (
	"context"
	"log"
	"net/http"

	"github.com/qwwqq1000-arch/tower/internal/api"
	"github.com/qwwqq1000-arch/tower/internal/config"
	"github.com/qwwqq1000-arch/tower/internal/crypto"
	"github.com/qwwqq1000-arch/tower/internal/db"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if _, err := crypto.NewCipher(cfg.MasterKeyB64); err != nil {
		log.Fatalf("crypto: %v", err)
	}
	ctx := context.Background()
	if err := db.Migrate(ctx, cfg.DatabaseURL); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	log.Printf("tower listening on %s", cfg.HTTPAddr)
	if err := http.ListenAndServe(cfg.HTTPAddr, api.NewRouter(pool, cfg.SessionSecret)); err != nil {
		log.Fatal(err)
	}
}
