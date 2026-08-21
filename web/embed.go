// Package web 内嵌 WebUI 静态资源。
package web

import "embed"

// FS 包含 WebUI 的全部静态文件。
//
//go:embed index.html
var FS embed.FS
