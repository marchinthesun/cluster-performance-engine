package topology

import (
	"fmt"
	"sort"
	"strings"
)

// FormatASCII renders a human-readable topology tree.
func FormatASCII(t *Topology) string {
	var b strings.Builder
	fmt.Fprintf(&b, "NexusFlow — NUMA-aware topology\n")
	fmt.Fprintf(&b, "Logical CPUs: %d", len(t.CPUs))
	if len(t.NUMANodes) > 0 {
		fmt.Fprintf(&b, "  NUMA nodes: %d", len(t.NUMANodes))
	}
	fmt.Fprintf(&b, "\n\n")

	for _, pkg := range t.PackageIDs() {
		fmt.Fprintf(&b, "Socket / package %d\n", pkg)
		sub := filterPackage(t.CPUs, pkg)
		byNode := map[int][]CPU{}
		for _, c := range sub {
			n := c.NUMANode
			if n < 0 {
				n = -1
			}
			byNode[n] = append(byNode[n], c)
		}
		var nodeIDs []int
		for n := range byNode {
			nodeIDs = append(nodeIDs, n)
		}
		sort.Ints(nodeIDs)
		for _, nid := range nodeIDs {
			var distHint string
			if nid >= 0 {
				for _, nn := range t.NUMANodes {
					if nn.ID == nid && len(nn.Distance) > 0 {
						distHint = fmt.Sprintf("  distance sysfs: %v", nn.Distance)
						break
					}
				}
			}
			nodeLabel := "unknown NUMA"
			if nid >= 0 {
				nodeLabel = fmt.Sprintf("NUMA node %d", nid)
			}
			if distHint != "" {
				fmt.Fprintf(&b, "  ├── %s  (%s)\n", nodeLabel, distHint)
			} else {
				fmt.Fprintf(&b, "  ├── %s\n", nodeLabel)
			}

			cores := byNode[nid]
			sort.Slice(cores, func(i, j int) bool {
				if cores[i].CoreID != cores[j].CoreID {
					return cores[i].CoreID < cores[j].CoreID
				}
				return cores[i].ID < cores[j].ID
			})
			coreSeen := map[int]bool{}
			for _, c := range cores {
				if coreSeen[c.CoreID] {
					continue
				}
				coreSeen[c.CoreID] = true
				threadIDs := coresOnCore(cores, c.CoreID)
				sort.Ints(threadIDs)
				fmt.Fprintf(&b, "  │     Core %-4d → CPUs %v\n", c.CoreID, threadIDs)
			}
		}
		fmt.Fprintf(&b, "\n")
	}

	if len(t.NUMANodes) > 0 {
		fmt.Fprintf(&b, "NUMA CPU lists (sysfs cpulist):\n")
		for _, n := range t.NUMANodes {
			fmt.Fprintf(&b, "  node%d → %v\n", n.ID, n.CPUs)
		}
	}

	return b.String()
}

func filterPackage(cpus []CPU, pkg int) []CPU {
	var out []CPU
	for _, c := range cpus {
		if c.PackageID == pkg {
			out = append(out, c)
		}
	}
	return out
}

func coresOnCore(cpus []CPU, coreID int) []int {
	var ids []int
	for _, c := range cpus {
		if c.CoreID == coreID {
			ids = append(ids, c.ID)
		}
	}
	return ids
}
