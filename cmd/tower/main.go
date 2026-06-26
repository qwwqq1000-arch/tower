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
	cipher, err := crypto.NewCipher(cfg.MasterKeyB64)
	if err != nil {
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
	// Warm-start the per-account slot capacity from the LIVE global MaxConcurrent
	// override (not the compiled default), so after a restart idle accounts show the
	// configured concurrency (e.g. 5) instead of reverting to the default 3 until a
	// dispatch re-caps them. Mirrors telemetry.Poller.maxConcurrent / discovery.Refresh.
	warmCap := base.MaxConcurrent
	if rows, perr := q.ListPolicies(ctx); perr == nil {
		for _, row := range rows {
			if row.ScopeType == "global" {
				warmCap = policy.PickMaxConcurrent(row.Params, base.MaxConcurrent)
				break
			}
		}
	}
	if err := state.LoadStates(ctx, q, store, warmCap); err != nil {
		log.Printf("warm-start: %v", err)
	}
	svc := dispatch.NewService(q, store, base, nowMs, cipher)

	// Restore persisted quota-limit state so limits survive restart.
	if rows, err := q.ListActiveAccountLimitState(ctx, nowMs()); err != nil {
		log.Printf("restore limit state: %v", err)
	} else {
		for _, r := range rows {
			// warmCap (live global MaxConcurrent), not the compiled default, so a
			// restored-limited account's slot capacity matches the configured value.
			store.SetLimitedReason(r.Key, warmCap, r.LimitedUntil, r.LimitReason)
		}
		if len(rows) > 0 {
			log.Printf("restored %d active limit(s) from DB", len(rows))
		}
	}

	// Restore persisted spend thresholds so the raising-threshold bar survives restart.
	// The threshold is restored so the account doesn't restart from T₀ mid-cycle (would
	// re-fire too early if the bar was raised). On day change the threshold is re-anchored
	// automatically in recordSpend.
	svc.RestoreSpendThresholds(ctx)

	// Restore each account's cumulative spend for TODAY so the daily spend cap counts the
	// full day across restarts. Previously todaySpend reset to 0 on every restart, which
	// re-granted each account a full cap window — on a frequent-deploy day the daily cap
	// never actually bounded daily spend (an account at $360 with a $130 cap kept getting
	// fresh $130 windows). Seed from dispatch_logs (billed local-node rows since the start
	// of today UTC); later AddSpend calls accumulate on top of the restored total.
	{
		dayStart := (nowMs() / 86_400_000) * 86_400_000
		if rows, err := pool.Query(ctx, `SELECT target, COALESCE(SUM(cost_usd),0)::float8 FROM dispatch_logs WHERE ts >= $1 AND target LIKE 'n\_%' GROUP BY target`, dayStart); err != nil {
			log.Printf("restore today spend: %v", err)
		} else {
			n := 0
			for rows.Next() {
				var key string
				var total float64
				if scanErr := rows.Scan(&key, &total); scanErr != nil {
					continue
				}
				if total > 0 {
					store.SeedSpendToday(key, total, nowMs())
					n++
				}
			}
			rows.Close()
			if n > 0 {
				log.Printf("restored today spend for %d account(s)", n)
			}
		}
	}

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

	poller := &telemetry.Poller{Q: q, Store: store, DefaultTTLMs: 3600000, Capacity: base.MaxConcurrent, Now: nowMs, Cipher: cipher}
	go poller.Run(context.Background(), 60*time.Second)

	go (&reconcile.Reconciler{Q: q, Cipher: cipher}).Run(context.Background(), 120*time.Second)

	// Every 5min: discover accounts on CPA (CLIProxyAPI) nodes into the pool.
	// SyncAll resolves the effective MaxConcurrent from the live global policy each
	// cycle (mirroring the meridian poller's maxConcurrent), so the MaxConcurrent
	// override gates CPA and meridian accounts identically. BaseCapacity is the
	// fallback default when the policy omits an override. Discovery is pool/display
	// only: it no longer projects utilization into the limit store — rotation is
	// reactive (dispatch usage-limit responses) plus the 5h/7d spend caps.
	//
	// The interval is 5min, NOT 60s: each manual quota refresh calls the Anthropic
	// account-usage endpoint (CPA proxies it) once per account — polling it too often
	// piles extra requests onto an already-loaded subscription account, can itself be
	// rate-limited (429), and is needlessly conspicuous.
	cpaRot := &cpaclient.RotateConfig{
		Store:        store,
		BaseCapacity: base.MaxConcurrent,
		DefaultTTLMs: 3600000,
		Cipher:       cipher,
	}
	go func() {
		// Run once shortly after startup, then on a ticker.
		if err := cpaclient.SyncAll(context.Background(), q, cpaRot); err != nil {
			log.Printf("cpa discovery: %v", err)
		}
		ticker := time.NewTicker(5 * time.Minute)
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
				usd, fetchErr := dispatch.FetchChannelBalance(context.Background(), ch.BaseUrl, cipher.DecryptOrPlaintext(ch.BalanceToken), ch.BalanceUserID)
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

	// Every 30m: prune stored request detail (body+headers) older than 7d so the
	// log "view request" feature never bloats the database (logs-detail-1,
	// nexaxis-disk-wal-bloat). Log rows themselves are retained; only the heavy
	// per-request bodies expire.
	go func() {
		const retainMs = int64(7 * 24 * 60 * 60 * 1000)
		prune := func() {
			if err := q.DeleteDispatchLogDetailBefore(context.Background(), nowMs()-retainMs); err != nil {
				log.Printf("log-detail prune: %v", err)
			}
		}
		prune()
		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			prune()
		}
	}()

	log.Printf("tower listening on %s", cfg.HTTPAddr)
	// Explicit timeouts harden against slowloris / slow-body DoS (security-audit).
	// WriteTimeout is 0: dispatch proxies long upstream responses and streams SSE,
	// which a write deadline would sever mid-stream. ReadHeaderTimeout bounds the
	// header-read phase (the slowloris vector); ReadTimeout bounds the full request.
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           api.NewRouter(pool, cfg.SessionSecret, svc, q, cfg.SecureCookies, cipher, cfg.PushToken),
		ReadHeaderTimeout: 15 * time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      0,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
