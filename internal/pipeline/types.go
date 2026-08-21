// Package pipeline 定义流水线的静态配置模型，并提供 YAML 解析与 DAG 校验能力。
package pipeline

// Pipeline 是一条流水线的完整定义，由 YAML 文件描述。
type Pipeline struct {
	Name    string            `yaml:"name" json:"name"`
	Env     map[string]string `yaml:"env" json:"env,omitempty"`
	Trigger Trigger           `yaml:"trigger" json:"trigger,omitempty"`
	Notify  Notify            `yaml:"notify" json:"notify,omitempty"`
	Stages  []Stage           `yaml:"stages" json:"stages"`
}

// Notify 描述构建终态时的外部通知。
type Notify struct {
	Webhooks []string `yaml:"webhooks" json:"webhooks,omitempty"`
}

// Trigger 描述 Webhook 触发规则。Repo 为空表示该流水线不参与 Webhook 匹配。
type Trigger struct {
	Repo     string   `yaml:"repo" json:"repo,omitempty"`
	Events   []string `yaml:"events" json:"events,omitempty"`     // push / pull_request / merge_request
	Branches []string `yaml:"branches" json:"branches,omitempty"` // 为空表示匹配所有分支
}

// Stage 是流水线中的一个阶段，阶段之间通过 DependsOn 构成 DAG。
type Stage struct {
	Name      string            `yaml:"name" json:"name"`
	DependsOn []string          `yaml:"depends_on" json:"depends_on,omitempty"`
	Env       map[string]string `yaml:"env" json:"env,omitempty"`
	Steps     []Step            `yaml:"steps" json:"steps"`
}

// Step 是阶段内串行执行的最小单元，每个 Step 运行在独立的 Docker 容器中。
type Step struct {
	Name         string            `yaml:"name" json:"name"`
	Image        string            `yaml:"image" json:"image"`
	Commands     []string          `yaml:"commands" json:"commands"`
	Env          map[string]string `yaml:"env" json:"env,omitempty"`
	WorkDir      string            `yaml:"workdir" json:"workdir,omitempty"`
	Pull         bool              `yaml:"pull" json:"pull,omitempty"`
	Timeout      string            `yaml:"timeout" json:"timeout,omitempty"`             // 如 5m、30s
	Retries      int               `yaml:"retries" json:"retries,omitempty"`             // 失败后额外重试次数
	RetryDelay   string            `yaml:"retry_delay" json:"retry_delay,omitempty"`     // 重试间隔
	AllowFailure bool              `yaml:"allow_failure" json:"allow_failure,omitempty"` // 失败不阻断下游
	Artifacts    []string          `yaml:"artifacts" json:"artifacts,omitempty"`         // 相对工作区的产物路径/glob
}
