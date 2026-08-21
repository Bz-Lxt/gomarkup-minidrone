package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"minidrone/internal/pipeline"
	"minidrone/internal/store"
	"minidrone/internal/webhook"
)

// --- 工具 ---

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// --- 基础 ---

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if s.opts.Metrics == nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "disabled"})
		return
	}
	writeJSON(w, http.StatusOK, s.opts.Metrics.Snapshot())
}

// --- 流水线 ---

func (s *Server) handleListPipelines(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.opts.Store.ListPipelines())
}

// handleCreatePipeline 以 YAML 请求体注册一条流水线。
func (s *Server) handleCreatePipeline(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "读取请求体失败")
		return
	}
	p, err := pipeline.Parse(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.opts.Store.PutPipeline(p)
	slog.Info("流水线已注册", "name", p.Name)
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) handleDeletePipeline(w http.ResponseWriter, r *http.Request) {
	if !s.opts.Store.DeletePipeline(r.PathValue("name")) {
		writeError(w, http.StatusNotFound, "流水线不存在")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleRunPipeline 手动触发一次构建，可选 JSON 体指定 repo/branch/commit。
func (s *Server) handleRunPipeline(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	p, ok := s.opts.Store.GetPipeline(name)
	if !ok {
		writeError(w, http.StatusNotFound, "流水线不存在")
		return
	}
	var req struct {
		Repo   string `json:"repo"`
		Branch string `json:"branch"`
		Commit string `json:"commit"`
	}
	if r.Body != nil && r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "请求体 JSON 解析失败")
			return
		}
	}
	b := s.opts.Store.CreateBuild(p, "manual", req.Repo, req.Branch, req.Commit, "", "", true)
	s.opts.Scheduler.Submit(b)
	writeJSON(w, http.StatusAccepted, b.Snapshot())
}

// --- 构建 ---

func (s *Server) handleListBuilds(w http.ResponseWriter, r *http.Request) {
	builds := s.opts.Store.ListBuilds(100)
	out := make([]*store.Build, 0, len(builds))
	for _, b := range builds {
		out = append(out, b.Snapshot())
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetBuild(w http.ResponseWriter, r *http.Request) {
	b, ok := s.opts.Store.GetBuild(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "构建不存在")
		return
	}
	writeJSON(w, http.StatusOK, b.Snapshot())
}

func (s *Server) handleGetLogs(w http.ResponseWriter, r *http.Request) {
	b, ok := s.opts.Store.GetBuild(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "构建不存在")
		return
	}
	stage := r.URL.Query().Get("stage")
	step := r.URL.Query().Get("step")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(s.opts.Store.Log(b.ID, stage, step)))
}

func (s *Server) handleGetArtifacts(w http.ResponseWriter, r *http.Request) {
	b, ok := s.opts.Store.GetBuild(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "构建不存在")
		return
	}
	snap := b.Snapshot()
	if snap.Artifacts == nil {
		writeJSON(w, http.StatusOK, []store.Artifact{})
		return
	}
	writeJSON(w, http.StatusOK, snap.Artifacts)
}

func (s *Server) handleCancelBuild(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.opts.Scheduler.Cancel(id) {
		writeError(w, http.StatusConflict, "构建不存在或已结束")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "canceling"})
}

// --- SSE 实时事件 ---

// handleEvents 以 Server-Sent Events 推送构建的状态变化与日志流。
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	b, ok := s.opts.Store.GetBuild(id)
	if !ok {
		writeError(w, http.StatusNotFound, "构建不存在")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "不支持流式响应")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	ch := s.opts.Store.Broker().Subscribe(id)
	defer s.opts.Store.Broker().Unsubscribe(id, ch)

	// 先推送一次当前快照，让晚接入的客户端立即获得全量状态
	snap, _ := json.Marshal(map[string]any{
		"type":  "snapshot",
		"build": b.Snapshot(),
	})
	fmt.Fprintf(w, "data: %s\n\n", snap)
	flusher.Flush()

	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case ev, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			if ev.Type == store.EventDone {
				return // 构建结束，关闭流
			}
		}
	}
}

// --- Webhook ---

func (s *Server) handleGitHub(w http.ResponseWriter, r *http.Request) {
	ev, err := webhook.ParseGitHub(r, s.opts.GitHubSecret)
	if err != nil {
		slog.Warn("GitHub Webhook 校验失败", "err", err)
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	s.dispatch(w, ev)
}

func (s *Server) handleGitLab(w http.ResponseWriter, r *http.Request) {
	ev, err := webhook.ParseGitLab(r, s.opts.GitLabToken)
	if err != nil {
		slog.Warn("GitLab Webhook 校验失败", "err", err)
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	s.dispatch(w, ev)
}

// dispatch 按触发信息匹配流水线并提交构建。
func (s *Server) dispatch(w http.ResponseWriter, ev *webhook.TriggerInfo) {
	if ev == nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored"})
		return
	}
	slog.Info("收到 Webhook 事件",
		"source", ev.Source, "event", ev.Event,
		"repo", ev.Repo, "branch", ev.Branch, "commit", short(ev.Commit))

	var triggered []string
	for _, p := range s.opts.Store.ListPipelines() {
		if !webhook.MatchPipeline(p, ev) {
			continue
		}
		trigger := fmt.Sprintf("%s-%s", ev.Source, ev.Event)
		b := s.opts.Store.CreateBuild(p, trigger, ev.Repo, ev.Branch, ev.Commit, ev.Message, ev.Author, true)
		s.opts.Scheduler.Submit(b)
		triggered = append(triggered, b.ID)
		slog.Info("Webhook 触发构建", "build", b.ID, "pipeline", p.Name)
	}

	if len(triggered) == 0 {
		writeJSON(w, http.StatusOK, map[string]string{"status": "no matching pipeline"})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "triggered", "builds": triggered})
}

func short(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}
