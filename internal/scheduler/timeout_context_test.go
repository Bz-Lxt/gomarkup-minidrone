package scheduler_test

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"minidrone/internal/executor"
	"minidrone/internal/pipeline"
	"minidrone/internal/scheduler"
	"minidrone/internal/store"
)

type blockingExecutor struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (e *blockingExecutor) Run(ctx context.Context, _ executor.RunConfig, _ io.Writer) (int, error) {
	e.once.Do(func() { close(e.started) })
	select {
	case <-ctx.Done():
		return -1, ctx.Err()
	case <-e.release:
		return 0, nil
	}
}

func (*blockingExecutor) CreateVolume(context.Context, string) error { return nil }
func (*blockingExecutor) RemoveVolume(context.Context, string) error { return nil }
func (*blockingExecutor) CopyOut(context.Context, string, string, string, []string) error {
	return nil
}
func (*blockingExecutor) Ping(context.Context) error { return nil }

func TestStepTimeoutCancelsRunningExecutor(t *testing.T) {
	exec := &blockingExecutor{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	st := store.New()
	p := &pipeline.Pipeline{
		Name: "step-timeout",
		Stages: []pipeline.Stage{{
			Name: "build",
			Steps: []pipeline.Step{{
				Name: "compile", Image: "alpine", Commands: []string{"sleep 60"}, Timeout: "25ms",
			}},
		}},
	}
	st.PutPipeline(p)

	ctx, cancel := context.WithCancel(context.Background())
	sched := scheduler.New(exec, st, scheduler.Options{Workers: 1, MaxParallelStages: 1})
	sched.Start(ctx)
	t.Cleanup(func() {
		close(exec.release)
		cancel()
		sched.Stop()
	})

	b := st.CreateBuild(p, "manual", "", "", "", "", "", false)
	sched.Submit(b)
	select {
	case <-exec.started:
	case <-time.After(time.Second):
		t.Fatal("executor did not start")
	}

	deadline := time.Now().Add(750 * time.Millisecond)
	for time.Now().Before(deadline) {
		snap := b.Snapshot()
		if snap.State.Terminal() {
			if snap.State != store.StateFailed {
				t.Fatalf("timed-out build state = %s, want failed", snap.State)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("build did not honor the step timeout; state = %s", b.Snapshot().State)
}
