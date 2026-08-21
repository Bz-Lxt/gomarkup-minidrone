// Package config 集中管理 MiniDrone 的运行时配置，支持默认值、环境变量与命令行覆盖。
package config

import (
	"flag"
	"os"
	"strconv"
	"strings"
)

// Config 是服务进程的完整配置。
type Config struct {
	Addr              string
	PipelinesDir      string
	Workers           int
	MaxParallelStages int
	GitHubSecret      string
	GitLabToken       string
	ArtifactDir       string
	Verbose           bool
}

// Defaults 返回内置默认配置。
func Defaults() Config {
	return Config{
		Addr:              ":8080",
		PipelinesDir:      "pipelines",
		Workers:           4,
		MaxParallelStages: 8,
		ArtifactDir:       "artifacts",
	}
}

// FromEnv 以默认值为底，再用环境变量覆盖。
func FromEnv() Config {
	c := Defaults()
	if v := os.Getenv("MINIDRONE_ADDR"); v != "" {
		c.Addr = v
	}
	if v := os.Getenv("MINIDRONE_PIPELINES_DIR"); v != "" {
		c.PipelinesDir = v
	}
	if v := os.Getenv("MINIDRONE_WORKERS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.Workers = n
		}
	}
	if v := os.Getenv("MINIDRONE_MAX_PARALLEL_STAGES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.MaxParallelStages = n
		}
	}
	c.GitHubSecret = os.Getenv("MINIDRONE_GITHUB_SECRET")
	c.GitLabToken = os.Getenv("MINIDRONE_GITLAB_TOKEN")
	if v := os.Getenv("MINIDRONE_ARTIFACT_DIR"); v != "" {
		c.ArtifactDir = v
	}
	c.Verbose = strings.EqualFold(os.Getenv("MINIDRONE_LOG_LEVEL"), "debug")
	return c
}

// RegisterFlags 把配置项注册到给定 FlagSet，供命令行覆盖。
func (c *Config) RegisterFlags(fs *flag.FlagSet) {
	fs.StringVar(&c.Addr, "addr", c.Addr, "HTTP 监听地址")
	fs.StringVar(&c.PipelinesDir, "pipelines", c.PipelinesDir, "流水线 YAML 目录")
	fs.IntVar(&c.Workers, "workers", c.Workers, "并行构建数")
	fs.IntVar(&c.MaxParallelStages, "max-parallel-stages", c.MaxParallelStages, "单构建内并行阶段数")
	fs.StringVar(&c.GitHubSecret, "github-secret", c.GitHubSecret, "GitHub Webhook Secret")
	fs.StringVar(&c.GitLabToken, "gitlab-token", c.GitLabToken, "GitLab Webhook Token")
	fs.StringVar(&c.ArtifactDir, "artifact-dir", c.ArtifactDir, "构建产物落地目录")
	fs.BoolVar(&c.Verbose, "v", c.Verbose, "debug 日志")
}

// Validate 修正非法数值。
func (c *Config) Validate() {
	if c.Workers <= 0 {
		c.Workers = 4
	}
	if c.MaxParallelStages <= 0 {
		c.MaxParallelStages = 8
	}
	if c.Addr == "" {
		c.Addr = ":8080"
	}
	if c.PipelinesDir == "" {
		c.PipelinesDir = "pipelines"
	}
	if c.ArtifactDir == "" {
		c.ArtifactDir = "artifacts"
	}
}

// Load 读取环境变量、解析默认 FlagSet，并完成校验。
func Load() Config {
	c := FromEnv()
	c.RegisterFlags(flag.CommandLine)
	flag.Parse()
	c.Validate()
	return c
}
