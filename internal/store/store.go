// Package store 提供流水线定义与构建运行态的内存存储，以及日志缓冲和事件广播。
package store

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"minidrone/internal/pipeline"
)

// State 表示构建 / 阶段 / 步骤的生命周期状态。
type State string

const (
	StatePending  State = "pending"
	StateRunning  State = "running"
	StateSuccess  State = "success"
	StateFailed   State = "failed"
	StateSkipped  State = "skipped"
	StateCanceled State = "canceled"
)

// Terminal 报告状态是否为终态。
func (s State) Terminal() bool {
	switch s {
	case StateSuccess, StateFailed, StateSkipped, StateCanceled:
		return true
	}
	return false
}

// Artifact 是一次构建采集到的产物条目。
type Artifact struct {
	Stage string `json:"stage"`
	Step  string `json:"step"`
	Name  string `json:"name"`
	Path  string `json:"path,omitempty"`
	Size  int64  `json:"size"`
}

// StepStatus 是一个步骤的运行时状态。
type StepStatus struct {
	Name      string    `json:"name"`
	Image     string    `json:"image"`
	State     State     `json:"state"`
	ExitCode  int       `json:"exit_code"`
	Attempts  int       `json:"attempts"`
	StartedAt time.Time `json:"started_at,omitempty"`
	EndedAt   time.Time `json:"ended_at,omitempty"`
}

// StageStatus 是一个阶段的运行时状态。
type StageStatus struct {
	Name      string        `json:"name"`
	DependsOn []string      `json:"depends_on,omitempty"`
	State     State         `json:"state"`
	Steps     []*StepStatus `json:"steps"`
	StartedAt time.Time     `json:"started_at,omitempty"`
	EndedAt   time.Time     `json:"ended_at,omitempty"`
}

// Build 是一次流水线运行的完整运行时状态。
type Build struct {
	ID       string `json:"id"`
	Pipeline string `json:"pipeline"`
	Number   int    `json:"number"`
	State    State  `json:"state"`

	Trigger string `json:"trigger"` // manual / github-push / github-pull_request / gitlab-...
	Repo    string `json:"repo,omitempty"`
	Branch  string `json:"branch,omitempty"`
	Commit  string `json:"commit,omitempty"`
	Message string `json:"message,omitempty"`
	Author  string `json:"author,omitempty"`

	Stages    []*StageStatus `json:"stages"`
	Artifacts []Artifact     `json:"artifacts,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	StartedAt time.Time      `json:"started_at,omitempty"`
	EndedAt   time.Time      `json:"ended_at,omitempty"`
	Error     string         `json:"error,omitempty"`

	Cancel context.CancelFunc `json:"-"`

	mu sync.Mutex
}

// Update 在持锁状态下修改构建，保证并发读写安全。
func (b *Build) Update(fn func(b *Build)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	fn(b)
}

// Snapshot 返回构建的深拷贝，用于 JSON 序列化等只读场景。
func (b *Build) Snapshot() *Build {
	b.mu.Lock()
	defer b.mu.Unlock()
	cp := &Build{
		ID: b.ID, Pipeline: b.Pipeline, Number: b.Number, State: b.State,
		Trigger: b.Trigger, Repo: b.Repo, Branch: b.Branch, Commit: b.Commit,
		Message: b.Message, Author: b.Author,
		CreatedAt: b.CreatedAt, StartedAt: b.StartedAt, EndedAt: b.EndedAt,
		Error: b.Error,
	}
	if len(b.Artifacts) > 0 {
		cp.Artifacts = append([]Artifact{}, b.Artifacts...)
	}
	cp.Stages = make([]*StageStatus, len(b.Stages))
	for i, st := range b.Stages {
		scp := *st
		scp.Steps = make([]*StepStatus, len(st.Steps))
		for j, sp := range st.Steps {
			pcp := *sp
			scp.Steps[j] = &pcp
		}
		cp.Stages[i] = &scp
	}
	return cp
}

// AddArtifacts 追加产物清单。
func (b *Build) AddArtifacts(items []Artifact) {
	if len(items) == 0 {
		return
	}
	b.Update(func(b *Build) {
		b.Artifacts = append(b.Artifacts, items...)
	})
}

// FindStage 按名称查找阶段状态（调用方需在 Update 内或自行保证安全）。
func (b *Build) FindStage(name string) *StageStatus {
	for _, st := range b.Stages {
		if st.Name == name {
			return st
		}
	}
	return nil
}

// maxLogBytes 单个步骤日志缓冲上限，超出后停止追加并标注截断。
const maxLogBytes = 1 << 20

// Store 是线程安全的内存存储。
type Store struct {
	mu        sync.RWMutex
	pipelines map[string]*pipeline.Pipeline
	builds    map[string]*Build
	order     []string // 构建 ID，按创建顺序
	counters  map[string]int
	logs      map[string]map[string]*logBuffer // buildID -> "stage/step" -> buffer
	broker    *Broker
}

// New 创建存储实例。
func New() *Store {
	return &Store{
		pipelines: make(map[string]*pipeline.Pipeline),
		builds:    make(map[string]*Build),
		counters:  make(map[string]int),
		logs:      make(map[string]map[string]*logBuffer),
		broker:    NewBroker(),
	}
}

// Broker 返回全局事件总线。
func (s *Store) Broker() *Broker { return s.broker }

// --- 流水线定义 ---

// PutPipeline 注册或覆盖一条流水线定义。
func (s *Store) PutPipeline(p *pipeline.Pipeline) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pipelines[p.Name] = p
}

// GetPipeline 按名称获取流水线定义。
func (s *Store) GetPipeline(name string) (*pipeline.Pipeline, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.pipelines[name]
	return p, ok
}

// ListPipelines 返回所有已注册流水线。
func (s *Store) ListPipelines() []*pipeline.Pipeline {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*pipeline.Pipeline, 0, len(s.pipelines))
	for _, p := range s.pipelines {
		out = append(out, p)
	}
	return out
}

// DeletePipeline 移除一条流水线定义。
func (s *Store) DeletePipeline(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.pipelines[name]; !ok {
		return false
	}
	delete(s.pipelines, name)
	return true
}

// --- 构建 ---

// CreateBuild 基于流水线定义创建一次构建，分配编号并登记。
func (s *Store) CreateBuild(p *pipeline.Pipeline, trigger, repo, branch, commit, message, author string, cloneStage bool) *Build {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.counters[p.Name]++
	b := &Build{
		ID:        newID(),
		Pipeline:  p.Name,
		Number:    s.counters[p.Name],
		State:     StatePending,
		Trigger:   trigger,
		Repo:      repo,
		Branch:    branch,
		Commit:    commit,
		Message:   message,
		Author:    author,
		CreatedAt: time.Now(),
	}

	// Webhook 触发的构建带有仓库信息，前置一个隐式 clone 阶段把代码拉进共享卷。
	if cloneStage && repo != "" {
		clone := &StageStatus{
			Name:  "clone",
			State: StatePending,
			Steps: []*StepStatus{{Name: "git-clone", Image: "alpine/git", State: StatePending, ExitCode: -1}},
		}
		b.Stages = append(b.Stages, clone)
	}
	for _, st := range p.Stages {
		ss := &StageStatus{Name: st.Name, DependsOn: st.DependsOn, State: StatePending}
		if cloneStage && repo != "" && len(st.DependsOn) == 0 {
			// 无依赖的根阶段隐式依赖 clone，保证代码先就位
			ss.DependsOn = []string{"clone"}
		}
		for _, sp := range st.Steps {
			ss.Steps = append(ss.Steps, &StepStatus{Name: sp.Name, Image: sp.Image, State: StatePending, ExitCode: -1})
		}
		b.Stages = append(b.Stages, ss)
	}

	s.builds[b.ID] = b
	s.order = append(s.order, b.ID)
	s.logs[b.ID] = make(map[string]*logBuffer)
	return b
}

// GetBuild 获取构建（返回的是共享指针，调用方读取字段需自行持锁或用 Snapshot）。
func (s *Store) GetBuild(id string) (*Build, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.builds[id]
	return b, ok
}

// ListBuilds 按创建时间倒序返回构建，最多 limit 条。
func (s *Store) ListBuilds(limit int) []*Build {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 || limit > len(s.order) {
		limit = len(s.order)
	}
	out := make([]*Build, 0, limit)
	for i := len(s.order) - 1; i >= 0 && len(out) <= limit; i-- {
		out = append(out, s.builds[s.order[i]])
	}
	return out
}

// --- 日志 ---

// LogKey 拼接阶段/步骤的日志键。
func LogKey(stage, step string) string { return stage + "/" + step }

// AppendLog 向指定步骤的日志缓冲追加内容，并按行广播给订阅者。
func (s *Store) AppendLog(buildID, stage, step string, chunk []byte) {
	s.mu.Lock()
	buf, ok := s.logs[buildID][LogKey(stage, step)]
	if !ok {
		buf = &logBuffer{}
		if s.logs[buildID] == nil {
			s.logs[buildID] = make(map[string]*logBuffer)
		}
		s.logs[buildID][LogKey(stage, step)] = buf
	}
	s.mu.Unlock()

	lines := buf.write(chunk)
	for _, ln := range lines {
		s.broker.Publish(Event{
			Type:  EventLog,
			Build: buildID,
			Stage: stage,
			Step:  step,
			Line:  ln,
			Time:  time.Now(),
		})
	}
}

// Log 返回指定步骤的完整日志文本。
func (s *Store) Log(buildID, stage, step string) string {
	s.mu.RLock()
	buf := s.logs[buildID][LogKey(stage, step)]
	s.mu.RUnlock()
	if buf == nil {
		return ""
	}
	return buf.string()
}

// logBuffer 是带上限的日志缓冲，按行切分以便 SSE 推送。
type logBuffer struct {
	mu        sync.Mutex
	data      strings.Builder
	partial   string // 尚未遇到换行的半行
	size      int
	truncated bool
}

// write 追加数据，返回本次产生的完整行（不含末尾半行）。
func (b *logBuffer) write(chunk []byte) []string {
	b.mu.Lock()
	defer b.mu.Unlock()

	text := b.partial + string(chunk)
	parts := strings.Split(text, "\n")
	b.partial = parts[len(parts)-1]
	complete := parts[:len(parts)-1]

	if b.size < maxLogBytes {
		remain := maxLogBytes - b.size
		if len(chunk) > remain {
			chunk = chunk[:remain]
			b.truncated = true
		}
		b.data.Write(chunk)
		b.size += len(chunk)
	} else if !b.truncated {
		b.truncated = true
		b.data.WriteString("\n*** 日志超出 1MB 上限，已截断 ***\n")
	}
	return complete
}

func (b *logBuffer) string() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.partial != "" {
		return b.data.String() + b.partial
	}
	return b.data.String()
}

// newID 生成短随机 ID。
func newID() string {
	var b [6]byte
	if _, err := randRead(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x", b[:])
}
