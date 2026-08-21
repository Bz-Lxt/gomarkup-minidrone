// Package server 提供 REST API、SSE 实时事件流与内嵌 WebUI。
package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"minidrone/internal/metrics"
	"minidrone/internal/scheduler"
	"minidrone/internal/store"
	"minidrone/web"
)

// Options 服务配置。
type Options struct {
	Addr         string
	Store        *store.Store
	Scheduler    *scheduler.Scheduler
	GitHubSecret string
	GitLabToken  string
	Metrics      *metrics.Registry
}

// Server 是 HTTP 服务。
type Server struct {
	opts Options
	http *http.Server
}

// New 创建服务并注册路由。
func New(opts Options) *Server {
	s := &Server{opts: opts}
	mux := http.NewServeMux()

	// WebUI
	mux.Handle("GET /", http.FileServerFS(web.FS))

	// REST API
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/metrics", s.handleMetrics)
	mux.HandleFunc("GET /api/pipelines", s.handleListPipelines)
	mux.HandleFunc("POST /api/pipelines", s.handleCreatePipeline)
	mux.HandleFunc("DELETE /api/pipelines/{name}", s.handleDeletePipeline)
	mux.HandleFunc("POST /api/pipelines/{name}/run", s.handleRunPipeline)
	mux.HandleFunc("GET /api/builds", s.handleListBuilds)
	mux.HandleFunc("GET /api/builds/{id}", s.handleGetBuild)
	mux.HandleFunc("GET /api/builds/{id}/logs", s.handleGetLogs)
	mux.HandleFunc("GET /api/builds/{id}/artifacts", s.handleGetArtifacts)
	mux.HandleFunc("POST /api/builds/{id}/cancel", s.handleCancelBuild)
	mux.HandleFunc("GET /api/builds/{id}/events", s.handleEvents)

	// Webhook 触发器
	mux.HandleFunc("POST /api/webhooks/github", s.handleGitHub)
	mux.HandleFunc("POST /api/webhooks/gitlab", s.handleGitLab)

	s.http = &http.Server{
		Addr:              opts.Addr,
		Handler:           logMiddleware(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s
}

// Start 启动服务，ctx 取消时优雅关闭。
func (s *Server) Start(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		slog.Info("HTTP 服务已启动", "addr", s.http.Addr)
		if err := s.http.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return s.http.Shutdown(shCtx)
	}
}

func logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		slog.Debug("http", "method", r.Method, "path", r.URL.Path, "dur", time.Since(start).Round(time.Millisecond))
	})
}
