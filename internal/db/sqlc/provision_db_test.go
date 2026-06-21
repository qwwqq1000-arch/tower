package sqlc

import (
	"context"
	"os"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/db"
)

func TestProvisionJob(t *testing.T) {
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
	q := New(pool)
	id := "job_" + suffixC(t)

	if _, err := q.CreateProvisionJob(ctx, CreateProvisionJobParams{ID: id, Host: "1.2.3.4", Name: "n1", OwnerID: "o1", CreatedAt: 1}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := q.AppendProvisionLog(ctx, AppendProvisionLogParams{ID: id, Chunk: "step1\n", UpdatedAt: 2}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := q.AppendProvisionLog(ctx, AppendProvisionLogParams{ID: id, Chunk: "step2\n", UpdatedAt: 3}); err != nil {
		t.Fatalf("append2: %v", err)
	}
	if err := q.SetProvisionStatus(ctx, SetProvisionStatusParams{ID: id, Status: "success", Step: "done", UpdatedAt: 4}); err != nil {
		t.Fatalf("status: %v", err)
	}
	j, err := q.GetProvisionJob(ctx, id)
	if err != nil || j.Status != "success" || j.Log != "step1\nstep2\n" {
		t.Fatalf("job=%+v err=%v", j, err)
	}
}
