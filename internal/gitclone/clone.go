// Package gitclone 生成隐式 clone 阶段的步骤定义。
package gitclone

import (
	"fmt"

	"minidrone/internal/pipeline"
)

const (
	// Image 是浅克隆所用镜像。
	Image = "alpine/git:latest"
	// StepName 是隐式步骤名。
	StepName = "git-clone"
	// StageName 是隐式阶段名。
	StageName = "clone"
)

// Step 根据仓库信息生成 git clone 步骤。branch / commit 为空时跳过对应参数。
func Step(repo, branch, commit string) *pipeline.Step {
	var cmds []string
	if branch != "" {
		cmds = append(cmds, fmt.Sprintf("git clone --depth 50 --branch %q %q .", branch, repo))
	} else {
		cmds = append(cmds, fmt.Sprintf("git clone --depth 50 %q .", repo))
	}
	if commit != "" {
		cmds = append(cmds, fmt.Sprintf("git checkout %q || true", commit))
	}
	return &pipeline.Step{
		Name:     StepName,
		Image:    Image,
		Commands: cmds,
		Env:      map[string]string{"GIT_TERMINAL_PROMPT": "0"},
	}
}
