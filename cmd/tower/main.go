// Command tower is the Tower control-plane server.
package main

import (
	"context"
	"log"
	"math/rand"
	"net/http"
	"time"
	_ "time/tzdata" // embed the timezone DB (alpine runtime has no tzdata)

	"github.com/qwwqq1000-arch/tower/internal/api"
	"github.com/qwwqq1000-arch/tower/internal/bootstrap"
	"github.com/qwwqq1000-arch/tower/internal/config"
	"github.com/qwwqq1000-arch/tower/internal/cpaclient"
	"github.com/qwwqq1000-arch/tower/internal/crypto"
	"github.com/qwwqq1000-arch/tower/internal/db"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
	"github.com/qwwqq1000-arch/tower/internal/dispatch"
	"github.com/qwwqq1000-arch/tower/internal/events"
	"github.com/qwwqq1000-arch/tower/internal/policy"
	"github.com/qwwqq1000-arch/tower/internal/reconcile"
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

	// Periodically persist account_state so warm-start after restart has data.
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if err := store.PersistAll(context.Background(), q, nowMs()); err != nil {
				log.Printf("persist account_state: %v", err)
			}
		}
	}()

	poller := &telemetry.Poller{Q: q, Store: store, Threshold: 0.95, DefaultTTLMs: 3600000, Capacity: base.MaxConcurrent, Now: nowMs}
	go poller.Run(context.Background(), 60*time.Second)

	go (&reconcile.Reconciler{Q: q}).Run(context.Background(), 120*time.Second)

	// Every 60s: discover accounts on CPA (CLIProxyAPI) nodes into the pool and
	// project their quota utilization into the live store so saturated CPA
	// accounts rotate out of dispatch (same threshold as the meridian poller).
	cpaRot := &cpaclient.RotateConfig{Store: store, Threshold: 0.95, Capacity: base.MaxConcurrent, DefaultTTLMs: 3600000}
	go func() {
		// Run once shortly after startup, then on a ticker.
		if err := cpaclient.SyncAll(context.Background(), q, cpaRot); err != nil {
			log.Printf("cpa discovery: %v", err)
		}
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if err := cpaclient.SyncAll(context.Background(), q, cpaRot); err != nil {
				log.Printf("cpa discovery: %v", err)
			}
		}
	}()

	// Every 60s: poll balance for all channels that have balance credentials.
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			channels, err := q.ListAllFallbackChannels(context.Background())
			if err != nil {
				log.Printf("balance poll: list channels: %v", err)
				continue
			}
			for _, ch := range channels {
				if ch.BalanceToken == "" || ch.BalanceUserID == "" {
					continue
				}
				usd, fetchErr := dispatch.FetchChannelBalance(context.Background(), ch.BaseUrl, ch.BalanceToken, ch.BalanceUserID)
				errStr := ""
				if fetchErr != nil {
					errStr = fetchErr.Error()
					log.Printf("balance poll: channel %s: %v", ch.ID, fetchErr)
				}
				if err := q.SetFallbackBalance(context.Background(), sqlc.SetFallbackBalanceParams{
					ID:               ch.ID,
					BalanceUsd:       usd,
					BalanceCheckedAt: time.Now().UnixMilli(),
					BalanceError:     errStr,
				}); err != nil {
					log.Printf("balance poll: set balance channel %s: %v", ch.ID, err)
				}
			}
		}
	}()

	// Every 10s: fire balance_low events for channels below their alert threshold.
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			channels, err := q.ListAllFallbackChannels(context.Background())
			if err != nil {
				log.Printf("balance alert: list channels: %v", err)
				continue
			}
			for _, ch := range channels {
				if ch.BalanceAlertUsd <= 0 || ch.BalanceCheckedAt == 0 {
					continue
				}
				if ch.BalanceUsd < ch.BalanceAlertUsd {
					_ = events.Record(context.Background(), q, time.Now().UnixMilli(), events.Event{
						Type:   "balance_low",
						Target: ch.ID,
						Detail: map[string]any{
							"balance": ch.BalanceUsd,
							"alert":   ch.BalanceAlertUsd,
						},
					})
				}
			}
		}
	}()

	log.Printf("tower listening on %s", cfg.HTTPAddr)
	if err := http.ListenAndServe(cfg.HTTPAddr, api.NewRouter(pool, cfg.SessionSecret, svc, q, cfg.SecureCookies)); err != nil {
		log.Fatal(err)
	}
}
