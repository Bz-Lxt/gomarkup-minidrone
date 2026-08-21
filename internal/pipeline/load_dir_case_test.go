package pipeline

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDirAcceptsUppercaseYAMLExtension(t *testing.T) {
	dir := t.TempDir()
	data := []byte(`name: release
stages:
  - name: build
    steps:
      - name: compile
        image: golang:1.22
        commands:
          - go build ./...
`)
	if err := os.WriteFile(filepath.Join(dir, "release.YAML"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	pipelines, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("加载有效配置失败: %v", err)
	}
	if len(pipelines) != 1 {
		t.Fatalf("应加载 1 条流水线，实际加载 %d 条", len(pipelines))
	}
	if pipelines[0].Name != "release" {
		t.Fatalf("加载了错误的流水线: %q", pipelines[0].Name)
	}
}
