package gitclone

import (
	"strings"
	"testing"
)

func TestStepWithBranchAndCommit(t *testing.T) {
	sp := Step("https://github.com/a/b.git", "main", "abc123")
	if sp.Name != StepName || sp.Image != Image {
		t.Fatalf("步骤元数据不符: %+v", sp)
	}
	joined := strings.Join(sp.Commands, "\n")
	if !strings.Contains(joined, "--branch \"main\"") {
		t.Fatalf("缺少 branch 参数: %s", joined)
	}
	if !strings.Contains(joined, "git checkout \"abc123\"") {
		t.Fatalf("缺少 checkout: %s", joined)
	}
}

func TestStepWithoutBranch(t *testing.T) {
	sp := Step("https://example.com/r.git", "", "")
	if strings.Contains(sp.Commands[0], "--branch") {
		t.Fatalf("无分支时不应带 --branch: %s", sp.Commands[0])
	}
}
