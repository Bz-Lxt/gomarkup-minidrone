package executor

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/docker/docker/client"
)

func TestCopyOutRemovesContainerWhenStartFails(t *testing.T) {
	var removed atomic.Bool
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		status := http.StatusOK
		body := ""
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/images/"):
			body = `{"Id":"sha256:alpine"}`
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/create"):
			body = `{"Id":"artifact-copy-id","Warnings":[]}`
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/artifact-copy-id/start"):
			status = http.StatusInternalServerError
			body = `{"message":"runtime refused to start container"}`
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/containers/artifact-copy-id"):
			if r.URL.Query().Get("force") != "1" {
				t.Errorf("删除临时容器时未强制回收: %s", r.URL.RawQuery)
			}
			removed.Store(true)
			status = http.StatusNoContent
		default:
			return nil, fmt.Errorf("未预期的 Docker API 请求: %s %s", r.Method, r.URL.Path)
		}
		return &http.Response{
			StatusCode: status,
			Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    r,
		}, nil
	})}

	cli, err := client.NewClientWithOpts(
		client.WithHost("http://docker.test"),
		client.WithVersion("1.47"),
		client.WithHTTPClient(httpClient),
	)
	if err != nil {
		t.Fatalf("创建测试 Docker 客户端失败: %v", err)
	}
	defer cli.Close()

	d := &Docker{cli: cli}
	err = d.CopyOut(context.Background(), "build-volume", "/workspace", t.TempDir(), []string{"dist/*"})
	if err == nil || !strings.Contains(err.Error(), "启动产物采集容器失败") {
		t.Fatalf("期望返回启动失败，实际为: %v", err)
	}
	if !removed.Load() {
		t.Fatal("产物采集容器创建后启动失败，但未被回收")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }
