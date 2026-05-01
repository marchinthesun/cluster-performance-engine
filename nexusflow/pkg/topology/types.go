package topology

import "sort"

// CPU describes one logical processor from sysfs or hwloc.
type CPU struct {
	ID        int
	PackageID int
	CoreID    int
	NUMANode  int   // -1 if unknown
	Siblings  []int // thread siblings (includes self); optional / sysfs-only detail
}

// NUMANode is one memory affinity domain.
type NUMANode struct {
	ID       int
	CPUs     []int
	Distance []int // sysfs distance line when present
}

// Topology is a snapshot of machine CPU / NUMA layout.
type Topology struct {
	CPUs      []CPU
	NUMANodes []NUMANode
}

// CPUsOnNode returns logical IDs belonging to a NUMA node.
func (t *Topology) CPUsOnNode(nodeID int) []int {
	for _, n := range t.NUMANodes {
		if n.ID == nodeID {
			cp := make([]int, len(n.CPUs))
			copy(cp, n.CPUs)
			return cp
		}
	}
	return nil
}

// PackageIDs returns sorted unique socket / package ids.
func (t *Topology) PackageIDs() []int {
	seen := map[int]struct{}{}
	for _, c := range t.CPUs {
		seen[c.PackageID] = struct{}{}
	}
	var ids []int
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids
}
