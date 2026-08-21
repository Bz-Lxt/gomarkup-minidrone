package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

// Docker 是基于 Docker 官方 Go SDK 的容器执行引擎。
type Docker struct {
	cli *client.Client
}

// NewDocker 创建客户端并自动协商 API 版本。
// 优先使用 DOCKER_HOST 环境变量；未设置时按常见路径自动探测本地 socket，
// 兼容 Linux 默认路径、macOS Docker Desktop、OrbStack、Colima。
func NewDocker() (*Docker, error) {
	opts := []client.Opt{client.WithAPIVersionNegotiation()}
	if os.Getenv("DOCKER_HOST") != "" {
		opts = append(opts, client.FromEnv)
	} else if host := detectDockerHost(); host != "" {
		opts = append(opts, client.WithHost(host))
	} else {
		opts = append(opts, client.FromEnv)
	}
	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("创建 Docker 客户端失败: %w", err)
	}
	return &Docker{cli: cli}, nil
}

// detectDockerHost 在常见位置探测 Docker socket。
func detectDockerHost() string {
	home, _ := os.UserHomeDir()
	candidates := []string{
		"/var/run/docker.sock",
		filepath.Join(home, ".docker/run/docker.sock"),   // Docker Desktop (macOS)
		filepath.Join(home, ".orbstack/run/docker.sock"), // OrbStack
		filepath.Join(home, ".colima/default/docker.sock"),
	}
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && st.Mode()&os.ModeSocket != 0 {
			return "unix://" + p
		}
	}
	return ""
}

// Ping 检测 Docker 守护进程连通性。
func (d *Docker) Ping(ctx context.Context) error {
	_, err := d.cli.Ping(ctx)
	return err
}

// CreateVolume 创建命名卷。
func (d *Docker) CreateVolume(ctx context.Context, name string) error {
	_, err := d.cli.VolumeCreate(ctx, volume.CreateOptions{
		Name:   name,
		Labels: map[string]string{"minidrone": "true"},
	})
	return err
}

// RemoveVolume 强制删除命名卷。
func (d *Docker) RemoveVolume(ctx context.Context, name string) error {
	return d.cli.VolumeRemove(ctx, name, true)
}

// Run 实现 Executor 接口：拉镜像 -> 建容器 -> 启动 -> 流式日志 -> 等待退出 -> 清理。
func (d *Docker) Run(ctx context.Context, cfg RunConfig, logs io.Writer) (int, error) {
	if err := d.ensureImage(ctx, cfg.Image, cfg.Pull, logs); err != nil {
		return -1, err
	}

	// set -e 保证任一命令失败即整体失败，语义与主流 CI 一致
	script := "set -e\n" + strings.Join(cfg.Commands, "\n")
	resp, err := d.cli.ContainerCreate(ctx, &container.Config{
		Image:        cfg.Image,
		Cmd:          []string{"sh", "-c", script},
		Env:          cfg.Env,
		WorkingDir:   cfg.WorkDir,
		Labels:       cfg.Labels,
		AttachStdout: true,
		AttachStderr: true,
	}, &container.HostConfig{
		Mounts: []mount.Mount{{
			Type:   mount.TypeVolume,
			Source: cfg.Volume,
			Target: cfg.MountPath,
		}},
	}, nil, nil, cfg.Name)
	if err != nil {
		return -1, fmt.Errorf("创建容器失败: %w", err)
	}
	containerID := resp.ID

	// 无论成功失败，最终强制回收容器
	defer func() {
		rmCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = d.cli.ContainerRemove(rmCtx, containerID, container.RemoveOptions{Force: true})
	}()

	if err := d.cli.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		return -1, fmt.Errorf("启动容器失败: %w", err)
	}

	// 先挂日志流再等待退出；即使容器秒退，未删除前日志仍可完整读取
	logsDone := make(chan struct{})
	if rc, err := d.cli.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
	}); err == nil {
		go func() {
			defer close(logsDone)
			defer rc.Close()
			// 非 TTY 容器的日志流是多路复用格式，需要 stdcopy 解复用
			_, _ = stdcopy.StdCopy(logs, logs, rc)
		}()
	} else {
		close(logsDone)
		fmt.Fprintf(logs, "[minidrone] 警告: 无法挂载日志流: %v\n", err)
	}

	statusCh, errCh := d.cli.ContainerWait(ctx, containerID, container.WaitConditionNotRunning)
	select {
	case <-ctx.Done():
		// 构建被取消：杀掉容器，等待日志流收尾
		killCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = d.cli.ContainerKill(killCtx, containerID, "SIGKILL")
		<-logsDone
		return -1, ctx.Err()
	case err := <-errCh:
		if err != nil {
			<-logsDone
			return -1, fmt.Errorf("等待容器退出失败: %w", err)
		}
	case st := <-statusCh:
		<-logsDone
		if st.Error != nil {
			// 容器等待返回错误信息（如连接中断）。此时退出码可能为 0，
			// 用 -1 表示无法获取有效退出码，避免调度器误判为成功。
			code := int(st.StatusCode)
			if code == 0 {
				code = -1
			}
			return code, fmt.Errorf("容器运行错误: %s", st.Error.Message)
		}
		return int(st.StatusCode), nil
	}
	<-logsDone
	return -1, fmt.Errorf("未知的容器等待结果")
}

// ensureImage 确保镜像在本地可用，缺失或强制拉取时执行 ImagePull 并输出进度。
func (d *Docker) ensureImage(ctx context.Context, ref string, alwaysPull bool, logs io.Writer) error {
	if !alwaysPull {
		if _, _, err := d.cli.ImageInspectWithRaw(ctx, ref); err == nil {
			return nil
		}
	}

	fmt.Fprintf(logs, "[minidrone] 拉取镜像 %s ...\n", ref)
	rc, err := d.cli.ImagePull(ctx, ref, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("拉取镜像 %s 失败: %w", ref, err)
	}
	defer rc.Close()

	// 拉取进度是 JSON 流，按层去重后输出关键状态，避免刷屏
	type progressMsg struct {
		ID      string `json:"id"`
		Status  string `json:"status"`
		Message string `json:"error"`
	}
	last := make(map[string]string)
	dec := json.NewDecoder(rc)
	for {
		var msg progressMsg
		if err := dec.Decode(&msg); err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("读取镜像拉取进度失败: %w", err)
		}
		if msg.Message != "" {
			return fmt.Errorf("拉取镜像失败: %s", msg.Message)
		}
		if msg.ID != "" && msg.Status != last[msg.ID] {
			last[msg.ID] = msg.Status
			switch msg.Status {
			case "Pulling fs layer", "Downloading", "Extracting":
				// 高频中间状态不输出
			default:
				fmt.Fprintf(logs, "[minidrone] pull %s: %s\n", msg.ID, msg.Status)
			}
		} else if msg.ID == "" && msg.Status != "" {
			fmt.Fprintf(logs, "[minidrone] pull: %s\n", msg.Status)
		}
	}
	return nil
}
