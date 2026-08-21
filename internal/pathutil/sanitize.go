// Package pathutil 提供路径安全校验工具，防止产物配置中的目录穿越。
package pathutil

import (
	"fmt"
	"path/filepath"
	"strings"
)

// SanitizePattern 拒绝含路径穿越的相对产物模式。
//
// 通过 filepath.Clean 规范化路径后再判断，可检测嵌套穿越，
// 例如 reports/../../secrets.txt 经规范化后变为 ../secrets.txt。
// 同时将反斜杠统一为正斜杠后再判定，以拦截 Windows 风格的
// 路径穿越（如 ..\..\secrets.txt）。合法模式如 dist/*.tgz、
// reports/*.html 不受影响。
func SanitizePattern(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", fmt.Errorf("空产物路径")
	}
	// 统一为正斜杠后再 Clean，使反斜杠穿越也能被检测到
	cleaned := filepath.Clean(strings.ReplaceAll(p, "\\", "/"))
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") {
		return "", fmt.Errorf("非法产物路径 %q", p)
	}
	return p, nil
}

// Filter 过滤并规范化产物路径列表，任一路径非法则整体失败。
func Filter(patterns []string) ([]string, error) {
	var out []string
	for _, p := range patterns {
		s, err := SanitizePattern(p)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}
