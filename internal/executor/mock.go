package executor

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Script 描述 mock 执行器对某一步骤的预设行为。
type Script struct {
	ExitCode int
	Log      string
	Err      error
	FailN    int // 前 N 次返回失败，之后成功（用于验证重试）
}

// Mock 是内存执行器，供调度器单测使用。
type Mock struct {
	mu       sync.Mutex
	scripts  map[string]Script // 按 step 名或 "stage/step" 匹配
	calls    []RunConfig
	volumes  map[string]bool
	copyDirs []string
	runCount map[string]int
}

// NewMock 创建空 mock。
func NewMock() *Mock {
	return &Mock{
		scripts:  make(map[string]Script),
		volumes:  make(map[string]bool),
		runCount: make(map[string]int),
	}
}

// On 为指定步骤名登记预设结果。
func (m *Mock) On(step string, s Script) { m.scripts[step] = s }

func (m *Mock) lookup(cfg RunConfig) Script {
	if s, ok := m.scripts[cfg.Labels["step"]]; ok {
		return s
	}
	if s, ok := m.scripts[cfg.Labels["stage"]+"/"+cfg.Labels["step"]]; ok {
		return s
	}
	if s, ok := m.scripts[cfg.Name]; ok {
		return s
	}
	return Script{}
}

// Run 按预设脚本输出日志并返回退出码。
func (m *Mock) Run(ctx context.Context, cfg RunConfig, logs io.Writer) (int, error) {
	if err := ctx.Err(); err != nil {
		return -1, err
	}
	m.mu.Lock()
	m.calls = append(m.calls, cfg)
	key := cfg.Labels["step"]
	m.runCount[key]++
	n := m.runCount[key]
	s := m.lookup(cfg)
	m.mu.Unlock()

	if s.Log != "" {
		_, _ = io.WriteString(logs, s.Log)
		if !strings.HasSuffix(s.Log, "\n") {
			_, _ = io.WriteString(logs, "\n")
		}
	} else {
		fmt.Fprintf(logs, "mock run %s\n", cfg.Name)
	}
	if s.FailN > 0 && n <= s.FailN {
		return 1, nil
	}
	if s.Err != nil {
		return s.ExitCode, s.Err
	}
	return s.ExitCode, nil
}

// CreateVolume 登记卷。
func (m *Mock) CreateVolume(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.volumes[name] = true
	return nil
}

// RemoveVolume 删除卷登记。
func (m *Mock) RemoveVolume(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.volumes, name)
	return nil
}

// CopyOut 在 destDir 写入占位文件，模拟产物采集。
func (m *Mock) CopyOut(_ context.Context, _, _, destDir string, patterns []string) error {
	m.mu.Lock()
	m.copyDirs = append(m.copyDirs, destDir)
	m.mu.Unlock()
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	for _, p := range patterns {
		name := filepath.Base(p)
		if name == "" || name == "." || name == "*" {
			name = "artifact.bin"
		}
		name = strings.ReplaceAll(name, "*", "file")
		return os.WriteFile(filepath.Join(destDir, name), []byte("ok"), 0o644)
	}
	return nil
}

// Ping 始终成功。
func (m *Mock) Ping(context.Context) error { return nil }

// Calls 返回已执行的 RunConfig 副本。
func (m *Mock) Calls() []RunConfig {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]RunConfig, len(m.calls))
	copy(out, m.calls)
	return out
}

// Volumes 返回当前登记的卷名。
func (m *Mock) Volumes() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for n := range m.volumes {
		out = append(out, n)
	}
	return out
}
