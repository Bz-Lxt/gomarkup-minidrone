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

type gatedExecutor struct {
	started chan string
	release chan struct{}
}

func (e *gatedExecutor) Run(ctx context.Context, cfg executor.RunConfig, _ io.Writer) (int, error) {
	select {
	case e.started <- cfg.Labels["build"]:
	case <-ctx.Done():
		return -1, ctx.Err()
	}
	select {
	case <-e.release:
		return 0, nil
	case <-ctx.Done():
		return -1, ctx.Err()
	}
}

func (*gatedExecutor) CreateVolume(context.Context, string) error { return nil }
func (*gatedExecutor) RemoveVolume(context.Context, string) error { return nil }
func (*gatedExecutor) CopyOut(context.Context, string, string, string, []string) error {
	return nil
}
func (*gatedExecutor) Ping(context.Context) error { return nil }

func waitTerminal(t *testing.T, builds ...*store.Build) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		allDone := true
		for _, b := range builds {
			if !b.Snapshot().State.Terminal() {
				allDone = false
				break
			}
		}
		if allDone {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("builds did not finish: %s, %s", builds[0].Snapshot().State, builds[1].Snapshot().State)
}

func TestWorkerLimitSerializesConcurrentBuilds(t *testing.T) {
	exec := &gatedExecutor{
		started: make(chan string, 2),
		release: make(chan struct{}),
	}
	st := store.New()
	p := &pipeline.Pipeline{
		Name: "deploy",
		Stages: []pipeline.Stage{{
			Name: "release",
			Steps: []pipeline.Step{{
				Name: "publish", Image: "alpine", Commands: []string{"deploy"},
			}},
		}},
	}
	st.PutPipeline(p)

	ctx, cancel := context.WithCancel(context.Background())
	s := scheduler.New(exec, st, scheduler.Options{Workers: 1, MaxParallelStages: 1})
	s.Start(ctx)
	var releaseOnce sync.Once
	defer func() {
		releaseOnce.Do(func() { close(exec.release) })
		cancel()
		s.Stop()
	}()

	b1 := st.CreateBuild(p, "manual", "", "", "", "", "", false)
	b2 := st.CreateBuild(p, "manual", "", "", "", "", "", false)
	s.Submit(b1)
	s.Submit(b2)

	select {
	case <-exec.started:
	case <-time.After(2 * time.Second):
		t.Fatal("first build did not start")
	}

	concurrent := false
	select {
	case <-exec.started:
		concurrent = true
	case <-time.After(100 * time.Millisecond):
	}

	releaseOnce.Do(func() { close(exec.release) })
	waitTerminal(t, b1, b2)
	if concurrent {
		t.Fatal("Workers=1 allowed a second build to execute before the first one finished")
	}
}
