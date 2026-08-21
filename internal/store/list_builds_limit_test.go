package store_test

import (
	"testing"

	"minidrone/internal/pipeline"
	"minidrone/internal/store"
)

func TestListBuildsRespectsLimit(t *testing.T) {
	st := store.New()
	p := &pipeline.Pipeline{Name: "history"}
	st.PutPipeline(p)

	for i := 0; i < 3; i++ {
		st.CreateBuild(p, "manual", "", "", "", "", "", false)
	}

	builds := st.ListBuilds(2)
	if len(builds) != 2 {
		t.Fatalf("ListBuilds(2) returned %d builds, want 2", len(builds))
	}
	if builds[0].Number != 3 || builds[1].Number != 2 {
		t.Fatalf("ListBuilds(2) returned build numbers %d and %d, want 3 and 2", builds[0].Number, builds[1].Number)
	}
}
