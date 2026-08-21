package pipeline

import (
	"strings"
	"testing"
)

const validYAML = `
name: demo
stages:
  - name: lint
    steps:
      - name: check
        image: alpine
        commands: ["echo ok"]
  - name: test
    depends_on: [lint]
    steps:
      - name: unit
        image: alpine
        commands: ["echo test"]
  - name: security
    depends_on: [lint]
    steps:
      - name: scan
        image: alpine
        commands: ["echo scan"]
  - name: deploy
    depends_on: [test, security]
    steps:
      - name: publish
        image: alpine
        commands: ["echo deploy"]
`

func TestParseValidPipeline(t *testing.T) {
	p, err := Parse([]byte(validYAML))
	if err != nil {
		t.Fatalf("合法流水线解析失败: %v", err)
	}
	if p.Name != "demo" || len(p.Stages) != 4 {
		t.Fatalf("解析结果不符: %+v", p)
	}
	if got := p.Stages[3].DependsOn; len(got) != 2 || got[0] != "test" || got[1] != "security" {
		t.Fatalf("依赖解析错误: %v", got)
	}
}

func TestValidateDetectsCycle(t *testing.T) {
	yaml := `
name: cyclic
stages:
  - name: a
    depends_on: [c]
    steps: [{name: s1, image: alpine, commands: ["true"]}]
  - name: b
    depends_on: [a]
    steps: [{name: s2, image: alpine, commands: ["true"]}]
  - name: c
    depends_on: [b]
    steps: [{name: s3, image: alpine, commands: ["true"]}]
`
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "环") {
		t.Fatalf("应检测出循环依赖, got: %v", err)
	}
}

func TestValidateDetectsUnknownDependency(t *testing.T) {
	yaml := `
name: bad-ref
stages:
  - name: a
    depends_on: [ghost]
    steps: [{name: s1, image: alpine, commands: ["true"]}]
`
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("应检测出悬空依赖, got: %v", err)
	}
}

func TestValidateRejectsBadTimeout(t *testing.T) {
	yaml := `
name: x
stages:
  - name: a
    steps:
      - name: s
        image: alpine
        timeout: not-a-duration
        commands: ["true"]
`
	if _, err := Parse([]byte(yaml)); err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("非法 timeout 应失败, got %v", err)
	}
}

func TestValidateRejectsMissingFields(t *testing.T) {
	cases := []string{
		`stages: [{name: a, steps: [{name: s, image: alpine, commands: ["true"]}]}]`, // 缺 name
		`name: x
stages: []`, // 无 stage
		`name: x
stages: [{name: a, steps: []}]`, // 无 step
		`name: x
stages: [{name: a, steps: [{name: s, commands: ["true"]}]}]`, // 缺 image
		`name: x
stages: [{name: a, steps: [{name: s, image: alpine}]}]`, // 缺 commands
	}
	for i, y := range cases {
		if _, err := Parse([]byte(y)); err == nil {
			t.Errorf("用例 %d 应校验失败", i)
		}
	}
}
