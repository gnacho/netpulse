package alerts

import (
	"sync"
)

// SuppressionGraph models router parent-child relationships for alert
// suppression: if a parent router is offline, child router alerts are marked
// as suppressed instead of discarded (issue #332).
type SuppressionGraph struct {
	mu      sync.RWMutex
	parent  map[string]string   // child -> parent routerId
	down    map[string]bool     // routerId -> currently offline
}

func NewSuppressionGraph() *SuppressionGraph {
	return &SuppressionGraph{
		parent: map[string]string{},
		down:   map[string]bool{},
	}
}

// SetTopology replaces the parent map with fresh topology data.
// parent maps child routerId to its parent routerId.
func (g *SuppressionGraph) SetTopology(parent map[string]string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.parent = make(map[string]string, len(parent))
	for k, v := range parent {
		g.parent[k] = v
	}
}

// MarkDown records a router as offline.
func (g *SuppressionGraph) MarkDown(routerID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.down[routerID] = true
}

// MarkUp clears a router's offline status.
func (g *SuppressionGraph) MarkUp(routerID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.down, routerID)
}

// IsDown reports whether a router is currently offline.
func (g *SuppressionGraph) IsDown(routerID string) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.down[routerID]
}

// SuppressedBy returns the routerId suppressing alerts for child, or ""
// if none. It walks up the parent chain: if any ancestor is down, that
// ancestor suppresses the child.
func (g *SuppressionGraph) SuppressedBy(child string) string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	visited := map[string]bool{}
	cur := child
	for {
		p, ok := g.parent[cur]
		if !ok || p == "" || visited[p] {
			return ""
		}
		visited[p] = true
		if g.down[p] {
			return p
		}
		cur = p
	}
}

// DownRouters returns a copy of the current down set.
func (g *SuppressionGraph) DownRouters() []string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make([]string, 0, len(g.down))
	for id := range g.down {
		out = append(out, id)
	}
	return out
}
