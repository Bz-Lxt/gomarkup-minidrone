// Package notify 在构建终态时向配置的 HTTP 端点投递 JSON 通知。
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"minidrone/internal/store"
)

// Payload 是发送给外部系统的构建摘要。
type Payload struct {
	BuildID    string      `json:"build_id"`
	Pipeline   string      `json:"pipeline"`
	Number     int         `json:"number"`
	State      store.State `json:"state"`
	Trigger    string      `json:"trigger"`
	Repo       string      `json:"repo,omitempty"`
	Branch     string      `json:"branch,omitempty"`
	Commit     string      `json:"commit,omitempty"`
	Author     string      `json:"author,omitempty"`
	Error      string      `json:"error,omitempty"`
	DurationMS int64       `json:"duration_ms"`
}

// FromBuild 从构建快照构造通知负载。
func FromBuild(b *store.Build) Payload {
	var dur int64
	if !b.StartedAt.IsZero() && !b.EndedAt.IsZero() {
		dur = b.EndedAt.Sub(b.StartedAt).Milliseconds()
	}
	return Payload{
		BuildID:    b.ID,
		Pipeline:   b.Pipeline,
		Number:     b.Number,
		State:      b.State,
		Trigger:    b.Trigger,
		Repo:       b.Repo,
		Branch:     b.Branch,
		Commit:     b.Commit,
		Author:     b.Author,
		Error:      b.Error,
		DurationMS: dur,
	}
}

// Notifier 以有限并发向多个 URL 投递通知。
type Notifier struct {
	client  *http.Client
	timeout time.Duration
}

// New 创建通知器，timeout 为单次请求超时。
func New(timeout time.Duration) *Notifier {
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	return &Notifier{
		client:  &http.Client{Timeout: timeout},
		timeout: timeout,
	}
}

// Send 异步向 urls 投递 payload；任一失败只记日志，不影响构建。
func (n *Notifier) Send(ctx context.Context, urls []string, payload Payload) {
	if len(urls) == 0 {
		return
	}
	body, err := json.Marshal(payload)
	if err != nil {
		slog.Warn("序列化通知失败", "err", err)
		return
	}
	var wg sync.WaitGroup
	for _, u := range urls {
		if u == "" {
			continue
		}
		wg.Add(1)
		go func(url string) {
			defer wg.Done()
			n.post(ctx, url, body)
		}(u)
	}
	wg.Wait()
}

func (n *Notifier) post(ctx context.Context, url string, body []byte) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		slog.Warn("构造通知请求失败", "url", url, "err", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "minidrone-notify/1.0")
	resp, err := n.client.Do(req)
	if err != nil {
		slog.Warn("投递构建通知失败", "url", url, "err", err)
		return
	}
	_ = resp.Body.Close()
	if resp.StatusCode >= 300 {
		slog.Warn("通知端点返回非 2xx", "url", url, "status", resp.StatusCode)
	}
}
