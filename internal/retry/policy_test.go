package retry

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDoSucceedsAfterRetry(t *testing.T) {
	p := Policy{MaxAttempts: 3, Delay: time.Millisecond}
	n := 0
	err := p.Do(context.Background(), func(attempt int) error {
		n++
		if attempt < 3 {
			return errors.New("flaky")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("应执行 3 次, got %d", n)
	}
}

func TestDoExhausted(t *testing.T) {
	p := Policy{MaxAttempts: 2, Delay: time.Millisecond}
	err := p.Do(context.Background(), func(int) error { return errors.New("always") })
	if err == nil || err.Error() != "always" {
		t.Fatalf("应返回最后一次错误, got %v", err)
	}
}

func TestWaitBackoffCapped(t *testing.T) {
	p := Policy{Delay: time.Second, Backoff: true, MaxDelay: 3 * time.Second}
	if d := p.Wait(1); d != time.Second {
		t.Fatalf("第 1 次等待应为 1s, got %s", d)
	}
	if d := p.Wait(8); d != 3*time.Second {
		t.Fatalf("退避应被上限截断, got %s", d)
	}
}

func TestParseDelay(t *testing.T) {
	if got := ParseDelay("2s", time.Second); got != 2*time.Second {
		t.Fatalf("got %s", got)
	}
	if got := ParseDelay("bad", time.Second); got != time.Second {
		t.Fatalf("非法值应回落默认, got %s", got)
	}
}

func TestDoHonorsCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Policy{MaxAttempts: 3, Delay: time.Second}.Do(ctx, func(int) error {
		t.Fatal("已取消不应再执行")
		return nil
	})
	if err == nil {
		t.Fatal("取消后应返回错误")
	}
}
