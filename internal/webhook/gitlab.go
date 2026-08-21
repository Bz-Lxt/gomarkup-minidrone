package webhook

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// GitLab Push Hook / Merge Request Hook 负载（仅提取需要的字段）。
type gitlabPushEvent struct {
	ObjectKind  string `json:"object_kind"`
	Ref         string `json:"ref"`
	CheckoutSHA string `json:"checkout_sha"`
	UserName    string `json:"user_username"`
	Project     struct {
		GitHTTPURL string `json:"git_http_url"`
		WebURL     string `json:"web_url"`
	} `json:"project"`
	Commits []struct {
		Message string `json:"message"`
	} `json:"commits"`
}

type gitlabMREvent struct {
	ObjectKind string `json:"object_kind"`
	User       struct {
		Username string `json:"username"`
	} `json:"user"`
	Project struct {
		GitHTTPURL string `json:"git_http_url"`
		WebURL     string `json:"web_url"`
	} `json:"project"`
	ObjectAttributes struct {
		Action       string `json:"action"`
		SourceBranch string `json:"source_branch"`
		TargetBranch string `json:"target_branch"`
		Title        string `json:"title"`
		LastCommit   struct {
			ID string `json:"id"`
		} `json:"last_commit"`
	} `json:"object_attributes"`
}

// ParseGitLab 解析并校验 GitLab Webhook 请求，返回标准化触发信息。
// 不支持或不关心的事件返回 (nil, nil)。
func ParseGitLab(r *http.Request, secret string) (*TriggerInfo, error) {
	body, err := readBody(r)
	if err != nil {
		return nil, fmt.Errorf("读取请求体失败: %w", err)
	}
	if err := verifyGitLabToken(secret, r.Header.Get("X-Gitlab-Token")); err != nil {
		return nil, err
	}

	switch r.Header.Get("X-Gitlab-Event") {
	case "Push Hook":
		var ev gitlabPushEvent
		if err := json.Unmarshal(body, &ev); err != nil {
			return nil, fmt.Errorf("解析 Push Hook 失败: %w", err)
		}
		if ev.CheckoutSHA == "" {
			return nil, nil // 分支删除等无 checkout 的场景
		}
		repo := ev.Project.GitHTTPURL
		if repo == "" {
			repo = ev.Project.WebURL
		}
		msg := ""
		if len(ev.Commits) > 0 {
			msg = ev.Commits[len(ev.Commits)-1].Message
		}
		return &TriggerInfo{
			Source:  "gitlab",
			Event:   "push",
			Repo:    repo,
			Branch:  strings.TrimPrefix(ev.Ref, "refs/heads/"),
			Commit:  ev.CheckoutSHA,
			Message: msg,
			Author:  ev.UserName,
		}, nil

	case "Merge Request Hook":
		var ev gitlabMREvent
		if err := json.Unmarshal(body, &ev); err != nil {
			return nil, fmt.Errorf("解析 Merge Request Hook 失败: %w", err)
		}
		switch ev.ObjectAttributes.Action {
		case "open", "reopen", "update":
		default:
			return nil, nil
		}
		repo := ev.Project.GitHTTPURL
		if repo == "" {
			repo = ev.Project.WebURL
		}
		return &TriggerInfo{
			Source:  "gitlab",
			Event:   "merge_request",
			Repo:    repo,
			Branch:  ev.ObjectAttributes.TargetBranch,
			Commit:  ev.ObjectAttributes.LastCommit.ID,
			Message: ev.ObjectAttributes.Title,
			Author:  ev.User.Username,
		}, nil
	}
	return nil, nil
}
