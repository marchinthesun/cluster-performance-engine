package topology

import "testing"

func TestPrimaryNUMANodeForCPUs(t *testing.T) {
	topo := &Topology{
		CPUs: []CPU{
			{ID: 0, NUMANode: 0},
			{ID: 1, NUMANode: 0},
			{ID: 8, NUMANode: 1},
		},
	}
	n, ok := PrimaryNUMANodeForCPUs(topo, []int{0, 1})
	if !ok || n != 0 {
		t.Fatalf("got node=%d ok=%v want 0 true", n, ok)
	}
	_, ok = PrimaryNUMANodeForCPUs(topo, []int{0, 8})
	if ok {
		t.Fatal("expected false for split-node cpus")
	}
	if _, ok := PrimaryNUMANodeForCPUs(topo, []int{99}); ok {
		t.Fatal("unknown cpu id should fail")
	}
}
