package store

import (
	"strings"
	"testing"

	"minidrone/internal/pipeline"
)

func samplePipeline() *pipeline.Pipeline {
	return &pipeline.Pipeline{
		Name: "demo",
		Stages: []pipeline.Stage{{
			Name:  "lint",
			Steps: []pipeline.Step{{Name: "check", Image: "alpine", Commands: []string{"true"}}},
		}},
	}
}

func TestCreateAndListBuilds(t *testing.T) {
	st := New()
	p := samplePipeline()
	st.PutPipeline(p)
	b1 := st.CreateBuild(p, "manual", "", "", "", "", "", false)
	b2 := st.CreateBuild(p, "manual", "", "", "", "", "", false)
	if b1.Number != 1 || b2.Number != 2 {
		t.Fatalf("编号应为递增, got %d %d", b1.Number, b2.Number)
	}
	list := st.ListBuilds(10)
	if len(list) != 2 || list[0].ID != b2.ID {
		t.Fatalf("列表应倒序, got %+v", list)
	}
}

func TestCloneStageInjected(t *testing.T) {
	st := New()
	p := samplePipeline()
	b := st.CreateBuild(p, "github-push", "https://github.com/a/b", "main", "abc", "msg", "u", true)
	if len(b.Stages) != 2 || b.Stages[0].Name != "clone" {
		t.Fatalf("应前置 clone 阶段: %+v", b.Stages)
	}
	if got := b.Stages[1].DependsOn; len(got) != 1 || got[0] != "clone" {
		t.Fatalf("根阶段应依赖 clone: %v", got)
	}
}

func TestAppendLogAndSnapshot(t *testing.T) {
	st := New()
	p := samplePipeline()
	b := st.CreateBuild(p, "manual", "", "", "", "", "", false)
	st.AppendLog(b.ID, "lint", "check", []byte("hello\nworld"))
	if got := st.Log(b.ID, "lint", "check"); !strings.Contains(got, "hello") {
		t.Fatalf("日志丢失: %q", got)
	}
	b.AddArtifacts([]Artifact{{Stage: "lint", Step: "check", Name: "a.txt", Size: 1}})
	snap := b.Snapshot()
	if len(snap.Artifacts) != 1 || snap.Artifacts[0].Name != "a.txt" {
		t.Fatalf("快照未包含产物: %+v", snap.Artifacts)
	}
}

func TestBrokerPubSub(t *testing.T) {
	br := NewBroker()
	ch := br.Subscribe("b1")
	br.Publish(Event{Type: EventLog, Build: "b1", Line: "x"})
	select {
	case ev := <-ch:
		if ev.Line != "x" {
			t.Fatalf("事件不符: %+v", ev)
		}
	default:
		t.Fatal("未收到事件")
	}
	br.Unsubscribe("b1", ch)
}

func TestStateTerminal(t *testing.T) {
	if !StateFailed.Terminal() || StateRunning.Terminal() {
		t.Fatal("终态判断错误")
	}
}
