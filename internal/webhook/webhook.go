// Package webhook 接收并校验 GitHub / GitLab 的 Webhook 事件，匹配流水线并触发构建。
package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"

	"minidrone/internal/pipeline"
)

// maxPayload 限制 Webhook 请求体大小，防止滥用。
const maxPayload = 1 << 20

// TriggerInfo 是从 Webhook 事件中提取的标准化触发信息。
type TriggerInfo struct {
	Source  string // github / gitlab
	Event   string // push / pull_request / merge_request
	Repo    string // 仓库克隆地址（HTTP）
	Branch  string // 目标分支（PR/MR 为目标分支，Push 为推送分支）
	Commit  string
	Message string
	Author  string
}

// readBody 读取并限制请求体大小。
func readBody(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	return io.ReadAll(http.MaxBytesReader(nil, r.Body, maxPayload))
}

// verifyGitHubSignature 校验 X-Hub-Signature-256（HMAC-SHA256）。
// secret 为空时跳过校验（仅限内网可信环境）。
func verifyGitHubSignature(secret string, body []byte, header string) error {
	if secret == "" {
		return nil
	}
	if !strings.HasPrefix(header, "sha256=") {
		return fmt.Errorf("缺少 X-Hub-Signature-256 签名头")
	}
	got, err := hex.DecodeString(strings.TrimPrefix(header, "sha256="))
	if err != nil {
		return fmt.Errorf("签名格式非法")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	if !hmac.Equal(got, mac.Sum(nil)) {
		return fmt.Errorf("签名校验失败")
	}
	return nil
}

// verifyGitLabToken 校验 X-Gitlab-Token（明文比对）。
func verifyGitLabToken(secret, header string) error {
	if secret == "" {
		return nil
	}
	if subtleCompare(header, secret) {
		return nil
	}
	return fmt.Errorf("X-Gitlab-Token 校验失败")
}

func subtleCompare(a, b string) bool {
	return hmac.Equal([]byte(a), []byte(b))
}

// MatchPipeline 判断流水线是否应被该事件触发。
func MatchPipeline(p *pipeline.Pipeline, ev *TriggerInfo) bool {
	t := p.Trigger
	if t.Repo == "" {
		return false
	}
	if !repoEqual(t.Repo, ev.Repo) {
		return false
	}
	if len(t.Events) > 0 {
		matched := false
		for _, e := range t.Events {
			if eventEqual(e, ev.Event) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if len(t.Branches) > 0 {
		matched := false
		for _, br := range t.Branches {
			if br == ev.Branch {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// eventEqual 归一化 PR/MR 等同义事件名后比较。
func eventEqual(configured, actual string) bool {
	norm := func(s string) string {
		switch strings.ToLower(strings.TrimSpace(s)) {
		case "pr", "pull_request", "pull-request", "merge_request", "merge-request", "mr":
			return "pr"
		default:
			return strings.ToLower(strings.TrimSpace(s))
		}
	}
	return norm(configured) == norm(actual)
}

// repoEqual 归一化各种仓库地址写法后比较 host/path。
// 支持 https://host/path(.git)、ssh://git@host/path、git@host:path 等形式。
func repoEqual(a, b string) bool {
	return normalizeRepo(a) == normalizeRepo(b)
}

func normalizeRepo(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimSuffix(s, ".git")
	s = strings.TrimSuffix(s, "/")
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	// git@host:path -> host/path
	if i := strings.Index(s, "@"); i >= 0 && !strings.Contains(s[:i], "/") {
		s = s[i+1:]
	}
	s = strings.Replace(s, ":", "/", 1)
	return s
}
