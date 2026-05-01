package topology

import (
	"strings"
	"testing"
)

func TestBuildClusterHints_twoSockets(t *testing.T) {
	topo := &Topology{
		CPUs: []CPU{
			{ID: 0, PackageID: 0, CoreID: 0, NUMANode: 0},
			{ID: 1, PackageID: 0, CoreID: 1, NUMANode: 0},
			{ID: 2, PackageID: 1, CoreID: 0, NUMANode: 1},
			{ID: 3, PackageID: 1, CoreID: 1, NUMANode: 1},
		},
		NUMANodes: []NUMANode{
			{ID: 0, CPUs: []int{0, 1}},
			{ID: 1, CPUs: []int{2, 3}},
		},
	}
	h := BuildClusterHints(topo, "test")
	if h.SocketCount != 2 || h.PhysicalCoresTotal != 4 {
		t.Fatalf("sockets/cores: %+v", h)
	}
	if h.MaxLogicalPerSocket != 2 || h.SuggestedCPUsPerTaskSocketLocal != 2 {
		t.Fatalf("per-socket: %+v", h)
	}
	if len(h.MakefileJPerSocketCore) != 2 || h.MakefileJPerSocketCore[0] != 2 {
		t.Fatalf("make -j per socket: %+v", h.MakefileJPerSocketCore)
	}
}

func TestWriteHintsShell_containsExports(t *testing.T) {
	h := BuildClusterHints(&Topology{
		CPUs: []CPU{{ID: 0, PackageID: 0, CoreID: 0, NUMANode: 0}},
		NUMANodes: []NUMANode{{ID: 0, CPUs: []int{0}}},
	}, "unit")
	var sb strings.Builder
	if err := WriteHintsShell(&sb, h); err != nil {
		t.Fatal(err)
	}
	out := sb.String()
	if !strings.Contains(out, "NEXUSFLOW_LOGICAL_CPUS=1") || !strings.Contains(out, "NEXUSFLOW_SRUN_EXTRA=") {
		t.Fatalf("unexpected shell output:\n%s", out)
	}
}
