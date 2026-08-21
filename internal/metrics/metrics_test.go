package metrics

import (
	"testing"
	"time"

	"minidrone/internal/store"
)

func TestRegistryLifecycle(t *testing.T) {
	r := New()
	r.OnStart()
	r.OnStart()
	start := time.Now().Add(-2 * time.Second)
	end := time.Now()
	r.OnFinish(store.StateSuccess, start, end)
	r.OnFinish(store.StateFailed, start, end)
	s := r.Snapshot()
	if s.BuildsTotal != 2 || s.BuildsSuccess != 1 || s.BuildsFailed != 1 || s.Running != 0 {
		t.Fatalf("计数不符: %+v", s)
	}
	if s.AvgDurationMS <= 0 || s.LastDurationMS <= 0 {
		t.Fatalf("耗时应被记录: %+v", s)
	}
}
