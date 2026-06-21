package provision

import (
	"context"
	"strings"
	"testing"
)

type fakeExec struct {
	cmds   []string
	failAt int // 1-based index to fail; 0 = never
}

func (f *fakeExec) Exec(_ context.Context, cmd string) (ExecResult, error) {
	f.cmds = append(f.cmds, cmd)
	if f.failAt != 0 && len(f.cmds) == f.failAt {
		return ExecResult{Stderr: "boom", Code: 1}, nil
	}
	return ExecResult{Stdout: "ok", Code: 0}, nil
}

type capSink struct {
	logs  []string
	steps []string
}

func (s *capSink) Log(l string)     { s.logs = append(s.logs, l) }
func (s *capSink) SetStep(k string) { s.steps = append(s.steps, k) }

func TestRun_AllStepsSucceed(t *testing.T) {
	steps := Steps(Input{APIKey: "sk-mer-x", FingerprintSeed: "s", SourceRepo: "r", InstallDir: "/opt/meridian"})
	ex := &fakeExec{}
	sink := &capSink{}
	if err := Run(context.Background(), ex, steps, sink); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(ex.cmds) != len(steps) {
		t.Fatalf("ran %d cmds, want %d", len(ex.cmds), len(steps))
	}
	if len(sink.steps) != len(steps) {
		t.Fatalf("steps reported %d, want %d", len(sink.steps), len(steps))
	}
}

func TestRun_StopsOnFailure(t *testing.T) {
	steps := Steps(Input{InstallDir: "/opt/meridian"})
	ex := &fakeExec{failAt: 2} // fail at second step
	sink := &capSink{}
	if err := Run(context.Background(), ex, steps, sink); err == nil {
		t.Fatal("Run should error when a step fails")
	}
	if len(ex.cmds) != 2 {
		t.Fatalf("should stop after failing step, ran %d", len(ex.cmds))
	}
	joined := strings.Join(sink.logs, "\n")
	if !strings.Contains(joined, "✗") {
		t.Fatal("failure should be logged with ✗")
	}
}
