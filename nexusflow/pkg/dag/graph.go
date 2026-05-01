package dag

import (
	"fmt"
)

// TopoOrder returns nodes in dependency-respecting order (Kahn).
func TopoOrder(nodes []Node) ([]Node, error) {
	byID := map[string]Node{}
	inDeg := map[string]int{}
	edges := map[string][]string{}

	for _, n := range nodes {
		byID[n.ID] = n
		if _, ok := inDeg[n.ID]; !ok {
			inDeg[n.ID] = 0
		}
	}
	for _, n := range nodes {
		for _, d := range n.DependsOn {
			edges[d] = append(edges[d], n.ID)
			inDeg[n.ID]++
		}
	}

	var q []string
	for id, deg := range inDeg {
		if deg == 0 {
			q = append(q, id)
		}
	}

	var ordered []Node
	for len(q) > 0 {
		id := q[0]
		q = q[1:]
		n := byID[id]
		ordered = append(ordered, n)
		for _, succ := range edges[id] {
			inDeg[succ]--
			if inDeg[succ] == 0 {
				q = append(q, succ)
			}
		}
	}

	if len(ordered) != len(nodes) {
		return nil, fmt.Errorf("DAG cycle detected or unresolved nodes")
	}
	return ordered, nil
}
