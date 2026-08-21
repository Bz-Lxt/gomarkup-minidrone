// MiniDrone —— 自研轻量级 CI/CD 自动化流水线系统。
//
// 功能：YAML 定义多阶段流水线、Docker 容器化隔离执行、DAG 调度串并行编排、
// GitHub/GitLab Webhook 自动触发、WebUI 实时状态与日志。
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"minidrone/internal/artifact"
	"minidrone/internal/config"
	"minidrone/internal/executor"
	"minidrone/internal/metrics"
	"minidrone/internal/notify"
	"minidrone/internal/pipeline"
	"minidrone/internal/scheduler"
	"minidrone/internal/server"
	"minidrone/internal/store"
)

func main() {
	cfg := config.Load()

	level := slog.LevelInfo
	if cfg.Verbose {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	exec, err := executor.NewDocker()
	if err != nil {
		slog.Error("Docker 客户端初始化失败", "err", err)
		os.Exit(1)
	}
	pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
	if err := exec.Ping(pingCtx); err != nil {
		slog.Warn("Docker 守护进程暂不可用，构建执行将失败", "err", err)
	} else {
		slog.Info("Docker 守护进程连接正常")
	}
	pingCancel()

	st := store.New()
	reg := metrics.New()
	sch := scheduler.New(exec, st, scheduler.Options{
		Workers:           cfg.Workers,
		MaxParallelStages: cfg.MaxParallelStages,
		Metrics:           reg,
		Notifier:          notify.New(8 * time.Second),
		Artifacts:         artifact.New(exec, cfg.ArtifactDir),
	})
	sch.Start(ctx)

	pipelines, err := pipeline.LoadDir(cfg.PipelinesDir)
	for _, p := range pipelines {
		st.PutPipeline(p)
		slog.Info("已加载流水线", "name", p.Name, "stages", len(p.Stages))
	}
	if err != nil {
		slog.Warn("流水线目录加载存在错误", "err", err)
	}

	srv := server.New(server.Options{
		Addr:         cfg.Addr,
		Store:        st,
		Scheduler:    sch,
		GitHubSecret: cfg.GitHubSecret,
		GitLabToken:  cfg.GitLabToken,
		Metrics:      reg,
	})

	if err := srv.Start(ctx); err != nil {
		slog.Error("HTTP 服务异常退出", "err", err)
	}
	sch.Stop()
	slog.Info("MiniDrone 已退出")
}
