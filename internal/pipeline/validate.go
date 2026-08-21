package pipeline

import (
	"fmt"
	"time"

	"minidrone/internal/dag"
)

// Validate 校验流水线定义的合法性：
// 必填字段、阶段名唯一、depends_on 引用存在、DAG 无环、时长字段可解析。
func Validate(p *Pipeline) error {
	if p.Name == "" {
		return fmt.Errorf("流水线缺少 name 字段")
	}
	if len(p.Stages) == 0 {
		return fmt.Errorf("流水线 %q 至少需要一个 stage", p.Name)
	}

	g := dag.New()
	seen := make(map[string]bool, len(p.Stages))
	for i := range p.Stages {
		st := &p.Stages[i]
		if st.Name == "" {
			return fmt.Errorf("第 %d 个 stage 缺少 name 字段", i+1)
		}
		if seen[st.Name] {
			return fmt.Errorf("stage 名 %q 重复", st.Name)
		}
		seen[st.Name] = true
		if len(st.Steps) == 0 {
			return fmt.Errorf("stage %q 至少需要一个 step", st.Name)
		}
		for j := range st.Steps {
			if err := validateStep(st.Name, &st.Steps[j]); err != nil {
				return err
			}
		}
		g.Add(st.Name, st.DependsOn)
	}

	for _, st := range p.Stages {
		for _, dep := range st.DependsOn {
			if dep == st.Name {
				return fmt.Errorf("stage %q 不能依赖自身", st.Name)
			}
			if !seen[dep] {
				return fmt.Errorf("stage %q 依赖了不存在的 stage %q", st.Name, dep)
			}
		}
	}

	if cycle := g.Cycle(); cycle != nil {
		return fmt.Errorf("stage 依赖存在环: %v", cycle)
	}
	return nil
}

func validateStep(stage string, sp *Step) error {
	if sp.Name == "" {
		return fmt.Errorf("stage %q 存在缺少 name 的 step", stage)
	}
	if sp.Image == "" {
		return fmt.Errorf("step %q 缺少 image 字段", sp.Name)
	}
	if len(sp.Commands) == 0 {
		return fmt.Errorf("step %q 缺少 commands 字段", sp.Name)
	}
	if sp.Retries < 0 {
		return fmt.Errorf("step %q 的 retries 不能为负数", sp.Name)
	}
	if sp.Timeout != "" {
		if _, err := time.ParseDuration(sp.Timeout); err != nil {
			return fmt.Errorf("step %q 的 timeout %q 非法: %w", sp.Name, sp.Timeout, err)
		}
	}
	if sp.RetryDelay != "" {
		if _, err := time.ParseDuration(sp.RetryDelay); err != nil {
			return fmt.Errorf("step %q 的 retry_delay %q 非法: %w", sp.Name, sp.RetryDelay, err)
		}
	}
	for _, a := range sp.Artifacts {
		if a == "" {
			return fmt.Errorf("step %q 存在空的 artifacts 路径", sp.Name)
		}
		if len(a) > 0 && (a[0] == '/' || a[0] == '\\') {
			return fmt.Errorf("step %q 的产物路径必须相对工作区: %s", sp.Name, a)
		}
	}
	return nil
}

// Graph 把流水线阶段转换为 DAG。
func Graph(p *Pipeline) *dag.Graph {
	g := dag.New()
	for _, st := range p.Stages {
		g.Add(st.Name, st.DependsOn)
	}
	return g
}
