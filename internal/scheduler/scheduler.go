// Package scheduler 实现基于 DAG 的流水线调度：构建排队、阶段并行编排、步骤容器化执行。
package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"minidrone/internal/artifact"
	"minidrone/internal/executor"
	"minidrone/internal/gitclone"
	"minidrone/internal/metrics"
	"minidrone/internal/notify"
	"minidrone/internal/pipeline"
	"minidrone/internal/retry"
	"minidrone/internal/store"
)

// Options 调度器配置。
type Options struct {
	Workers           int // 并行运行的构建数
	MaxParallelStages int // 单个构建内并行运行的阶段数
	WorkDir           string
	Metrics           *metrics.Registry
	Notifier          *notify.Notifier
	Artifacts         *artifact.Collector
}

// Scheduler 是任务调度器。
type Scheduler struct {
	exec  executor.Executor
	store *store.Store
	opts  Options
	queue chan *store.Build
	wg    sync.WaitGroup
}

// New 创建调度器。
func New(exec executor.Executor, st *store.Store, opts Options) *Scheduler {
	if opts.Workers <= 0 {
		opts.Workers = 4
	}
	if opts.MaxParallelStages <= 0 {
		opts.MaxParallelStages = 8
	}
	if opts.WorkDir == "" {
		opts.WorkDir = "/workspace"
	}
	return &Scheduler{
		exec:  exec,
		store: st,
		opts:  opts,
		queue: make(chan *store.Build, 64),
	}
}

// Start 启动 worker 协程池，直到 ctx 取消。
func (s *Scheduler) Start(ctx context.Context) {
	for i := 0; i < s.opts.Workers; i++ {
		s.wg.Add(1)
		go func(id int) {
			defer s.wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case b := <-s.queue:
					s.runBuild(ctx, b)
				}
			}
		}(i)
	}
	slog.Info("调度器已启动", "workers", s.opts.Workers)
}

// Stop 等待所有 worker 退出。
func (s *Scheduler) Stop() { s.wg.Wait() }

// Submit 将构建放入队列。队列满时构建直接标记失败。
func (s *Scheduler) Submit(b *store.Build) {
	select {
	case s.queue <- b:
		slog.Info("构建已入队", "build", b.ID, "pipeline", b.Pipeline)
	default:
		b.Update(func(b *store.Build) {
			b.State = store.StateFailed
			b.Error = "任务队列已满"
			b.EndedAt = time.Now()
		})
	}
}

// Cancel 取消构建：排队中的直接标记取消，运行中的触发 context 取消杀掉容器。
func (s *Scheduler) Cancel(id string) bool {
	b, ok := s.store.GetBuild(id)
	if !ok {
		return false
	}
	canceled := false
	b.Update(func(b *store.Build) {
		switch b.State {
		case store.StatePending:
			b.State = store.StateCanceled
			b.EndedAt = time.Now()
			canceled = true
		case store.StateRunning:
			if b.Cancel != nil {
				b.Cancel()
			}
			canceled = true
		}
	})
	return canceled
}

// runBuild 执行一次完整构建。
func (s *Scheduler) runBuild(parent context.Context, b *store.Build) {
	// 排队期间可能已被取消
	var skip bool
	b.Update(func(b *store.Build) {
		if b.State == store.StateCanceled {
			skip = true
		}
	})
	if skip {
		return
	}

	p, ok := s.store.GetPipeline(b.Pipeline)
	if !ok {
		b.Update(func(b *store.Build) {
			b.State = store.StateFailed
			b.Error = "流水线定义已不存在"
			b.EndedAt = time.Now()
		})
		return
	}

	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	b.Update(func(b *store.Build) {
		b.State = store.StateRunning
		b.StartedAt = time.Now()
		b.Cancel = cancel
	})
	s.publish(b, store.EventBuild, "", "", store.StateRunning)
	if s.opts.Metrics != nil {
		s.opts.Metrics.OnStart()
	}
	slog.Info("构建开始", "build", b.ID, "pipeline", b.Pipeline)

	// 每个构建一个共享卷，作为跨阶段、跨步骤的工作区
	volume := "minidrone-" + b.ID
	if err := s.exec.CreateVolume(ctx, volume); err != nil {
		s.finishBuild(b, store.StateFailed, fmt.Sprintf("创建工作区卷失败: %v", err))
		return
	}
	defer func() {
		rmCtx, rmCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer rmCancel()
		if err := s.exec.RemoveVolume(rmCtx, volume); err != nil {
			slog.Warn("清理工作区卷失败", "volume", volume, "err", err)
		}
	}()

	s.runStages(ctx, b, p, volume)

	// 汇总最终状态：任一阶段失败则构建失败；被取消则标记取消。
	// 额外检查步骤级 failed 状态作为兜底，防止执行器错误被错误地标记为
	// success 后，整条构建也显示 success。
	final := store.StateSuccess
	errMsg := ""
	b.Update(func(b *store.Build) {
		for _, st := range b.Stages {
			if st.State == store.StateFailed {
				final = store.StateFailed
				continue
			}
			// 兜底：阶段未标记 failed，但其中某步骤为 failed（执行器错误等），
			// 同样视为构建失败。
			if final != store.StateFailed {
				for _, sp := range st.Steps {
					if sp.State == store.StateFailed {
						final = store.StateFailed
						break
					}
				}
			}
		}
		if ctx.Err() != nil && final == store.StateSuccess {
			final = store.StateCanceled
		}
		if final == store.StateFailed {
			errMsg = "一个或多个阶段执行失败"
		}
	})
	s.finishBuild(b, final, errMsg)
}

// runStages 按 DAG 编排阶段执行。
//
// 实现方式：每个阶段一个协程，通过每阶段的 done 通道等待全部依赖完成；
// 依赖中有失败/跳过/取消的，当前阶段标记跳过；信号量限制并行度。
// 流水线定义已通过无环校验，因此不存在死锁。
func (s *Scheduler) runStages(ctx context.Context, b *store.Build, p *pipeline.Pipeline, volume string) {
	stageDefs := make(map[string]*pipeline.Stage, len(p.Stages))
	for i := range p.Stages {
		stageDefs[p.Stages[i].Name] = &p.Stages[i]
	}

	done := make(map[string]chan struct{}, len(b.Stages))
	for _, st := range b.Stages {
		done[st.Name] = make(chan struct{})
	}

	sem := make(chan struct{}, s.opts.MaxParallelStages)
	var wg sync.WaitGroup

	for _, st := range b.Stages {
		wg.Add(1)
		go func(st *store.StageStatus) {
			defer wg.Done()
			defer close(done[st.Name])

			// 等待全部依赖阶段结束（通道关闭即终态，且状态写入先于关闭，可见性有保证）
			depsFailed := false
			for _, dep := range st.DependsOn {
				depCh, ok := done[dep]
				if !ok {
					continue
				}
				<-depCh
				b.Update(func(b *store.Build) {
					if ds := b.FindStage(dep); ds != nil && ds.State != store.StateSuccess {
						depsFailed = true
					}
				})
			}

			if depsFailed {
				b.Update(func(b *store.Build) {
					st.State = store.StateSkipped
					st.EndedAt = time.Now()
					for _, sp := range st.Steps {
						sp.State = store.StateSkipped
					}
				})
				s.publish(b, store.EventStage, st.Name, "", store.StateSkipped)
				return
			}

			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				b.Update(func(b *store.Build) { st.State = store.StateCanceled })
				return
			}

			s.runStage(ctx, b, p, stageDefs[st.Name], st, volume)
		}(st)
	}
	wg.Wait()
}

// runStage 串行执行阶段内的所有步骤。
func (s *Scheduler) runStage(ctx context.Context, b *store.Build, p *pipeline.Pipeline, def *pipeline.Stage, st *store.StageStatus, volume string) {
	b.Update(func(b *store.Build) {
		st.State = store.StateRunning
		st.StartedAt = time.Now()
	})
	s.publish(b, store.EventStage, st.Name, "", store.StateRunning)

	failed := false
	for i, sp := range st.Steps {
		if ctx.Err() != nil {
			b.Update(func(b *store.Build) {
				for _, rest := range st.Steps[i:] {
					rest.State = store.StateCanceled
				}
			})
			break
		}

		// 合成 clone 阶段或常规阶段：解析步骤定义
		var stepDef *pipeline.Step
		if def != nil && i < len(def.Steps) {
			stepDef = &def.Steps[i]
		} else if st.Name == gitclone.StageName {
			stepDef = gitclone.Step(b.Repo, b.Branch, b.Commit)
		}
		if stepDef == nil {
			continue
		}

		if err := s.runStep(ctx, b, p, def, st, stepDef, sp, volume); err != nil {
			failed = true
			b.Update(func(b *store.Build) {
				for _, rest := range st.Steps[i+1:] {
					rest.State = store.StateSkipped
				}
			})
			break
		}
	}

	b.Update(func(b *store.Build) {
		st.EndedAt = time.Now()
		switch {
		case failed:
			st.State = store.StateFailed
		case ctx.Err() != nil && st.State != store.StateFailed:
			st.State = store.StateCanceled
		default:
			st.State = store.StateSuccess
		}
	})
	s.publish(b, store.EventStage, st.Name, "", st.State)
}

// runStep 在独立容器中执行单个步骤，支持超时、重试与 allow_failure。
func (s *Scheduler) runStep(ctx context.Context, b *store.Build, p *pipeline.Pipeline, stageDef *pipeline.Stage, st *store.StageStatus, stepDef *pipeline.Step, sp *store.StepStatus, volume string) error {
	b.Update(func(b *store.Build) {
		sp.State = store.StateRunning
		sp.StartedAt = time.Now()
	})
	s.publish(b, store.EventStep, st.Name, sp.Name, store.StateRunning)

	stepCtx := ctx
	if d, err := time.ParseDuration(stepDef.Timeout); err == nil && d > 0 {
		var cancel context.CancelFunc
		stepCtx, cancel = context.WithTimeout(ctx, d)
		defer cancel()
	}

	policy := retry.Policy{
		MaxAttempts: stepDef.Retries + 1,
		Delay:       retry.ParseDelay(stepDef.RetryDelay, time.Second),
		Backoff:     true,
		MaxDelay:    30 * time.Second,
	}
	err := policy.Do(stepCtx, func(attempt int) error {
		return s.runStepOnce(stepCtx, b, p, stageDef, st, stepDef, sp, volume, attempt)
	})

	if err != nil && stepDef.AllowFailure && ctx.Err() == nil {
		s.store.AppendLog(b.ID, st.Name, sp.Name, []byte("[minidrone] allow_failure=true，步骤失败不阻断下游\n"))
		b.Update(func(b *store.Build) {
			if sp.State != store.StateCanceled {
				sp.State = store.StateSuccess
			}
		})
		s.publish(b, store.EventStep, st.Name, sp.Name, store.StateSuccess)
		return nil
	}
	return err
}

func (s *Scheduler) runStepOnce(ctx context.Context, b *store.Build, p *pipeline.Pipeline, stageDef *pipeline.Stage, st *store.StageStatus, stepDef *pipeline.Step, sp *store.StepStatus, volume string, attempt int) error {
	b.Update(func(b *store.Build) {
		sp.State = store.StateRunning
		sp.Attempts = attempt
	})
	if attempt > 1 {
		s.store.AppendLog(b.ID, st.Name, sp.Name, []byte(fmt.Sprintf("[minidrone] 第 %d 次尝试\n", attempt)))
	}

	env := mergeEnv(p.Env, stageEnv(stageDef), stepDef.Env)
	env = append(env,
		"CI=true",
		"CI_BUILD_ID="+b.ID,
		fmt.Sprintf("CI_BUILD_NUMBER=%d", b.Number),
		"CI_PIPELINE="+b.Pipeline,
		"CI_STAGE="+st.Name,
		"CI_STEP="+sp.Name,
		"CI_REPO="+b.Repo,
		"CI_BRANCH="+b.Branch,
		"CI_COMMIT_SHA="+b.Commit,
		fmt.Sprintf("CI_ATTEMPT=%d", attempt),
	)

	workDir := stepDef.WorkDir
	if workDir == "" {
		workDir = s.opts.WorkDir
	}

	logs := &logWriter{fn: func(chunk []byte) {
		s.store.AppendLog(b.ID, st.Name, sp.Name, chunk)
	}}

	name := containerName(b.ID, st.Name, sp.Name)
	if attempt > 1 {
		name = fmt.Sprintf("%s-try%d", name, attempt)
	}

	exitCode, err := s.exec.Run(ctx, executor.RunConfig{
		Name:      name,
		Image:     stepDef.Image,
		Commands:  stepDef.Commands,
		Env:       env,
		WorkDir:   workDir,
		Volume:    volume,
		MountPath: s.opts.WorkDir,
		Pull:      stepDef.Pull,
		Labels:    map[string]string{"minidrone": "true", "build": b.ID, "stage": st.Name, "step": sp.Name},
	}, logs)

	b.Update(func(b *store.Build) {
		sp.EndedAt = time.Now()
		sp.ExitCode = exitCode
		switch {
		case err != nil && ctx.Err() != nil:
			sp.State = store.StateCanceled
		case err != nil || exitCode != 0:
			sp.State = store.StateFailed
		default:
			sp.State = store.StateSuccess
		}
	})
	s.publish(b, store.EventStep, st.Name, sp.Name, sp.State)

	// 执行器返回错误（如连接中断、容器运行错误）时必须向上传播，
	// 即使退出码为 0 也要终止流水线，避免"步骤已 failed 但构建最终 success"。
	if err != nil {
		return err
	}
	if exitCode != 0 {
		return fmt.Errorf("退出码 %d", exitCode)
	}

	if len(stepDef.Artifacts) > 0 && s.opts.Artifacts != nil {
		items, aerr := s.opts.Artifacts.Collect(ctx, volume, s.opts.WorkDir, b.ID, st.Name, sp.Name, stepDef.Artifacts)
		if aerr != nil {
			s.store.AppendLog(b.ID, st.Name, sp.Name, []byte("[minidrone] 产物采集失败: "+aerr.Error()+"\n"))
		} else {
			b.AddArtifacts(items)
			s.store.AppendLog(b.ID, st.Name, sp.Name, []byte(fmt.Sprintf("[minidrone] 已采集 %d 个产物\n", len(items))))
		}
	}
	return nil
}

// finishBuild 设置构建终态并广播 done 事件。
func (s *Scheduler) finishBuild(b *store.Build, state store.State, errMsg string) {
	b.Update(func(b *store.Build) {
		b.State = state
		b.Error = errMsg
		b.EndedAt = time.Now()
		b.Cancel = nil
	})
	s.publish(b, store.EventBuild, "", "", state)
	s.store.Broker().Publish(store.Event{
		Type:  store.EventDone,
		Build: b.ID,
		State: state,
		Time:  time.Now(),
	})
	if s.opts.Metrics != nil {
		snap := b.Snapshot()
		s.opts.Metrics.OnFinish(state, snap.StartedAt, snap.EndedAt)
	}
	if s.opts.Notifier != nil {
		if p, ok := s.store.GetPipeline(b.Pipeline); ok && len(p.Notify.Webhooks) > 0 {
			go s.opts.Notifier.Send(context.Background(), p.Notify.Webhooks, notify.FromBuild(b.Snapshot()))
		}
	}
	slog.Info("构建结束", "build", b.ID, "pipeline", b.Pipeline, "state", state)
}

func (s *Scheduler) publish(b *store.Build, typ, stage, step string, state store.State) {
	s.store.Broker().Publish(store.Event{
		Type:  typ,
		Build: b.ID,
		Stage: stage,
		Step:  step,
		State: state,
		Time:  time.Now(),
	})
}

// logWriter 将容器日志流接入存储与事件总线。
type logWriter struct {
	fn func([]byte)
}

func (w *logWriter) Write(p []byte) (int, error) {
	w.fn(p)
	return len(p), nil
}

// mergeEnv 按 流水线 -> 阶段 -> 步骤 的优先级合并环境变量，转为 KEY=VALUE 列表。
func mergeEnv(layers ...map[string]string) []string {
	merged := make(map[string]string)
	var keys []string
	for _, layer := range layers {
		for k, v := range layer {
			if _, seen := merged[k]; !seen {
				keys = append(keys, k)
			}
			merged[k] = v
		}
	}
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+merged[k])
	}
	return out
}

func stageEnv(def *pipeline.Stage) map[string]string {
	if def == nil {
		return nil
	}
	return def.Env
}

var nameSanitizer = regexp.MustCompile(`[^a-z0-9-]+`)

// containerName 生成唯一且合法的容器名。
func containerName(buildID, stage, step string) string {
	s := strings.ToLower("minidrone-" + buildID + "-" + stage + "-" + step)
	s = nameSanitizer.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}
