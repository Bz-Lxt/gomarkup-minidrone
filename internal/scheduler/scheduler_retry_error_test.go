package scheduler

import (
	"testing"
	"time"

	"minidrone/internal/executor"
	"minidrone/internal/pipeline"
	"minidrone/internal/store"
)

func TestExhaustedRetriesFailBuildAndSkipDownstream(t *testing.T) {
	mock := executor.NewMock()
	mock.On("compile", executor.Script{FailN: 3})
	st := store.New()
	p := &pipeline.Pipeline{
		Name: "release",
		Stages: []pipeline.Stage{
			{
				Name: "build",
				Steps: []pipeline.Step{{
					Name:       "compile",
					Image:      "alpine",
					Commands:   []string{"make"},
					Retries:    2,
					RetryDelay: "1ms",
				}},
			},
			{
				Name:      "deploy",
				DependsOn: []string{"build"},
				Steps: []pipeline.Step{{
					Name:     "publish",
					Image:    "alpine",
					Commands: []string{"publish"},
				}},
			},
		},
	}
	st.PutPipeline(p)
	s, _ := startSched(t, mock, st)
	b := st.CreateBuild(p, "manual", "", "", "", "", "", false)
	s.Submit(b)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		state := b.Snapshot().State
		if state == store.StateSuccess || state == store.StateFailed || state == store.StateCanceled {
			break
		}
		time.Sleep(time.Millisecond)
	}

	snap := b.Snapshot()
	if snap.State != store.StateFailed {
		t.Fatalf("重试耗尽后构建应失败, got %s", snap.State)
	}
	if snap.Stages[0].State != store.StateFailed {
		t.Fatalf("重试耗尽的阶段应失败, got %s", snap.Stages[0].State)
	}
	if snap.Stages[1].State != store.StateSkipped {
		t.Fatalf("失败阶段的下游应跳过, got %s", snap.Stages[1].State)
	}
	if calls := mock.Calls(); len(calls) != 3 {
		t.Fatalf("首次执行加两次重试应执行 3 次, got %d", len(calls))
	}
}
