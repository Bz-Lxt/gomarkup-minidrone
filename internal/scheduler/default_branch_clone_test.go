package scheduler

import (
	"context"
	"testing"

	"minidrone/internal/artifact"
	"minidrone/internal/executor"
	"minidrone/internal/pipeline"
	"minidrone/internal/store"
)

func TestDefaultBranchBuildStillClonesRepository(t *testing.T) {
	mock := executor.NewMock()
	st := store.New()
	p := &pipeline.Pipeline{
		Name: "default-branch",
		Stages: []pipeline.Stage{{
			Name: "test",
			Steps: []pipeline.Step{{
				Name: "unit", Image: "golang", Commands: []string{"go test ./..."},
			}},
		}},
	}
	st.PutPipeline(p)

	ctx, cancel := context.WithCancel(context.Background())
	s := New(mock, st, Options{
		Workers: 1, MaxParallelStages: 2,
		Artifacts: artifact.New(mock, t.TempDir()),
	})
	s.Start(ctx)
	t.Cleanup(func() {
		cancel()
		s.Stop()
	})

	b := st.CreateBuild(p, "github-push", "https://example.invalid/acme/repo.git", "", "8f17c21", "", "", true)
	s.Submit(b)
	waitState(t, b, store.StateSuccess)

	calls := mock.Calls()
	if len(calls) != 2 {
		t.Fatalf("default-branch repository build executed %d container steps; want clone followed by pipeline step", len(calls))
	}
	if calls[0].Labels["stage"] != "clone" || calls[0].Labels["step"] != "git-clone" {
		t.Fatalf("first container step was %q/%q; want clone/git-clone", calls[0].Labels["stage"], calls[0].Labels["step"])
	}
	if calls[1].Labels["stage"] != "test" || calls[1].Labels["step"] != "unit" {
		t.Fatalf("second container step was %q/%q; want test/unit", calls[1].Labels["stage"], calls[1].Labels["step"])
	}
}
