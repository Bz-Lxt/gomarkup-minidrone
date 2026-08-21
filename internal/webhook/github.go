package webhook

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// GitHub push / pull_request 事件负载（仅提取需要的字段）。
type githubPushEvent struct {
	Ref   string `json:"ref"`
	After string `json:"after"`
	Repo  struct {
		CloneURL string `json:"clone_url"`
		HTMLURL  string `json:"html_url"`
	} `json:"repository"`
	HeadCommit struct {
		Message string `json:"message"`
		Author  struct {
			Name string `json:"name"`
		} `json:"author"`
	} `json:"head_commit"`
	Pusher struct {
		Name string `json:"name"`
	} `json:"pusher"`
}

type githubPREvent struct {
	Action      string `json:"action"`
	PullRequest struct {
		Title string `json:"title"`
		Head  struct {
			Ref  string `json:"ref"`
			SHA  string `json:"sha"`
			Repo struct {
				CloneURL string `json:"clone_url"`
			} `json:"repo"`
		} `json:"head"`
		Base struct {
			Ref string `json:"ref"`
		} `json:"base"`
		User struct {
			Login string `json:"login"`
		} `json:"user"`
	} `json:"pull_request"`
}

// ParseGitHub 解析并校验 GitHub Webhook 请求，返回标准化触发信息。
// 不支持或不关心的事件返回 (nil, nil)。
func ParseGitHub(r *http.Request, secret string) (*TriggerInfo, error) {
	body, err := readBody(r)
	if err != nil {
		return nil, fmt.Errorf("读取请求体失败: %w", err)
	}
	if err := verifyGitHubSignature(secret, body, r.Header.Get("X-Hub-Signature-256")); err != nil {
		return nil, err
	}

	switch r.Header.Get("X-GitHub-Event") {
	case "push":
		var ev githubPushEvent
		if err := json.Unmarshal(body, &ev); err != nil {
			return nil, fmt.Errorf("解析 push 事件失败: %w", err)
		}
		// 删除分支时 after 全零，不触发
		if ev.After == "" || ev.After == "0000000000000000000000000000000000000000" {
			return nil, nil
		}
		repo := ev.Repo.CloneURL
		if repo == "" {
			repo = ev.Repo.HTMLURL
		}
		author := ev.HeadCommit.Author.Name
		if author == "" {
			author = ev.Pusher.Name
		}
		return &TriggerInfo{
			Source:  "github",
			Event:   "push",
			Repo:    repo,
			Branch:  strings.TrimLeft(ev.Ref, "refs/heads/"),
			Commit:  ev.After,
			Message: ev.HeadCommit.Message,
			Author:  author,
		}, nil

	case "pull_request":
		var ev githubPREvent
		if err := json.Unmarshal(body, &ev); err != nil {
			return nil, fmt.Errorf("解析 pull_request 事件失败: %w", err)
		}
		switch ev.Action {
		case "opened", "reopened", "synchronize":
		default:
			return nil, nil
		}
		return &TriggerInfo{
			Source:  "github",
			Event:   "pull_request",
			Repo:    ev.PullRequest.Head.Repo.CloneURL,
			Branch:  ev.PullRequest.Base.Ref,
			Commit:  ev.PullRequest.Head.SHA,
			Message: ev.PullRequest.Title,
			Author:  ev.PullRequest.User.Login,
		}, nil
	}
	return nil, nil
}
