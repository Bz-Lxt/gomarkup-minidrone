package executor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
)

// CopyOut 启动一次性 alpine 容器，把共享卷中匹配 patterns 的文件拷到宿主机 destDir。
func (d *Docker) CopyOut(ctx context.Context, volume, workDir, destDir string, patterns []string) error {
	if len(patterns) == 0 {
		return nil
	}
	abs, err := filepath.Abs(destDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return err
	}
	if workDir == "" {
		workDir = "/workspace"
	}
	if err := d.ensureImage(ctx, "alpine:3.20", false, ioDiscard{}); err != nil {
		return err
	}

	var b strings.Builder
	b.WriteString("set +e\n")
	for _, p := range patterns {
		fmt.Fprintf(&b, `for f in %s; do [ -e "$f" ] && cp -a "$f" /minidrone-out/; done`+"\n", p)
	}
	b.WriteString("exit 0\n")

	resp, err := d.cli.ContainerCreate(ctx, &container.Config{
		Image:      "alpine:3.20",
		Cmd:        []string{"sh", "-c", b.String()},
		WorkingDir: workDir,
		Labels:     map[string]string{"minidrone": "true", "role": "artifact-copy"},
	}, &container.HostConfig{
		Mounts: []mount.Mount{
			{Type: mount.TypeVolume, Source: volume, Target: workDir},
			{Type: mount.TypeBind, Source: abs, Target: "/minidrone-out"},
		},
	}, nil, nil, "")
	if err != nil {
		return fmt.Errorf("创建产物采集容器失败: %w", err)
	}

	if err := d.cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("启动产物采集容器失败: %w", err)
	}
	defer func() {
		_ = d.cli.ContainerRemove(context.Background(), resp.ID, container.RemoveOptions{Force: true})
	}()
	statusCh, errCh := d.cli.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("等待产物采集容器失败: %w", err)
		}
	case st := <-statusCh:
		if st.Error != nil {
			return fmt.Errorf("产物采集容器错误: %s", st.Error.Message)
		}
		if st.StatusCode != 0 {
			return fmt.Errorf("产物采集退出码 %d", st.StatusCode)
		}
	}
	return nil
}

// ioDiscard 实现 io.Writer，用于静默拉取镜像进度。
type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }
