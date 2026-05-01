package plasma

import (
	"fmt"
	"sort"

	"github.com/kube-metrics/nexusflow/pkg/dag"
)

// AppendDAG merges new vertices into nodes and rejects duplicates / cycles / dangling deps.
func AppendDAG(existing []dag.Node, add []dag.Node) ([]dag.Node, error) {
	if len(add) == 0 {
		return existing, nil
	}
	ids := map[string]struct{}{}
	for _, n := range existing {
		ids[n.ID] = struct{}{}
	}
	pending := map[string]struct{}{}
	for _, n := range add {
		if _, dup := ids[n.ID]; dup {
			return nil, fmt.Errorf("duplicate node id %q", n.ID)
		}
		pending[n.ID] = struct{}{}
	}
	for _, n := range add {
		if len(n.Cmd) == 0 {
			return nil, fmt.Errorf("node %q: empty cmd", n.ID)
		}
		for _, d := range n.DependsOn {
			if _, ok := ids[d]; !ok {
				if _, ok2 := pending[d]; !ok2 {
					return nil, fmt.Errorf("node %q: unknown dependency %q", n.ID, d)
				}
			}
		}
	}
	merged := append(append([]dag.Node(nil), existing...), add...)
	if _, err := dag.TopoOrder(merged); err != nil {
		return nil, err
	}
	return merged, nil
}

type runnableCand struct {
	id string
	n  dag.Node
}

// PickRunnable chooses the lexicographically smallest id among vertices ready to run.
func PickRunnable(nodes []dag.Node, completed, running map[string]struct{}) *dag.Node {
	var c []runnableCand
	for _, n := range nodes {
		if _, ok := completed[n.ID]; ok {
			continue
		}
		if _, ok := running[n.ID]; ok {
			continue
		}
		ready := true
		for _, d := range n.DependsOn {
			if _, ok := completed[d]; !ok {
				ready = false
				break
			}
		}
		if ready {
			c = append(c, runnableCand{id: n.ID, n: n})
		}
	}
	if len(c) == 0 {
		return nil
	}
	sort.Slice(c, func(i, j int) bool { return c[i].id < c[j].id })
	n := c[0].n
	return &n
}
