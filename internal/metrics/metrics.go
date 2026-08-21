// Package metrics 提供进程内构建指标：计数、耗时、当前运行数。
package metrics

import (
	"sync"
	"time"

	"minidrone/internal/store"
)

// Snapshot 是可序列化的指标快照。
type Snapshot struct {
	BuildsTotal    int   `json:"builds_total"`
	BuildsSuccess  int   `json:"builds_success"`
	BuildsFailed   int   `json:"builds_failed"`
	BuildsCanceled int   `json:"builds_canceled"`
	Running        int   `json:"running"`
	AvgDurationMS  int64 `json:"avg_duration_ms"`
	LastDurationMS int64 `json:"last_duration_ms"`
}

// Registry 是线程安全的指标寄存器。
type Registry struct {
	mu       sync.Mutex
	total    int
	success  int
	failed   int
	canceled int
	running  int
	sumMS    int64
	lastMS   int64
	finished int
}

// New 创建空寄存器。
func New() *Registry { return &Registry{} }

// OnStart 记录构建开始。
func (r *Registry) OnStart() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.total++
	r.running++
}

// OnFinish 记录构建结束。
func (r *Registry) OnFinish(state store.State, started, ended time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running > 0 {
		r.running--
	}
	switch state {
	case store.StateSuccess:
		r.success++
	case store.StateCanceled:
		r.canceled++
	default:
		r.failed++
	}
	if !started.IsZero() && !ended.IsZero() && ended.After(started) {
		ms := ended.Sub(started).Milliseconds()
		r.sumMS += ms
		r.lastMS = ms
		r.finished++
	}
}

// Snapshot 返回当前指标。
func (r *Registry) Snapshot() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := Snapshot{
		BuildsTotal:    r.total,
		BuildsSuccess:  r.success,
		BuildsFailed:   r.failed,
		BuildsCanceled: r.canceled,
		Running:        r.running,
		LastDurationMS: r.lastMS,
	}
	if r.finished > 0 {
		s.AvgDurationMS = r.sumMS / int64(r.finished)
	}
	return s
}
