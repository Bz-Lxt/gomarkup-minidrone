// Package dag 提供有向无环图的构建、环检测、拓扑排序与就绪集计算。
package dag

import "fmt"

const (
	white = iota
	gray
	black
)

// Graph 是以节点名为键的有向图，边方向为「依赖 → 被依赖」（执行前必须先完成依赖）。
type Graph struct {
	nodes map[string][]string
	order []string
}

// New 创建空图。
func New() *Graph {
	return &Graph{nodes: make(map[string][]string)}
}

// Add 登记一个节点及其依赖列表。重复添加会覆盖旧依赖。
func (g *Graph) Add(name string, deps []string) {
	if _, ok := g.nodes[name]; !ok {
		g.order = append(g.order, name)
	}
	cp := make([]string, len(deps))
	copy(cp, deps)
	g.nodes[name] = cp
}

// Has 判断节点是否存在。
func (g *Graph) Has(name string) bool {
	_, ok := g.nodes[name]
	return ok
}

// Nodes 按添加顺序返回节点名。
func (g *Graph) Nodes() []string {
	out := make([]string, len(g.order))
	copy(out, g.order)
	return out
}

// Deps 返回节点的依赖副本。
func (g *Graph) Deps(name string) []string {
	src := g.nodes[name]
	out := make([]string, len(src))
	copy(out, src)
	return out
}

// Cycle 用三色 DFS 检测环，返回构成环的路径（含回到起点的节点）；无环返回 nil。
func (g *Graph) Cycle() []string {
	color := make(map[string]int, len(g.nodes))
	var stack, cycle []string

	var dfs func(name string) bool
	dfs = func(name string) bool {
		color[name] = gray
		stack = append(stack, name)
		for _, dep := range g.nodes[name] {
			if !g.Has(dep) {
				continue
			}
			switch color[dep] {
			case gray:
				for i, n := range stack {
					if n == dep {
						cycle = append(append([]string{}, stack[i:]...), dep)
						return true
					}
				}
			case white:
				if dfs(dep) {
					return true
				}
			}
		}
		stack = stack[:len(stack)-1]
		color[name] = black
		return false
	}

	for _, name := range g.order {
		if color[name] == white && dfs(name) {
			return cycle
		}
	}
	return nil
}

// Topo 返回拓扑序（依赖在前）。存在环时返回错误。
func (g *Graph) Topo() ([]string, error) {
	if cycle := g.Cycle(); cycle != nil {
		return nil, fmt.Errorf("存在环: %v", cycle)
	}
	indeg := make(map[string]int, len(g.nodes))
	for _, name := range g.order {
		indeg[name] = 0
	}
	for _, name := range g.order {
		for _, dep := range g.nodes[name] {
			if g.Has(dep) {
				indeg[name]++
			}
		}
	}
	var queue []string
	for _, name := range g.order {
		if indeg[name] == 0 {
			queue = append(queue, name)
		}
	}
	out := make([]string, 0, len(g.order))
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		out = append(out, n)
		for _, name := range g.order {
			for _, dep := range g.nodes[name] {
				if dep == n {
					indeg[name]--
					if indeg[name] == 0 {
						queue = append(queue, name)
					}
				}
			}
		}
	}
	if len(out) != len(g.order) {
		return nil, fmt.Errorf("拓扑排序未覆盖全部节点")
	}
	return out, nil
}

// Ready 返回所有依赖均已出现在 done 中、且自身尚未完成的节点。
func (g *Graph) Ready(done map[string]bool) []string {
	var ready []string
	for _, name := range g.order {
		if done[name] {
			continue
		}
		ok := true
		for _, dep := range g.nodes[name] {
			if !done[dep] {
				ok = false
				break
			}
		}
		if ok {
			ready = append(ready, name)
		}
	}
	return ready
}

// FromPairs 由「节点 → 依赖列表」快速建图。
func FromPairs(pairs map[string][]string) *Graph {
	g := New()
	for name, deps := range pairs {
		g.Add(name, deps)
	}
	return g
}
