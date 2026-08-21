package scheduler_test

import (
	"context"
	"io"
	"testing"
	"time"

	"minidrone/internal/artifact"
	"minidrone/internal/executor"
	"minidrone/internal/pipeline"
	"minidrone/internal/scheduler"
	"minidrone/internal/store"
)

type blockingCopyExecutor struct {
	copyStarted chan struct{}
	releaseCopy chan struct{}
}

func (e *blockingCopyExecutor) Run(context.Context, executor.RunConfig, io.Writer) (int, error) {
	return 0, nil
}

func (e *blockingCopyExecutor) CreateVolume(context.Context, string) error { return nil }
func (e *blockingCopyExecutor) RemoveVolume(context.Context, string) error { return nil }
func (e *blockingCopyExecutor) Ping(context.Context) error                 { return nil }

func (e *blockingCopyExecutor) CopyOut(ctx context.Context, _, _, _ string, _ []string) error {
	close(e.copyStarted)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-e.releaseCopy:
		return nil
	}
}

func TestCancelBuildDuringArtifactCollection(t *testing.T) {
	exec := &blockingCopyExecutor{
		copyStarted: make(chan struct{}),
		releaseCopy: make(chan struct{}),
	}
	defer close(exec.releaseCopy)

	st := store.New()
	p := &pipeline.Pipeline{
		Name: "release",
		Stages: []pipeline.Stage{{
			Name: "package",
			Steps: []pipeline.Step{{
				Name:      "archive",
				Image:     "alpine",
				Commands:  []string{"true"},
				Artifacts: []string{"dist/app.tgz"},
			}},
		}},
	}
	st.PutPipeline(p)

	ctx, stop := context.WithCancel(context.Background())
	s := scheduler.New(exec, st, scheduler.Options{
		Workers:   1,
		Artifacts: artifact.New(exec, t.TempDir()),
	})
	s.Start(ctx)
	t.Cleanup(func() {
		stop()
		s.Stop()
	})

	b := st.CreateBuild(p, "manual", "", "", "", "", "", false)
	s.Submit(b)
	select {
	case <-exec.copyStarted:
	case <-time.After(time.Second):
		t.Fatal("产物导出未开始")
	}

	if !s.Cancel(b.ID) {
		t.Fatal("运行中的构建应可取消")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if b.Snapshot().State == store.StateCanceled {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("取消后产物导出仍未停止，构建状态为 %s", b.Snapshot().State)
}
