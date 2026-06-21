package provision

import (
	"context"
	"fmt"
)

// ExecResult is the outcome of one remote command.
type ExecResult struct {
	Stdout string
	Stderr string
	Code   int
}

// Executor runs a shell command on the target host.
type Executor interface {
	Exec(ctx context.Context, cmd string) (ExecResult, error)
}

// Sink receives provisioning progress.
type Sink interface {
	Log(line string)
	SetStep(key string)
}

// Run executes steps in order, reporting progress; it aborts on the first failure.
func Run(ctx context.Context, ex Executor, steps []Step, sink Sink) error {
	for _, s := range steps {
		sink.SetStep(s.Key)
		sink.Log("▶ " + s.Label)
		res, err := ex.Exec(ctx, s.Cmd)
		if err != nil {
			sink.Log(fmt.Sprintf("✗ %s: %v", s.Label, err))
			return fmt.Errorf("step %s: %w", s.Key, err)
		}
		if res.Code != 0 {
			sink.Log(fmt.Sprintf("✗ %s (exit %d): %s", s.Label, res.Code, res.Stderr))
			return fmt.Errorf("step %s exit %d", s.Key, res.Code)
		}
		sink.Log("✓ " + s.Label)
	}
	return nil
}
