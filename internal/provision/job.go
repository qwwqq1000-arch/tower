package provision

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	"github.com/qwwqq1000-arch/tower/internal/crypto"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

// nodeCreator is the single DB method Provision needs when registering a node.
// It is satisfied by *sqlc.Queries and can be stubbed in unit tests.
type nodeCreator interface {
	CreateNode(ctx context.Context, arg sqlc.CreateNodeParams) (sqlc.Node, error)
}

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
// cipher is used to encrypt the generated api_key before storage; if nil the
// key is stored as plaintext (matches test behaviour where cipher is absent).
func Provision(ctx context.Context, q *sqlc.Queries, ex Executor, jobID, name, host, ownerID string, now func() int64, cipher *crypto.Cipher) {
	sink := &DBSink{Q: q, ID: jobID, Now: now}
	statusFn := func(status, step string) {
		_ = q.SetProvisionStatus(ctx, sqlc.SetProvisionStatusParams{ID: jobID, Status: status, Step: step, UpdatedAt: now()})
	}
	provisionCore(ctx, q, sink, statusFn, ex, jobID, name, host, ownerID, cipher)
}

// provisionCore is the internal implementation that accepts a Sink and nodeCreator
// so that unit tests can stub both without a real DB.
func provisionCore(ctx context.Context, nc nodeCreator, sink Sink, statusFn func(status, step string), ex Executor, jobID, name, host, ownerID string, cipher *crypto.Cipher) {
	apiKey, seed := newRand()
	steps := Steps(Input{
		APIKey:          apiKey,
		FingerprintSeed: seed,
		SourceRepo:      "https://github.com/qwwqq1000-arch/new-meridian",
		InstallDir:      "/opt/meridian",
	})
	if err := Run(ctx, ex, steps, sink); err != nil {
		statusFn("failed", "")
		return
	}
	// Encrypt the api_key before storage so it matches the manual createNode path
	// (admin_handlers.go). If cipher is nil (tests without a master key), store
	// plaintext — the reconciler read path uses DecryptOrPlaintext which is a
	// transparent no-op for unencrypted values.
	storedKey := apiKey
	if cipher != nil {
		storedKey = cipher.EncryptStr(apiKey)
	}
	_, err := nc.CreateNode(ctx, sqlc.CreateNodeParams{
		ID:              "n_" + jobID,
		Name:            name,
		BaseUrl:         "http://" + host + ":3456",
		ApiKey:          storedKey,
		MgmtKey:         "",
		OwnerID:         ownerID,
		GroupID:         "",
		Region:          "",
		ShortID:         "",
		Version:         "",
		FingerprintSeed: seed,
		Kind:            "meridian", // provision-2: provisioned nodes are always meridian
	})
	if err != nil {
		sink.Log("✗ 纳管失败: " + err.Error())
		statusFn("failed", "register")
		return
	}
	sink.Log("✓ 已纳管节点")
	statusFn("success", "done")
}
