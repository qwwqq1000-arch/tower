package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/db"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

// TestProvisionOwnerScope verifies that startProvisionHandler forces the
// provision job's owner_id to the caller's own sub for a non-superadmin,
// ignoring any ownerId supplied in the request body.
// A superadmin may supply an explicit ownerId (or leave it empty for
// unscoped / own provisioning).
func TestProvisionOwnerScope(t *testing.T) {
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
	t.Cleanup(pool.Close)
	q := sqlc.New(pool)
	const secret = "test-secret-padding-to-32-chars!"
	router := NewRouter(pool, secret, nil, q, false)

	// Seed a non-superadmin caller and a different target owner.
	caller := randHex("prov_caller_")
	otherOwner := randHex("prov_other_")
	for _, s := range []struct{ id, role string }{
		{caller, "admin"},
		{otherOwner, "admin"},
	} {
		if _, err := q.CreateTenant(ctx, sqlc.CreateTenantParams{
			ID: s.id, Username: s.id, PwHash: "h", Salt: "s",
			Role: s.role, IngestKey: randHex("ik_"),
		}); err != nil {
			_ = q.SetTenantRole(ctx, sqlc.SetTenantRoleParams{ID: s.id, Role: s.role})
		}
	}
	t.Cleanup(func() {
		cctx := context.Background()
		_ = q.DeleteTenant(cctx, caller)
		_ = q.DeleteTenant(cctx, otherOwner)
	})

	callerCookie := seedSessionCookie(t, ctx, q, secret, caller, "admin")

	// Non-superadmin sends a body with ownerId=otherOwner; the handler must
	// override it with the caller's own sub.
	body := `{"host":"192.0.2.1","password":"pw","name":"testnode","ownerId":"` + otherOwner + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/provision", strings.NewReader(body))
	req.Header.Set("X-Requested-With", "tower")
	req.AddCookie(callerCookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("startProvision: code=%d body=%s", rec.Code, rec.Body)
	}
	var resp map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	jobID := resp["jobId"]
	if jobID == "" {
		t.Fatalf("no jobId in response: %s", rec.Body)
	}

	// The provision_jobs row must carry the caller's id as owner, not otherOwner.
	job, err := q.GetProvisionJob(ctx, jobID)
	if err != nil {
		t.Fatalf("get provision job: %v", err)
	}
	if job.OwnerID != caller {
		t.Fatalf("provision job owner_id=%q, want caller %q (attacker-supplied ownerId must be rejected)", job.OwnerID, caller)
	}

	// Cleanup the inserted job row.
	t.Cleanup(func() {
		cctx := context.Background()
		_, _ = pool.Exec(cctx, "DELETE FROM provision_jobs WHERE id = $1", jobID)
	})
}

// TestProvisionSuperadminCanSetOwner verifies that a superadmin may specify an
// explicit ownerId in the body and it is honoured.
func TestProvisionSuperadminCanSetOwner(t *testing.T) {
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
	t.Cleanup(pool.Close)
	q := sqlc.New(pool)
	const secret = "test-secret-padding-to-32-chars!"
	router := NewRouter(pool, secret, nil, q, false)

	targetOwner := randHex("prov_tgt_")
	if _, err := q.CreateTenant(ctx, sqlc.CreateTenantParams{
		ID: targetOwner, Username: targetOwner, PwHash: "h", Salt: "s",
		Role: "admin", IngestKey: randHex("ik_"),
	}); err != nil {
		_ = q.SetTenantRole(ctx, sqlc.SetTenantRoleParams{ID: targetOwner, Role: "admin"})
	}
	t.Cleanup(func() {
		cctx := context.Background()
		_ = q.DeleteTenant(cctx, targetOwner)
	})

	sadminCookie := adminCookie(t, ctx, q, secret)

	body := `{"host":"192.0.2.2","password":"pw","name":"testnode2","ownerId":"` + targetOwner + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/provision", strings.NewReader(body))
	req.Header.Set("X-Requested-With", "tower")
	req.AddCookie(sadminCookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("startProvision superadmin: code=%d body=%s", rec.Code, rec.Body)
	}
	var resp map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	jobID := resp["jobId"]
	if jobID == "" {
		t.Fatalf("no jobId in response: %s", rec.Body)
	}

	job, err := q.GetProvisionJob(ctx, jobID)
	if err != nil {
		t.Fatalf("get provision job: %v", err)
	}
	if job.OwnerID != targetOwner {
		t.Fatalf("provision job owner_id=%q, want targetOwner %q (superadmin should be able to set owner)", job.OwnerID, targetOwner)
	}

	t.Cleanup(func() {
		cctx := context.Background()
		_, _ = pool.Exec(cctx, "DELETE FROM provision_jobs WHERE id = $1", jobID)
	})
}
