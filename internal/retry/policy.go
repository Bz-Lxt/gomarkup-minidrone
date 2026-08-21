// Package retry 提供步骤级重试策略：固定间隔或指数退避。
package retry

import (
	"context"
	"fmt"
	"time"
)

// Policy 描述一次可重试操作的策略。
type Policy struct {
	MaxAttempts int           // 含首次执行，最小为 1
	Delay       time.Duration // 基础等待
	Backoff     bool          // 是否指数退避（delay * 2^(attempt-1)）
	MaxDelay    time.Duration // 退避上限，0 表示不限制
}

// Default 返回保守的默认策略：不重试。
func Default() Policy {
	return Policy{MaxAttempts: 1, Delay: time.Second, MaxDelay: 30 * time.Second}
}

// ParseDelay 解析 YAML 中的时长字符串，空值回落默认。
func ParseDelay(s string, fallback time.Duration) time.Duration {
	if s == "" {
		return fallback
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return fallback
	}
	return d
}

// Attempts 规范化最大尝试次数。
func (p Policy) Attempts() int {
	if p.MaxAttempts < 1 {
		return 1
	}
	return p.MaxAttempts
}

// Wait 计算第 attempt 次失败后的等待时间（attempt 从 1 起）。
func (p Policy) Wait(attempt int) time.Duration {
	base := p.Delay
	if base <= 0 {
		base = time.Second
	}
	d := base
	if p.Backoff && attempt > 1 {
		// 防止位移溢出：超过 16 次按上限处理
		shift := attempt - 1
		if shift > 16 {
			shift = 16
		}
		d = base * time.Duration(1<<uint(shift))
	}
	if p.MaxDelay > 0 && d > p.MaxDelay {
		d = p.MaxDelay
	}
	return d
}

// Do 按策略执行 fn，直到成功、耗尽次数或 ctx 取消。
func (p Policy) Do(ctx context.Context, fn func(attempt int) error) error {
	var last error
	n := p.Attempts()
	for i := 1; i <= n; i++ {
		if err := ctx.Err(); err != nil {
			if last != nil {
				return fmt.Errorf("已取消，上次错误: %w", last)
			}
			return err
		}
		last = fn(i)
		if last == nil {
			return nil
		}
		if i == n {
			break
		}
		delay := p.Wait(i)
		select {
		case <-ctx.Done():
			return fmt.Errorf("等待重试时取消: %w", last)
		case <-time.After(delay):
		}
	}
	if last == nil {
		return fmt.Errorf("重试耗尽")
	}
	return last
}
