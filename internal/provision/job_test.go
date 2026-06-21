package provision

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/db"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

func sk(t *testing.T) string {
	t.Helper()
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func TestProvision_SuccessRegistersNode(t *testing.T) {
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
	q := sqlc.New(pool)
	s := sk(t)
	jobID := "job_" + s
	now := func() int64 { return 1 }
	if _, err := q.CreateProvisionJob(ctx, sqlc.CreateProvisionJobParams{ID: jobID, Host: "10.0.0.5", Name: "node-" + s, OwnerID: "o_" + s, CreatedAt: 1}); err != nil {
		t.Fatalf("create job: %v", err)
	}

	Provision(ctx, q, &fakeExec{}, jobID, "node-"+s, "10.0.0.5", "o_"+s, now)

	j, _ := q.GetProvisionJob(ctx, jobID)
	if j.Status != "success" {
		t.Fatalf("job status=%s log=%s", j.Status, j.Log)
	}
	// node registered
	nodes, _ := q.ListNodesByOwner(ctx, "o_"+s)
	if len(nodes) != 1 || nodes[0].BaseUrl != "http://10.0.0.5:3456" {
		t.Fatalf("node not registered: %+v", nodes)
	}
}

func TestProvision_FailureMarksJob(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	_ = db.Migrate(ctx, url)
	pool, err := db.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	q := sqlc.New(pool)
	s := sk(t)
	jobID := "job_" + s
	_, _ = q.CreateProvisionJob(ctx, sqlc.CreateProvisionJobParams{ID: jobID, Host: "10.0.0.6", Name: "n", OwnerID: "o_" + s, CreatedAt: 1})

	Provision(ctx, q, &fakeExec{failAt: 1}, jobID, "n", "10.0.0.6", "o_"+s, func() int64 { return 1 })

	j, _ := q.GetProvisionJob(ctx, jobID)
	if j.Status != "failed" {
		t.Fatalf("status=%s, want failed", j.Status)
	}
	nodes, _ := q.ListNodesByOwner(ctx, "o_"+s)
	if len(nodes) != 0 {
		t.Fatal("no node should be registered on failure")
	}
}
