package topology

import (
	"fmt"
	"sort"
)

// SelectionStrategy selects how CPUs are chosen for pinning.
type SelectionStrategy string

const (
	// StrategySameNUMA prefers a single NUMA node; if one node cannot fit,
	// cpus are taken in node id order until the count is satisfied.
	StrategySameNUMA SelectionStrategy = "same-numa"
)

// SelectCPUs returns up to want logical CPU ids for pinning.
// want <= 0 means all logical CPUs (sorted).
func SelectCPUs(t *Topology, want int, numaPrefer *int, strategy SelectionStrategy) ([]int, error) {
	ids := allCPUIDs(t)
	if want <= 0 || want >= len(ids) {
		return ids, nil
	}

	if strategy != "" && strategy != StrategySameNUMA {
		return nil, fmt.Errorf("unknown strategy %q (supported: same-numa)", strategy)
	}

	if len(t.NUMANodes) == 0 {
		return ids[:want], nil
	}

	if numaPrefer != nil {
		cpus := t.CPUsOnNode(*numaPrefer)
		sort.Ints(cpus)
		if len(cpus) < want {
			return nil, fmt.Errorf("NUMA node %d has %d cpus; need %d", *numaPrefer, len(cpus), want)
		}
		return cpus[:want], nil
	}

	type cand struct {
		id   int
		size int
		cpus []int
	}
	var cands []cand
	for _, n := range t.NUMANodes {
		cp := append([]int(nil), n.CPUs...)
		sort.Ints(cp)
		if len(cp) >= want {
			cands = append(cands, cand{id: n.ID, size: len(cp), cpus: cp})
		}
	}
	if len(cands) > 0 {
		sort.Slice(cands, func(i, j int) bool {
			if cands[i].size != cands[j].size {
				return cands[i].size > cands[j].size
			}
			return cands[i].id < cands[j].id
		})
		return cands[0].cpus[:want], nil
	}

	var acc []int
	for _, n := range t.NUMANodes {
		cp := append([]int(nil), n.CPUs...)
		sort.Ints(cp)
		for _, id := range cp {
			if len(acc) >= want {
				break
			}
			acc = append(acc, id)
		}
		if len(acc) >= want {
			break
		}
	}
	sort.Ints(acc)
	if len(acc) < want {
		return nil, fmt.Errorf("need %d cpus but only %d available across NUMA nodes", want, len(acc))
	}
	return acc[:want], nil
}

func allCPUIDs(t *Topology) []int {
	var ids []int
	for _, c := range t.CPUs {
		ids = append(ids, c.ID)
	}
	sort.Ints(ids)
	return ids
}

// PrimaryNUMANodeForCPUs returns the NUMA node id if every listed logical CPU belongs to
// the same NUMA domain in t and all ids are known. Otherwise ok is false.
func PrimaryNUMANodeForCPUs(t *Topology, cpus []int) (node int, ok bool) {
	if t == nil || len(cpus) == 0 {
		return -1, false
	}
	want := make(map[int]struct{}, len(cpus))
	for _, id := range cpus {
		want[id] = struct{}{}
	}
	var first *int
	seen := 0
	for _, cpu := range t.CPUs {
		if _, w := want[cpu.ID]; !w {
			continue
		}
		seen++
		nid := cpu.NUMANode
		if nid < 0 {
			return -1, false
		}
		if first == nil {
			v := nid
			first = &v
		} else if *first != nid {
			return -1, false
		}
	}
	if first == nil || seen != len(want) {
		return -1, false
	}
	return *first, true
}
