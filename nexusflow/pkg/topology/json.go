package topology

import (
	"encoding/json"
	"fmt"
)

// Snapshot is the stable JSON export shape for SDKs (Python / Go).
type Snapshot struct {
	CPUs      []SnapshotCPU `json:"cpus"`
	NUMANodes []SnapshotNUMA `json:"numa_nodes"`
	Source    string        `json:"source,omitempty"` // e.g. sysfs, hwloc-xml
}

// SnapshotCPU is one logical CPU in JSON form.
type SnapshotCPU struct {
	ID               int   `json:"id"`
	Package          int   `json:"package"`
	Core             int   `json:"core"`
	NUMANode         int   `json:"numa_node"`
	ThreadSiblings   []int `json:"thread_siblings,omitempty"`
}

// SnapshotNUMA is one NUMA domain in JSON form.
type SnapshotNUMA struct {
	ID       int   `json:"id"`
	CPUs     []int `json:"cpus"`
	Distance []int `json:"distance,omitempty"`
}

// ToSnapshot converts Topology to Snapshot (optional source tag).
func ToSnapshot(t *Topology, source string) *Snapshot {
	if t == nil {
		return nil
	}
	s := &Snapshot{Source: source}
	for _, c := range t.CPUs {
		s.CPUs = append(s.CPUs, SnapshotCPU{
			ID:             c.ID,
			Package:        c.PackageID,
			Core:           c.CoreID,
			NUMANode:       c.NUMANode,
			ThreadSiblings: append([]int(nil), c.Siblings...),
		})
	}
	for _, n := range t.NUMANodes {
		s.NUMANodes = append(s.NUMANodes, SnapshotNUMA{
			ID:       n.ID,
			CPUs:     append([]int(nil), n.CPUs...),
			Distance: append([]int(nil), n.Distance...),
		})
	}
	return s
}

// MarshalTopologyJSON encodes topology as JSON (compact).
func MarshalTopologyJSON(t *Topology, source string) ([]byte, error) {
	return json.Marshal(ToSnapshot(t, source))
}

// MarshalTopologyJSONIndent encodes topology as indented JSON.
func MarshalTopologyJSONIndent(t *Topology, prefix, indent string, source string) ([]byte, error) {
	return json.MarshalIndent(ToSnapshot(t, source), prefix, indent)
}

// UnmarshalTopologyJSON reconstructs Topology from Snapshot JSON.
func UnmarshalTopologyJSON(data []byte) (*Topology, error) {
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, err
	}
	if len(snap.CPUs) == 0 && len(snap.NUMANodes) == 0 {
		return nil, fmt.Errorf("empty topology snapshot")
	}
	t := &Topology{}
	for _, c := range snap.CPUs {
		t.CPUs = append(t.CPUs, CPU{
			ID:        c.ID,
			PackageID: c.Package,
			CoreID:    c.Core,
			NUMANode:  c.NUMANode,
			Siblings:  append([]int(nil), c.ThreadSiblings...),
		})
	}
	for _, n := range snap.NUMANodes {
		t.NUMANodes = append(t.NUMANodes, NUMANode{
			ID:       n.ID,
			CPUs:     append([]int(nil), n.CPUs...),
			Distance: append([]int(nil), n.Distance...),
		})
	}
	return t, nil
}
