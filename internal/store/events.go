package store

import (
	"crypto/rand"
	"sync"
	"time"
)

// 事件类型
const (
	EventBuild = "build" // 构建整体状态变化
	EventStage = "stage" // 阶段状态变化
	EventStep  = "step"  // 步骤状态变化
	EventLog   = "log"   // 日志行
	EventDone  = "done"  // 构建结束（终态）
)

// Event 是推送给 WebUI 的实时事件。
type Event struct {
	Type  string    `json:"type"`
	Build string    `json:"build"`
	Stage string    `json:"stage,omitempty"`
	Step  string    `json:"step,omitempty"`
	Line  string    `json:"line,omitempty"`
	State State     `json:"state,omitempty"`
	Time  time.Time `json:"time"`
}

// Broker 是基于内存订阅的事件总线，按构建 ID 隔离频道。
type Broker struct {
	mu   sync.Mutex
	subs map[string]map[chan Event]struct{}
}

// NewBroker 创建事件总线。
func NewBroker() *Broker {
	return &Broker{subs: make(map[string]map[chan Event]struct{})}
}

// Subscribe 订阅某个构建的事件流，返回带缓冲的通道。
func (b *Broker) Subscribe(buildID string) chan Event {
	ch := make(chan Event, 256)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.subs[buildID] == nil {
		b.subs[buildID] = make(map[chan Event]struct{})
	}
	b.subs[buildID][ch] = struct{}{}
	return ch
}

// Unsubscribe 取消订阅并关闭通道。
func (b *Broker) Unsubscribe(buildID string, ch chan Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if set, ok := b.subs[buildID]; ok {
		if _, ok := set[ch]; ok {
			delete(set, ch)
			close(ch)
		}
		if len(set) == 0 {
			delete(b.subs, buildID)
		}
	}
}

// Publish 向某个构建的所有订阅者广播事件。订阅者消费过慢时丢弃事件，
// 保证调度器永不被阻塞（客户端可通过 REST 全量拉取兜底）。
func (b *Broker) Publish(ev Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs[ev.Build] {
		select {
		case ch <- ev:
		default:
		}
	}
}

// randRead 封装 crypto/rand，便于单测替换。
var randRead = rand.Read
