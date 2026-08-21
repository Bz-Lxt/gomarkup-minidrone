package webhook

import "testing"

func TestNormalizeRepo(t *testing.T) {
	pairs := [][2]string{
		{"https://github.com/User/Repo.git", "git@github.com:user/repo"},
		{"https://github.com/user/repo", "http://github.com/user/repo.git"},
		{"ssh://git@gitlab.com/group/proj.git", "https://gitlab.com/group/proj"},
		{"git@gitlab.com:group/proj", "gitlab.com/group/proj/"},
	}
	for _, p := range pairs {
		if !repoEqual(p[0], p[1]) {
			t.Errorf("应判定为同一仓库: %q vs %q", p[0], p[1])
		}
	}
	if repoEqual("https://github.com/a/b", "https://github.com/a/c") {
		t.Error("不同仓库不应匹配")
	}
}

func TestEventEqual(t *testing.T) {
	for _, alias := range []string{"pr", "pull_request", "mr", "merge_request", "PR"} {
		if !eventEqual(alias, "merge_request") {
			t.Errorf("%q 应与 merge_request 等价", alias)
		}
	}
	if eventEqual("push", "pull_request") {
		t.Error("push 不应匹配 pull_request")
	}
}
