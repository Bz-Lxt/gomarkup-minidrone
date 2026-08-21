package scheduler

import (
	"errors"
	"testing"
	"time"

	"minidrone/internal/executor"
	"minidrone/internal/pipeline"
	"minidrone/internal/store"
)

func TestExecutorErrorWithZeroExitCodeFailsBuild(t *testing.T) {
	mock := executor.NewMock()
	mock.On("compile", executor.Script{ExitCode: 0, Err: errors.New("container runtime connection lost")})
	st := store.New()
	p := &pipeline.Pipeline{
		Name: "release",
		Stages: []pipeline.Stage{
			{Name: "build", Steps: []pipeline.Step{{Name: "compile", Image: "alpine", Commands: []string{"make"}}}},
			{Name: "deploy", DependsOn: []string{"build"}, Steps: []pipeline.Step{{Name: "publish", Image: "alpine", Commands: []string{"publish"}}}},
		},
	}
	st.PutPipeline(p)
	s, _ := startSched(t, mock, st)
	b := st.CreateBuild(p, "manual", "", "", "", "", "", false)
	s.Submit(b)
	deadline := time.Now().Add(3 * time.Second)
	for !b.Snapshot().State.Terminal() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}

	snap := b.Snapshot()
	if snap.State != store.StateFailed {
		t.Fatalf("executor error should fail build, got %s", snap.State)
	}
	if snap.Stages[0].State != store.StateFailed {
		t.Fatalf("executor error should fail build stage, got %s", snap.Stages[0].State)
	}
	if snap.Stages[1].State != store.StateSkipped {
		t.Fatalf("deploy stage should not run after executor error, got %s", snap.Stages[1].State)
	}
	if len(mock.Calls()) != 1 {
		t.Fatalf("only failing step should run, got %d executor calls", len(mock.Calls()))
	}
}
