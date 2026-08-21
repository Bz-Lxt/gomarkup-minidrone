package webhook_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"minidrone/internal/webhook"
)

func TestParseGitHubPreservesFeatureBranch(t *testing.T) {
	body := `{
		"ref":"refs/heads/feature/login",
		"after":"0123456789abcdef0123456789abcdef01234567",
		"repository":{"clone_url":"https://github.com/acme/widget.git"},
		"head_commit":{"message":"add login","author":{"name":"dev"}}
	}`
	req := httptest.NewRequest("POST", "/webhooks/github", strings.NewReader(body))
	req.Header.Set("X-GitHub-Event", "push")

	event, err := webhook.ParseGitHub(req, "")
	if err != nil {
		t.Fatalf("ParseGitHub returned error: %v", err)
	}
	if event == nil {
		t.Fatal("ParseGitHub ignored a valid push event")
	}
	if event.Branch != "feature/login" {
		t.Fatalf("push branch = %q, want %q", event.Branch, "feature/login")
	}
}
