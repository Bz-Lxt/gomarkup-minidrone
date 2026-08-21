// Package executor 定义步骤执行引擎抽象，并提供基于 Docker 官方 Go SDK 的实现。
package executor

import (
	"context"
	"io"
)

// RunConfig 描述一次容器化步骤执行所需的全部参数。
type RunConfig struct {
	Name      string            // 容器名（需唯一）
	Image     string            // 容器镜像
	Commands  []string          // 依次执行的 shell 命令（set -e 语义）
	Env       []string          // KEY=VALUE 形式的环境变量
	WorkDir   string            // 容器内工作目录
	Volume    string            // 挂载的 Docker 卷名（跨步骤共享工作区）
	MountPath string            // 卷在容器内的挂载点
	Pull      bool              // 是否总是拉取镜像
	Labels    map[string]string // 容器标签
}

// Executor 是执行引擎接口，便于测试时替换为 mock。
type Executor interface {
	// Run 创建并启动容器，流式输出日志，等待结束后回收容器，返回退出码。
	Run(ctx context.Context, cfg RunConfig, logs io.Writer) (exitCode int, err error)
	// CreateVolume 创建构建级共享卷。
	CreateVolume(ctx context.Context, name string) error
	// RemoveVolume 删除共享卷。
	RemoveVolume(ctx context.Context, name string) error
	// CopyOut 把 volume 中匹配 patterns 的文件拷到宿主机 destDir。
	CopyOut(ctx context.Context, volume, workDir, destDir string, patterns []string) error
	// Ping 检测后端是否可用。
	Ping(ctx context.Context) error
}
