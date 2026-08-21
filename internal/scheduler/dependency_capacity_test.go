package scheduler

import (
	"context"
	"fmt"
	"runtime"
	"testing"
	"time"

	"minidrone/internal/executor"
	"minidrone/internal/pipeline"
	"minidrone/internal/store"
)

func TestDependentStageDoesNotBlockItsDependency(t *testing.T) {
	previous := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previous)

	st := store.New()
	p := &pipeline.Pipeline{Name: "release"}
	p.Stages = append(p.Stages, pipeline.Stage{Name: "compile", Steps: []pipeline.Step{{Name: "build", Image: "alpine"}}})
	for i := 0; i < 64; i++ {
		p.Stages = append(p.Stages, pipeline.Stage{
			Name:      fmt.Sprintf("publish-%d", i),
			DependsOn: []string{"compile"},
			Steps:     []pipeline.Step{{Name: "publish", Image: "alpine"}},
		})
	}
	st.PutPipeline(p)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := New(executor.NewMock(), st, Options{Workers: 1, MaxParallelStages: 1})
	s.Start(ctx)
	b := st.CreateBuild(p, "manual", "", "", "", "", "", false)
	s.Submit(b)

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if snap := b.Snapshot(); snap.State.Terminal() {
			if snap.State != store.StateSuccess {
				t.Fatalf("dependency pipeline finished in %s, want success", snap.State)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("dependency pipeline did not finish: state=%s", b.Snapshot().State)
}
