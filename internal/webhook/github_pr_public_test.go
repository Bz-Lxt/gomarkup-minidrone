package webhook_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"minidrone/internal/pipeline"
	"minidrone/internal/webhook"
)

func TestGitHubPullRequestMatchesTargetBranch(t *testing.T) {
	body := `{
		"action":"opened",
		"pull_request":{
			"title":"ship feature",
			"head":{"ref":"feature/payments","sha":"abc123","repo":{"clone_url":"https://github.com/acme/shop.git"}},
			"base":{"ref":"main"},
			"user":{"login":"alice"}
		}
	}`
	req := httptest.NewRequest("POST", "/api/webhooks/github", strings.NewReader(body))
	req.Header.Set("X-GitHub-Event", "pull_request")

	event, err := webhook.ParseGitHub(req, "")
	if err != nil {
		t.Fatalf("ParseGitHub() error = %v", err)
	}
	if event == nil {
		t.Fatal("ParseGitHub() ignored an opened pull request")
	}

	p := &pipeline.Pipeline{Trigger: pipeline.Trigger{
		Repo:     "git@github.com:acme/shop.git",
		Events:   []string{"pull_request"},
		Branches: []string{"main"},
	}}
	if !webhook.MatchPipeline(p, event) {
		t.Fatalf("opened PR into main did not match main trigger (normalized branch = %q)", event.Branch)
	}
}
