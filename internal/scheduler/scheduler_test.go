package scheduler

import (
	"context"
	"testing"
	"time"

	"minidrone/internal/artifact"
	"minidrone/internal/executor"
	"minidrone/internal/pipeline"
	"minidrone/internal/store"
)

func waitState(t *testing.T, b *store.Build, want store.State) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var got store.State
		b.Update(func(b *store.Build) { got = b.State })
		if got == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("构建未进入 %s, 当前 %s", want, b.Snapshot().State)
}

func startSched(t *testing.T, mock *executor.Mock, st *store.Store) (*Scheduler, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	s := New(mock, st, Options{Workers: 1, MaxParallelStages: 4, Artifacts: artifact.New(mock, t.TempDir())})
	s.Start(ctx)
	t.Cleanup(func() {
		cancel()
		s.Stop()
	})
	return s, cancel
}

func TestParallelStagesAndSuccess(t *testing.T) {
	mock := executor.NewMock()
	st := store.New()
	p := &pipeline.Pipeline{
		Name: "p",
		Stages: []pipeline.Stage{
			{Name: "lint", Steps: []pipeline.Step{{Name: "c", Image: "alpine", Commands: []string{"true"}}}},
			{Name: "test", DependsOn: []string{"lint"}, Steps: []pipeline.Step{{Name: "u", Image: "alpine", Commands: []string{"true"}}}},
			{Name: "sec", DependsOn: []string{"lint"}, Steps: []pipeline.Step{{Name: "s", Image: "alpine", Commands: []string{"true"}}}},
		},
	}
	st.PutPipeline(p)
	s, _ := startSched(t, mock, st)
	b := st.CreateBuild(p, "manual", "", "", "", "", "", false)
	s.Submit(b)
	waitState(t, b, store.StateSuccess)
	if n := len(mock.Calls()); n != 3 {
		t.Fatalf("应执行 3 个步骤, got %d", n)
	}
	if len(mock.Volumes()) != 0 {
		t.Fatalf("构建结束后卷应被回收: %v", mock.Volumes())
	}
}

func TestFailureSkipsDownstream(t *testing.T) {
	mock := executor.NewMock()
	mock.On("c", executor.Script{ExitCode: 1})
	st := store.New()
	p := &pipeline.Pipeline{
		Name: "p",
		Stages: []pipeline.Stage{
			{Name: "lint", Steps: []pipeline.Step{{Name: "c", Image: "alpine", Commands: []string{"true"}}}},
			{Name: "test", DependsOn: []string{"lint"}, Steps: []pipeline.Step{{Name: "u", Image: "alpine", Commands: []string{"true"}}}},
		},
	}
	st.PutPipeline(p)
	s, _ := startSched(t, mock, st)
	b := st.CreateBuild(p, "manual", "", "", "", "", "", false)
	s.Submit(b)
	waitState(t, b, store.StateFailed)
	snap := b.Snapshot()
	if snap.Stages[1].State != store.StateSkipped {
		t.Fatalf("下游应跳过, got %s", snap.Stages[1].State)
	}
}

func TestRetryThenSuccess(t *testing.T) {
	mock := executor.NewMock()
	mock.On("c", executor.Script{FailN: 2, Log: "flaky"})
	st := store.New()
	p := &pipeline.Pipeline{
		Name: "p",
		Stages: []pipeline.Stage{{
			Name:  "lint",
			Steps: []pipeline.Step{{Name: "c", Image: "alpine", Commands: []string{"true"}, Retries: 2, RetryDelay: "1ms"}},
		}},
	}
	st.PutPipeline(p)
	s, _ := startSched(t, mock, st)
	b := st.CreateBuild(p, "manual", "", "", "", "", "", false)
	s.Submit(b)
	waitState(t, b, store.StateSuccess)
	if n := len(mock.Calls()); n != 3 {
		t.Fatalf("失败两次后第三次成功，应调用 3 次, got %d", n)
	}
}

func TestAllowFailure(t *testing.T) {
	mock := executor.NewMock()
	mock.On("c", executor.Script{ExitCode: 2})
	st := store.New()
	p := &pipeline.Pipeline{
		Name: "p",
		Stages: []pipeline.Stage{
			{Name: "lint", Steps: []pipeline.Step{{Name: "c", Image: "alpine", Commands: []string{"true"}, AllowFailure: true}}},
			{Name: "test", DependsOn: []string{"lint"}, Steps: []pipeline.Step{{Name: "u", Image: "alpine", Commands: []string{"true"}}}},
		},
	}
	st.PutPipeline(p)
	s, _ := startSched(t, mock, st)
	b := st.CreateBuild(p, "manual", "", "", "", "", "", false)
	s.Submit(b)
	waitState(t, b, store.StateSuccess)
	if b.Snapshot().Stages[1].State != store.StateSuccess {
		t.Fatal("allow_failure 后下游应继续执行")
	}
}

func TestCollectArtifacts(t *testing.T) {
	mock := executor.NewMock()
	st := store.New()
	p := &pipeline.Pipeline{
		Name: "p",
		Stages: []pipeline.Stage{{
			Name: "build",
			Steps: []pipeline.Step{{
				Name: "c", Image: "alpine", Commands: []string{"true"},
				Artifacts: []string{"dist/app.bin"},
			}},
		}},
	}
	st.PutPipeline(p)
	s, _ := startSched(t, mock, st)
	b := st.CreateBuild(p, "manual", "", "", "", "", "", false)
	s.Submit(b)
	waitState(t, b, store.StateSuccess)
	if len(b.Snapshot().Artifacts) == 0 {
		t.Fatal("应采集到产物")
	}
}
