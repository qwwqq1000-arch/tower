package bootstrap

import (
	"context"
	"os"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/auth"
	"github.com/qwwqq1000-arch/tower/internal/db"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

func TestEnsureAdmin(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	if err := db.Migrate(ctx, url); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := db.Connect(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()
	// clean slate
	_, _ = pool.Exec(ctx, "DELETE FROM tenants")
	q := sqlc.New(pool)

	created, err := EnsureAdmin(ctx, q, "root", "pw12345678")
	if err != nil || !created {
		t.Fatalf("first EnsureAdmin: created=%v err=%v", created, err)
	}
	// login works → password stored correctly
	u, err := q.GetTenantByUsername(ctx, "root")
	if err != nil || u.Role != "superadmin" || !auth.VerifyPassword("pw12345678", u.PwHash, u.Salt) {
		t.Fatalf("admin row wrong: %+v err=%v", u, err)
	}
	// idempotent: second call does nothing (table non-empty)
	created2, err := EnsureAdmin(ctx, q, "root2", "pw87654321")
	if err != nil || created2 {
		t.Fatalf("second EnsureAdmin should be no-op: created=%v err=%v", created2, err)
	}
	// empty creds → no-op
	_, _ = pool.Exec(ctx, "DELETE FROM tenants")
	created3, _ := EnsureAdmin(ctx, q, "", "")
	if created3 {
		t.Fatal("empty creds should not create admin")
	}
}
