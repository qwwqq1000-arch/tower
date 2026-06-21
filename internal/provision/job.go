package provision

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

// DBSink writes provisioning progress into a provision_jobs row.
type DBSink struct {
	Q   *sqlc.Queries
	ID  string
	Now func() int64
}

// Log appends a line to the job log.
func (s *DBSink) Log(line string) {
	_ = s.Q.AppendProvisionLog(context.Background(), sqlc.AppendProvisionLogParams{ID: s.ID, Chunk: line + "\n", UpdatedAt: s.Now()})
}

// SetStep records the current step (status stays running).
func (s *DBSink) SetStep(key string) {
	_ = s.Q.SetProvisionStatus(context.Background(), sqlc.SetProvisionStatusParams{ID: s.ID, Status: "running", Step: key, UpdatedAt: s.Now()})
}

func newRand() (apiKey, seed string) {
	apiKey = GenAPIKey(func(b []byte) { _, _ = rand.Read(b) })
	sb := make([]byte, 8)
	_, _ = rand.Read(sb)
	return apiKey, hex.EncodeToString(sb)
}

// Provision runs the full provisioning flow and registers the node on success.
func Provision(ctx context.Context, q *sqlc.Queries, ex Executor, jobID, name, host, ownerID string, now func() int64) {
	apiKey, seed := newRand()
	steps := Steps(Input{
		APIKey:          apiKey,
		FingerprintSeed: seed,
		SourceRepo:      "https://github.com/qwwqq1000-arch/new-meridian",
		InstallDir:      "/opt/meridian",
	})
	sink := &DBSink{Q: q, ID: jobID, Now: now}
	if err := Run(ctx, ex, steps, sink); err != nil {
		_ = q.SetProvisionStatus(ctx, sqlc.SetProvisionStatusParams{ID: jobID, Status: "failed", Step: "", UpdatedAt: now()})
		return
	}
	_, err := q.CreateNode(ctx, sqlc.CreateNodeParams{
		ID:              "n_" + jobID,
		Name:            name,
		BaseUrl:         "http://" + host + ":3456",
		ApiKey:          apiKey,
		MgmtKey:         "",
		OwnerID:         ownerID,
		GroupID:         "",
		Region:          "",
		ShortID:         "",
		Version:         "",
		FingerprintSeed: seed,
	})
	if err != nil {
		sink.Log("✗ 纳管失败: " + err.Error())
		_ = q.SetProvisionStatus(ctx, sqlc.SetProvisionStatusParams{ID: jobID, Status: "failed", Step: "register", UpdatedAt: now()})
		return
	}
	sink.Log("✓ 已纳管节点")
	_ = q.SetProvisionStatus(ctx, sqlc.SetProvisionStatusParams{ID: jobID, Status: "success", Step: "done", UpdatedAt: now()})
}
