package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Parse 从字节流解析流水线定义，并完成校验。
func Parse(data []byte) (*Pipeline, error) {
	var p Pipeline
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true) // 拒绝未知字段，尽早暴露配置笔误
	if err := dec.Decode(&p); err != nil {
		return nil, fmt.Errorf("解析 YAML 失败: %w", err)
	}
	if err := Validate(&p); err != nil {
		return nil, err
	}
	return &p, nil
}

// ParseFile 从单个 YAML 文件加载流水线。
func ParseFile(path string) (*Pipeline, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	p, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return p, nil
}

// LoadDir 加载目录下所有 .yaml/.yml 流水线文件，返回成功解析的流水线列表。
// 单个文件解析失败不会中断整体加载，错误会聚合后返回。
func LoadDir(dir string) ([]*Pipeline, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var (
		pipelines []*Pipeline
		errs      []string
	)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := filepath.Ext(e.Name())
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		p, err := ParseFile(filepath.Join(dir, e.Name()))
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		pipelines = append(pipelines, p)
	}
	if len(errs) > 0 {
		return pipelines, fmt.Errorf("部分流水线加载失败:\n%s", strings.Join(errs, "\n"))
	}
	return pipelines, nil
}
