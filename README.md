# MiniDrone — 自研轻量级 CI/CD 自动化流水线系统

基于 Go 实现的轻量级 CI/CD 平台：YAML 定义流水线、Docker 容器化隔离执行、DAG 调度串并行编排、GitHub/GitLab Webhook 自动触发，内置实时 WebUI。

## 功能特性

| 模块 | 说明 |
| --- | --- |
| 流水线解析 | YAML 定义多阶段（Stages）与步骤（Steps），严格字段校验、依赖引用检查、环检测 |
| 容器化执行引擎 | Docker 官方 Go SDK（`github.com/docker/docker`），每个 Step 独立容器：拉镜像 → 创建 → 启动 → 流式日志 → 等待退出 → 强制回收 |
| 任务调度器 | DAG 调度：阶段按 `depends_on` 拓扑编排，无依赖阶段自动并行；构建级任务队列 + Worker 池；信号量限制并行度 |
| Webhook 触发器 | GitHub（`X-Hub-Signature-256` HMAC-SHA256 校验）与 GitLab（`X-Gitlab-Token`）的 Push / PR / MR 事件 |
| WebUI | 构建看板、DAG 阶段视图、SSE 实时日志终端、手动触发 / 取消 / 注册流水线 |
| 步骤策略 | `timeout`、`retries` + 指数退避、`allow_failure` |
| 产物采集 | 步骤声明 `artifacts`，从共享卷拷到宿主机 `artifacts/` |
| 完成通知 | 流水线 `notify.webhooks`，构建终态 POST JSON |
| 指标 | `/api/metrics` 统计成功率、平均耗时、当前运行数 |
| CLI | `dronectl` 查看流水线、触发构建、拉日志、取消、看指标 |

## 快速开始

```bash
# 需要：Go 1.25+，运行中的 Docker
go build -o bin/minidrone ./cmd/minidrone

./bin/minidrone -addr :8080
# 打开 http://localhost:8080
```

启动时自动加载 `pipelines/` 目录下的所有 YAML。仓库自带两个示例：

- `pipelines/demo.yaml` — 纯本地演示（alpine 镜像），含并行阶段，可直接手动运行
- `pipelines/go-project.yaml` — 真实 Go 项目模板，配合 Webhook / 填写仓库地址使用

### 命令行参数 / 环境变量

| 参数 | 环境变量 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `-addr` | `MINIDRONE_ADDR` | `:8080` | HTTP 监听地址 |
| `-pipelines` | `MINIDRONE_PIPELINES_DIR` | `pipelines` | 流水线 YAML 目录 |
| `-workers` | — | `4` | 并行构建数（任务队列 Worker） |
| `-max-parallel-stages` | — | `8` | 单构建内并行阶段数 |
| `-github-secret` | `MINIDRONE_GITHUB_SECRET` | 空 | GitHub Webhook Secret（为空则跳过签名校验） |
| `-gitlab-token` | `MINIDRONE_GITLAB_TOKEN` | 空 | GitLab Webhook Secret Token |
| `-artifact-dir` | `MINIDRONE_ARTIFACT_DIR` | `artifacts` | 构建产物落地目录 |

Docker 连接：优先 `DOCKER_HOST`；未设置时自动探测 `/var/run/docker.sock`、`~/.docker/run/docker.sock`（Docker Desktop）、OrbStack、Colima。

## 流水线 YAML 语法

```yaml
name: demo                      # 必填，唯一
env:                            # 全局环境变量（可选）
  GOFLAGS: -mod=vendor

trigger:                        # Webhook 触发规则（可选）
  repo: "https://github.com/your-org/your-repo"
  events: [push, pull_request]  # push / pull_request（pr、merge_request 为同义别名）
  branches: [main]              # 为空匹配所有分支

stages:
  - name: lint                  # 阶段名，必填唯一
    steps:
      - name: go-vet            # 步骤名，必填
        image: golang:1.25-alpine   # 容器镜像，必填
        commands:                   # shell 命令列表，set -e 语义
          - go vet ./...
        env: {}               # 步骤级环境变量（可选）
        workdir: /workspace   # 容器内工作目录（可选）
        pull: false           # 执行前总是拉取镜像（可选）
        timeout: 5m           # 步骤超时（可选）
        retries: 1            # 失败后额外重试次数（可选）
        retry_delay: 2s       # 重试基础间隔，指数退避
        allow_failure: false  # 失败不阻断下游
        artifacts: [bin/app]  # 采集到宿主机的产物路径

  - name: test
    depends_on: [lint]          # 阶段依赖，构成 DAG；无依赖阶段并行执行
    steps:
      - name: unit-test
        image: golang:1.25-alpine
        commands: [go test ./...]

  - name: security
    depends_on: [lint]          # 与 test 并行
    steps: [...]

  - name: build
    depends_on: [test, security]  # 汇聚多个上游阶段
    steps: [...]

notify:
  webhooks: ["http://example.com/hooks/minidrone"]
```

执行语义：

- **阶段间**：按 `depends_on` 构成 DAG 拓扑调度，无依赖关系的阶段并行执行；任一上游失败则下游标记 `skipped`
- **阶段内**：步骤串行执行，前一步失败则后续步骤跳过
- **工作区**：每次构建创建一个 Docker 卷挂载到所有步骤容器的 `/workspace`，跨阶段共享文件（如编译产物）
- **环境变量**：按 流水线 → 阶段 → 步骤 合并覆盖，并自动注入 `CI=true`、`CI_BUILD_ID`、`CI_COMMIT_SHA`、`CI_BRANCH` 等变量

## Webhook 配置

在 GitHub/GitLab 仓库设置中添加 Webhook：

| 平台 | URL | 密钥 | 事件 |
| --- | --- | --- | --- |
| GitHub | `http://<host>:8080/api/webhooks/github` | Secret 填 `MINIDRONE_GITHUB_SECRET` | Just push / Pull requests |
| GitLab | `http://<host>:8080/api/webhooks/gitlab` | Secret Token 填 `MINIDRONE_GITLAB_TOKEN` | Push events / Merge request events |

事件到达后按 `trigger.repo`（地址写法自动归一化：https/ssh/scp、`.git` 后缀、大小写）、`events`、`branches` 匹配流水线并触发构建。带仓库信息的构建会自动前置一个隐式 **clone 阶段**（`alpine/git` 浅克隆到共享卷）。

## HTTP API

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/health` | 健康检查 |
| GET | `/api/metrics` | 构建计数与耗时指标 |
| GET | `/api/pipelines` | 流水线列表 |
| POST | `/api/pipelines` | 注册流水线（请求体为 YAML） |
| DELETE | `/api/pipelines/{name}` | 删除流水线 |
| POST | `/api/pipelines/{name}/run` | 手动触发（可选 JSON：`{repo, branch, commit}`） |
| GET | `/api/builds` | 构建列表（倒序，最多 100 条） |
| GET | `/api/builds/{id}` | 构建详情快照 |
| GET | `/api/builds/{id}/logs?stage=&step=` | 步骤完整日志 |
| GET | `/api/builds/{id}/artifacts` | 构建产物清单 |
| POST | `/api/builds/{id}/cancel` | 取消构建（运行中会杀掉容器） |
| GET | `/api/builds/{id}/events` | SSE 实时事件流（状态变化 + 日志行） |
| POST | `/api/webhooks/github` | GitHub Webhook |
| POST | `/api/webhooks/gitlab` | GitLab Webhook |

## 架构

```
cmd/minidrone        服务入口
cmd/dronectl         命令行客户端
internal/config      默认值 / 环境变量 / flag 配置
internal/dag         环检测、拓扑排序、就绪集
internal/pipeline    YAML 解析与校验
internal/store       运行态、日志缓冲、SSE 事件总线
internal/executor    Docker SDK + 测试用 Mock
internal/scheduler   DAG 调度、超时/重试、产物采集
internal/retry       指数退避重试
internal/artifact    产物路径校验与落地
internal/gitclone    隐式 clone 步骤生成
internal/notify      构建终态 HTTP 通知
internal/metrics     进程内指标
internal/webhook     GitHub/GitLab 触发器
internal/server      REST API、SSE、WebUI
```

调度核心：每个阶段一个协程，通过每阶段的 `done` channel 等待全部依赖到达终态；通道关闭先于状态读取，happens-before 保证可见性；信号量控制并行度；构建取消通过 `context` 级联杀掉运行中的容器。

## 测试

```bash
go test ./...
go build -o bin/minidrone ./cmd/minidrone
go build -o bin/dronectl  ./cmd/dronectl
```

```bash
# 常用 CLI
export MINIDRONE_URL=http://localhost:8080
dronectl pipelines
dronectl run demo
dronectl builds
dronectl logs <id> build compile
dronectl metrics
```
