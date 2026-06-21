// Command tower is the Tower control-plane server.
package main

import (
	"context"
	"log"
	"math/rand"
	"net/http"
	"time"

	"github.com/qwwqq1000-arch/tower/internal/api"
	"github.com/qwwqq1000-arch/tower/internal/bootstrap"
	"github.com/qwwqq1000-arch/tower/internal/config"
	"github.com/qwwqq1000-arch/tower/internal/crypto"
	"github.com/qwwqq1000-arch/tower/internal/db"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
	"github.com/qwwqq1000-arch/tower/internal/dispatch"
	"github.com/qwwqq1000-arch/tower/internal/policy"
	"github.com/qwwqq1000-arch/tower/internal/state"
	"github.com/qwwqq1000-arch/tower/internal/telemetry"
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

	q := sqlc.New(pool)
	if created, err := bootstrap.EnsureAdmin(ctx, q, cfg.AdminUser, cfg.AdminPassword); err != nil {
		log.Printf("bootstrap admin: %v", err)
	} else if created {
		log.Printf("bootstrap: created initial admin %q", cfg.AdminUser)
	}
	nowMs := func() int64 { return time.Now().UnixMilli() }
	rng := func(min, max int64) int64 {
		if max <= min {
			return min
		}
		return min + rand.Int63n(max-min+1)
	}
	store := state.NewStore(nowMs, rng)
	base := policy.Defaults()
	if err := state.LoadStates(ctx, q, store, base.MaxConcurrent); err != nil {
		log.Printf("warm-start: %v", err)
	}
	svc := dispatch.NewService(q, store, base, nowMs)

	poller := &telemetry.Poller{Q: q, Store: store, Threshold: 0.95, DefaultTTLMs: 3600000, Capacity: base.MaxConcurrent, Now: nowMs}
	go poller.Run(context.Background(), 60*time.Second)

	log.Printf("tower listening on %s", cfg.HTTPAddr)
	if err := http.ListenAndServe(cfg.HTTPAddr, api.NewRouter(pool, cfg.SessionSecret, svc, q)); err != nil {
		log.Fatal(err)
	}
}
