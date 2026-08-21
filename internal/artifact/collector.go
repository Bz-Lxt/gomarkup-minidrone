// Package artifact 从构建共享卷中采集产物文件到宿主机目录。
package artifact

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"minidrone/internal/executor"
	"minidrone/internal/store"
)

// Collector 负责把步骤声明的产物从 Docker 卷拷到本地。
type Collector struct {
	Exec executor.Executor
	Root string
}

// New 创建采集器。root 为产物根目录。
func New(exec executor.Executor, root string) *Collector {
	if root == "" {
		root = "artifacts"
	}
	return &Collector{Exec: exec, Root: root}
}

// DestDir 返回某次构建某步骤的落地目录。
func (c *Collector) DestDir(buildID, stage, step string) string {
	return filepath.Join(c.Root, buildID, stage, step)
}

// Collect 把 volume 中匹配 patterns 的文件拷到宿主机，并返回产物清单。
func (c *Collector) Collect(ctx context.Context, volume, workDir, buildID, stage, step string, patterns []string) ([]store.Artifact, error) {
	if len(patterns) == 0 {
		return nil, nil
	}
	dest := c.DestDir(buildID, stage, step)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return nil, fmt.Errorf("创建产物目录失败: %w", err)
	}
	if err := c.Exec.CopyOut(ctx, volume, workDir, dest, patterns); err != nil {
		return nil, err
	}
	return scan(dest, stage, step)
}

func scan(dir, stage, step string) ([]store.Artifact, error) {
	var out []store.Artifact
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			rel = filepath.Base(path)
		}
		out = append(out, store.Artifact{
			Stage: stage,
			Step:  step,
			Name:  filepath.ToSlash(rel),
			Path:  path,
			Size:  info.Size(),
		})
		return nil
	})
	return out, err
}

// SanitizePattern 拒绝含路径穿越的产物模式。
func SanitizePattern(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", fmt.Errorf("空产物路径")
	}
	if p == ".." || strings.HasPrefix(p, "../") || strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("非法产物路径 %q", p)
	}
	return p, nil
}

// Filter 过滤并规范化产物路径列表。
func Filter(patterns []string) ([]string, error) {
	var out []string
	for _, p := range patterns {
		s, err := SanitizePattern(p)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}
