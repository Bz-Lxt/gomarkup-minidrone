package dag

import (
	"reflect"
	"testing"
)

func TestCycleDetection(t *testing.T) {
	g := New()
	g.Add("a", []string{"c"})
	g.Add("b", []string{"a"})
	g.Add("c", []string{"b"})
	if g.Cycle() == nil {
		t.Fatal("应检测出 a→c→b→a 环")
	}
}

func TestTopoOrder(t *testing.T) {
	g := New()
	g.Add("lint", nil)
	g.Add("test", []string{"lint"})
	g.Add("security", []string{"lint"})
	g.Add("build", []string{"test", "security"})
	order, err := g.Topo()
	if err != nil {
		t.Fatal(err)
	}
	pos := map[string]int{}
	for i, n := range order {
		pos[n] = i
	}
	if pos["lint"] > pos["test"] || pos["lint"] > pos["security"] {
		t.Fatalf("lint 应先于下游: %v", order)
	}
	if pos["test"] > pos["build"] || pos["security"] > pos["build"] {
		t.Fatalf("build 应在 test/security 之后: %v", order)
	}
}

func TestReadySet(t *testing.T) {
	g := New()
	g.Add("lint", nil)
	g.Add("test", []string{"lint"})
	g.Add("security", []string{"lint"})
	g.Add("build", []string{"test", "security"})

	if got := g.Ready(nil); !reflect.DeepEqual(got, []string{"lint"}) {
		t.Fatalf("初始就绪应为 lint, got %v", got)
	}
	if got := g.Ready(map[string]bool{"lint": true}); !reflect.DeepEqual(got, []string{"test", "security"}) {
		t.Fatalf("lint 完成后应就绪 test+security, got %v", got)
	}
	if got := g.Ready(map[string]bool{"lint": true, "test": true, "security": true}); !reflect.DeepEqual(got, []string{"build"}) {
		t.Fatalf("汇聚后应就绪 build, got %v", got)
	}
}

func TestTopoRejectsCycle(t *testing.T) {
	g := FromPairs(map[string][]string{"a": {"b"}, "b": {"a"}})
	if _, err := g.Topo(); err == nil {
		t.Fatal("有环图的拓扑排序应失败")
	}
}
